package persist

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreDirCreatesAndIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "mlinzi-data")
	if err := StoreDir(dir); err != nil {
		t.Fatalf("first StoreDir: %v", err)
	}
	// The second run must succeed too — restore-on-boot runs on every
	// startup, and failing on the second boot would be a self-inflicted
	// outage.
	if err := StoreDir(dir); err != nil {
		t.Fatalf("second StoreDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("expected a directory, got something else at %s", dir)
	}
}

func TestSaveJSONRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snap.json")
	type shape struct {
		Name  string
		Count int
	}
	in := shape{Name: "reports", Count: 7}
	if err := SaveJSON(path, in); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}
	var out shape
	if err := LoadJSON(path, &out); err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	if out != in {
		t.Fatalf("round trip mismatch: %+v != %+v", out, in)
	}
}

func TestSaveJSONLeavesNoTempFilesBehind(t *testing.T) {
	dir := t.TempDir()
	if err := SaveJSON(filepath.Join(dir, "snap.json"), map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "snap.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected exactly snap.json in the directory, got %v — a leftover temp file means the rename cleanup is broken", names)
	}
}

func TestSaveFileOverwritesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snap.json")
	if err := SaveFile(path, []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := SaveFile(path, []byte(`{"v":2}`)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"v":2}` {
		t.Fatalf("expected the second write to win, got %s", raw)
	}
}

func TestSaveJSONFilePermissionsAreRestrictive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snap.json")
	if err := SaveJSON(path, "sensitive"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("expected owner-only permissions on %s, got %o — report content must not be group/world readable", path, perm)
	}
}

func TestSaveJSONRejectsUnmarshalableValue(t *testing.T) {
	if err := SaveJSON(filepath.Join(t.TempDir(), "snap.json"), make(chan int)); err == nil {
		t.Fatal("expected an error marshalling a channel, got none")
	}
}

func TestLoadJSONMissingFileIsErrNotExist(t *testing.T) {
	var v map[string]any
	err := LoadJSON(filepath.Join(t.TempDir(), "absent.json"), &v)
	if !Missing(err) {
		t.Fatalf("expected a missing-file error Missing() recognises, got %v", err)
	}
}

func TestLoadJSONCorruptFileFailsWithBadParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snap.json")
	if err := os.WriteFile(path, []byte("{not json"), FilePermissions); err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	err := LoadJSON(path, &v)
	if err == nil {
		t.Fatal("expected a parse failure, got none")
	}
	if Missing(err) {
		t.Fatal("a corrupt file must not be reported as missing — that would silently skip a real snapshot")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected the error to name the parse step, got %v", err)
	}
}

func TestMissingDistinguishesOtherErrors(t *testing.T) {
	if Missing(errors.New("some other failure")) {
		t.Fatal("a non-filesystem error must not be classified as missing")
	}
}
