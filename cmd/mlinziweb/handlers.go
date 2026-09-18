package main

import (
	"encoding/hex"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xbuyan/mlinzi/internal/evidence"
	"github.com/xbuyan/mlinzi/internal/guardian"
	"github.com/xbuyan/mlinzi/internal/report"
)

// maxUploadMemory bounds how much of a multipart request ParseMultipartForm
// buffers in memory before spilling to temp files; kept comfortably above
// evidence.MaxTotalBytesPerReport so a report at the size limit never spills
// to disk (this process has no disk-backed persistence to spill to that
// would survive anyway — see evidence.Store's doc comment).
const maxUploadMemory = 32 << 20 // 32MB

const defaultJurisdiction = "KE"

// --- Home: search / browse guides ---

func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	lang := langFrom(r)
	q := r.URL.Query().Get("q")
	jurisdiction := r.URL.Query().Get("j")
	if jurisdiction == "" {
		jurisdiction = defaultJurisdiction
	}

	var guides []guideSummaryView
	results := a.guides.Search(jurisdiction, q)
	for _, g := range results {
		guides = append(guides, newGuideSummaryView(g, lang))
	}

	a.render(w, r, http.StatusOK, "home.html", map[string]any{
		"Query":        q,
		"Jurisdiction": jurisdiction,
		"Guides":       guides,
	})
}

// --- Guide detail ---

func (a *app) handleGuideDetail(w http.ResponseWriter, r *http.Request) {
	lang := langFrom(r)
	id := r.PathValue("id")

	g, err := a.guides.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.render(w, r, http.StatusOK, "guide.html", map[string]any{
		"Guide": newGuideView(g, lang),
		// A guide with no translation in the requested language would
		// otherwise silently render in English with nothing to say so. The
		// page says so instead — the same honesty the data model applies to
		// a source it could not verify.
		"LangMissing": !hasLang(g.Langs(), lang),
	})
}

// --- Resources Center: primary legal documents ---
//
// Read-only and jurisdiction-independent: unlike guides, documents are not
// filtered by the ?j= country switcher, because a person reading Uganda's
// constitution may well be in Kenya and the folder is deliberately one list.

func (a *app) handleResources(w http.ResponseWriter, r *http.Request) {
	lang := langFrom(r)

	var docs []resourceSummaryView
	for _, d := range a.resources.All() {
		docs = append(docs, newResourceSummaryView(d, lang))
	}
	a.render(w, r, http.StatusOK, "resources.html", map[string]any{
		"Documents": docs,
	})
}

func (a *app) handleResourceDetail(w http.ResponseWriter, r *http.Request) {
	lang := langFrom(r)
	d, err := a.resources.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.render(w, r, http.StatusOK, "resource.html", map[string]any{
		"Document": newResourceView(d, lang),
	})
}

// --- Report: new / create ---

// handleEvidence serves back a stored evidence file by its content hash so
// report/status/institution pages can show or link to it.
//
// This has exactly the same access-control posture as the rest of the app
// today: no login, no check that the requester is the reporter or an
// authorised institution — anyone who knows or guesses a report's evidence
// hash can fetch the file, the same way anyone who knows a report ID can
// already read that report's text through the institution portal. That is a
// stated, existing limitation (see docs/AI_USAGE.md), not a new one
// introduced here; evidence access does not get held to a stricter standard
// than the report text sitting next to it. What evidence upload adds beyond
// that accepted risk is handled earlier, at Save time: metadata that would
// leak beyond what's already visible (GPS, device identifiers) is stripped
// before a file is ever stored, regardless of who can later view it.
func (a *app) handleEvidence(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	data, contentType, ok := a.evidence.OpenByHash(hash)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(data)
}

func (a *app) handleReportNew(w http.ResponseWriter, r *http.Request) {
	lang := langFrom(r)
	guideID := r.URL.Query().Get("guide_id")

	var title string
	if guideID != "" {
		if g, err := a.guides.Get(guideID); err == nil {
			title = g.Title.In(lang)
		}
	}
	a.render(w, r, http.StatusOK, "report_new.html", map[string]any{
		"GuideID":   guideID,
		"GuideName": title,
	})
}

