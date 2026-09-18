package evidence

import (
	"fmt"
	"sync"
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
// reports attaching byte-identical files share one copy. This mirrors
// report.Store and guardian.Store: no disk, no encryption at rest, gone on
// restart. That parity is deliberate (see the package doc) rather than an
// oversight specific to this file.
type Store struct {
	mu    sync.RWMutex
	files map[string]storedFile // sha256 hex -> stored bytes and their type
	total int64
}

type storedFile struct {
	bytes       []byte
	contentType string
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
	s.files[ref.SHA256] = storedFile{bytes: clean, contentType: ref.ContentType}
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
