package resource

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func src() Source {
	return Source{
		Publisher:  "Government Printer",
		URL:        "https://example.org/constitution",
		Retrieved:  "2026-09-17",
		Confidence: Confidence("official"),
	}
}

func validDocument() Document {
	return Document{
		ID:           "kenya-constitution",
		Kind:         Constitution,
		Jurisdiction: "KE",
		Title:        Text{"en": "The Constitution of Kenya, 2010"},
		Summary:      Text{"en": "Supreme law."},
		Adopted:      "2010-08-27",
		Publisher:    "National Council for Law Reporting",
		FullTextURL:  "https://example.org/full",
		Provisions: []Provision{{
			Ref:     "Article 28",
			Heading: Text{"en": "Human dignity"},
			Text:    Text{"en": "Every person has inherent dignity."},
			Topic:   "dignity",
			Sources: []Source{src()},
		}},
		Sources:      []Source{src()},
		LastVerified: "2026-09-17",
	}
}

// --- Validation: the trust guarantee ---

func TestValidDocumentPasses(t *testing.T) {
	if err := validDocument().Validate(); err != nil {
		t.Fatalf("expected valid document to pass, got %v", err)
	}
}

func TestDocumentWithoutSourcesIsRejected(t *testing.T) {
	d := validDocument()
	d.Sources = nil
	err := d.Validate()
	if err == nil || !strings.Contains(err.Error(), "no sources") {
		t.Fatalf("expected unsourced document to be rejected, got %v", err)
	}
}

func TestProvisionWithoutItsOwnSourceIsRejected(t *testing.T) {
	// The point of per-provision sourcing: a document-level source must not
	// be able to launder an unsourced article.
	d := validDocument()
	d.Provisions[0].Sources = nil
	if err := d.Validate(); err == nil {
		t.Fatal("expected a provision with no source of its own to be rejected")
	}
}

func TestProvisionWithInvalidSourceIsRejected(t *testing.T) {
	d := validDocument()
	d.Provisions[0].Sources[0].URL = ""
	err := d.Validate()
	if err == nil || !strings.Contains(err.Error(), "no URL") {
		t.Fatalf("expected a provision whose source has no URL to be rejected, got %v", err)
	}
}

func TestDocumentWithoutFullTextLinkIsRejected(t *testing.T) {
	// The Resources Center exists to send people to the authoritative copy.
	// A document that cannot do that is a dead end.
	d := validDocument()
	d.FullTextURL = ""
	err := d.Validate()
	if err == nil || !strings.Contains(err.Error(), "full_text_url") {
		t.Fatalf("expected a document with no custodian link to be rejected, got %v", err)
	}
}

func TestDocumentWithoutProvisionsIsRejected(t *testing.T) {
	d := validDocument()
	d.Provisions = nil
	err := d.Validate()
	if err == nil || !strings.Contains(err.Error(), "no provisions") {
		t.Fatalf("expected a pointer-only document to be rejected, got %v", err)
	}
}

func TestDocumentWithUnknownKindIsRejected(t *testing.T) {
	d := validDocument()
	d.Kind = "blog post"
	if err := d.Validate(); err == nil {
		t.Fatal("expected an unknown kind to be rejected")
	}
}

func TestDocumentWithBadDatesIsRejected(t *testing.T) {
	d := validDocument()
	d.Adopted = "some time in 2010"
	if err := d.Validate(); err == nil {
		t.Fatal("expected an invalid adopted date to be rejected")
	}
	d = validDocument()
	d.LastVerified = "recently"
	if err := d.Validate(); err == nil {
		t.Fatal("expected an invalid last_verified to be rejected")
	}
}

func TestDisputedProvisionIsFlagged(t *testing.T) {
	p := Provision{Ref: "Article 1", Sources: []Source{{Confidence: "conflicting"}}}
	if !p.Disputed() {
		t.Fatal("expected a provision with a conflicting source to be flagged as disputed")
	}
}

func TestStaleAfterCountsUnparseableDatesAsStale(t *testing.T) {
	d := validDocument()
	d.LastVerified = "nonsense"
	if !d.StaleAfter(time.Hour, time.Now()) {
		t.Fatal("expected an unparseable verification date to count as stale")
	}
	d.LastVerified = "2026-01-01"
	now := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	if !d.StaleAfter(90*24*time.Hour, now) {
		t.Fatal("expected a document verified 8 months ago to be stale at 90 days")
	}
	if d.StaleAfter(365*24*time.Hour, now) {
		t.Fatal("expected a document verified 8 months ago not to be stale at a year")
	}
}

// --- Store: one file per instrument, flat folder ---

