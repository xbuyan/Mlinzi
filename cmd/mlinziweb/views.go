package main

import (
	"time"

	"github.com/xbuyan/mlinzi/internal/guide"
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
	for _, s := range g.Sources {
		v.Sources = append(v.Sources, sourceView{
			Publisher:  s.Publisher,
			URL:        s.URL,
			Confidence: string(s.Confidence),
		})
	}
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
