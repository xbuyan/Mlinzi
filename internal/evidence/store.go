package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// MaxStoreBytes caps the store's total memory footprint across every report,
// independent of the per-report cap. Without this, many small reports could
// still add up to more than a 512MB instance can hold. Once the cap is hit,
// new uploads are refused with a clear error rather than the process
// crashing or silently evicting someone else's evidence.
const MaxStoreBytes = 150 << 20 // 150MB

// ErrStoreFull is returned when accepting a file would exceed MaxStoreBytes.
var ErrStoreFull = fmt.Errorf("evidence store is full")

// Store holds evidence file bytes in memory, keyed by content hash — two
// reports attaching byte-identical files share one copy.
//
// Persistence sits on top of this in-memory cache rather than replacing it:
// reading a file is a RAM lookup, and only the bytes that came in while the
// process was up ever need saving. The files that go to disk are the exact
// processed bytes (metadata already stripped, content-verified) that the
// hash in a report's ledger entry commits to, so restoring them keeps the
// integrity guarantee intact — a swapped file would no longer match its
// hash, and the swap is as detectable after a restore as before it.
type Store struct {
	mu    sync.RWMutex
	files map[string]storedFile // sha256 hex -> stored bytes and their type
	total int64
}

type storedFile struct {
	bytes       []byte
	contentType string
	storedAt    time.Time
}

// NewStore returns an empty evidence store.
func NewStore() *Store {
	return &Store{files: make(map[string]storedFile)}
}

// Save validates, strips metadata where applicable, and stores raw. It
// returns a Ref describing what was actually stored — which may differ from
// the input bytes (a re-encoded JPEG, for instance).
func (s *Store) Save(raw []byte) (Ref, error) {
	clean, ref, err := process(raw)
	if err != nil {
		return Ref{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.files[ref.SHA256]; exists {
		return ref, nil // identical file already held; no additional memory used
	}
	if s.total+int64(len(clean)) > MaxStoreBytes {
		return Ref{}, ErrStoreFull
	}
	s.files[ref.SHA256] = storedFile{bytes: clean, contentType: ref.ContentType, storedAt: ref.StoredAt}
	s.total += int64(len(clean))
	return ref, nil
}

// SaveAll validates a batch of files against the per-report limits
// (MaxFilesPerReport, MaxTotalBytesPerReport) before storing any of them —
// a report's evidence is all-or-nothing, so a reporter never ends up with
// three files attached and a silent fourth rejected.
func (s *Store) SaveAll(files [][]byte) ([]Ref, error) {
	if len(files) > MaxFilesPerReport {
		return nil, fmt.Errorf("%d files exceeds the %d file limit per report", len(files), MaxFilesPerReport)
	}
	var total int64
	for _, f := range files {
		total += int64(len(f))
	}
	if total > MaxTotalBytesPerReport {
		return nil, fmt.Errorf("%w: %d bytes across %d files exceeds the %d byte limit per report", ErrTooLarge, total, len(files), MaxTotalBytesPerReport)
	}

	refs := make([]Ref, 0, len(files))
	for _, f := range files {
		ref, err := s.Save(f)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// Open returns the stored bytes for a ref, for serving back to a viewer.
func (s *Store) Open(ref Ref) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.files[ref.SHA256]
	return f.bytes, ok
}

// OpenByHash looks up stored bytes and content type by hash alone, for an
// HTTP handler that only has the hash from a URL path — it does not need a
// full Ref to serve a file back, only enough to set the right Content-Type.
func (s *Store) OpenByHash(sha256hex string) (data []byte, contentType string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.files[sha256hex]
	return f.bytes, f.contentType, ok
}

// FileRecord is one stored file in its on-disk form. The SHA256 key
// travels with the record so a snapshot can be validated on load — a file
// whose bytes no longer hash to its key is refused, which is the same
// integrity rule the ledger snapshot gets, applied to files.
type FileRecord struct {
	SHA256      string    `json:"sha256"`
	ContentType string    `json:"content_type"`
	StoredAt    time.Time `json:"stored_at"`
	Bytes       []byte    `json:"bytes,omitempty"` // base64 in JSON; the persistence layer writes bytes to individual files instead
}

// Snapshot is the whole-store form used for persistence. Bytes are carried
// here so the store stays domain-pure (bytes in, bytes out); the persistence
// layer decides how they reach disk.
type Snapshot struct {
	Files []FileRecord `json:"files"`
}

// Snapshot returns every stored file in its on-disk form. The returned
// structure shares no state with the store.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := Snapshot{Files: make([]FileRecord, 0, len(s.files))}
	for hash, f := range s.files {
		out.Files = append(out.Files, FileRecord{
			SHA256:      hash,
			ContentType: f.contentType,
			StoredAt:    f.storedAt,
			Bytes:       f.bytes,
		})
	}
	return out
}

// Restore loads a snapshot taken by Snapshot, verifying each file's bytes
// against the hash it is keyed by. A mismatch fails the whole restore rather
// than quietly loading every file except the altered one: partial evidence
// is worse than no evidence, because the missing file's absence would be
// invisible in a report that claims to carry four.
func (s *Store) Restore(snap Snapshot) error {
	// Validate everything first, then mutate: a snapshot that fails halfway
	// through must not leave the store half-populated.
	for _, f := range snap.Files {
		if len(f.Bytes) == 0 {
			return fmt.Errorf("evidence: restore: file %s is empty", f.SHA256)
		}
		if len(f.SHA256) != 64 {
			return fmt.Errorf("evidence: restore: file key %q is not a sha256 hex digest", f.SHA256)
		}
		sum := sha256.Sum256(f.Bytes)
		if got := hex.EncodeToString(sum[:]); got != f.SHA256 {
			return fmt.Errorf("evidence: restore: file keyed %s does not hash to its key (got %s) — the snapshot has been altered", f.SHA256, got)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	files := make(map[string]storedFile, len(snap.Files))
	var total int64
	for _, f := range snap.Files {
		files[f.SHA256] = storedFile{bytes: f.Bytes, contentType: f.ContentType, storedAt: f.StoredAt}
		total += int64(len(f.Bytes))
	}
	s.files = files
	s.total = total
	return nil
}
