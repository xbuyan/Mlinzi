package main

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/xbuyan/mlinzi/internal/guardian"
	"github.com/xbuyan/mlinzi/internal/report"
)

// The localization tests below cover the two distinct ways a translated page
// can go wrong, which is what "the translations are not working well" turned
// out to mean in practice:
//
//  1. The content is translated but the frame around it is not, because
//     chrome strings were written into the templates as literal English.
//  2. The language is chosen but not carried, because links and forms drop it
//     on the way to the next page — so a user is bounced back to English by
//     the first thing they click.
//
// Both are plumbing, not translation quality, and both are testable.

// templateStringKeyPattern matches a catalogue lookup written in a template:
// either `.T.some_key` or `$T.some_key`.
var templateStringKeyPattern = regexp.MustCompile(`(?:\$T|\.T)\.([a-z0-9_]+)`)

// TestEveryTemplateStringKeyExistsInCatalog is the tripwire that keeps the two
// halves of localization in step. Templates are text; a key with a typo, or
// one whose catalogue entry was renamed or deleted, does not fail the build —
// it renders an empty string, so a heading or button simply vanishes. Scanning
// the parsed templates against the loaded catalogue turns that silent hole
// into a failing test.
func TestEveryTemplateStringKeyExistsInCatalog(t *testing.T) {
	a, _ := newTestAppWithStore(t)

	files, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		t.Fatalf("glob templates: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected to find templates to scan")
	}

	found := 0
	for _, file := range files {
		src, err := templateFS.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, m := range templateStringKeyPattern.FindAllStringSubmatch(string(src), -1) {
			key := m[1]
			found++
			if !a.strings.Has(key) {
				t.Errorf("%s references interface string %q, which the catalogue does not define", file, key)
			}
		}
	}
	if found == 0 {
		t.Fatal("expected the templates to reference catalogue strings, found none — did the scan break?")
	}
}

// TestDynamicLabelKeysExist covers the labels a template cannot name
// literally, because they come from the data: channel kinds, guide categories,
// report and case statuses, and document kinds are all looked up as
// `<prefix>_<data value>`. Those lookups are the ones most likely to be missed,
// since no compiler sees them.
func TestDynamicLabelKeysExist(t *testing.T) {
	a, _ := newTestAppWithStore(t)

	// Enumerated from the real dataset, so a new kind or category in the data
	// fails here until the catalogue carries its label.
	for _, jur := range a.guides.Jurisdictions() {
		for _, g := range a.guides.ByJurisdiction(jur) {
			requireString(t, a, "category_"+g.Category,
				fmt.Sprintf("guide %s has category %q", g.ID, g.Category))
			for _, inst := range g.Institutions {
				for _, c := range inst.Channels {
					requireString(t, a, "channel_"+string(c.Kind),
						fmt.Sprintf("guide %s institution %s has channel kind %q", g.ID, inst.ID, c.Kind))
				}
			}
		}
	}
	for _, d := range a.resources.All() {
		requireString(t, a, "kind_"+string(d.Kind),
			fmt.Sprintf("document %s has kind %q", d.ID, d.Kind))
	}

	// Report statuses are discovered by walking the state machine from
	// submitted via NextOptions, rather than being listed here — so a status
	// added to the domain later is picked up without editing this test.
	seen := map[report.Status]bool{}
	queue := []report.Status{report.Submitted}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		if seen[s] {
			continue
		}
		seen[s] = true
		requireString(t, a, "status_"+string(s), fmt.Sprintf("report status %q", s))
		queue = append(queue, s.NextOptions()...)
	}
	if len(seen) < 3 {
		t.Fatalf("expected the report state machine to yield several statuses, got %v", seen)
	}

	// Case statuses have no equivalent enumeration in the domain package, so
	// they are named explicitly — with the note that a status added to
	// internal/guardian needs a line here and an entry in the catalogue.
	for _, s := range []guardian.Status{guardian.Active, guardian.Escalated, guardian.Released} {
		requireString(t, a, "status_"+string(s), fmt.Sprintf("case status %q", s))
	}
}

