package main

import (
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/xbuyan/mlinzi/internal/guide"
	"github.com/xbuyan/mlinzi/internal/rag"
)

// --- Ask: RAG question answering over the sourced corpus ---
//
// The whole feature exists under one constraint: an answer without its
// source is exactly what Mlinzi is against. So this page cannot produce an
// uncited claim. The retrieval runs over the same guide and provision data
// the guide and resource pages render, every answer names the guide or
// article it came from with the source URL it was checked against, and a
// question the corpus cannot support is answered with an explicit refusal
// rather than a confident guess.

// sourceView is already defined in views.go for guides and documents; the
// ask page reuses it, so provenance renders identically everywhere.

// askSourceView pairs a retrieved chunk with the sources to render for it.
type askSourceView struct {
	DocID        string
	Ref          string
	Section      string
	Kind         string
	Jurisdiction string
	Link         string
	Sources      []sourceView
	Disputed     bool
}

// handleAskForm renders the empty ask page. GET, so it is precacheable,
// linkable, and works like every other read path in this app.
func (a *app) handleAskForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, http.StatusOK, "ask.html", map[string]any{})
}

// handleAsk runs the pipeline. The question is posted (it is a write-shaped
// action — it costs server work — and POST keeps it out of precache and
// browser prefetch), the answer is rendered with every chunk it was built
// from and that chunk's sources.
func (a *app) handleAsk(w http.ResponseWriter, r *http.Request) {
	lang := langFrom(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	query := strings.TrimSpace(r.FormValue("q"))
	if query == "" {
		a.render(w, r, http.StatusBadRequest, "ask.html", map[string]any{
			"Error": a.strings.Get(lang, "ask_error_empty"),
		})
		return
	}
	// This endpoint is unauthenticated, same posture as /institution and
	// /evidence — but unlike those, an unbounded query here becomes
	// unbounded work (retrieval scoring cost) and, if RAG_LLM_ENDPOINT is
	// configured, unbounded third-party API spend on a key nobody else can
	// see. A generous cap still allows any real civic question through
	// while making a paste-the-whole-book request a normal error instead
	// of a normal cost.
	const maxQueryRunes = 500
	if utf8.RuneCountInString(query) > maxQueryRunes {
		a.render(w, r, http.StatusBadRequest, "ask.html", map[string]any{
			"Error": a.strings.Get(lang, "ask_error_too_long"),
		})
		return
	}

	builder := a.askBuilder(lang)
	ans := rag.Ask(r.Context(), http.DefaultClient, builder, query, "", lang)

	var srcs []askSourceView
	for _, sc := range ans.Chunks {
		sv := askSourceView{
			DocID:        sc.DocID,
			Ref:          sc.Ref,
			Section:      sc.Section,
			Kind:         string(sc.Kind),
			Jurisdiction: sc.Jurisdiction,
			Sources:      sourceViews(toGuideSources(sc.Sources)),
			Disputed:     sc.Disputed,
		}
		switch sc.Kind {
		case rag.KindGuide:
			sv.Link = "/guides/" + sc.DocID + "?lang=" + lang
		case rag.KindProvision:
			sv.Link = "/resources/" + sc.DocID + "?lang=" + lang
		}
		srcs = append(srcs, sv)
	}

	a.render(w, r, http.StatusOK, "ask.html", map[string]any{
		"Query":    query,
		"Answer":   ans.Text,
		"Grounded": ans.Grounded,
		// The page states how the answer was phrased — model-written against
		// the sourced passages, or composed directly from them — rather than
		// leaving the reader to assume one or the other.
		"MethodLLM": ans.Method == rag.MethodLLM,
		"Sources":   srcs,
	})
}

// askBuilder picks the index for the requested language, falling back to
// English the same way every other language resolution in the app does.
func (a *app) askBuilder(lang string) *rag.Builder {
	if b, ok := a.ask[lang]; ok {
		return b
	}
	return a.ask["en"]
}

// snapshotHook wraps a handler so durable state is written after any
// mutating request. Snapshots are atomic per file, and a failed snapshot is
// logged loudly but does not fail the user's request: the request already
// committed to the in-memory ledger, and reporting an error after the fact
// would leave the user unsure whether their report exists — it does, and
// the next successful snapshot will carry it.
func (a *app) snapshotHook(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(w, r)
		if a.dataDir == "" {
			return
		}
		a.mu.Lock()
		err := snapshotAll(a.dataDir, a.reports, a.evidence, a.cases)
		a.mu.Unlock()
		if err != nil {
			log.Printf("persistence: snapshot failed (state remains in memory): %v", err)
		}
	}
}

// toGuideSources converts the rag layer's flattened source refs back into
// the guide.Source shape sourceViews renders. An answer's provenance should
// look identical to a guide's — same fields, same rendering — because a
// citation is a citation wherever it appears.
func toGuideSources(refs []rag.SourceRef) []guide.Source {
	out := make([]guide.Source, 0, len(refs))
	for _, r := range refs {
		out = append(out, guide.Source{
			Publisher:  r.Publisher,
			URL:        r.URL,
			Confidence: guide.Confidence(r.Confidence),
		})
	}
	return out
}
