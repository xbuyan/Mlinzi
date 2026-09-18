package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// postMultipart builds and sends a multipart/form-data POST the way a real
// browser submitting report_new.html would: ordinary fields plus zero or
// more files under the "evidence" field name.
func postMultipart(t *testing.T, h http.Handler, path string, fields map[string]string, files map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	for fieldName, testdataPath := range files {
		data, err := os.ReadFile(testdataPath)
		if err != nil {
			t.Fatalf("read fixture %s: %v", testdataPath, err)
		}
		part, err := w.CreateFormFile("evidence", fieldName)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatalf("write file part: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestReportWithEvidenceEndToEnd drives the exact path a reporter attaching
// a phone photo follows: submit, get the evidence link back on the result
// page, fetch it, and confirm what comes back is genuinely stripped of the
// GPS and device data the original file carried — not just that the upload
// was "accepted".
func TestReportWithEvidenceEndToEnd(t *testing.T) {
	h := newTestApp(t)

	rec := postMultipart(t, h, "/report",
		map[string]string{
			"category": "policing", "guide_id": "ke-police-misconduct",
			"content": "Officer demanded a bribe and I have a photo of the checkpoint.",
		},
		map[string]string{"checkpoint.jpg": "testdata/orientation6_with_gps.jpg"},
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/evidence/") {
		t.Fatal("expected the result page to link to the stored evidence file")
	}

	hash := extractEvidenceHash(t, body)
	fetchRec := get(t, h, "/evidence/"+hash)
	if fetchRec.Code != http.StatusOK {
		t.Fatalf("expected 200 fetching stored evidence, got %d", fetchRec.Code)
	}
	if got := fetchRec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", got)
	}
	stored := fetchRec.Body.Bytes()
	if bytes.Contains(stored, []byte("Exif")) {
		t.Error("evidence served back through the HTTP layer still contains an Exif marker")
	}
	if bytes.Contains(stored, []byte("ModelX-Identifying-Device-Serial-12345")) {
		t.Error("evidence served back through the HTTP layer still contains the device identifier")
	}
}

func extractEvidenceHash(t *testing.T, body string) string {
	t.Helper()
	const marker = "/evidence/"
	i := strings.Index(body, marker)
	if i == -1 {
		t.Fatal("no /evidence/ link found in response body")
	}
	rest := body[i+len(marker):]
	end := strings.IndexAny(rest, `"'`)
	if end == -1 {
		t.Fatal("could not find end of evidence hash in response body")
	}
	return rest[:end]
}

// TestReportRejectsUnsupportedEvidenceType proves a disallowed file type
// does not silently get dropped or silently stored — the whole submission
// is rejected with a message the reporter can act on, and no report is
// created for it (evidence is all-or-nothing with the rest of the report,
// per the evidence.SaveAll contract).
func TestReportRejectsUnsupportedEvidenceType(t *testing.T) {
	h := newTestApp(t)
	rec := postMultipart(t, h, "/report",
		map[string]string{"category": "policing", "guide_id": "ke-police-misconduct", "content": "content"},
		map[string]string{"not-a-real-file.jpg": "testdata/garbage.bin"},
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unsupported file type, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "rpt_") {
		t.Error("a report ID appears in the response — a rejected upload must not still create a report")
	}
}

// TestReportTooManyEvidenceFilesRejected checks the per-report file-count
// limit is enforced at the HTTP layer, not only inside the evidence
// package's own unit tests.
func TestReportTooManyEvidenceFilesRejected(t *testing.T) {
	h := newTestApp(t)
	files := map[string]string{}
	for i := 0; i < 5; i++ { // evidence.MaxFilesPerReport is 4
		files[string(rune('a'+i))+".jpg"] = "testdata/orientation6_with_gps.jpg"
	}
	rec := postMultipart(t, h, "/report",
		map[string]string{"category": "policing", "guide_id": "ke-police-misconduct", "content": "content"},
		files,
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for exceeding the per-report file count, got %d", rec.Code)
	}
}

// TestReportOnBehalfOfFlagRecorded proves the proxy-submission flag and its
// note survive the full round trip — submitted, stored, and visible again
// on the result page — since this is the mechanism a family member or
// paralegal filing for someone with no device access actually depends on.
func TestReportOnBehalfOfFlagRecorded(t *testing.T) {
	h := newTestApp(t)
	rec := postMultipart(t, h, "/report",
		map[string]string{
			"category": "detention", "guide_id": "",
			"content":           "Held for three weeks with no charge and no lawyer allowed to visit.",
			"on_behalf_of":      "on",
			"on_behalf_of_note": "His sister",
		},
		nil,
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "His sister") {
		t.Error("expected the on-behalf-of note to appear on the result page")
	}
}

// TestReportWithoutEvidenceStillWorks guards the backward-compatible path:
// a plain application/x-www-form-urlencoded submission with no files, which
// is what every report submission looked like before this feature and is
// still by far the common case.
func TestReportWithoutEvidenceStillWorks(t *testing.T) {
	h := newTestApp(t)
	rec := postForm(t, h, "/report", url.Values{
		"category": {"policing"}, "guide_id": {"ke-police-misconduct"}, "content": {"No photo, just text."},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestEvidenceRouteReturns404ForUnknownHash(t *testing.T) {
	h := newTestApp(t)
	rec := get(t, h, "/evidence/0000000000000000000000000000000000000000000000000000000000000000")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown evidence hash, got %d", rec.Code)
	}
}