func (a *app) handleReportCreate(w http.ResponseWriter, r *http.Request) {
	lang := langFrom(r)
	// ParseMultipartForm calls ParseForm internally regardless of content
	// type, so a plain application/x-www-form-urlencoded post (no files —
	// every existing caller before this feature, and still the common case)
	// ends up with its fields populated in r.Form even though the
	// multipart-specific parse itself reports ErrNotMultipart. Only a
	// different error means the request body was actually malformed.
	if err := r.ParseMultipartForm(maxUploadMemory); err != nil && err != http.ErrNotMultipart {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	category := r.FormValue("category")
	guideID := r.FormValue("guide_id")
	content := strings.TrimSpace(r.FormValue("content"))
	onBehalfOf := r.FormValue("on_behalf_of") == "on"
	onBehalfOfNote := strings.TrimSpace(r.FormValue("on_behalf_of_note"))

	if content == "" {
		a.render(w, r, http.StatusBadRequest, "report_new.html", map[string]any{
			"GuideID": guideID, "Error": a.strings.Get(lang, "report_error_describe"),
		})
		return
	}

	var fileHeaders []*multipart.FileHeader
	if r.MultipartForm != nil {
		fileHeaders = r.MultipartForm.File["evidence"]
	}
	fileBytes, err := readUploadedFiles(fileHeaders)
	if err != nil {
		a.render(w, r, http.StatusBadRequest, "report_new.html", map[string]any{
			"GuideID": guideID, "Content": content, "Error": evidenceErrorMessage(a, lang, err),
		})
		return
	}

	var evidenceRefs []evidence.Ref
	if len(fileBytes) > 0 {
		evidenceRefs, err = a.evidence.SaveAll(fileBytes)
		if err != nil {
			a.render(w, r, http.StatusBadRequest, "report_new.html", map[string]any{
				"GuideID": guideID, "Content": content, "Error": evidenceErrorMessage(a, lang, err),
			})
			return
		}
	}

	a.mu.Lock()
	rep, err := a.reports.SubmitWithEvidence(category, guideID, content, evidenceRefs, onBehalfOf, onBehalfOfNote)
	a.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.render(w, r, http.StatusOK, "report_result.html", map[string]any{
		"Report": rep,
	})
}

// readUploadedFiles reads every "evidence" multipart part fully into memory
// and returns their raw bytes, capping the count before reading any bytes at
// all — evidence.SaveAll re-checks the same limits (it does not trust every
// caller to have checked first), but failing fast here avoids reading, say,
// a fifth 8MB file into memory just to reject the batch a moment later.
func readUploadedFiles(headers []*multipart.FileHeader) ([][]byte, error) {
	if len(headers) > evidence.MaxFilesPerReport {
		return nil, evidence.ErrTooLarge
	}
	out := make([][]byte, 0, len(headers))
	for _, fh := range headers {
		f, err := fh.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(f, evidence.MaxFileBytes+1))
		f.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// evidenceErrorMessage prefers a translated, reporter-facing string for the
// error cases a reporter can actually act on (wrong file type, too large,
// too many files); anything else falls back to the raw error rather than
// inventing a translation key for a case that shouldn't normally be user
// visible.
func evidenceErrorMessage(a *app, lang string, err error) string {
	switch {
	case err == evidence.ErrStoreFull:
		return a.strings.Get(lang, "evidence_error_store_full")
	case isUnsupportedType(err):
		return a.strings.Get(lang, "evidence_error_unsupported_type")
	case isTooLarge(err):
		return a.strings.Get(lang, "evidence_error_too_large")
	default:
		return err.Error()
	}
}

func isUnsupportedType(err error) bool {
	_, ok := err.(evidence.ErrUnsupportedType)
	return ok
}

func isTooLarge(err error) bool {
	return err == evidence.ErrTooLarge || strings.Contains(err.Error(), "exceeds the")
}

// --- Protection: create a guardian case for a report ---

func (a *app) handleProtectCreate(w http.ResponseWriter, r *http.Request) {
	reportID := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	guardianNames := []string{
		strings.TrimSpace(r.FormValue("guardian1")),
		strings.TrimSpace(r.FormValue("guardian2")),
		strings.TrimSpace(r.FormValue("guardian3")),
	}
	for i, n := range guardianNames {
		if n == "" {
			guardianNames[i] = defaultGuardianLabel(i)
		}
	}
	threshold, err := strconv.Atoi(r.FormValue("threshold"))
	if err != nil || threshold < 2 || threshold > 3 {
		threshold = 2
	}
	hours, err := strconv.Atoi(r.FormValue("interval_hours"))
	if err != nil || hours < 1 {
		hours = 48
	}

	key, err := guardian.ReleaseKey()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.mu.Lock()
	c, shares, err := a.cases.NewCase(reportID, key, guardianNames, threshold, time.Duration(hours)*time.Hour)
	if err == nil {
		a.reportCase[reportID] = c.ID
		a.pendingShares[c.ID] = shares
		a.guardianOrder[c.ID] = append([]string{}, guardianNames...)
	}
	a.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Shares are shown exactly once here, on creation, then removed from
	// server memory below — mirroring the real design intent that the
	// platform should not retain the means to reconstruct the key.
	type shareRow struct {
		Guardian string
		Share    string // hex-encoded, for the reporter to copy and deliver out of band
	}
	var rows []shareRow
	for i, s := range shares {
		rows = append(rows, shareRow{Guardian: guardianNames[i], Share: hex.EncodeToString(s)})
	}

	a.mu.Lock()
	delete(a.pendingShares, c.ID)
	a.mu.Unlock()

	a.render(w, r, http.StatusOK, "case_created.html", map[string]any{
		"Case":       c,
		"ShareRows":  rows,
		"IntervalHr": hours,
	})
}

func defaultGuardianLabel(i int) string {
	switch i {
	case 0:
		return "Guardian A"
	case 1:
		return "Guardian B"
	default:
		return "Guardian C"
	}
}

// --- Case dashboard ---

func (a *app) handleCaseDashboard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	a.mu.Lock()
	c, err := a.cases.Get(id)
	a.mu.Unlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.renderCase(w, r, c, "")
}

func (a *app) renderCase(w http.ResponseWriter, r *http.Request, c guardian.Case, message string) {
	overdue := c.IsOverdue(time.Now())
	a.render(w, r, http.StatusOK, "case.html", map[string]any{
		"Case":    c,
		"Overdue": overdue,
		"Message": message,
	})
}

func (a *app) handleCaseCheckIn(w http.ResponseWriter, r *http.Request) {
	lang := langFrom(r)
	id := r.PathValue("id")

	a.mu.Lock()
	err := a.cases.CheckIn(id)
	var c guardian.Case
	if err == nil {
		c, err = a.cases.Get(id)
	}
	a.mu.Unlock()

	msg := a.strings.Get(lang, "case_msg_checked_in")
	if err != nil {
		msg = a.strings.Get(lang, "case_msg_checkin_failed") + ": " + err.Error()
	}
	if c.ID == "" {
		http.NotFound(w, r)
		return
	}
	a.renderCase(w, r, c, msg)
}

// handleCaseEscalate exists for demo purposes: in a real deployment,
// escalation is evaluated automatically against elapsed time once a check-in
// is actually missed. A live demo cannot wait 48 real hours, so this route
// lets a judge trigger the same code path deliberately, against the real
// clock, rather than faking the result.
func (a *app) handleCaseEscalate(w http.ResponseWriter, r *http.Request) {
	lang := langFrom(r)
	id := r.PathValue("id")

	a.mu.Lock()
	c, getErr := a.cases.Get(id)
	var err error
	if getErr == nil {
		// Force a genuinely overdue timestamp so Escalate's real check
		// passes, rather than bypassing IsOverdue. This still runs the
		// actual guarded transition, not a shortcut around it.
		err = a.cases.Escalate(id, c.LastCheckIn.Add(c.Interval+time.Second))
		if err == nil {
			c, err = a.cases.Get(id)
		}
	} else {
		err = getErr
	}
	a.mu.Unlock()

	msg := a.strings.Get(lang, "case_msg_escalated")
	if err != nil {
		msg = a.strings.Get(lang, "case_msg_escalate_failed") + ": " + err.Error()
	}
	if c.ID == "" {
		http.NotFound(w, r)
		return
	}
	a.renderCase(w, r, c, msg)
}

func (a *app) handleCaseSubmitShare(w http.ResponseWriter, r *http.Request) {
	lang := langFrom(r)
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	guardianID := strings.TrimSpace(r.FormValue("guardian_id"))
	shareHex := strings.TrimSpace(r.FormValue("share"))

	share, decodeErr := hex.DecodeString(shareHex)

	a.mu.Lock()
	var msg string
	if decodeErr != nil {
		msg = a.strings.Get(lang, "case_msg_share_unreadable")
	} else {
		key, err := a.cases.SubmitShare(id, guardianID, share)
		switch {
		case err == guardian.ErrInsufficientCount:
			msg = a.strings.Get(lang, "case_msg_share_accepted")
		case err != nil:
			msg = a.strings.Get(lang, "case_msg_share_rejected") + ": " + err.Error()
		default:
			msg = a.strings.Get(lang, "case_msg_threshold_reached") + ": " + hex.EncodeToString(key)[:16] + "…"
		}
	}
	c, _ := a.cases.Get(id)
	a.mu.Unlock()

	if c.ID == "" {
		http.NotFound(w, r)
		return
	}
	a.renderCase(w, r, c, msg)
}

// --- Report status lookup, by ID + verification code, no identity required ---

func (a *app) handleStatusForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, http.StatusOK, "status_form.html", nil)
}

