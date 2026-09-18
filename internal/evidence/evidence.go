// Package evidence lets a reporter attach files — photos, video, documents —
// to a report, and makes the same two guarantees the rest of Mlinzi makes
// for text: the file's integrity is provable (its hash enters the ledger
// alongside the report, so a file swapped after submission is detectable),
// and nothing about it is trusted from the client. The declared filename and
// declared content type are both discarded; every byte is re-inspected here.
//
// Storage is in-memory, matching every other store in this codebase (see
// fly.toml: there is no persistence layer yet, and evidence does not get one
// either — it disappears on restart exactly like reports do today). That
// means the limits in this file are sized against a small VM's RAM, not
// against what a person might reasonably want to upload. A production
// deployment would move this to encrypted object storage with its own,
// much larger limits; this is the honestly-scoped version for a single
// 512MB instance.
//
// The one risk this package treats as categorically different from the rest
// of the app's accepted plaintext-storage posture: a photo taken on a phone
// routinely carries the GPS coordinates and device identifiers of the
// person who took it, embedded silently in EXIF. A reporter can choose to
// accept that their report text is visible to anyone who reaches the
// (currently unauthenticated) institution portal — that is a stated,
// visible limitation. They cannot make an informed choice about metadata
// they don't know is there. So this package strips it unconditionally,
// before a file is ever stored, for every image it can safely re-encode.
package evidence

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"net/http"
	"time"
)

// Kind is the broad category a file falls into, used to decide how (or
// whether) it can be previewed, and whether metadata stripping applies.
type Kind string

const (
	KindImage    Kind = "image"
	KindVideo    Kind = "video"
	KindDocument Kind = "document"
)

// MaxFileBytes is the largest single file accepted. Small on purpose: this
// process runs in 512MB total and holds every report in memory already.
const MaxFileBytes = 8 << 20 // 8MB

// MaxFilesPerReport caps how many files one report can carry.
const MaxFilesPerReport = 4

// MaxTotalBytesPerReport caps the combined size of one report's files,
// independent of the per-file cap (four 8MB files would otherwise pass the
// per-file check while still being 32MB in one report).
const MaxTotalBytesPerReport = 20 << 20 // 20MB

// allowedType maps a sniffed MIME type to its Kind and whether this package
// knows how to strip identifying metadata from it. Types not in this map are
// rejected outright — there is no "unknown, allow anyway" path, because an
// unrecognised type is exactly the case where trusting the upload is
// riskiest.
var allowedType = map[string]struct {
	kind      Kind
	stripable bool
}{
	"image/jpeg":      {KindImage, true},
	"image/png":       {KindImage, true},
	"application/pdf": {KindDocument, false},
	"video/mp4":       {KindVideo, false},
	"video/webm":      {KindVideo, false},
	"video/quicktime": {KindVideo, false},
}

// ErrTooLarge is returned when a file (or a report's combined files) exceeds
// the configured limit.
var ErrTooLarge = errors.New("file too large")

// ErrUnsupportedType is returned when the sniffed content does not match a
// type this package accepts, regardless of what the client claimed.
type ErrUnsupportedType struct{ Sniffed string }

func (e ErrUnsupportedType) Error() string {
	return fmt.Sprintf("unsupported file type %q", e.Sniffed)
}

// Ref is what a stored file is known by afterward: never the original
// filename (which can itself be identifying — "IMG_2026_johndoe.jpg" — and
// is discarded on purpose), only what this package independently determined
// about the bytes.
type Ref struct {
	ID               string    `json:"id"`
	SHA256           string    `json:"sha256"`
	Kind             Kind      `json:"kind"`
	ContentType      string    `json:"content_type"`
	SizeBytes        int64     `json:"size_bytes"`
	MetadataStripped bool      `json:"metadata_stripped"`
	StoredAt         time.Time `json:"stored_at"`
}

// process sniffs, validates, and — for strippable image types — re-encodes
// raw to remove embedded metadata, returning the bytes that should actually
// be stored and the Ref describing them. It never trusts declaredType; it is
// accepted only for the error message when sniffing disagrees with it.
func process(raw []byte) ([]byte, Ref, error) {
	if int64(len(raw)) > MaxFileBytes {
		return nil, Ref{}, fmt.Errorf("%w: %d bytes exceeds the %d byte limit", ErrTooLarge, len(raw), MaxFileBytes)
	}
	if len(raw) == 0 {
		return nil, Ref{}, errors.New("empty file")
	}

	sniffed := http.DetectContentType(raw)
	// DetectContentType returns e.g. "image/jpeg" cleanly for the types we
	// care about, but appends "; charset=..." for text-like types we don't
	// accept anyway, so an exact map lookup is the right check here.
	spec, ok := allowedType[sniffed]
	if !ok {
		return nil, Ref{}, ErrUnsupportedType{Sniffed: sniffed}
	}

	stored := raw
	stripped := false
	if spec.stripable {
		clean, err := stripImageMetadata(raw, sniffed)
		if err != nil {
			// A file that sniffs as image/jpeg or image/png but that Go's own
			// decoder rejects is malformed or deliberately malicious (e.g. a
			// polyglot file). Refuse it rather than storing bytes we could not
			// verify are what they claim to be.
			return nil, Ref{}, fmt.Errorf("could not decode %s to strip metadata: %w", sniffed, err)
		}
		stored = clean
		stripped = true
	}

	sum := sha256.Sum256(stored)
	id, err := randomID()
	if err != nil {
		return nil, Ref{}, err
	}

	return stored, Ref{
		ID:               id,
		SHA256:           hex.EncodeToString(sum[:]),
		Kind:             spec.kind,
		ContentType:      sniffed,
		SizeBytes:        int64(len(stored)),
		MetadataStripped: stripped,
		StoredAt:         time.Now().UTC(),
	}, nil
}

// stripImageMetadata decodes and re-encodes a JPEG or PNG, which discards
// every metadata segment neither Go encoder ever writes (EXIF, XMP, ICC
// profiles, thumbnails) — the image content is preserved, everything else
// about the file that produced it is not.
//
// JPEG orientation is the one piece of that discarded metadata that affects
// how the image looks, not just what it reveals: many phones save pixels in
// sensor orientation and rely on the EXIF orientation tag to display them
// upright. Stripping that tag without correcting for it would silently
// rotate or mirror a reporter's photo — exactly the kind of quiet corruption
// this whole project exists to prevent elsewhere. So orientation is read
// before the metadata carrying it is discarded, and applied to the pixels
// directly.
func stripImageMetadata(raw []byte, mimeType string) ([]byte, error) {
	switch mimeType {
	case "image/jpeg":
		orientation := jpegOrientation(raw)
		img, err := jpeg.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		img = applyOrientation(img, orientation)
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "image/png":
		// PNG has no equivalent orientation convention in practice; decoding
		// and re-encoding is enough to drop any ancillary metadata chunks.
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("stripImageMetadata: unsupported type %q", mimeType)
	}
}

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ev_" + hex.EncodeToString(b), nil
}

// PreviewableInline reports whether a ref's kind can be shown directly in an
// <img> tag. Video and documents are offered as a download instead — Go's
// standard library has no video/PDF metadata story to strip, so a preview
// there would need a decision this package deliberately does not make yet.
func (r Ref) PreviewableInline() bool {
	return r.Kind == KindImage
}
