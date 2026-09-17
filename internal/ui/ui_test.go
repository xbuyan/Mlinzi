package ui

import (
	"strings"
	"testing"
)

func TestCatalogLoads(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Keys()) == 0 {
		t.Fatal("expected interface strings, got none")
	}
	if len(c.Langs()) < 3 {
		t.Fatalf("expected at least three languages, got %v", c.Langs())
	}
}

// TestEveryStringExistsInEveryLanguage is the tripwire for the whole point of
// this package: a language that is "supported" must actually be supported
// everywhere. Without it, adding a key with only English text would ship a
// blank heading in Kiswahili and French — a silent hole in a page, which is
// exactly the class of defect this package was created to stop.
func TestEveryStringExistsInEveryLanguage(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	langs := c.Langs()
	for _, key := range c.Keys() {
		for _, lang := range langs {
			if got := c.Get(lang, key); strings.TrimSpace(got) == "" {
				t.Errorf("string %q has no %q text", key, lang)
			}
		}
	}
}

func TestGetFallsBackToEnglish(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// Same fallback contract as the civic data: an unknown language resolves
	// to English rather than to nothing, so a half-finished language can
	// never blank a control.
	if got := c.Get("pt", "home_search"); got != c.Get("en", "home_search") {
		t.Fatalf("expected English fallback, got %q", got)
	}
}

func TestGetUnknownKeyIsEmptyNotPanic(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Get("en", "no_such_key_exists"); got != "" {
		t.Fatalf("expected empty string for an unknown key, got %q", got)
	}
}

func TestResolveCoversEveryKey(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range c.Langs() {
		resolved := c.Resolve(lang)
		if len(resolved) != len(c.Keys()) {
			t.Fatalf("Resolve(%q) returned %d strings, expected %d", lang, len(resolved), len(c.Keys()))
		}
		for key, s := range resolved {
			if strings.TrimSpace(s) == "" {
				t.Errorf("Resolve(%q)[%q] is empty", lang, key)
			}
		}
	}
}

// TestParseRejectsIncompleteStrings proves the validation actually refuses bad
// input rather than merely looking like it would. Each case is a file that
// would render a blank control if it were accepted.
func TestParseRejectsIncompleteStrings(t *testing.T) {
	cases := map[string]string{
		"no English text":    `{"a": {"sw": "swahili", "fr": "french"}}`,
		"empty English text": `{"a": {"en": "   ", "sw": "swahili"}}`,
		"empty translation":  `{"a": {"en": "english", "fr": ""}}`,
		"no strings at all":  `{}`,
		"malformed json":     `{"a": `,
	}
	for name, data := range cases {
		if _, err := parse([]byte(data)); err == nil {
			t.Errorf("%s: expected a refused load, got none", name)
		}
	}
}

func TestParseAcceptsCompleteStrings(t *testing.T) {
	c, err := parse([]byte(`{"a": {"en": "english", "sw": "swahili", "fr": "french"}}`))
	if err != nil {
		t.Fatalf("expected a valid file to load, got %v", err)
	}
	if got := c.Get("fr", "a"); got != "french" {
		t.Fatalf("expected the French text, got %q", got)
	}
	if langs := c.Langs(); langs[0] != "en" {
		t.Fatalf("expected English first in %v", langs)
	}
}
