// Package resource models the Resources Center: primary legal documents a
// person can go and read for themselves, so that "you have a right to X" can
// point at the instrument that actually says so rather than at our summary of
// it.
//
// It is deliberately a sibling of internal/guide, not a mode of it. A guide
// answers "what should I do about my situation?" and is indexed by situation;
// a document answers "what does the law actually say?" and is indexed by
// instrument. They go stale for different reasons (a hotline changes; a
// constitution is amended), are read by different people, and are versioned
// differently.
//
// What they do share is one provenance model, via type aliases, and one
// validator (guide.Source.Validate). A second definition of Text or Source
// would be a second place for the sourcing rules to drift — and the whole
// project rests on there being exactly one such place.
//
// Storage is one JSON object per instrument, all directly inside a single
// resources/ folder. Kenya's constitution and Uganda's sit side by side in
// that folder and share nothing: no cross-references, no shared provisioning
// step, no ordering dependency. That is what "one folder, standing
// independently" means concretely, and it is enforced rather than described —
// a document's id must match its filename, so each instrument has exactly one
// home, and TestDocumentsInOneFolderStandIndependently is the regression test.
package resource

import (
	"fmt"
	"strings"
	"time"

	"github.com/xbuyan/mlinzi/internal/guide"
)

// Text, Source and Confidence are the guide layer's provenance types, reused
// by alias rather than redefined.
type (
	Text       = guide.Text
	Source     = guide.Source
	Confidence = guide.Confidence
)

// DefaultLang is the fallback language, shared with the guide layer.
const DefaultLang = guide.DefaultLang

// Kind is the class of instrument a document is.
type Kind string

const (
	Constitution Kind = "constitution"
	Statute      Kind = "statute"
	Charter      Kind = "charter"
)

// Valid reports whether k is a kind this layer knows how to present.
func (k Kind) Valid() bool {
	switch k {
	case Constitution, Statute, Charter:
		return true
	}
	return false
}

// Provision is one article or section of a document, with its own sources.
//
// Per-provision sourcing is the point of the type. A document is not
// trustworthy merely because the file around it is, and one provision whose
// wording is disputed or whose text was checked against a weaker source must
// stay visible as exactly that instead of dissolving into a wall of text.
type Provision struct {
	Ref     string   `json:"ref"`     // e.g. "Article 42"
	Heading Text     `json:"heading"` // the instrument's own heading for it
	Text    Text     `json:"text"`    // what it says, close to the enacted wording
	Topic   string   `json:"topic"`   // e.g. "accountability", "fair process"
	Sources []Source `json:"sources"`
}

// Disputed reports whether any source for this provision flags disagreement.
func (p Provision) Disputed() bool {
	for _, s := range p.Sources {
		if s.Confidence == guide.Conflicting {
			return true
		}
	}
	return false
}

func (p Provision) validate() error {
	if strings.TrimSpace(p.Ref) == "" {
		return fmt.Errorf("provision has no ref")
	}
	if p.Heading.In(DefaultLang) == "" {
		return fmt.Errorf("provision %q has no heading", p.Ref)
	}
	if p.Text.In(DefaultLang) == "" {
		return fmt.Errorf("provision %q has no text", p.Ref)
	}
	if len(p.Sources) == 0 {
		return fmt.Errorf("provision %q has no source of its own", p.Ref)
	}
	for _, s := range p.Sources {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("provision %q: %w", p.Ref, err)
		}
	}
	return nil
}

// Document is one standalone instrument: a constitution, a statute, a charter.
type Document struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	// Jurisdiction is ISO 3166-1 alpha-2, e.g. "KE". It is what lets one
	// folder hold documents from many countries without them becoming a set.
	Jurisdiction string `json:"jurisdiction"`
	Title        Text   `json:"title"`
	Summary      Text   `json:"summary"`
	Adopted      string `json:"adopted"` // YYYY-MM-DD
	// Publisher and FullTextURL point at the custodian's authoritative copy.
	// They are a pointer, not a claim: they are deliberately not part of
	// Sources, so that nothing here silently claims we verified them.
	Publisher   string `json:"publisher"`
	FullTextURL string `json:"full_text_url"`
	// Caveat states plainly what this extract is and is not — that it is a
	// short selection, not the whole instrument, and not legal advice.
	Caveat     Text        `json:"caveat,omitempty"`
	Provisions []Provision `json:"provisions"`
	Sources    []Source    `json:"sources"`
	// LastVerified is when the provisions were last checked, YYYY-MM-DD.
	LastVerified string `json:"last_verified"`
}

// VerifiedAt parses LastVerified.
func (d Document) VerifiedAt() (time.Time, error) {
	return time.Parse("2006-01-02", d.LastVerified)
}

// StaleAfter reports whether the document was last verified more than d ago.
// Constitutions change slowly, but the extract, the custodian's URL, and the
// article numbering under amendment do not — so the age is still shown.
func (d Document) StaleAfter(dur time.Duration, now time.Time) bool {
	t, err := d.VerifiedAt()
	if err != nil {
		return true
	}
	return now.Sub(t) > dur
}

// Langs returns the language codes the document's title is available in.
func (d Document) Langs() []string { return d.Title.Langs() }

// Validate enforces the guarantee the Resources Center rests on: a document
// that cannot show where its provisions came from does not get published,
// and neither does any single provision inside an otherwise sourced one.
func (d Document) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return fmt.Errorf("document has no id")
	}
	if !d.Kind.Valid() {
		return fmt.Errorf("document %q has invalid kind %q", d.ID, d.Kind)
	}
	if len(d.Jurisdiction) != 2 {
		return fmt.Errorf("document %q has invalid jurisdiction %q", d.ID, d.Jurisdiction)
	}
	if d.Title.In(DefaultLang) == "" {
		return fmt.Errorf("document %q has no title", d.ID)
	}
	if d.Summary.In(DefaultLang) == "" {
		return fmt.Errorf("document %q has no summary", d.ID)
	}
	if _, err := time.Parse("2006-01-02", d.Adopted); err != nil {
		return fmt.Errorf("document %q has invalid adopted date %q", d.ID, d.Adopted)
	}
	if strings.TrimSpace(d.Publisher) == "" {
		return fmt.Errorf("document %q has no publisher", d.ID)
	}
	if strings.TrimSpace(d.FullTextURL) == "" {
		return fmt.Errorf("document %q has no full_text_url: a reader must be able to reach the authoritative text", d.ID)
	}
	if len(d.Provisions) == 0 {
		return fmt.Errorf("document %q has no provisions: a pointer to a law with nothing quoted from it is not a resource", d.ID)
	}
	for _, p := range d.Provisions {
		if err := p.validate(); err != nil {
			return fmt.Errorf("document %q: %w", d.ID, err)
		}
	}
	if len(d.Sources) == 0 {
		return fmt.Errorf("document %q has no sources", d.ID)
	}
	for _, s := range d.Sources {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("document %q: %w", d.ID, err)
		}
	}
	if _, err := d.VerifiedAt(); err != nil {
		return fmt.Errorf("document %q has invalid last_verified %q", d.ID, d.LastVerified)
	}
	return nil
}
