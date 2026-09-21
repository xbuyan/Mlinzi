package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// newTestApp builds a fresh app and mux, wired the same way main() wires
// them, so these tests exercise the real routing and handler code — not a
// simplified stand-in for it.
func newTestApp(t *testing.T) http.Handler {
	t.Helper()
	_, h := newTestAppWithStore(t)
	return h
}

// newTestAppWithStore also hands back the app itself, for tests that need to
// enumerate the loaded data — every channel kind, every category, every
// document kind — rather than only make requests against it. That matters for
// the localization tests: the labels a page looks up dynamically are driven by
// whatever the data actually contains, so a test can only check them against
// the real dataset, not against a list someone remembered to update.
func newTestAppWithStore(t *testing.T) (*app, http.Handler) {
	t.Helper()
	a, err := newApp()
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	return a, muxForTest(t, a)
}

func muxForTest(t *testing.T, a *app) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.handleHome)
	mux.HandleFunc("GET /guides/{id}", a.handleGuideDetail)
	mux.HandleFunc("GET /resources", a.handleResources)
	mux.HandleFunc("GET /resources/{id}", a.handleResourceDetail)
	mux.HandleFunc("GET /ask", a.handleAskForm)
	mux.HandleFunc("POST /ask", a.handleAsk)
	mux.HandleFunc("GET /report/new", a.handleReportNew)
	// Wrapped the same way main() wraps them, so the tests exercise the
	// same persistence hook the deployed routes go through.
	mux.HandleFunc("POST /report", a.snapshotHook(a.handleReportCreate))
	mux.HandleFunc("GET /report/status", a.handleStatusForm)
	mux.HandleFunc("POST /report/status", a.handleStatusResult)
	mux.HandleFunc("GET /evidence/{hash}", a.handleEvidence)
	mux.HandleFunc("POST /report/{id}/protect", a.snapshotHook(a.handleProtectCreate))
	mux.HandleFunc("GET /cases/{id}", a.handleCaseDashboard)
	mux.HandleFunc("POST /cases/{id}/checkin", a.snapshotHook(a.handleCaseCheckIn))
	mux.HandleFunc("POST /cases/{id}/escalate", a.snapshotHook(a.handleCaseEscalate))
	mux.HandleFunc("POST /cases/{id}/submit-share", a.snapshotHook(a.handleCaseSubmitShare))
	mux.HandleFunc("GET /institution", a.handleInstitutionList)
	mux.HandleFunc("POST /institution/{id}/advance", a.snapshotHook(a.handleInstitutionAdvance))

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		t.Fatalf("static assets: %v", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))
	mux.HandleFunc("GET /sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Service-Worker-Allowed", "/")
		data, err := staticFS.ReadFile("static/sw.js")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	})
	return mux
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func postForm(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHomeServesGuides(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Know your rights") {
		t.Error("expected the home page heading in the response")
	}
}

func TestHomeSearchFiltersResults(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/?q=rushwa")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ke-bribery-public-service") {
		t.Error("expected a Kiswahili search for 'rushwa' to surface the bribery guide")
	}
}

func TestHomeDefaultsToKenya(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/")
	if !strings.Contains(rec.Body.String(), "ke-bribery-public-service") {
		t.Error("expected Kenya guides to show by default with no jurisdiction param")
	}
}

func TestHomeSwitchesToUganda(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/?j=UG")
	body := rec.Body.String()
	if !strings.Contains(body, "ug-bribery-public-service") {
		t.Error("expected the Uganda guides when switching jurisdiction")
	}
	if strings.Contains(body, "ke-bribery-public-service") {
		t.Error("expected Kenya guides not to leak into the Uganda view")
	}
}

func TestHomeSwitchesToNigeria(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/?j=NG")
	body := rec.Body.String()
	if !strings.Contains(body, "ng-bribery-public-service") {
		t.Error("expected the Nigeria guide when switching jurisdiction")
	}
	if strings.Contains(body, "ke-bribery-public-service") {
		t.Error("expected Kenya guides not to leak into the Nigeria view")
	}
}

