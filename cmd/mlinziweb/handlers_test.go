package main

import (
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
	a, err := newApp()
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.handleHome)
	mux.HandleFunc("GET /guides/{id}", a.handleGuideDetail)
	mux.HandleFunc("GET /report/new", a.handleReportNew)
	mux.HandleFunc("POST /report", a.handleReportCreate)
	mux.HandleFunc("GET /report/status", a.handleStatusForm)
	mux.HandleFunc("POST /report/status", a.handleStatusResult)
	mux.HandleFunc("POST /report/{id}/protect", a.handleProtectCreate)
	mux.HandleFunc("GET /cases/{id}", a.handleCaseDashboard)
	mux.HandleFunc("POST /cases/{id}/checkin", a.handleCaseCheckIn)
	mux.HandleFunc("POST /cases/{id}/escalate", a.handleCaseEscalate)
	mux.HandleFunc("POST /cases/{id}/submit-share", a.handleCaseSubmitShare)
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
	if !strings.Contains(strings.ToLower(statusRec.Body.String()), "submitted") {
		t.Error("expected the looked-up report to show submitted status")
	}

	// A wrong code must not succeed.
	wrongRec := postForm(t, h, "/report/status", url.Values{
		"report_id": {reportID}, "code": {"0000000000"},
	})
	if strings.Contains(strings.ToLower(wrongRec.Body.String()), "submitted") {
		t.Error("a fabricated code must not return the report's status")
	}

	// Set up protection.
	protectRec := postForm(t, h, "/report/"+reportID+"/protect", url.Values{
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

	protectRec := postForm(t, h, "/report/"+reportID+"/protect", url.Values{
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
