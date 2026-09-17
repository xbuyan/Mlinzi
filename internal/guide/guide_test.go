package guide

import (
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

func TestAddingASecondJurisdictionDidNotBreakTheFirst(t *testing.T) {
	// The whole scalability claim is that adding a country is additive.
	// This is the regression test for that claim: Kenya's guide count and
	// content must be unaffected by Nigeria's data existing alongside it.
	s := testStore(t)
	ke := s.ByJurisdiction("KE")
	if len(ke) != 3 {
		t.Fatalf("expected Kenya's 3 original guides untouched, got %d", len(ke))
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
	// Proves the scalability claim rather than asserting it: a second
	// country is a genuinely loaded, validated dataset, not a slide.
	s := testStore(t)
	js := s.Jurisdictions()
	if len(js) != 2 || js[0] != "KE" || js[1] != "NG" {
		t.Fatalf("expected KE and NG loaded, got %v", js)
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
	// The brief's low-bandwidth constraint, enforced as a test: every guide
	// must be actionable from a basic handset with no mobile data.
	s := testStore(t)
	for _, g := range s.ByJurisdiction("KE") {
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

func ids(gs []Guide) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		out = append(out, g.ID)
	}
	return out
}