func requireString(t *testing.T, a *app, key, context string) {
	t.Helper()
	if !a.strings.Has(key) {
		t.Errorf("%s, but the catalogue has no %q entry to label it with", context, key)
	}
}

// TestEveryPageCarriesTheRequestedLanguage asserts the fix for the first
// failure mode directly: on a French page, the header links and the switcher
// must keep `lang=fr` rather than sending the reader back to English.
func TestEveryPageCarriesTheRequestedLanguage(t *testing.T) {
	h := newTestApp(t)

	for _, path := range []string{
		"/", "/?j=UG", "/guides/ug-bribery-public-service", "/resources",
		"/resources/kenya-constitution", "/report/new", "/report/status", "/institution",
	} {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		rec := get(t, h, path+sep+"lang=fr")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{
			"/resources?lang=fr",
			"/report/status?lang=fr",
			"/institution?lang=fr",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: expected the header to link to %q so the language survives navigation", path, want)
			}
		}
	}
}

// TestLanguageSwitcherPreservesTheRestOfTheQuery is the regression test for
// the specific bug that made translations feel broken: the switcher used to be
// a form posting to action="", which replaces the entire query string. On the
// home page that meant changing language silently discarded the chosen country
// and the search term — pick Uganda, switch to Kiswahili, land back in Kenya.
func TestLanguageSwitcherPreservesTheRestOfTheQuery(t *testing.T) {
	h := newTestApp(t)

	rec := get(t, h, "/?j=UG&q=rushwa&lang=en")
	body := rec.Body.String()

	hrefs := regexp.MustCompile(`href="([^"]*lang=fr[^"]*)"`).FindAllStringSubmatch(body, -1)
	if len(hrefs) == 0 {
		t.Fatal("expected a link to the French version of this page")
	}
	for _, m := range hrefs {
		link := m[1]
		if !strings.Contains(link, "j=UG") || !strings.Contains(link, "q=rushwa") {
			t.Errorf("language link %q dropped the country or search term", link)
		}
	}
}

// TestSwitcherIsLinksNotAForm pins the mechanism, because the mechanism was
// the bug. A select-and-submit control cannot preserve a query string without
// hidden fields, and needs JavaScript to submit at all — so the switcher must
// stay plain links, which work offline and on a basic browser too.
func TestSwitcherIsLinksNotAForm(t *testing.T) {
	h := newTestApp(t)
	body := get(t, h, "/?lang=fr").Body.String()

	if strings.Contains(body, `select name="lang"`) {
		t.Error("the language switcher is a form select again, which cannot carry the rest of the query string")
	}
	if !strings.Contains(body, `aria-current="true"`) {
		t.Error("expected the current language to be marked, so it is clear which one is active")
	}
}

// TestNoEnglishChromeInTranslatedPages is the other half of the same story:
// translated content inside an untranslated frame. It asserts that the English
// chrome strings are genuinely gone from French pages — not that French
// appears somewhere, which a single translated line would satisfy.
func TestNoEnglishChromeInTranslatedPages(t *testing.T) {
	h := newTestApp(t)

	// Chrome that used to be hardcoded, and its French counterpart. Each
	// English string must be absent and its French equivalent present.
	type pair struct{ english, french string }
	pairs := []pair{
		{"Resources Center", "Centre de ressources"},
		{"Check a report", "Suivre un signalement"},
		{"Institution portal", "Portail institutionnel"},
		{"Next steps", "Étapes suivantes"},
		{"Where to go", "Où s'adresser"},
		{"Your rights", "Vos droits"},
	}

	pages := []string{
		"/?lang=fr",
		"/guides/ug-bribery-public-service?lang=fr",
		"/resources?lang=fr",
		"/resources/kenya-constitution?lang=fr",
		"/report/new?lang=fr",
		"/report/status?lang=fr",
	}
	for _, path := range pages {
		body := get(t, h, path).Body.String()
		for _, p := range pairs {
			if strings.Contains(body, p.english) {
				t.Errorf("%s: still shows English chrome %q", path, p.english)
			}
		}
	}

	// And the French must actually be there, so a page that dropped its
	// chrome entirely cannot pass the check above.
	// Fragments without apostrophes on purpose: html/template escapes one to
	// &#39; inside the rendered page, so a test looking for the literal French
	// wording would fail on correct output. Cheaper to avoid the character in
	// the assertion than to decode entities here.
	guideBody := get(t, h, "/guides/ug-bribery-public-service?lang=fr").Body.String()
	for _, want := range []string{"Vos droits", "Étapes suivantes", "adresser", "Sources"} {
		if !strings.Contains(guideBody, want) {
			t.Errorf("French guide page is missing %q", want)
		}
	}
	if !strings.Contains(get(t, h, "/?lang=fr").Body.String(), "Rechercher") {
		t.Error("French home page is missing its search button label")
	}
}

