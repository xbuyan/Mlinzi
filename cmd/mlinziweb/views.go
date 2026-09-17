package main

import (
	"strings"
	"time"

	"github.com/xbuyan/mlinzi/internal/guide"
	"github.com/xbuyan/mlinzi/internal/resource"
)

// This file converts domain types into flat, language-resolved view structs.
// Templates never call .In(lang) themselves — resolving language happens
// once, here, in Go, so templates stay simple and every page renders the
// same language consistently without threading a lang parameter through
// every template call.

type channelView struct {
	Kind         string
	Value        string
	TollFree     bool
	Anonymous    bool
	LowBandwidth bool
	Disputed     bool
	Note         string
}

type institutionView struct {
	Name     string
	Role     string
	Channels []channelView
}

type stepView struct {
	Action   string
	Detail   string
	Deadline string
	Critical bool
}

type sourceView struct {
	Publisher  string
	URL        string
	Confidence string
}

type guideView struct {
	ID           string
	Title        string
	Summary      string
	Rights       []string
	Evidence     []string
	Steps        []stepView
	Institutions []institutionView
	Timeline     string
	Sources      []sourceView
	LastVerified string
	Stale        bool
	Langs        []string
}

// sourceViews flattens sources the same way everywhere they are shown, so a
// claimed source looks identical whether it backs a guide's hotline or a
// constitution's article.
func sourceViews(srcs []guide.Source) []sourceView {
	var out []sourceView
	for _, s := range srcs {
		out = append(out, sourceView{
			Publisher:  s.Publisher,
			URL:        s.URL,
			Confidence: string(s.Confidence),
		})
	}
	return out
}

func newGuideView(g guide.Guide, lang string) guideView {
	v := guideView{
		ID:           g.ID,
		Title:        g.Title.In(lang),
		Summary:      g.Summary.In(lang),
		Timeline:     g.Timeline.In(lang),
		LastVerified: g.LastVerified,
		Stale:        g.StaleAfter(90*24*time.Hour, time.Now()),
		Langs:        g.Langs(),
	}
	for _, r := range g.Rights {
		v.Rights = append(v.Rights, r.In(lang))
	}
	for _, e := range g.Evidence {
		v.Evidence = append(v.Evidence, e.In(lang))
	}
	for _, st := range g.Steps {
		v.Steps = append(v.Steps, stepView{
			Action:   st.Action.In(lang),
			Detail:   st.Detail.In(lang),
			Deadline: st.Deadline.In(lang),
			Critical: st.Critical,
		})
	}
	for _, inst := range g.Institutions {
		iv := institutionView{Name: inst.Name, Role: inst.Role.In(lang)}
		for _, c := range inst.Channels {
			iv.Channels = append(iv.Channels, channelView{
				Kind:         string(c.Kind),
				Value:        c.Value,
				TollFree:     c.TollFree,
				Anonymous:    c.Anonymous,
				LowBandwidth: c.Kind.LowBandwidth(),
				Disputed:     c.Disputed(),
				Note:         c.Note.In(lang),
			})
		}
		v.Institutions = append(v.Institutions, iv)
	}
	v.Sources = sourceViews(g.Sources)
	return v
}

// guideSummaryView is the trimmed-down view used in list/search results.
type guideSummaryView struct {
	ID       string
	Title    string
	Summary  string
	Category string
}

func newGuideSummaryView(g guide.Guide, lang string) guideSummaryView {
	return guideSummaryView{
		ID:       g.ID,
		Title:    g.Title.In(lang),
		Summary:  g.Summary.In(lang),
		Category: g.Category,
	}
}

// --- Resources Center ---

// provisionView carries a provision's sources with it, rather than relying on
// the document's sources, because that is the guarantee the data enforces:
// every article shown was checked on its own.
type provisionView struct {
	Ref     string
	Heading string
	// Paragraphs is the provision's text split into clause-per-line
	// paragraphs. Legal text is numbered clause by clause, and collapsing
	// that into one blob is exactly what makes a long article unreadable.
	Paragraphs []string
	Topic      string
	Disputed   bool
	Sources    []sourceView
}

type resourceSummaryView struct {
	ID           string
	Jurisdiction string
	Kind         string
	Title        string
	Summary      string
	Adopted      string
	Provisions   int
	LastVerified string
	Stale        bool
}

type resourceView struct {
	ID           string
	Jurisdiction string
	Kind         string
	Title        string
	Summary      string
	Adopted      string
	Publisher    string
	FullTextURL  string
	Caveat       string
	Provisions   []provisionView
	Sources      []sourceView
	LastVerified string
	Stale        bool
	Langs        []string
}

func newResourceSummaryView(d resource.Document, lang string) resourceSummaryView {
	return resourceSummaryView{
		ID:           d.ID,
		Jurisdiction: d.Jurisdiction,
		Kind:         string(d.Kind),
		Title:        d.Title.In(lang),
		Summary:      d.Summary.In(lang),
		Adopted:      d.Adopted,
		Provisions:   len(d.Provisions),
		LastVerified: d.LastVerified,
		Stale:        d.StaleAfter(90*24*time.Hour, time.Now()),
	}
}

func newResourceView(d resource.Document, lang string) resourceView {
	v := resourceView{
		ID:           d.ID,
		Jurisdiction: d.Jurisdiction,
		Kind:         string(d.Kind),
		Title:        d.Title.In(lang),
		Summary:      d.Summary.In(lang),
		Adopted:      d.Adopted,
		Publisher:    d.Publisher,
		FullTextURL:  d.FullTextURL,
		Caveat:       d.Caveat.In(lang),
		LastVerified: d.LastVerified,
		Stale:        d.StaleAfter(90*24*time.Hour, time.Now()),
		Langs:        d.Langs(),
		Sources:      sourceViews(d.Sources),
	}
	for _, p := range d.Provisions {
		var paras []string
		for _, line := range strings.Split(p.Text.In(lang), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				paras = append(paras, line)
			}
		}
		v.Provisions = append(v.Provisions, provisionView{
			Ref:        p.Ref,
			Heading:    p.Heading.In(lang),
			Paragraphs: paras,
			Topic:      p.Topic,
			Disputed:   p.Disputed(),
			Sources:    sourceViews(p.Sources),
		})
	}
	return v
}
