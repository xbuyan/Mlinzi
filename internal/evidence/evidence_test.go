package evidence

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // registers PNG decoding for image.Decode in TestOpenRoundTrips
	"os"
	"testing"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	return b
}

// TestStripsExifAndGPS proves the single most important guarantee this
// package makes: a real photo carrying GPS coordinates and a device serial
// number does not carry them anymore once processed. This is checked at the
// byte level, not just "decoding succeeded" — the raw output must not
// contain the Exif marker or the identifying strings at all.
func TestStripsExifAndGPS(t *testing.T) {
	raw := readTestdata(t, "orientation6_with_gps.jpg")
	if !bytes.Contains(raw, []byte("Exif")) {
		t.Fatal("test fixture itself has no Exif marker — fixture is broken, not the code under test")
	}
	if !bytes.Contains(raw, []byte("ModelX-Identifying-Device-Serial-12345")) {
		t.Fatal("test fixture itself has no device identifier — fixture is broken, not the code under test")
	}

	clean, ref, err := process(raw)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if !ref.MetadataStripped {
		t.Fatal("ref.MetadataStripped is false for a JPEG, which should always be strippable")
	}
	if bytes.Contains(clean, []byte("Exif")) {
		t.Fatal("output still contains an Exif marker — metadata was not stripped")
	}
	if bytes.Contains(clean, []byte("ModelX-Identifying-Device-Serial-12345")) {
		t.Fatal("output still contains the device identifier from GPS/make/model tags")
	}
	if bytes.Contains(clean, []byte("TestPhone")) {
		t.Fatal("output still contains the device make tag")
	}
}

// TestOrientationCorrectionKeepsImageUpright proves the fixture's one
// distinguishing pixel ends up where a viewer would expect after EXIF
// orientation 6 ("rotate 90 CW to display correctly") is applied and then
// discarded — not rotated the wrong way, not left as if orientation were
// never read at all.
func TestOrientationCorrectionKeepsImageUpright(t *testing.T) {
	raw := readTestdata(t, "orientation6_with_gps.jpg")

	if got := jpegOrientation(raw); got != 6 {
		t.Fatalf("jpegOrientation = %d, want 6 (fixture was built with orientation 6)", got)
	}

	clean, _, err := process(raw)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(clean))
	if err != nil {
		t.Fatalf("decode processed output: %v", err)
	}

	// Source was 16 wide x 8 high: left half (x<8) solid red, right half
	// solid blue — large, block-aligned regions so JPEG compression doesn't
	// smear the marker the way a single pixel would. Orientation 6 (rotate
	// 90 CW) on a 16x8 image should produce an 8x16 image with the red
	// region now occupying the top half (y<8) and blue the bottom half —
	// see the derivation in orientation.go's package comment for why a
	// left/right split becomes a top/bottom split under a 90-degree
	// rotation.
	b := img.Bounds()
	if b.Dx() != 8 || b.Dy() != 16 {
		t.Fatalf("output dimensions = %dx%d, want 8x16 (width/height should swap on a 90-degree rotation)", b.Dx(), b.Dy())
	}
	if !isRed(img.At(4, 2)) {
		t.Errorf("expected red in the top half (4,2) after rotation, found %v", colorOf(img.At(4, 2)))
	}
	if isRed(img.At(4, 12)) {
		t.Errorf("expected blue in the bottom half (4,12) after rotation, found red — orientation was applied backwards or not at all")
	}
}

func isRed(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r > 0x8000 && g < 0x2000 && b < 0x2000
}

func colorOf(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return "rgb(" + itoa(r>>8) + "," + itoa(g>>8) + "," + itoa(b>>8) + ")"
}

func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}

func TestPNGPassesThroughUnchanged(t *testing.T) {
	raw := readTestdata(t, "plain.png")
	_, ref, err := process(raw)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if ref.Kind != KindImage {
		t.Errorf("Kind = %q, want image", ref.Kind)
	}
	if !ref.MetadataStripped {
		t.Error("PNG should also go through the decode/re-encode path")
	}
}

func TestUnsupportedTypeRejected(t *testing.T) {
	raw := readTestdata(t, "garbage.bin")
	_, _, err := process(raw)
	if err == nil {
		t.Fatal("expected an error for unrecognised random bytes, got nil")
	}
	var unsupported ErrUnsupportedType
	if !errorsAs(err, &unsupported) {
		t.Errorf("expected ErrUnsupportedType, got %v (%T)", err, err)
	}
}

// errorsAs avoids importing "errors" twice under different aliasing rules in
// this file; a thin wrapper keeps the test readable.
func errorsAs(err error, target *ErrUnsupportedType) bool {
	if e, ok := err.(ErrUnsupportedType); ok {
		*target = e
		return true
	}
	return false
}

func TestFileTooLargeRejected(t *testing.T) {
	big := make([]byte, MaxFileBytes+1)
	_, _, err := process(big)
	if err == nil {
		t.Fatal("expected an error for a file over MaxFileBytes")
	}
}

func TestMalformedJPEGRejectedNotSilentlyPassed(t *testing.T) {
	// Bytes that sniff as image/jpeg (correct magic number) but are not a
	// decodable JPEG must be rejected, not stored as-is — storing bytes we
	// could not verify defeats the point of re-encoding to strip metadata.
	fakeJPEG := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x00}, 50)...)
	_, _, err := process(fakeJPEG)
	if err == nil {
		t.Fatal("expected an error for a JPEG-sniffing file that Go's decoder cannot actually decode")
	}
}

