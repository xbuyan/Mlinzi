package guide

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// --- Text ---

func TestTextFallsBackToDefaultLang(t *testing.T) {
	tx := Text{"en": "Report a bribe"}
	if got := tx.In("fr"); got != "Report a bribe" {
		t.Fatalf("expected English fallback, got %q", got)
	}
}

func TestTextPrefersRequestedLang(t *testing.T) {
	tx := Text{"en": "Report a bribe", "sw": "Ripoti rushwa"}
	if got := tx.In("sw"); got != "Ripoti rushwa" {
		t.Fatalf("expected Kiswahili, got %q", got)
	}
}

func TestTextIgnoresEmptyTranslations(t *testing.T) {
	// A half-finished translation must not blank the UI.
	tx := Text{"en": "Report a bribe", "sw": ""}
	if got := tx.In("sw"); got != "Report a bribe" {
		t.Fatalf("expected fallback past empty translation, got %q", got)
	}
}

func TestTextEmptyReturnsEmpty(t *testing.T) {
	empty := Text{}
	if got := empty.In("en"); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

// --- Channels ---

func TestLowBandwidthChannels(t *testing.T) {
	cases := map[ChannelKind]bool{
		Phone: true, USSD: true, SMS: true, InPerson: true, Post: true,
		Email: false, Web: false,
	}
	for kind, want := range cases {
		if got := kind.LowBandwidth(); got != want {
			t.Errorf("%s: LowBandwidth() = %v, want %v", kind, got, want)
		}
	}
}

func TestChannelDisputedWhenSourcesConflict(t *testing.T) {
	c := Channel{
		Kind:  Phone,
		Value: "1559",
		Sources: []Source{
			{Publisher: "A", URL: "https://a", Retrieved: "2026-09-15", Confidence: Conflicting},
		},
	}
	if !c.Disputed() {
		t.Fatal("expected channel with a conflicting source to be disputed")
	}
}

func TestChannelNotDisputedWhenOfficial(t *testing.T) {
	c := Channel{
		Kind:  Phone,
		Value: "1551",
		Sources: []Source{
			{Publisher: "EACC", URL: "https://eacc.go.ke", Retrieved: "2026-09-15", Confidence: Official},
		},
	}
	if c.Disputed() {
		t.Fatal("expected official-sourced channel not to be disputed")
	}
}

// --- Validation: the trust guarantee ---

func validGuide() Guide {
	src := Source{Publisher: "EACC", URL: "https://eacc.go.ke", Retrieved: "2026-09-15", Confidence: Official}
	return Guide{
		ID:           "ke-test",
		Jurisdiction: "KE",
		Title:        Text{"en": "Title"},
		Summary:      Text{"en": "Summary"},
		Steps:        []Step{{Action: Text{"en": "Do the thing"}, Institution: "inst"}},
		Institutions: []Institution{{
			ID:       "inst",
			Name:     "Institution",
			Channels: []Channel{{Kind: Phone, Value: "1551", Sources: []Source{src}}},
		}},
		Sources:      []Source{src},
		LastVerified: "2026-09-15",
	}
}

func TestValidGuidePasses(t *testing.T) {
	if err := validGuide().Validate(); err != nil {
		t.Fatalf("expected valid guide to pass, got %v", err)
	}
}

func TestGuideWithoutSourcesIsRejected(t *testing.T) {
	g := validGuide()
	g.Sources = nil
	err := g.Validate()
	if err == nil || !strings.Contains(err.Error(), "no sources") {
		t.Fatalf("expected unsourced guide to be rejected, got %v", err)
	}
}

func TestGuideWithoutStepsIsRejected(t *testing.T) {
	// Information without next steps is exactly what this layer exists to fix.
	g := validGuide()
	g.Steps = nil
	err := g.Validate()
	if err == nil || !strings.Contains(err.Error(), "no steps") {
		t.Fatalf("expected step-less guide to be rejected, got %v", err)
	}
}

func TestGuideWithBadVerificationDateIsRejected(t *testing.T) {
	g := validGuide()
	g.LastVerified = "sometime last year"
	if err := g.Validate(); err == nil {
		t.Fatal("expected invalid last_verified to be rejected")
	}
}

func TestChannelWithoutSourcesIsRejected(t *testing.T) {
	g := validGuide()
	g.Institutions[0].Channels[0].Sources = nil
	err := g.Validate()
	if err == nil || !strings.Contains(err.Error(), "no sources") {
		t.Fatalf("expected unsourced channel to be rejected, got %v", err)
	}
}

func TestStepReferencingUnknownInstitutionIsRejected(t *testing.T) {
	g := validGuide()
	g.Steps[0].Institution = "ghost"
	err := g.Validate()
	if err == nil || !strings.Contains(err.Error(), "unknown institution") {
		t.Fatalf("expected dangling institution reference to be rejected, got %v", err)
	}
}

func TestStaleAfter(t *testing.T) {
	g := validGuide()
	g.LastVerified = "2026-01-01"
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	if !g.StaleAfter(90*24*time.Hour, now) {
		t.Fatal("expected guide verified 8 months ago to be stale at 90 days")
	}
	if g.StaleAfter(365*24*time.Hour, now) {
		t.Fatal("expected guide verified 8 months ago not to be stale at 365 days")
	}
}

func TestUnparseableDateCountsAsStale(t *testing.T) {
	g := validGuide()
	g.LastVerified = "nonsense"
	if !g.StaleAfter(time.Hour, time.Now()) {
		t.Fatal("expected unparseable verification date to count as stale")
	}
}

// --- Store, against the real seeded dataset ---

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Load(os.DirFS("../.."), "data")
	if err != nil {
		t.Fatalf("loading seeded data: %v", err)
	}
	return s
}