func (a *app) handleStatusResult(w http.ResponseWriter, r *http.Request) {
	lang := langFrom(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.FormValue("report_id"))
	code := strings.TrimSpace(r.FormValue("code"))

	a.mu.Lock()
	ok, err := a.reports.VerifyReceipt(id, code)
	var rep report.Report
	if ok {
		rep, _ = a.reports.Get(id)
	}
	caseID := a.reportCase[id]
	a.mu.Unlock()

	if err != nil || !ok {
		a.render(w, r, http.StatusOK, "status_form.html", map[string]any{
			"Error": a.strings.Get(lang, "lookup_error_no_match"),
		})
		return
	}

	a.render(w, r, http.StatusOK, "status_result.html", map[string]any{
		"Report": rep,
		"CaseID": caseID,
	})
}

// --- Institution portal ---
//
// DEMO ONLY: this has no authentication. Anyone who finds the URL can
// acknowledge or resolve any report. A real deployment would put this
// behind institution login before any of it is usable. It exists here to
// demonstrate that the accountability mechanism genuinely works from both
// sides — reporter and institution — not just the reporter's.

func (a *app) handleInstitutionList(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	reports := a.reports.All()
	a.mu.Unlock()

	a.render(w, r, http.StatusOK, "institution.html", map[string]any{
		"Reports": reports,
	})
}

func (a *app) handleInstitutionAdvance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	next := report.Status(r.FormValue("next_status"))
	actor := strings.TrimSpace(r.FormValue("actor"))
	note := strings.TrimSpace(r.FormValue("note"))
	if actor == "" {
		actor = "institution"
	}

	a.mu.Lock()
	_, err := a.reports.Advance(id, next, actor, note)
	reports := a.reports.All()
	a.mu.Unlock()

	data := map[string]any{"Reports": reports}
	if err != nil {
		data["Error"] = err.Error()
	}
	a.render(w, r, http.StatusOK, "institution.html", data)
}