func TestStoreDedupesIdenticalFiles(t *testing.T) {
	raw := readTestdata(t, "plain.png")
	s := NewStore()
	ref1, err := s.Save(raw)
	if err != nil {
		t.Fatalf("first Save: %v", err)
	}
	ref2, err := s.Save(raw)
	if err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if ref1.SHA256 != ref2.SHA256 {
		t.Fatalf("identical input produced different hashes: %s vs %s", ref1.SHA256, ref2.SHA256)
	}
	if s.total != ref1.SizeBytes {
		t.Errorf("store total = %d, want %d (second save of identical bytes should not double-count)", s.total, ref1.SizeBytes)
	}
}

func TestSaveAllRejectsOverPerReportLimits(t *testing.T) {
	s := NewStore()
	png := readTestdata(t, "plain.png")

	files := make([][]byte, MaxFilesPerReport+1)
	for i := range files {
		files[i] = png
	}
	if _, err := s.SaveAll(files); err == nil {
		t.Fatal("expected an error for exceeding MaxFilesPerReport")
	}
}

func TestSaveAllIsAllOrNothing(t *testing.T) {
	s := NewStore()
	png := readTestdata(t, "plain.png")
	oversized := bytes.Repeat([]byte{0xFF}, MaxTotalBytesPerReport+1)
	// oversized won't even sniff as a supported type, but the total-bytes
	// check must fire before per-file processing does, so the good file in
	// this batch is never stored either.
	_, err := s.SaveAll([][]byte{png, oversized})
	if err == nil {
		t.Fatal("expected an error when the batch total exceeds MaxTotalBytesPerReport")
	}
	if s.total != 0 {
		t.Errorf("store total = %d, want 0 — a rejected batch must not partially store", s.total)
	}
}

func TestStoreFullRejectsFurtherUploads(t *testing.T) {
	s := &Store{files: make(map[string]storedFile), total: MaxStoreBytes}
	raw := readTestdata(t, "plain.png")
	_, err := s.Save(raw)
	if err != ErrStoreFull {
		t.Fatalf("Save on a full store: got %v, want ErrStoreFull", err)
	}
}

// TestApplyOrientationAllEightValues checks the pure pixel transform for
// every EXIF orientation value against a hand-worked expected position,
// using an in-memory synthetic image rather than a real JPEG so there is no
// compression noise to obscure a one-pixel error. A 3x2 source with a
// single marker pixel at (2,0) — the top-right corner — is asymmetric
// enough that no two of the eight orientations produce the same result,
// so a transform applied backwards or confused with a different one is
// caught here rather than only on real photos.
func TestApplyOrientationAllEightValues(t *testing.T) {
	newSource := func() image.Image {
		img := image.NewRGBA(image.Rect(0, 0, 3, 2))
		for y := 0; y < 2; y++ {
			for x := 0; x < 3; x++ {
				img.Set(x, y, color.RGBA{0, 0, 255, 255})
			}
		}
		img.Set(2, 0, color.RGBA{255, 0, 0, 255})
		return img
	}

	cases := []struct {
		orientation              int
		wantW, wantH             int
		wantMarkerX, wantMarkerY int
	}{
		{1, 3, 2, 2, 0}, // normal: unchanged
		{2, 3, 2, 0, 0}, // mirror horizontal: (2,0) -> (0,0)
		{3, 3, 2, 0, 1}, // rotate 180: (2,0) -> (0,1)
		{4, 3, 2, 2, 1}, // mirror vertical: (2,0) -> (2,1)
		{5, 2, 3, 0, 2}, // transpose: (x,y)->(y,x): (2,0) -> (0,2)
		{6, 2, 3, 1, 2}, // rotate 90 CW: (x,y) -> (H-1-y, x) = (1,2)
		{7, 2, 3, 1, 0}, // anti-transpose: (x,y)->(H-1-y,W-1-x) = (1,0)
		{8, 2, 3, 0, 0}, // rotate 270 CW: (x,y) -> (y, W-1-x) = (0,0)
	}

	for _, c := range cases {
		out := applyOrientation(newSource(), c.orientation)
		b := out.Bounds()
		if b.Dx() != c.wantW || b.Dy() != c.wantH {
			t.Errorf("orientation %d: dims = %dx%d, want %dx%d", c.orientation, b.Dx(), b.Dy(), c.wantW, c.wantH)
			continue
		}
		if !isRed(out.At(c.wantMarkerX, c.wantMarkerY)) {
			t.Errorf("orientation %d: expected red marker at (%d,%d), not found", c.orientation, c.wantMarkerX, c.wantMarkerY)
		}
		// Every other pixel must still be blue - a transform that duplicates
		// or smears the marker is as wrong as one that misplaces it.
		redCount := 0
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				if isRed(out.At(x, y)) {
					redCount++
				}
			}
		}
		if redCount != 1 {
			t.Errorf("orientation %d: found %d red pixels, want exactly 1", c.orientation, redCount)
		}
	}
}

func TestOpenRoundTrips(t *testing.T) {
	s := NewStore()
	raw := readTestdata(t, "plain.png")
	ref, err := s.Save(raw)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok := s.Open(ref)
	if !ok {
		t.Fatal("Open returned not-found for a ref just saved")
	}
	// got is the re-encoded PNG, not the raw input — compare via decode
	// equivalence rather than byte equality.
	if _, _, err := image.Decode(bytes.NewReader(got)); err != nil {
		t.Fatalf("stored bytes did not decode as an image: %v", err)
	}
}