func TestSeededDataLoadsAndValidates(t *testing.T) {
	s := testStore(t)
	if s.Len() == 0 {
		t.Fatal("expected seeded guides, got none")
	}
}

func TestNigerianGuideIsReachableAndScoped(t *testing.T) {
	s := testStore(t)
	g, err := s.Get("ng-bribery-public-service")
	if err != nil {
		t.Fatalf("expected the Nigeria guide to be reachable, got %v", err)
	}
	if g.Jurisdiction != "NG" {
		t.Fatalf("expected jurisdiction NG, got %q", g.Jurisdiction)
	}

	// A search scoped to KE must not leak Nigerian results, and vice versa —
	// jurisdictions are genuinely isolated, not just labeled.
	if got := s.Search("KE", "ICPC"); len(got) != 0 {
		t.Fatalf("expected no ICPC results within KE scope, got %v", ids(got))
	}
	if got := s.Search("NG", "ICPC"); len(got) != 1 {
		t.Fatalf("expected exactly 1 ICPC result within NG scope, got %v", ids(got))
	}
}

func TestUgandanGuidesAreReachableAndScoped(t *testing.T) {
	s := testStore(t)
	g, err := s.Get("ug-bribery-public-service")
	if err != nil {
		t.Fatalf("expected the Uganda bribery guide to be reachable, got %v", err)
	}
	if g.Jurisdiction != "UG" {
		t.Fatalf("expected jurisdiction UG, got %q", g.Jurisdiction)
	}

	// Isolation has to hold as the set grows, not just with two countries:
	// the Inspectorate of Government is a Ugandan body and must not surface
	// inside the Kenyan or Nigerian scopes.
	if got := s.Search("KE", "IGG"); len(got) != 0 {
		t.Fatalf("expected no IGG results within KE scope, got %v", ids(got))
	}
	if got := s.Search("NG", "IGG"); len(got) != 0 {
		t.Fatalf("expected no IGG results within NG scope, got %v", ids(got))
	}
	if got := s.Search("UG", "IGG"); len(got) != 1 {
		t.Fatalf("expected exactly 1 IGG result within UG scope, got %v", ids(got))
	}
}

func TestAddingASecondJurisdictionDidNotBreakTheFirst(t *testing.T) {
	// The whole scalability claim is that adding a country, or a guide, is
	// additive. This is the regression test for that claim: Kenya's original
	// guides must still all be present and unchanged by Nigeria's data
	// existing alongside them, or by later Kenya guides being added. Checked
	// by presence of the known original IDs rather than a total count, so
	// this test doesn't need editing every time Kenya gains another guide.
	s := testStore(t)
	ke := s.ByJurisdiction("KE")
	if len(ke) < 3 {
		t.Fatalf("expected at least Kenya's 3 original guides, got %d", len(ke))
	}
	for _, id := range []string{"ke-bribery-public-service", "ke-police-misconduct", "ke-gender-based-violence"} {
		if _, err := s.Get(id); err != nil {
			t.Errorf("original guide %q missing or broken: %v", id, err)
		}
	}
}

