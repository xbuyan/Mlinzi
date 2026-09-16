// Command mlinziweb serves the Mlinzi web UI: the same guide, report, and
// guardian packages used by the CLI, wrapped in a thin HTTP layer.
//
// There is no persistence layer yet — that is a known, stated gap (see
// README and docs/AI_USAGE.md), not an oversight. Every report and case
// lives in memory for the lifetime of the process. This is a proof of
// concept demonstrating the mechanism, not a production deployment.
package main

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"sync"

	mlinziassets "github.com/xbuyan/mlinzi"
	"github.com/xbuyan/mlinzi/internal/guardian"
	"github.com/xbuyan/mlinzi/internal/guide"
	"github.com/xbuyan/mlinzi/internal/report"
)

//go:embed templates/*.html
var templateFS embed.FS

// app holds every piece of shared server state. All fields except pages and
// guides are mutated by handlers, so access goes through mu.
type app struct {
	guides *guide.Store
	pages  map[string]*template.Template

	mu            sync.Mutex
	reports       *report.Store
	cases         *guardian.Store
	reportCase    map[string]string   // report ID -> case ID
	pendingShares map[string][][]byte // case ID -> shares, deleted once shown
	guardianOrder map[string][]string // case ID -> guardian IDs in share order
}

// pageNames lists every content template that gets paired with layout.html.
// html/template shares one namespace of named templates across every file
// parsed together, so if every page file defined "content" in a single
// shared *template.Template, the last one parsed would silently win for
// every page. Pairing layout.html with exactly one page file at a time
// avoids that collision entirely.
var pageNames = []string{
	"home.html", "guide.html", "report_new.html", "report_result.html",
	"case_created.html", "case.html", "status_form.html", "status_result.html",
	"institution.html",
}

var templateFuncs = template.FuncMap{
	"inc": func(i int) int { return i + 1 },
}

func newApp() (*app, error) {
	guides, err := guide.Load(mlinziassets.DataFS, "data")
	if err != nil {
		return nil, err
	}

	pages := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		t, err := template.New("layout.html").Funcs(templateFuncs).ParseFS(
			templateFS, "templates/layout.html", "templates/"+name)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		pages[name] = t
	}

	return &app{
		guides:        guides,
		pages:         pages,
		reports:       report.NewStore(),
		cases:         guardian.NewStore(),
		reportCase:    make(map[string]string),
		pendingShares: make(map[string][][]byte),
		guardianOrder: make(map[string][]string),
	}, nil
}

// render executes the named page's layout into the response, logging and
// returning a 500 on failure rather than sending a half-written page.
func (a *app) render(w http.ResponseWriter, status int, name string, data any) {
	t, ok := a.pages[name]
	if !ok {
		http.Error(w, "unknown page: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("template %s: %v", name, err)
	}
}

// langFrom reads the ?lang= query parameter, defaulting to English.
func langFrom(r *http.Request) string {
	l := r.URL.Query().Get("lang")
	if l == "" {
		return "en"
	}
	return l
}

func main() {
	a, err := newApp()
	if err != nil {
		log.Fatalf("failed to start: %v", err)
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
	mux.HandleFunc("GET /institution", a.handleInstitutionList)
	mux.HandleFunc("POST /institution/{id}/advance", a.handleInstitutionAdvance)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("Mlinzi web listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
