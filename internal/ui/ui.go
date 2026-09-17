// Package ui holds the interface's own words: navigation, buttons, headings,
// field labels, the offline banner. These are the strings that are part of the
// application rather than part of the civic dataset.
//
// They deliberately do not live in data/ or resources/. Those folders are
// validated as civic data — every fact must carry a publisher, a URL, a
// retrieval date and a confidence level, or the load fails. Interface chrome
// has no such provenance and should not be able to borrow the credibility of
// a file that does, so the two are kept apart and the data validation stays
// strict. A strings catalogue that fails its own validation cannot be mistaken
// for verified civic information.
//
// The shape is the same as the civic data, though: every string is a
// language-keyed map. Adding a language is a data change here too — a new key
// in strings.json, no code changes — which is the property the whole project
// claims for translations and would be undermined by hardcoding chrome in
// templates, where it would sit in English forever.
package ui

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/xbuyan/mlinzi/internal/guide"
)

//go:embed strings.json
var catalogJSON []byte

// Catalog is the loaded set of interface strings.
type Catalog struct {
	entries map[string]guide.Text
	langs   []string
}

// Load parses and validates the embedded catalogue.
//
// Validation is deliberately strict and refuses the whole file rather than
// skipping a bad entry: a missing English string is a blank button or heading
// in the UI, and a blank control is worse than a build-time failure. This
// mirrors guide.Load's stance on an unsourced fact.
func Load() (*Catalog, error) { return parse(catalogJSON) }

// parse is Load's body, split out so the validation below can be tested
// against deliberately broken input. A validation path that only ever runs
// against known-good data is a claim, not a check.
func parse(data []byte) (*Catalog, error) {
	var raw map[string]guide.Text
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("ui: parse strings.json: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("ui: strings.json is empty")
	}

	seen := map[string]bool{}
	for key, txt := range raw {
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("ui: strings.json has an empty key")
		}
		// English is the fallback for every other language, so a key that
		// cannot render in English cannot render at all.
		if strings.TrimSpace(txt[guide.DefaultLang]) == "" {
			return nil, fmt.Errorf("ui: string %q has no %s text", key, guide.DefaultLang)
		}
		for lang, s := range txt {
			if strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("ui: string %q has an empty %q translation", key, lang)
			}
			seen[lang] = true
		}
	}

	// English first, then the rest alphabetically: a stable order for the
	// language switcher that does not depend on map iteration, which Go
	// randomises.
	langs := make([]string, 0, len(seen))
	for l := range seen {
		if l != guide.DefaultLang {
			langs = append(langs, l)
		}
	}
	sort.Strings(langs)
	langs = append([]string{guide.DefaultLang}, langs...)

	return &Catalog{entries: raw, langs: langs}, nil
}

// Langs lists every language any string in the catalogue is available in,
// English first.
func (c *Catalog) Langs() []string { return append([]string{}, c.langs...) }

// Keys lists every string key, sorted.
func (c *Catalog) Keys() []string {
	out := make([]string, 0, len(c.entries))
	for k := range c.entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Has reports whether the catalogue defines key.
func (c *Catalog) Has(key string) bool {
	_, ok := c.entries[key]
	return ok
}

// Get returns one string resolved for lang, falling back to English exactly
// as the civic data does.
func (c *Catalog) Get(lang, key string) string {
	txt, ok := c.entries[key]
	if !ok {
		// Not fatal at runtime — a missing key renders as an empty string —
		// but it is a bug in the caller, and TestEveryTemplateKeyExists
		// exists to catch it before it ships.
		return ""
	}
	return txt.In(lang)
}

// Resolve returns every string resolved for lang, ready to hand to a
// template as a map it can index directly.
func (c *Catalog) Resolve(lang string) map[string]string {
	out := make(map[string]string, len(c.entries))
	for key, txt := range c.entries {
		out[key] = txt.In(lang)
	}
	return out
}