// TestKiswahiliChromeRenders checks the same for Kiswahili, so the fix is not
// silently English-or-French only.
func TestKiswahiliChromeRenders(t *testing.T) {
	h := newTestApp(t)
	body := get(t, h, "/guides/ug-bribery-public-service?lang=sw").Body.String()

	for _, want := range []string{"Haki zako", "Hatua zinazofuata", "Mahali pa kwenda", "Vyanzo"} {
		if !strings.Contains(body, want) {
			t.Errorf("Kiswahili guide page is missing %q", want)
		}
	}
	if strings.Contains(body, "Next steps") || strings.Contains(body, "Resources Center") {
		t.Error("Kiswahili page still shows English chrome")
	}
}

// TestGuideSaysWhenItHasNoTranslation covers the honest-reporting side: a
// guide that only exists in English, viewed in French, should say so rather
// than silently presenting English as if it were the French version.
func TestGuideSaysWhenItHasNoTranslation(t *testing.T) {
	h := newTestApp(t)

	// Nigeria's guides are English-only, so the note belongs here...
	fr := get(t, h, "/guides/ng-bribery-public-service?lang=fr").Body.String()
	if !strings.Contains(fr, "pas encore disponible dans votre langue") {
		t.Error("expected the English-only note on a French page for an untranslated guide")
	}
	// ...and must be in the language the reader asked for, not English.
	if strings.Contains(fr, "not yet available in your language") {
		t.Error("the notice itself came back in English")
	}
	// ...and must not appear for a guide that does have the translation.
	ug := get(t, h, "/guides/ug-bribery-public-service?lang=fr").Body.String()
	if strings.Contains(ug, "pas encore disponible") {
		t.Error("a fully translated guide should not claim to be missing its translation")
	}
}

// TestResourcePageStatesThatTheLawIsEnglish pins the decision that legal text
// stays in its authoritative wording. A reader looking at a French page of a
// constitution must be told plainly that the articles are not the translated
// part, rather than being left to assume the surrounding translation covers
// them.
func TestResourcePageStatesThatTheLawIsEnglish(t *testing.T) {
	h := newTestApp(t)

	fr := get(t, h, "/resources/kenya-constitution?lang=fr").Body.String()
	if !strings.Contains(fr, "force de loi") {
		t.Error("expected the French legal-wording notice on a French resource page")
	}
	sw := get(t, h, "/resources/uganda-constitution?lang=sw").Body.String()
	if !strings.Contains(sw, "nguvu ya kisheria") {
		t.Error("expected the Kiswahili legal-wording notice on a Kiswahili resource page")
	}
	en := get(t, h, "/resources/kenya-constitution?lang=en").Body.String()
	if strings.Contains(en, "which is the version that has legal force") {
		t.Error("the notice should not appear on the English page, where it states the obvious")
	}

	// The provisions themselves stay in English, which is the point.
	if !strings.Contains(fr, "Sovereignty of the people") {
		t.Error("expected the article text to remain in its authoritative English wording")
	}
}

