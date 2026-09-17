package resource

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Store holds validated documents for any number of jurisdictions.
//
// Layout is deliberately different from the guide store's data/<country>/.
// Guides are many small entries that belong to a country, so they group by
// country. Documents are large stands of law that belong to nobody but
// themselves, so they sit flat in one folder, one file each. Adding a
// constitution is adding a file — no registration, no index to update, and no
// way for one document to affect another.
type Store struct {
	byID  map[string]Document
	order []string
}

// ErrNotFound is returned when a document ID is not in the store.
type ErrNotFound struct{ ID string }

func (e ErrNotFound) Error() string { return fmt.Sprintf("document %q not found", e.ID) }

// Load reads every .json file directly under root, each holding a single
// document, and validates as it goes. One invalid document fails the whole
// load, for the same reason the guide store refuses partial loads: publishing
// law-adjacent text that cannot show its provenance is worse than publishing
// none of it.
//
// A document's id must equal its filename. That keeps "one instrument, one
// file, one home" true by construction, so two documents can never quietly
// share a file and start depending on each other.
func Load(fsys fs.FS, root string) (*Store, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("read resources root %q: %w", root, err)
	}

	s := &Store{byID: make(map[string]Document)}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p := path.Join(root, e.Name())
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", p, err)
		}
		var d Document
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, fmt.Errorf("parse %q (one document per file, as a JSON object): %w", p, err)
		}
		if want := strings.TrimSuffix(e.Name(), ".json"); want != d.ID {
			return nil, fmt.Errorf("%s: holds document %q; a document's id must match its filename "+
				"so that each instrument has exactly one home", p, d.ID)
		}
		if err := d.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if _, dup := s.byID[d.ID]; dup {
			return nil, fmt.Errorf("%s: duplicate document id %q", p, d.ID)
		}
		s.byID[d.ID] = d
		s.order = append(s.order, d.ID)
	}
	sort.Strings(s.order)
	return s, nil
}

// Len reports how many documents are loaded.
func (s *Store) Len() int { return len(s.byID) }

// All returns every document, ID-ordered.
func (s *Store) All() []Document {
	out := make([]Document, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.byID[id])
	}
	return out
}

// Get returns one document by ID.
func (s *Store) Get(id string) (Document, error) {
	d, ok := s.byID[id]
	if !ok {
		return Document{}, ErrNotFound{ID: id}
	}
	return d, nil
}

// ByJurisdiction returns the documents for one jurisdiction, ID-ordered.
func (s *Store) ByJurisdiction(code string) []Document {
	code = strings.ToUpper(code)
	var out []Document
	for _, d := range s.All() {
		if strings.ToUpper(d.Jurisdiction) == code {
			out = append(out, d)
		}
	}
	return out
}

// Jurisdictions lists the jurisdiction codes that have at least one document.
func (s *Store) Jurisdictions() []string {
	seen := make(map[string]bool, len(s.byID))
	var out []string
	for _, d := range s.All() {
		j := strings.ToUpper(d.Jurisdiction)
		if !seen[j] {
			seen[j] = true
			out = append(out, j)
		}
	}
	sort.Strings(out)
	return out
}