func mapFS(files map[string]string) fstest.MapFS {
	m := make(fstest.MapFS, len(files))
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

const kenyaJSON = `{
  "id": "kenya-constitution",
  "kind": "constitution",
  "jurisdiction": "KE",
  "title": {"en": "The Constitution of Kenya, 2010"},
  "summary": {"en": "Supreme law."},
  "adopted": "2010-08-27",
  "publisher": "National Council for Law Reporting",
  "full_text_url": "https://example.org/ke",
  "provisions": [{
    "ref": "Article 28",
    "heading": {"en": "Human dignity"},
    "text": {"en": "Every person has inherent dignity."},
    "topic": "dignity",
    "sources": [{"publisher": "Government Printer", "url": "https://example.org/ke", "retrieved": "2026-09-17", "confidence": "official"}]
  }],
  "sources": [{"publisher": "Government Printer", "url": "https://example.org/ke", "retrieved": "2026-09-17", "confidence": "official"}],
  "last_verified": "2026-09-17"
}`

const ugandaJSON = `{
  "id": "uganda-constitution",
  "kind": "constitution",
  "jurisdiction": "UG",
  "title": {"en": "The Constitution of the Republic of Uganda, 1995"},
  "summary": {"en": "Supreme law."},
  "adopted": "1995-09-22",
  "publisher": "Uganda Legal Information Institute",
  "full_text_url": "https://example.org/ug",
  "provisions": [{
    "ref": "Article 42",
    "heading": {"en": "Right to just and fair treatment in administrative decisions"},
    "text": {"en": "Any person appearing before any administrative official or body has a right to be treated justly and fairly."},
    "topic": "fair process",
    "sources": [{"publisher": "Government of Uganda", "url": "https://example.org/ug", "retrieved": "2026-09-17", "confidence": "official"}]
  }],
  "sources": [{"publisher": "Government of Uganda", "url": "https://example.org/ug", "retrieved": "2026-09-17", "confidence": "official"}],
  "last_verified": "2026-09-17"
}`

// TestDocumentsInOneFolderStandIndependently is the regression test for the
// layout promise: two constitutions sit in one folder and share nothing. Each
// one loads from its own file, keeps its own jurisdiction and its own
// provision sources, and its validity does not depend on the other's
// presence — which is why the folder can grow a third document without a
// third code change.
func TestDocumentsInOneFolderStandIndependently(t *testing.T) {
	together, err := Load(mapFS(map[string]string{
		"resources/kenya-constitution.json":  kenyaJSON,
		"resources/uganda-constitution.json": ugandaJSON,
	}), "resources")
	if err != nil {
		t.Fatalf("loading both documents from one folder: %v", err)
	}
	if together.Len() != 2 {
		t.Fatalf("expected 2 documents, got %d", together.Len())
	}

	ke, err := together.Get("kenya-constitution")
	if err != nil {
		t.Fatal(err)
	}
	if got := ke.Provisions[0].Sources[0].URL; got != "https://example.org/ke" {
		t.Fatalf("expected the Kenyan document to keep its own sources, got %q", got)
	}
	if got := together.ByJurisdiction("UG"); len(got) != 1 || got[0].ID != "uganda-constitution" {
		t.Fatalf("expected the Ugandan document to stand alone in its jurisdiction, got %v", ids(got))
	}
	if got := together.ByJurisdiction("KE"); len(got) != 1 || got[0].ID != "kenya-constitution" {
		t.Fatalf("expected exactly the Kenyan document in KE, got %v", ids(got))
	}

	// Delete one file and the other must still load, unchanged.
	alone, err := Load(mapFS(map[string]string{"resources/uganda-constitution.json": ugandaJSON}), "resources")
	if err != nil {
		t.Fatalf("loading the Ugandan document on its own: %v", err)
	}
	ug, err := alone.Get("uganda-constitution")
	if err != nil {
		t.Fatal(err)
	}
	if ug.Title.In("en") != together.byID["uganda-constitution"].Title.In("en") {
		t.Fatal("expected the Ugandan document to be identical with and without its neighbour present")
	}
	if alone.Len() != 1 {
		t.Fatalf("expected only the Ugandan document, got %d", alone.Len())
	}
}

func TestDocumentIDMustMatchItsFilename(t *testing.T) {
	// Keeps "one instrument, one file, one home" true by construction, so two
	// documents can never quietly share a file and start depending on it.
	_, err := Load(mapFS(map[string]string{"resources/kenya-constitution.json": ugandaJSON}), "resources")
	if err == nil || !strings.Contains(err.Error(), "must match its filename") {
		t.Fatalf("expected the id/filename mismatch to be rejected, got %v", err)
	}
}

func TestMultipleDocumentsInOneFileAreRejected(t *testing.T) {
	// An array is what the guide store's files look like. Here it means
	// someone has bundled instruments together, which is exactly what this
	// layout exists to prevent.
	_, err := Load(mapFS(map[string]string{"resources/bundle.json": "[" + kenyaJSON + "]"}), "resources")
	if err == nil {
		t.Fatal("expected a bundled array of documents to be rejected")
	}
}

func TestInvalidDocumentFailsTheWholeLoad(t *testing.T) {
	bad := strings.Replace(kenyaJSON, `"sources": [{"publisher"`, `"sources": [], "x": [{"publisher"`, 1)
	if _, err := Load(mapFS(map[string]string{
		"resources/kenya-constitution.json":  bad,
		"resources/uganda-constitution.json": ugandaJSON,
	}), "resources"); err == nil {
		t.Fatal("expected one invalid document to fail the whole load rather than being skipped")
	}
}

func TestGetUnknownDocumentReturnsNotFound(t *testing.T) {
	s, err := Load(mapFS(map[string]string{"resources/kenya-constitution.json": kenyaJSON}), "resources")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("nope"); err == nil {
		t.Fatal("expected ErrNotFound for an unknown id")
	}
}

func TestNonJSONFilesAreIgnored(t *testing.T) {
	s, err := Load(mapFS(map[string]string{
		"resources/README.md":               "not a document",
		"resources/kenya-constitution.json": kenyaJSON,
	}), "resources")
	if err != nil {
		t.Fatalf("expected non-JSON files to be skipped, got %v", err)
	}
	if s.Len() != 1 {
		t.Fatalf("expected 1 document, got %d", s.Len())
	}
}

// --- The real seeded folder ---

func seededStore(t *testing.T) *Store {
	t.Helper()
	s, err := Load(os.DirFS("../.."), "resources")
	if err != nil {
		t.Fatalf("loading seeded resources: %v", err)
	}
	return s
}

func TestSeededResourcesLoadAndValidate(t *testing.T) {
	s := seededStore(t)
	if s.Len() != 4 {
		t.Fatalf("expected the three constitutions and the ICPC Act, got %d document(s)", s.Len())
	}
	want := map[string]int{"ke": 1, "ug": 1, "ng": 2}
	for juris, n := range want {
		if got := s.ByJurisdiction(juris); len(got) != n {
			t.Errorf("expected %d document(s) for %s, got %v", n, strings.ToUpper(juris), ids(got))
		}
	}
}

// TestSeededConstitutionListingsAreComplete holds the documentation's claim
// ("every article, in order") to something falsifiable. A regeneration that
// silently truncates a constitution — the failure mode that already bit once
// when a page's footer and Schedules were swallowed by the last article —
// cannot pass this.
func TestSeededConstitutionListingsAreComplete(t *testing.T) {
	cases := map[string]int{
		"kenya-constitution":  264,
		"uganda-constitution": 288,
	}
	s := seededStore(t)
	for id, wantCount := range cases {
		d, err := s.Get(id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if len(d.Provisions) != wantCount {
			t.Errorf("%s: expected %d provisions, got %d", id, wantCount, len(d.Provisions))
		}
		for i, p := range d.Provisions {
			if got := fmt.Sprintf("Article %d", i+1); p.Ref != got {
				t.Fatalf("%s: provision %d is %q, want %q — the listing must be complete and in order", id, i, p.Ref, got)
			}
		}
	}
}

func TestSeededDocumentForUgandaExists(t *testing.T) {
	d, err := seededStore(t).Get("uganda-constitution")
	if err != nil {
		t.Fatalf("expected the Ugandan constitution to be loadable, got %v", err)
	}
	if d.Jurisdiction != "UG" {
		t.Fatalf("expected jurisdiction UG, got %q", d.Jurisdiction)
	}
	if d.FullTextURL == "" {
		t.Fatal("expected a custodian link on the Ugandan document")
	}
	// Article 42 and the Inspectorate articles are the ones the Uganda
	// guides lean on; if the document loses them the guides become
	// unsupported claims.
	for _, ref := range []string{"Article 42", "Article 225"} {
		var found bool
		for _, p := range d.Provisions {
			if p.Ref == ref {
				found = true
			}
		}
		if !found {
			t.Errorf("expected the Ugandan document to carry %s", ref)
		}
	}
}

func TestEverySeededProvisionCarriesItsOwnSource(t *testing.T) {
	for _, d := range seededStore(t).All() {
		for _, p := range d.Provisions {
			if len(p.Sources) == 0 {
				t.Errorf("%s %s has no source of its own", d.ID, p.Ref)
			}
		}
	}
}

func ids(ds []Document) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.ID)
	}
	return out
}
