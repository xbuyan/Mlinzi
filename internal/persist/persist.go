// Package persist gives Mlinzi's in-memory stores a durable home on disk,
// and gives the app's honesty story a persistence chapter that holds.
//
// The design constraint comes from the project it serves: Mlinzi's whole
// value is tamper-evidence, so its persistence cannot be a place where
// tampering becomes easy. Ledger snapshots are verified on load (the ledger
// refuses an altered chain), files are written atomically (a crash mid-write
// leaves the previous snapshot intact, not a truncated one), and the directory
// is created 0700 rather than world-readable — report content is sensitive,
// and "at rest" must not mean "readable by every other user on the box".
package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DirPermissions is what StoreDir creates the data directory with. 0700
// because what lands in it — report text, evidence files — is exactly the
// material this app exists to protect; a world-readable directory would
// undercut that one privilege boundary the process does control.
const DirPermissions = 0o700

// FilePermissions is what SaveFile writes regular files with.
const FilePermissions = 0o600

// StoreDir creates dir (and any missing parents) if it does not already
// exist, with restrictive permissions. An existing directory's mode is left
// alone — tightening it silently under a running deployment could break
// whatever legitimately reads it, and loosening it here would be a change
// nobody asked for. This is deliberately a no-op when the directory exists,
// because restore-on-boot runs on every startup and must not fail on the
// second one.
func StoreDir(dir string) error {
	if err := os.MkdirAll(dir, DirPermissions); err != nil {
		return fmt.Errorf("persist: create directory %s: %w", dir, err)
	}
	return nil
}

// SaveJSON marshals v and writes it to path atomically: the bytes land in a
// temp file in the same directory, are flushed to disk with fsync, and only
// then rename over the destination. A crash partway through leaves the
// previous snapshot in place rather than a half-written file that could not
// be loaded. The rename is atomic within a filesystem — which is why the
// temp file lives beside the target, not in /tmp.
//
// The JSON is deliberately compact, not indented. This is not a style
// choice: a ledger entry's hash commits to its raw Data bytes, and
// MarshalIndent re-indents embedded json.RawMessage — so a pretty-printed
// snapshot would come back with altered bytes and refuse to verify, which
// the round-trip test caught the first time this was written with
// indentation. Snapshots are for machines; the ledger's integrity is for
// people.
func SaveJSON(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("persist: marshal %s: %w", path, err)
	}
	if err := SaveFile(path, raw); err != nil {
		return err
	}
	return nil
}

// SaveFile writes raw bytes to path atomically, with the same temp-file,
// fsync, rename discipline SaveJSON uses.
func SaveFile(path string, raw []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("persist: create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	// Clean up the temp file unless the rename below succeeds and we stop
	// needing it removed.
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("persist: write %s: %w", path, err)
	}
	// The fsync is the step that makes "atomic" mean more than "usually
	// works": without it, the rename can hit the disk before the bytes do,
	// and a power cut right after would leave an empty or truncated file
	// renamed into place.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("persist: sync %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("persist: close %s: %w", path, err)
	}
	if err := os.Chmod(tmpName, FilePermissions); err != nil {
		return fmt.Errorf("persist: chmod %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("persist: rename %s into place: %w", tmpName, err)
	}
	// The rename consumed the temp name; the deferred cleanup must not try
	// to remove the file that is now the destination.
	tmpName = ""
	return nil
}

// LoadJSON reads path into v. A missing file is reported as os.ErrNotExist
// so a first boot can treat it as "nothing to restore" without string-matching.
func LoadJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("persist: parse %s: %w", path, err)
	}
	return nil
}

// ReadFile is os.ReadFile with a wrapped error, kept beside the rest of the
// persistence path so callers work with one package's error vocabulary.
func ReadFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("persist: read %s: %w", path, err)
	}
	return raw, nil
}

// Missing reports whether err is the "no snapshot yet" case, so boot code
// can distinguish a fresh deployment from a corrupt one without reaching
// for errors.Is against os package details.
func Missing(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
