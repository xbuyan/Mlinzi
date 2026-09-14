package guide

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Store holds validated guides for one or more jurisdictions.
//
// Data lives on a filesystem laid out as data/<jurisdiction>/<name>.json.
// Adding a country means adding a directory: no code changes, no rebuild of
// the logic above it. That is the whole scalability story in one decision.
type Store struct {
	byID    map[string]Guide
	byJuris map[string][]string // jurisdiction (upper) -> guide IDs
}

// ErrNotFound is returned when a guide ID is not in the store.
type ErrNotFound struct{ ID string }

func (e ErrNotFound) Error() string { return fmt.Sprintf("guide %q not found", e.ID) }

// Load reads every .json file under root, validating as it goes. A single
// invalid guide fails the whole load: serving civic information that cannot
// show its provenance is worse than serving none, so this is deliberately
// not a "skip the bad ones and carry on" loader.
func Load(fsys fs.FS, root string) (*Store, error) {
	s := &Store{
		byID:    make(map[string]Guide),
		byJuris: make(map[string][]string),
	}

	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("read data root %q: %w", root, err)
	}

	for _, dir := range entries {
		if !dir.IsDir() {
			continue
		}
		jurisDir := path.Join(root, dir.Name())
		files, err := fs.ReadDir(fsys, jurisDir)
		if err != nil {
			return nil, fmt.Errorf("read jurisdiction %q: %w", dir.Name(), err)
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			p := path.Join(jurisDir, f.Name())
			raw, err := fs.ReadFile(fsys, p)
			if err != nil {
				return nil, fmt.Errorf("read %q: %w", p, err)
			}
			var guides []Guide
			if err := json.Unmarshal(raw, &guides); err != nil {
				return nil, fmt.Errorf("parse %q: %w", p, err)
			}
			for _, g := range guides {
				if err := g.Validate(); err != nil {
					return nil, fmt.Errorf("%s: %w", p, err)
				}
				if _, dup := s.byID[g.ID]; dup {
					return nil, fmt.Errorf("%s: duplicate guide id %q", p, g.ID)
				}
				if !strings.EqualFold(g.Jurisdiction, dir.Name()) {
					return nil, fmt.Errorf("%s: guide %q declares jurisdiction %q but sits in %q",
						p, g.ID, g.Jurisdiction, dir.Name())
				}
				j := strings.ToUpper(g.Jurisdiction)
				s.byID[g.ID] = g
				s.byJuris[j] = append(s.byJuris[j], g.ID)
			}
		}
	}

	for j := range s.byJuris {
		sort.Strings(s.byJuris[j])
	}
	return s, nil
}

// Len reports how many guides are loaded.
func (s *Store) Len() int { return len(s.byID) }

// Jurisdictions lists the loaded jurisdiction codes.
func (s *Store) Jurisdictions() []string {
	out := make([]string, 0, len(s.byJuris))
	for j := range s.byJuris {
		out = append(out, j)
	}
	sort.Strings(out)
	return out
}

// Get returns one guide by ID.
func (s *Store) Get(id string) (Guide, error) {
	g, ok := s.byID[id]
	if !ok {
		return Guide{}, ErrNotFound{ID: id}
	}
	return g, nil
}

// ByJurisdiction returns every guide for a jurisdiction, ID-ordered.
func (s *Store) ByJurisdiction(code string) []Guide {
	ids := s.byJuris[strings.ToUpper(code)]
	out := make([]Guide, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.byID[id])
	}
	return out
}

// Search finds guides in a jurisdiction whose title, summary or category
// match every whitespace-separated term in q, case-insensitively, across all
// languages the guide carries. Matching across languages matters: a user
// searching in Kiswahili should still reach a guide whose Kiswahili title is
// present even if the rest is English-only.
//
// This is a deliberately simple substring index. A civic guide set is
// hundreds of entries, not millions, and a dependency-free search that works
// offline on a cheap device beats a better-ranked one that needs a server.
func (s *Store) Search(jurisdiction, q string) []Guide {
	terms := strings.Fields(strings.ToLower(q))
	all := s.ByJurisdiction(jurisdiction)
	if len(terms) == 0 {
		return all
	}
	var out []Guide
	for _, g := range all {
		hay := searchable(g)
		match := true
		for _, t := range terms {
			if !strings.Contains(hay, t) {
				match = false
				break
			}
		}
		if match {
			out = append(out, g)
		}
	}
	return out
}

func searchable(g Guide) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(g.Category))
	b.WriteByte(' ')
	for _, t := range []Text{g.Title, g.Summary} {
		for _, v := range t {
			b.WriteString(strings.ToLower(v))
			b.WriteByte(' ')
		}
	}
	for _, i := range g.Institutions {
		b.WriteString(strings.ToLower(i.Name))
		b.WriteByte(' ')
	}
	return b.String()
}
