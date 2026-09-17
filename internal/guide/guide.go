// Package guide models civic information a person needs before they can act:
// which institution handles their situation, what it will ask of them, what
// they are entitled to, and what happens next.
//
// Two design rules run through every type here.
//
// First, every human-readable string is a Text (language code -> string), so
// adding a language is a data change and never a code change.
//
// Second, every factual claim carries its own provenance. Institutions change
// hotlines, offices move, and secondary sources disagree with official ones.
// A guide that cannot say where a fact came from and when it was last checked
// is not information a person can trust, and the store refuses to serve it.
package guide

import (
	"fmt"
	"strings"
	"time"
)

// DefaultLang is the fallback language used when a translation is missing.
const DefaultLang = "en"

// Text holds one string per language, keyed by a language code ("en", "sw").
type Text map[string]string

// In returns the text in lang, falling back to DefaultLang, then to any
// available translation. It returns "" only when the Text is empty.
func (t Text) In(lang string) string {
	if s, ok := t[lang]; ok && s != "" {
		return s
	}
	if s, ok := t[DefaultLang]; ok && s != "" {
		return s
	}
	for _, s := range t {
		if s != "" {
			return s
		}
	}
	return ""
}

// Langs lists the language codes this Text is available in.
func (t Text) Langs() []string {
	out := make([]string, 0, len(t))
	for k, v := range t {
		if v != "" {
			out = append(out, k)
		}
	}
	return out
}

// Confidence records how far a fact is from its authoritative source.
type Confidence string

const (
	// Official: published by the institution the fact is about.
	Official Confidence = "official"
	// Secondary: reported by a credible third party, not the institution.
	Secondary Confidence = "secondary"
	// Conflicting: credible sources disagree. Surfaced to the user as a
	// disagreement rather than silently resolved in favour of one source.
	Conflicting Confidence = "conflicting"
)

func (c Confidence) Valid() bool {
	switch c {
	case Official, Secondary, Conflicting:
		return true
	}
	return false
}

// Source is the provenance of a single claim.
type Source struct {
	Publisher  string     `json:"publisher"`
	URL        string     `json:"url"`
	Retrieved  string     `json:"retrieved"` // YYYY-MM-DD
	Confidence Confidence `json:"confidence"`
}

// Validate enforces the rules every sourced claim must satisfy. It is
// exported because the resource layer records the same kind of sourced fact
// and must not grow a second, weaker validator beside it: one provenance
// model, one check, wherever a claim is published from.
func (s Source) Validate() error {
	if strings.TrimSpace(s.Publisher) == "" {
		return fmt.Errorf("source has no publisher")
	}
	if strings.TrimSpace(s.URL) == "" {
		return fmt.Errorf("source %q has no URL", s.Publisher)
	}
	if _, err := time.Parse("2006-01-02", s.Retrieved); err != nil {
		return fmt.Errorf("source %q has invalid retrieved date %q", s.Publisher, s.Retrieved)
	}
	if !s.Confidence.Valid() {
		return fmt.Errorf("source %q has invalid confidence %q", s.Publisher, s.Confidence)
	}
	return nil
}

// ChannelKind is how a person reaches an institution. Kinds are ordered by
// how little they cost the user: a toll-free call or USSD session works on a
// basic handset with no data, a web form does not.
type ChannelKind string

const (
	Phone    ChannelKind = "phone"
	USSD     ChannelKind = "ussd"
	SMS      ChannelKind = "sms"
	Email    ChannelKind = "email"
	Web      ChannelKind = "web"
	InPerson ChannelKind = "in_person"
	Post     ChannelKind = "post"
)

// LowBandwidth reports whether the channel works without mobile data.
func (k ChannelKind) LowBandwidth() bool {
	return k == Phone || k == USSD || k == SMS || k == InPerson || k == Post
}

// Channel is one way to reach an institution. It carries its own sources
// because contact details go stale independently of the rest of a guide, and
// because this is where credible sources most often disagree.
type Channel struct {
	Kind      ChannelKind `json:"kind"`
	Value     string      `json:"value"`
	TollFree  bool        `json:"toll_free"`
	Anonymous bool        `json:"anonymous"`
	Note      Text        `json:"note,omitempty"`
	Sources   []Source    `json:"sources"`
}

// Disputed reports whether any source for this channel flags disagreement.
// The UI shows disputed channels with both candidate values rather than
// picking one, so the user can decide what to try first.
func (c Channel) Disputed() bool {
	for _, s := range c.Sources {
		if s.Confidence == Conflicting {
			return true
		}
	}
	return false
}

func (c Channel) validate() error {
	if strings.TrimSpace(c.Value) == "" {
		return fmt.Errorf("channel %q has no value", c.Kind)
	}
	if len(c.Sources) == 0 {
		return fmt.Errorf("channel %q (%s) has no sources", c.Kind, c.Value)
	}
	for _, s := range c.Sources {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("channel %q (%s): %w", c.Kind, c.Value, err)
		}
	}
	return nil
}

// Institution is a body that receives reports or provides a service.
type Institution struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Role     Text      `json:"role"`
	Mandate  Text      `json:"mandate,omitempty"`
	Channels []Channel `json:"channels"`
}