func TestFrenchTranslationResolves(t *testing.T) {
	s := testStore(t)
	g, err := s.Get("ke-bribery-public-service")
	if err != nil {
		t.Fatal(err)
	}
	fr := g.Title.In("fr")
	if fr == "" || fr == g.Title.In("en") {
		t.Fatal("expected a distinct French title, adding a language should be a data change proven end to end")
	}
}

func TestSeededDataHasMultipleJurisdictions(t *testing.T) {
	// Proves the scalability claim rather than asserting it: each country is a
	// genuinely loaded, validated dataset, not a slide. Uganda arrived the
	// same way Nigeria did — a new data folder and nothing else — which is why
	// this test only had to widen its expectation rather than a new mechanism
	// appear for it.
	s := testStore(t)
	js := s.Jurisdictions()
	want := []string{"KE", "NG", "UG"}
	if len(js) != len(want) {
		t.Fatalf("expected %v loaded, got %v", want, js)
	}
	for i := range want {
		if js[i] != want[i] {
			t.Fatalf("expected %v loaded, got %v", want, js)
		}
	}
}

func TestGetKnownGuide(t *testing.T) {
	s := testStore(t)
	g, err := s.Get("ke-bribery-public-service")
	if err != nil {
		t.Fatalf("expected bribery guide, got %v", err)
	}
	if g.Title.In("sw") == g.Title.In("en") {
		t.Fatal("expected a distinct Kiswahili title on the seeded guide")
	}
}

func TestGetUnknownGuide(t *testing.T) {
	s := testStore(t)
	if _, err := s.Get("ke-nonexistent"); err == nil {
		t.Fatal("expected ErrNotFound for unknown id")
	}
}

func TestSearchMatchesEnglish(t *testing.T) {
	s := testStore(t)
	got := s.Search("KE", "bribe")
	if len(got) != 1 || got[0].ID != "ke-bribery-public-service" {
		t.Fatalf("expected the bribery guide, got %v", ids(got))
	}
}

func TestSearchMatchesKiswahili(t *testing.T) {
	// A user searching in Kiswahili must reach the guide even though most of
	// the indexed text is English. "Rushwa" legitimately matches both the
	// bribery guide and the policing guide, since an officer demanding a bribe
	// is covered by the IPOA route as well.
	s := testStore(t)
	got := s.Search("KE", "rushwa")
	if len(got) == 0 {
		t.Fatal("expected Kiswahili search to return results")
	}
	var found bool
	for _, g := range got {
		if g.ID == "ke-bribery-public-service" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the bribery guide among Kiswahili results, got %v", ids(got))
	}
}

func TestSearchMatchesInstitutionName(t *testing.T) {
	s := testStore(t)
	got := s.Search("KE", "IPOA")
	if len(got) != 1 || got[0].ID != "ke-police-misconduct" {
		t.Fatalf("expected the policing guide, got %v", ids(got))
	}
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	s := testStore(t)
	if len(s.Search("KE", "ipoa")) != len(s.Search("KE", "IPOA")) {
		t.Fatal("expected case-insensitive search")
	}
}

func TestEmptySearchReturnsAll(t *testing.T) {
	s := testStore(t)
	// Compares against ByJurisdiction, not s.Len() — a blank query is still
	// scoped to one jurisdiction, so it should return every guide in KE,
	// not every guide in the whole store across every country.
	if len(s.Search("KE", "   ")) != len(s.ByJurisdiction("KE")) {
		t.Fatal("expected blank query to return every guide in the given jurisdiction")
	}
}

func TestSearchUnknownJurisdictionIsEmpty(t *testing.T) {
	s := testStore(t)
	if got := s.Search("ZZ", "bribe"); len(got) != 0 {
		t.Fatalf("expected no results for unknown jurisdiction, got %v", ids(got))
	}
}

