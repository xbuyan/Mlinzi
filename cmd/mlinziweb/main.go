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
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"

	mlinziassets "github.com/xbuyan/mlinzi"
	"github.com/xbuyan/mlinzi/internal/guardian"
	"github.com/xbuyan/mlinzi/internal/guide"
	"github.com/xbuyan/mlinzi/internal/report"
	"github.com/xbuyan/mlinzi/internal/resource"
	"github.com/xbuyan/mlinzi/internal/ui"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// app holds every piece of shared server state. All fields except pages,
// guides, resources and strings are mutated by handlers, so access goes
// through mu.
type app struct {
	guides    *guide.Store
	resources *resource.Store
	pages     map[string]*template.Template
	strings   *ui.Catalog

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
	"institution.html", "resources.html", "resource.html",
}

var templateFuncs = template.FuncMap{
	"inc": func(i int) int { return i + 1 },
}

func newApp() (*app, error) {
	guides, err := guide.Load(mlinziassets.DataFS, "data")
	if err != nil {
		return nil, err
	}

	// A separate store, over a separate folder, in the same binary. Adding a
	// constitution is a new file in resources/, exactly as adding a country is
	// a new folder in data/.
	resources, err := resource.Load(mlinziassets.ResourceFS, "resources")
	if err != nil {
		return nil, err
	}

	// Interface strings, from the catalogue embedded in internal/ui. Loaded
	// and validated like the civic data, and failing the same way: a missing
	// English string is a blank button, which is worse than a refused start.
	strings, err := ui.Load()
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
		resources:     resources,
		pages:         pages,
		strings:       strings,
		reports:       report.NewStore(),
		cases:         guardian.NewStore(),
		reportCase:    make(map[string]string),
		pendingShares: make(map[string][][]byte),
		guardianOrder: make(map[string][]string),
	}, nil
}

// render executes the named page's layout into the response, logging and
// returning a 500 on failure rather than sending a half-written page.
//
// The request is here rather than only the page data because rendering a page
// needs three things the handler should not have to remember to pass: which
// language was asked for, the interface strings resolved into it, and the
// language links that point back at *this* URL. Injecting all three in one
// place is what makes it impossible for a page to render an English shell
// around translated content — the failure this replaced, where chrome was
// hardcoded in the templates and simply stayed English forever.
func (a *app) render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	t, ok := a.pages[name]
	if !ok {
		http.Error(w, "unknown page: "+name, http.StatusInternalServerError)
		return
	}

	lang := langFrom(r)
	// An unsupported language code renders in English like anything else, so
	// the page must say it is English too. Declaring lang="pt" over English
	// text makes a screen reader read the page with the wrong pronunciation
	// rules — the same silent-English problem, in the one place it would be
	// hardest for a reader to notice and correct for themselves.
	if !hasLang(a.strings.Langs(), lang) {
		lang = "en"
	}
	page, ok := data.(map[string]any)
	if !ok {
		page = map[string]any{}
	}
	page["Lang"] = lang
	page["T"] = a.strings.Resolve(lang)
	page["LangLinks"] = languageLinks(r, a.strings, lang)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, "layout", page); err != nil {
		log.Printf("template %s: %v", name, err)
	}
}

// languageLink is one entry in the language switcher.
type languageLink struct {
	Code    string
	Name    string
	URL     string
	Current bool
}

// langNames are endonyms — each language's name in its own language, which is
// what a switcher should show. Adding a language to strings.json is a data
// change; adding its own name here is the one-line cosmetic step that belongs
// with it, and a language with no entry still works, falling back to its code.
var langNames = map[string]string{
	"en": "English",
	"sw": "Kiswahili",
	"fr": "Français",
}

// languageLinks builds one link per available language, each pointing at the
// current URL with only ?lang= changed.
//
// Links rather than the form-and-select this replaced, for two reasons. The
// form submitted to action="", which replaces the entire query string: on the
// home page, changing language silently threw away the chosen country and the
// search term. And a plain link needs no JavaScript, so the switcher keeps
// working offline and on the most basic browser — which is the same reason
// every other control in this UI is a link or a plain form.
func languageLinks(r *http.Request, cat *ui.Catalog, current string) []languageLink {
	var out []languageLink
	for _, code := range cat.Langs() {
		q := r.URL.Query()
		q.Set("lang", code)
		name, ok := langNames[code]
		if !ok {
			name = code
		}
		out = append(out, languageLink{
			Code:    code,
			Name:    name,
			URL:     r.URL.Path + "?" + q.Encode(),
			Current: code == current,
		})
	}
	return out
}

// langFrom reads the requested language, defaulting to English.
//
// It checks the query string first and then the submitted form, because every
// form in this app carries the language as a hidden field and the result of a
// form submission is rendered on a POST. Reading only ?lang= meant the
// language survived every link but was dropped by every button: filing a
// report, checking its status, or acting on a case all came back in English
// however the person had set the language. The hidden field was already being
// sent; nothing was reading it.
func langFrom(r *http.Request) string {
	l := r.URL.Query().Get("lang")
	if l == "" {
		l = r.FormValue("lang")
	}
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
	mux.HandleFunc("GET /resources", a.handleResources)
	mux.HandleFunc("GET /resources/{id}", a.handleResourceDetail)
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

	// Serving static assets under /static/ is straightforward via
	// http.FileServerFS. The service worker is the one exception: a service
	// worker's scope defaults to the directory it's served from, and it
	// needs to control the whole site (including "/" and "/guides/...") to
	// cache them — not just "/static/" — so it's served at the root path
	// explicitly rather than under the static prefix.
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("static assets: %v", err)
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