func TestGuideDetailOffersReadAloud(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/guides/ke-police-misconduct")
	body := rec.Body.String()
	if !strings.Contains(body, "readGuideAloud") {
		t.Error("expected the read-aloud script to be present on a guide page")
	}
	if !strings.Contains(body, `data-speak`) {
		t.Error("expected content to be tagged for read-aloud")
	}
}

func TestGuideDetailShowsFrenchWhenRequested(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/guides/ke-bribery-public-service?lang=fr")
	if !strings.Contains(rec.Body.String(), "pot-de-vin") {
		t.Error("expected the French translation to render when lang=fr is requested")
	}
}

func TestGuideDetailShowsDisputedSources(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/guides/ke-police-misconduct")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "sources disagree") {
		t.Error("expected the IPOA disputed-number flag to render on the guide page")
	}
}

func TestGuideDetailUnknownIDIs404(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/guides/does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestReportSubmissionRejectsEmptyContent(t *testing.T) {
	h := newTestApp(t)
	rec := postForm(t, h, "/report", url.Values{
		"category": {"policing"}, "guide_id": {"ke-police-misconduct"}, "content": {""},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty content, got %d", rec.Code)
	}
}

var reportIDPattern = regexp.MustCompile(`rpt_[a-f0-9]+`)
var codePattern = regexp.MustCompile(`font-bold text-\[#028090\]">([a-f0-9]{10})<`)
var casePattern = regexp.MustCompile(`case_[a-f0-9]+`)
var sharePattern = regexp.MustCompile(`<div class="font-semibold text-\[#0A2E36\]">([^<]+)</div>\s*<div class="mt-1 font-mono text-xs break-all bg-slate-100 rounded px-3 py-2">([a-f0-9]+)</div>`)

// TestFullReportToProtectionFlow drives the exact path a real user follows:
// file a report, look it up by its verification code, set up protection,
// and confirm the case is genuinely wired to the report. This is the
// browser-facing equivalent of the CLI's demo-full command.
func TestFullReportToProtectionFlow(t *testing.T) {
	h := newTestApp(t)

	submitRec := postForm(t, h, "/report", url.Values{
		"category": {"policing"}, "guide_id": {"ke-police-misconduct"},
		"content": {"Officer demanded a bribe at a checkpoint."},
	})
	if submitRec.Code != http.StatusOK {
		t.Fatalf("submit: expected 200, got %d", submitRec.Code)
	}
	body := submitRec.Body.String()
	reportID := reportIDPattern.FindString(body)
	codeMatch := codePattern.FindStringSubmatch(body)
	if reportID == "" || len(codeMatch) < 2 {
		t.Fatalf("could not extract report ID or code from response:\n%s", body)
	}
	code := codeMatch[1]

	// The reporter can look their report up later by ID + code, with no
	// identity required.
	statusRec := postForm(t, h, "/report/status", url.Values{
		"report_id": {reportID}, "code": {code},
	})
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status lookup: expected 200, got %d", statusRec.Code)
	}
	// "submitted" is asserted as a complete element text (">submitted<"):
	// the bare word also occurs in the layout footer ("...no government body
	// currently receives what is submitted here"), which renders on every
	// page — including the error page for a wrong code — and made the old
	// loose Contains match a page that had not revealed the report at all.
	// In status_result.html the status renders inside its own span, so the
	// word is bounded by tags there and nowhere in the footer prose.
	if !strings.Contains(statusRec.Body.String(), ">submitted<") {
		t.Error("expected the looked-up report to show submitted status")
	}

	// A wrong code must not succeed.
	wrongRec := postForm(t, h, "/report/status", url.Values{
		"report_id": {reportID}, "code": {"0000000000"},
	})
	if strings.Contains(wrongRec.Body.String(), ">submitted<") {
		t.Error("a fabricated code must not return the report's status")
	}

	// Set up protection. Only the reporter, who holds the verification
	// code, can do this — not anyone who merely knows the report ID (which
	// the zero-auth institution portal displays for every report).
	protectRec := postForm(t, h, "/report/"+reportID+"/protect", url.Values{
		"verification_code": {code},
		"guardian1": {"Lawyer"}, "guardian2": {"Journalist"}, "guardian3": {"Family"},
		"threshold": {"2"}, "interval_hours": {"48"},
	})
	if protectRec.Code != http.StatusOK {
		t.Fatalf("protect: expected 200, got %d", protectRec.Code)
	}
	caseBody := protectRec.Body.String()
	caseID := casePattern.FindString(caseBody)
	if caseID == "" {
		t.Fatalf("could not extract case ID from response:\n%s", caseBody)
	}

	shares := sharePattern.FindAllStringSubmatch(caseBody, -1)
	if len(shares) != 3 {
		t.Fatalf("expected 3 guardian shares on the case-created page, got %d", len(shares))
	}

	// The case dashboard must exist and show Active before any escalation.
	dashRec := get(t, h, "/cases/"+caseID)
	if dashRec.Code != http.StatusOK {
		t.Fatalf("dashboard: expected 200, got %d", dashRec.Code)
	}
	if !strings.Contains(strings.ToLower(dashRec.Body.String()), "active") {
		t.Error("expected a freshly created case to show as active")
	}
}

// TestWebSingleGuardianCannotRelease mirrors the package-level guarantee
// (internal/guardian's TestNoSinglePartyCanReleaseAlone) at the HTTP layer,
// since that invariant is only actually delivered to a user if the web
// handler enforces it too, not just the package underneath it.
func TestWebSingleGuardianCannotRelease(t *testing.T) {
	h := newTestApp(t)

	submitRec := postForm(t, h, "/report", url.Values{
		"category": {"gbv"}, "guide_id": {"ke-gender-based-violence"},
		"content": {"content"},
	})
	reportID := reportIDPattern.FindString(submitRec.Body.String())
	codeMatch := codePattern.FindStringSubmatch(submitRec.Body.String())
	if reportID == "" || len(codeMatch) < 2 {
		t.Fatalf("could not extract report ID or code from response:\n%s", submitRec.Body.String())
	}
	code := codeMatch[1]

	protectRec := postForm(t, h, "/report/"+reportID+"/protect", url.Values{
		"verification_code": {code},
		"guardian1": {"A"}, "guardian2": {"B"}, "guardian3": {"C"},
		"threshold": {"2"}, "interval_hours": {"1"},
	})
	caseBody := protectRec.Body.String()
	caseID := casePattern.FindString(caseBody)
	shares := sharePattern.FindAllStringSubmatch(caseBody, -1)
	shareByName := map[string]string{}
	for _, m := range shares {
		shareByName[m[1]] = m[2]
	}

	escRec := postForm(t, h, "/cases/"+caseID+"/escalate", url.Values{})
	if !strings.Contains(strings.ToLower(escRec.Body.String()), "escalated") {
		t.Fatalf("expected escalation to succeed, got:\n%s", escRec.Body.String())
	}

	oneShareRec := postForm(t, h, "/cases/"+caseID+"/submit-share", url.Values{
		"guardian_id": {"A"}, "share": {shareByName["A"]},
	})
	if strings.Contains(oneShareRec.Body.String(), "Release key reconstructed") {
		t.Fatal("a single guardian's share must never be sufficient to release the key")
	}
	if !strings.Contains(oneShareRec.Body.String(), "Waiting on more guardians") {
		t.Errorf("expected a waiting message below threshold, got:\n%s", oneShareRec.Body.String())
	}

	twoShareRec := postForm(t, h, "/cases/"+caseID+"/submit-share", url.Values{
		"guardian_id": {"B"}, "share": {shareByName["B"]},
	})
	if !strings.Contains(twoShareRec.Body.String(), "Release key reconstructed") {
		t.Errorf("expected the second independent share to cross threshold and release, got:\n%s", twoShareRec.Body.String())
	}
}

func TestCaseDashboardUnknownIDIs404(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/cases/case_does_not_exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestStatusFormRendersWithoutQuery(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/report/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// The tests below verify what a Go test actually can about PWA support:
// that the manifest, icon, and service worker are served correctly, with
// the right content type and scope header, and that the service worker's
// precache list only references guide IDs that really exist. Whether a
// browser actually caches them and serves guides offline is not something
// httptest can exercise — that needs a real browser (see docs/AI_USAGE.md
// for how this was verified manually).

func TestManifestServedCorrectly(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/static/manifest.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"name": "Mlinzi"`) {
		t.Error("expected the manifest to name the app Mlinzi")
	}
}

func TestServiceWorkerServedAtRootScope(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/sw.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// The scope header matters: without it, the service worker would only
	// ever be able to control /static/, never the guide pages it needs to
	// cache for genuine offline access.
	if got := rec.Header().Get("Service-Worker-Allowed"); got != "/" {
		t.Errorf("expected Service-Worker-Allowed: /, got %q", got)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("expected a JavaScript content type, got %q", ct)
	}
}

func TestServiceWorkerPrecachesOnlyRealGuides(t *testing.T) {
	// If the seed dataset ever changes, this test breaks — which is the
	// point: the precache list in sw.js is a hand-maintained static list,
	// not derived from the guide store, so it can silently drift from
	// reality. This test is the tripwire for that drift, and it covers every
	// jurisdiction rather than just the default one, so a country added as
	// pure data cannot end up silently unreachable offline.
	a, err := newApp()
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	swSource, err := staticFS.ReadFile("static/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range a.guides.Jurisdictions() {
		for _, g := range a.guides.ByJurisdiction(j) {
			path := "/guides/" + g.ID
			if !strings.Contains(string(swSource), path) {
				t.Errorf("guide %q exists in the dataset but is not precached in sw.js", g.ID)
			}
		}
	}
}

// TestServiceWorkerPrecachesEveryDocument is the same tripwire for the
// Resources Center: a constitution that only loads with a connection would
// undercut the point of shipping the documents in the binary at all.
func TestServiceWorkerPrecachesEveryDocument(t *testing.T) {
	a, err := newApp()
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	if a.resources.Len() == 0 {
		t.Fatal("expected the Resources Center store to be loaded")
	}
	swSource, err := staticFS.ReadFile("static/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range a.resources.All() {
		path := "/resources/" + d.ID
		if !strings.Contains(string(swSource), path) {
			t.Errorf("document %q exists in the dataset but is not precached in sw.js", d.ID)
		}
	}
}

// --- Resources Center ---

func TestResourcesCenterListsEveryDocument(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/resources")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Resources Center",
		"The Constitution of Kenya, 2010",
		"The Constitution of the Republic of Uganda, 1995",
		"The Constitution of the Federal Republic of Nigeria, 1999",
		"Corrupt Practices and Other Related Offences Act, 2000",
		"kenya-constitution",
		"uganda-constitution",
		"nigeria-constitution",
		"nigeria-icpc-act",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q on the Resources Center index", want)
		}
	}
}

func TestResourceDetailShowsProvisionsWithTheirOwnSources(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/resources/uganda-constitution")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Article 42",
		"Article 225",
		"Wikisource", // the provision's own source, not just the document's
		"ulii.org",   // the custodian's full text
		"Read the full authoritative text",
		"Not legal advice",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q on the Ugandan constitution page", want)
		}
	}
	// The Kenyan document must not bleed into the Ugandan page: the two stand
	// independently even though one page lists both.
	if strings.Contains(body, "National Council for Law Reporting") {
		t.Error("expected the Ugandan page not to show Kenya's custodian or provisions")
	}
}

func TestNigeriaDocumentsRenderTheirOwnCaveats(t *testing.T) {
	h := newTestApp(t)

	// The Nigerian constitution is a partial listing and must say so on its
	// own page, not only in a test.
	constitution := get(t, h, "/resources/nigeria-constitution").Body.String()
	if !strings.Contains(constitution, "NOT the whole Constitution") {
		t.Error("expected the Nigerian constitution page to state its partial coverage")
	}
	if !strings.Contains(constitution, "Section 36") {
		t.Error("expected the fundamental-rights section 36 to be present")
	}

	// The ICPC Act is a statute, sourced to the Commission's own PDF.
	act := get(t, h, "/resources/nigeria-icpc-act").Body.String()
	for _, want := range []string{"Section 64", "Protection of informers", "ICPC-Act-2000.pdf", "statute"} {
		if !strings.Contains(act, want) {
			t.Errorf("expected %q on the ICPC Act page", want)
		}
	}
}

func TestResourceDetailUnknownIDIs404(t *testing.T) {
	h := newTestApp(t)
	if rec := get(t, h, "/resources/does-not-exist"); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestInstitutionListShowsFiledReports(t *testing.T) {
	h := newTestApp(t)
	submitRec := postForm(t, h, "/report", url.Values{
		"category": {"bribery"}, "guide_id": {"ke-bribery-public-service"},
		"content": {"Asked to pay for a birth certificate."},
	})
	reportID := reportIDPattern.FindString(submitRec.Body.String())

	listRec := get(t, h, "/institution")
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRec.Code)
	}
	if !strings.Contains(listRec.Body.String(), reportID) {
		t.Error("expected the newly filed report to appear in the institution list")
	}
}

func TestInstitutionCanAcknowledgeAndResolve(t *testing.T) {
	h := newTestApp(t)
	submitRec := postForm(t, h, "/report", url.Values{
		"category": {"policing"}, "guide_id": {"ke-police-misconduct"}, "content": {"content"},
	})
	reportID := reportIDPattern.FindString(submitRec.Body.String())

	ackRec := postForm(t, h, "/institution/"+reportID+"/advance", url.Values{
		"next_status": {"acknowledged"}, "actor": {"ipoa"}, "note": {"Case opened."},
	})
	if ackRec.Code != http.StatusOK {
		t.Fatalf("acknowledge: expected 200, got %d", ackRec.Code)
	}
	if !strings.Contains(ackRec.Body.String(), "acknowledged") {
		t.Error("expected the report to show as acknowledged after advancing")
	}

	resolveRec := postForm(t, h, "/institution/"+reportID+"/advance", url.Values{
		"next_status": {"resolved"}, "actor": {"ipoa"}, "note": {"Officer disciplined."},
	})
	if !strings.Contains(resolveRec.Body.String(), "resolved") {
		t.Error("expected the report to show as resolved after the second advance")
	}
}

// TestInstitutionCannotSkipAcknowledgement re-verifies, at the HTTP layer,
// the invariant internal/report already tests at the package level: an
// institution cannot jump a report straight from submitted to resolved.
func TestInstitutionCannotSkipAcknowledgement(t *testing.T) {
	h := newTestApp(t)
	submitRec := postForm(t, h, "/report", url.Values{
		"category": {"bribery"}, "guide_id": {"ke-bribery-public-service"}, "content": {"content"},
	})
	reportID := reportIDPattern.FindString(submitRec.Body.String())

	rec := postForm(t, h, "/institution/"+reportID+"/advance", url.Values{
		"next_status": {"resolved"}, "actor": {"eacc"}, "note": {"closing quietly"},
	})
	if !strings.Contains(rec.Body.String(), "cannot move from") {
		t.Errorf("expected the illegal transition to be rejected and shown as an error, got:\n%s", rec.Body.String())
	}
}
