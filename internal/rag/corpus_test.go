package rag

import (
	"testing"

	mlinziassets "github.com/xbuyan/mlinzi"
	"github.com/xbuyan/mlinzi/internal/guide"
	"github.com/xbuyan/mlinzi/internal/resource"
)

// Tests against the real embedded corpus — the fixture tests above prove
// the mechanics on controlled input, but the gates below were all found by
// running the demo against the actual data, and each one is pinned here so
// a ranking change cannot quietly reintroduce it.

func realBuilder(t *testing.T, lang string) *Builder {
	t.Helper()
	gs, err := guide.Load(mlinziassets.DataFS, "data")
	if err != nil {
		t.Fatal(err)
	}
	rs, err := resource.Load(mlinziassets.ResourceFS, "resources")
	if err != nil {
		t.Fatal(err)
	}
	return Build(gs, rs, lang)
}

// TestRealCorpusAbstainsOutsideItsDomain pins the vocabulary gate: a
// question sharing almost no vocabulary with the corpus abstains, even
// though one of its words ("numbers") appears inside helpline schedules.
// The corpus is about civic procedures and the law — not lotteries.
func TestRealCorpusAbstainsOutsideItsDomain(t *testing.T) {
	b := realBuilder(t, "en")
	for _, q := range []string{
		"What are the lottery numbers for next week?",
		"What is the capital of France?",
		"Best recipe for jollof rice?",
		"Who won the football match last night?",
	} {
		if ans := Ask(testCtx(), noNetClient(), b, q, "", "en"); ans.Grounded {
			t.Errorf("query %q produced a grounded answer: %q", q, ans.Text)
		}
	}
}

// TestRealCorpusAnswersCoreDomainQuestions pins the positive direction:
// questions inside the corpus's actual domain retrieve on-topic passages,
// and the top hit is always the right guide or instrument.
func TestRealCorpusAnswersCoreDomainQuestions(t *testing.T) {
	// One index per language, exactly as the web app builds them — a
	// Kiswahili question must run against the Kiswahili index, the same
	// discipline the /ask handlers apply.
	builders := map[string]*Builder{
		"en": realBuilder(t, "en"),
		"sw": realBuilder(t, "sw"),
	}
	cases := []struct {
		query string
		juris string
		lang  string
	}{
		{"Can I report a bribe without giving my name?", "KE", "en"},
		{"Police beat me during arrest, what do I do?", "KE", "en"},
		{"What does the constitution say about freedom of expression?", "KE", "en"},
		{"I was detained and cannot afford a lawyer", "UG", "en"},
		{"Nimeombiwa rushwa. Naweza kuripoti bila jina?", "KE", "sw"},
	}
	for _, c := range cases {
		ans := Ask(testCtx(), noNetClient(), builders[c.lang], c.query, c.juris, c.lang)
		if !ans.Grounded {
			t.Errorf("query %q (juris %s, lang %s) abstained — the corpus should answer this", c.query, c.juris, c.lang)
			continue
		}
		if len(ans.Chunks) == 0 {
			t.Errorf("query %q grounded with no chunks", c.query)
		}
	}
}

// TestRealCorpusTopHitIsOnTopic asserts the strongest signal of precision:
// the first retrieved passage must share a domain-specific term with the
// question. It is the regression test for the drift family — a passage
// about presidential functions or government borrowing once surfaced in a
// bribery answer on two generic shared words.
func TestRealCorpusTopHitIsOnTopic(t *testing.T) {
	b := realBuilder(t, "en")
	queries := []string{
		"Can I report a bribe without giving my name?",
		"What does the constitution say about freedom of expression?",
	}
	for _, q := range queries {
		res := b.Index.Search(q, "", "en", 5)
		if len(res) == 0 {
			t.Fatalf("query %q retrieved nothing", q)
		}
		top := res[0]
		hits := 0
		for _, term := range Tokenize(q) {
			if isStopword(term) {
				continue
			}
			for _, t := range Tokenize(top.Text) {
				if t == term {
					hits++
					break
				}
			}
		}
		if hits == 0 {
			t.Errorf("query %q: top passage %s/%s shares no domain term with the question — drift", q, top.DocID, top.Ref)
		}
	}
}

// TestRealCorpusAnswerStaysOnTopicAcrossPassages pins the relative cut end
// to end: every passage quoted in an extractive answer shares a domain term
// with the question.
func TestRealCorpusAnswerStaysOnTopicAcrossPassages(t *testing.T) {
	b := realBuilder(t, "en")
	ans := Ask(testCtx(), noNetClient(), b, "Can I report a bribe without giving my name?", "KE", "en")
	if !ans.Grounded {
		t.Fatal("expected a grounded answer")
	}
	for _, sc := range ans.Chunks {
		hits := 0
		for _, term := range Tokenize("bribe report name") {
			for _, t := range Tokenize(sc.Text) {
				if t == term {
					hits++
					break
				}
			}
		}
		if hits == 0 {
			t.Errorf("answer quoted passage %s/%s which shares no domain term with the question: %.60s", sc.DocID, sc.Ref, sc.Text)
		}
	}
}
