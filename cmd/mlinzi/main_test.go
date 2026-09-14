package main

import (
	"strings"
	"testing"

	"github.com/xbuyan/mlinzi/internal/guide"
)

func TestFlagStringShowsDisputed(t *testing.T) {
	c := guide.Channel{
		Kind:      guide.Phone,
		TollFree:  true,
		Anonymous: true,
		Sources: []guide.Source{
			{Publisher: "A", URL: "https://a", Retrieved: "2026-09-15", Confidence: guide.Conflicting},
		},
	}
	got := flagString(c)
	for _, want := range []string{"toll-free", "no data needed", "anonymous", "SOURCES DISAGREE"} {
		if !strings.Contains(got, want) {
			t.Errorf("flagString() = %q, missing %q", got, want)
		}
	}
}

func TestFlagStringEmptyWhenNothingToFlag(t *testing.T) {
	c := guide.Channel{Kind: guide.Web}
	if got := flagString(c); got != "" {
		t.Errorf("expected no flags for a plain web channel, got %q", got)
	}
}