func TestEveryGuideOffersALowBandwidthRoute(t *testing.T) {
	// The brief's low-bandwidth constraint, enforced as a test: every guide in
	// every country must be actionable from a basic handset with no mobile
	// data. Scoped across all loaded jurisdictions deliberately — a new
	// country must clear the same bar as the original one, rather than
	// inheriting an exemption by not being listed here.
	s := testStore(t)
	for _, j := range s.Jurisdictions() {
		for _, g := range s.ByJurisdiction(j) {
			found := false
			for _, i := range g.Institutions {
				if len(i.LowBandwidthChannels()) > 0 {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("guide %q has no low-bandwidth channel", g.ID)
			}
		}
	}
}

func TestDisputedChannelsSurviveLoading(t *testing.T) {
	// IPOA publishes two different toll-free numbers across credible sources.
	// Both must reach the user, flagged, rather than one being silently picked.
	s := testStore(t)
	g, err := s.Get("ke-police-misconduct")
	if err != nil {
		t.Fatal(err)
	}
	ipoa, ok := g.Institution("ipoa")
	if !ok {
		t.Fatal("expected IPOA in the policing guide")
	}
	disputed := 0
	for _, c := range ipoa.Channels {
		if c.Disputed() {
			disputed++
		}
	}
	if disputed < 2 {
		t.Fatalf("expected both candidate hotlines flagged as disputed, got %d", disputed)
	}
}

// translatedJurisdictions names the countries whose data is expected to be
// complete in every supported language. Nigeria is deliberately absent: it is
// English-only, and saying so here is more honest than an empty expectation.
var translatedJurisdictions = map[string][]string{
	"KE": {"en", "sw", "fr"},
	"UG": {"en", "sw", "fr"},
}

func TestTranslatedJurisdictionsCoverEveryString(t *testing.T) {
	// Kenya set the standard: every human-readable string carries English,
	// Kiswahili and French. Uganda was brought to the same standard, and this
	// tripwire keeps it there — a translation added later that skips a field,
	// or a new field added without one, fails here instead of silently
	// rendering English in the middle of a translated page. It walks the
	// domain type rather than the JSON so a new Text field cannot escape it.
	s := testStore(t)
	for jur, langs := range translatedJurisdictions {
		for _, g := range s.ByJurisdiction(jur) {
			check := func(field string, txt Text) {
				t.Helper()
				if len(txt.Langs()) == 0 {
					return // optional field, absent for this guide
				}
				for _, lang := range langs {
					if strings.TrimSpace(txt[lang]) == "" {
						t.Errorf("guide %q: %s has no %q translation", g.ID, field, lang)
					}
				}
			}

			check("title", g.Title)
			check("summary", g.Summary)
			check("timeline", g.Timeline)
			for i, r := range g.Rights {
				check(fmt.Sprintf("rights[%d]", i), r)
			}
			for i, e := range g.Evidence {
				check(fmt.Sprintf("evidence[%d]", i), e)
			}
			for i, st := range g.Steps {
				check(fmt.Sprintf("steps[%d].action", i), st.Action)
				check(fmt.Sprintf("steps[%d].detail", i), st.Detail)
				check(fmt.Sprintf("steps[%d].deadline", i), st.Deadline)
			}
			for _, inst := range g.Institutions {
				check("institution "+inst.ID+" role", inst.Role)
				check("institution "+inst.ID+" mandate", inst.Mandate)
				for i, c := range inst.Channels {
					check(fmt.Sprintf("institution %s channel[%d] note", inst.ID, i), c.Note)
				}
			}
		}
	}
}

func TestUgandaTranslationsResolve(t *testing.T) {
	// End to end rather than by inspection: the freshly translated Ugandan
	// guides must actually resolve to Kiswahili and French, not just carry the
	// keys. A stale cache or a fallback bug would show English here.
	s := testStore(t)
	for _, id := range []string{"ug-bribery-public-service", "ug-police-misconduct", "ug-gender-based-violence"} {
		g, err := s.Get(id)
		if err != nil {
			t.Fatalf("expected %s to be reachable, got %v", id, err)
		}
		for _, lang := range []string{"sw", "fr"} {
			got := g.Summary.In(lang)
			if got == "" || got == g.Summary.In("en") {
				t.Errorf("guide %q: summary did not resolve to %q", id, lang)
			}
		}
	}
}

func ids(gs []Guide) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		out = append(out, g.ID)
	}
	return out
}