// TestUnsupportedLanguageIsDeclaredAsEnglish covers the case where a language
// code arrives that has no strings: the page falls back to English, and it
// must be *declared* as English, since lang="xx" over English text tells a
// screen reader to pronounce the page with the wrong rules.
func TestUnsupportedLanguageIsDeclaredAsEnglish(t *testing.T) {
	h := newTestApp(t)
	body := get(t, h, "/guides/ug-bribery-public-service?lang=pt").Body.String()

	if !strings.Contains(body, `<html lang="en">`) {
		t.Error("expected an unsupported language to be declared as English")
	}
	if !strings.Contains(body, "Next steps") {
		t.Error("expected English content as the fallback")
	}
}

// TestServiceWorkerSpeaksEveryLanguage closes the last gap in "the offline
// copy is the one you asked for". Offline support is keyed by URL, and the
// language lives in the URL, so an offline copy that only exists in English is
// an offline feature that only works in English. This checks the worker's
// language list against the catalogue's: they are maintained in two different
// files, so they can drift, and drift here is invisible until someone with no
// connection is served the wrong language — or an error page.
func TestServiceWorkerSpeaksEveryLanguage(t *testing.T) {
	a, _ := newTestAppWithStore(t)

	swSource, err := staticFS.ReadFile("static/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`const LANGUAGES = \[([^\]]*)\]`).FindStringSubmatch(string(swSource))
	if m == nil {
		t.Fatal("expected sw.js to declare a LANGUAGES list for the precache")
	}
	var precached []string
	for _, part := range strings.Split(m[1], ",") {
		if code := strings.Trim(strings.TrimSpace(part), `"'`); code != "" {
			precached = append(precached, code)
		}
	}

	catalog := a.strings.Langs()
	if len(precached) != len(catalog) {
		t.Fatalf("sw.js precaches languages %v but the catalogue supports %v", precached, catalog)
	}
	for _, want := range catalog {
		found := false
		for _, got := range precached {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("language %q is supported by the catalogue but not precached in sw.js", want)
		}
	}

	// The variants must actually be built from that list, not merely declared.
	if !strings.Contains(string(swSource), "SMALL_PAGES.map") {
		t.Error("expected sw.js to expand the small pages over every language")
	}

	// The bare paths must be precached as well as the ?lang= variants, because
	// a cache key is an exact URL and the manifest's start_url is "/". Omitting
	// them leaves the installed app unable to open offline — which is what
	// happened when this list first gained language variants, and why the check
	// is here rather than left to a comment.
	if !strings.Contains(string(swSource), "...SMALL_PAGES,") {
		t.Error("expected sw.js to precache the bare small-page paths too, not only the language variants")
	}
	manifest, err := staticFS.ReadFile("static/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	startURL := regexp.MustCompile(`"start_url":\s*"([^"]+)"`).FindStringSubmatch(string(manifest))
	if startURL == nil {
		t.Fatal("expected the manifest to declare a start_url")
	}
	if !strings.Contains(string(swSource), `"`+startURL[1]+`"`) {
		t.Errorf("the manifest's start_url %q is not precached, so an installed app cannot open offline", startURL[1])
	}
}

// TestLocalizedMessagesInHandlers covers the strings that never appear in a
// template at all, because a handler composes them: form errors and case
// messages. Those were the easiest to forget, since no template scan sees them.
func TestLocalizedMessagesInHandlers(t *testing.T) {
	h := newTestApp(t)

	// An empty report body is refused with a message from the catalogue.
	rec := postForm(t, h, "/report", url.Values{
		"category": {"bribery"}, "guide_id": {"ke-bribery-public-service"}, "content": {""}, "lang": {"fr"},
	})
	if !strings.Contains(rec.Body.String(), "Veuillez décrire") {
		t.Errorf("expected the French validation message, got:\n%s", rec.Body.String())
	}

	// A status lookup that matches nothing likewise.
	rec = postForm(t, h, "/report/status", url.Values{
		"report_id": {"rpt_nonexistent"}, "code": {"nope"}, "lang": {"sw"},
	})
	if !strings.Contains(rec.Body.String(), "Hakuna ripoti inayolingana") {
		t.Errorf("expected the Kiswahili lookup error, got:\n%s", rec.Body.String())
	}
}