// LowBandwidthChannels returns the channels usable without mobile data.
func (i Institution) LowBandwidthChannels() []Channel {
	var out []Channel
	for _, c := range i.Channels {
		if c.Kind.LowBandwidth() {
			out = append(out, c)
		}
	}
	return out
}

// AnonymousChannels returns the channels that accept a report without
// identifying the reporter.
func (i Institution) AnonymousChannels() []Channel {
	var out []Channel
	for _, c := range i.Channels {
		if c.Anonymous {
			out = append(out, c)
		}
	}
	return out
}

func (i Institution) validate() error {
	if strings.TrimSpace(i.ID) == "" {
		return fmt.Errorf("institution has no id")
	}
	if strings.TrimSpace(i.Name) == "" {
		return fmt.Errorf("institution %q has no name", i.ID)
	}
	if len(i.Channels) == 0 {
		return fmt.Errorf("institution %q has no channels", i.ID)
	}
	for _, c := range i.Channels {
		if err := c.validate(); err != nil {
			return fmt.Errorf("institution %q: %w", i.ID, err)
		}
	}
	return nil
}

// Step is one action in the pathway from "something happened" to "someone is
// handling it". Steps are what turn a guide from information into next steps.
type Step struct {
	Action      Text   `json:"action"`
	Detail      Text   `json:"detail,omitempty"`
	Institution string `json:"institution,omitempty"` // Institution.ID
	Deadline    Text   `json:"deadline,omitempty"`
	Critical    bool   `json:"critical,omitempty"` // time-bound; missing it costs the user
}

func (s Step) validate() error {
	if s.Action.In(DefaultLang) == "" {
		return fmt.Errorf("step has no action text")
	}
	return nil
}

// Guide answers one situation a person might be in.
type Guide struct {
	ID           string        `json:"id"`
	Jurisdiction string        `json:"jurisdiction"` // ISO 3166-1 alpha-2, e.g. "KE"
	Tracks       []string      `json:"tracks"`
	Category     string        `json:"category"`
	Title        Text          `json:"title"`
	Summary      Text          `json:"summary"`
	Rights       []Text        `json:"rights,omitempty"`
	Evidence     []Text        `json:"evidence,omitempty"`
	Steps        []Step        `json:"steps"`
	Institutions []Institution `json:"institutions"`
	Timeline     Text          `json:"timeline,omitempty"`
	Sources      []Source      `json:"sources"`
	LastVerified string        `json:"last_verified"` // YYYY-MM-DD
}

// VerifiedAt parses LastVerified.
func (g Guide) VerifiedAt() (time.Time, error) {
	return time.Parse("2006-01-02", g.LastVerified)
}

// StaleAfter reports whether the guide was last verified more than d ago,
// relative to now. Civic contact details rot quietly; the UI shows the age of
// every guide so a user can weigh it themselves.
func (g Guide) StaleAfter(d time.Duration, now time.Time) bool {
	t, err := g.VerifiedAt()
	if err != nil {
		return true
	}
	return now.Sub(t) > d
}

// Institution looks up one of the guide's institutions by ID.
func (g Guide) Institution(id string) (Institution, bool) {
	for _, i := range g.Institutions {
		if i.ID == id {
			return i, true
		}
	}
	return Institution{}, false
}

// Langs returns the language codes the guide's title is available in.
func (g Guide) Langs() []string { return g.Title.Langs() }

// Validate enforces the guarantee the whole layer rests on: a guide that
// cannot show its sources and a verification date does not get served.
func (g Guide) Validate() error {
	if strings.TrimSpace(g.ID) == "" {
		return fmt.Errorf("guide has no id")
	}
	if len(g.Jurisdiction) != 2 {
		return fmt.Errorf("guide %q has invalid jurisdiction %q", g.ID, g.Jurisdiction)
	}
	if g.Title.In(DefaultLang) == "" {
		return fmt.Errorf("guide %q has no title", g.ID)
	}
	if g.Summary.In(DefaultLang) == "" {
		return fmt.Errorf("guide %q has no summary", g.ID)
	}
	if len(g.Steps) == 0 {
		return fmt.Errorf("guide %q has no steps: information without next steps is not actionable", g.ID)
	}
	for _, s := range g.Steps {
		if err := s.validate(); err != nil {
			return fmt.Errorf("guide %q: %w", g.ID, err)
		}
		if s.Institution != "" {
			if _, ok := g.Institution(s.Institution); !ok {
				return fmt.Errorf("guide %q: step references unknown institution %q", g.ID, s.Institution)
			}
		}
	}
	if len(g.Institutions) == 0 {
		return fmt.Errorf("guide %q has no institutions", g.ID)
	}
	for _, i := range g.Institutions {
		if err := i.validate(); err != nil {
			return fmt.Errorf("guide %q: %w", g.ID, err)
		}
	}
	if len(g.Sources) == 0 {
		return fmt.Errorf("guide %q has no sources", g.ID)
	}
	for _, s := range g.Sources {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("guide %q: %w", g.ID, err)
		}
	}
	if _, err := g.VerifiedAt(); err != nil {
		return fmt.Errorf("guide %q has invalid last_verified %q", g.ID, g.LastVerified)
	}
	return nil
}
