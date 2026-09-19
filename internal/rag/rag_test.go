package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/xbuyan/mlinzi/internal/guide"
	"github.com/xbuyan/mlinzi/internal/resource"
)

// testCorpus builds a small but real corpus: one guide with rights, steps
// and a disputed hotline, and one document with two provisions. Shape
// matches what the real loaders produce, so chunking is exercised on
// realistic inputs — and the loaders' own validation runs over it, so a
// malformed fixture fails loudly here instead of skewing a ranking test.
func testCorpus(t *testing.T) (*guide.Store, *resource.Store) {
	t.Helper()
	guidesJSON := `[
	{
		"id": "ke-bribery-test",
		"jurisdiction": "KE",
		"tracks": ["transparency-accountability"],
		"category": "corruption",
		"title": {"en": "A public officer asked me for a bribe", "sw": "Afisa wa umma amenidai rushwa"},
		"summary": {"en": "You can report a bribe demand without giving your name.", "sw": "Unaweza kuripoti rushwa bila kutoa jina lako."},
		"rights": [{"en": "You can report anonymously to the anti-corruption commission.", "sw": "Unaweza kuripoti bila jina kwa tume ya kupambana na rushwa."}],
		"evidence": [],
		"steps": [
			{"action": {"en": "Write down what happened while it is fresh"}, "detail": {"en": "Date, place, amount demanded."}},
			{"action": {"en": "Report to the commission on the toll-free line"}}
		],
		"institutions": [
			{
				"id": "eacc-test",
				"name": "Ethics and Anti-Corruption Commission",
				"role": {"en": "Receives bribery reports."},
				"channels": [
					{"kind": "phone", "value": "0800 720 434", "toll_free": true, "anonymous": true, "sources": [{"publisher": "EACC", "url": "https://example.com/eacc", "retrieved": "2026-09-01", "confidence": "conflicting"}]}
				]
			}
		],
		"timeline": {"en": "The commission acknowledges within 3 days."},
		"sources": [{"publisher": "EACC", "url": "https://example.com/eacc", "retrieved": "2026-09-01", "confidence": "official"}],
		"last_verified": "2026-09-01"
	}
	]`
	docJSON := `{
		"id": "kenya-constitution-test",
		"kind": "constitution",
		"jurisdiction": "KE",
		"title": {"en": "Constitution of Kenya, 2010"},
		"summary": {"en": "The supreme law."},
		"adopted": "2010-08-27",
		"publisher": "Kenya Law",
		"full_text_url": "https://example.com/kenya-constitution",
		"provisions": [
			{
				"ref": "Article 10",
				"heading": {"en": "National values and principles of governance"},
				"text": {"en": "The national values and principles of governance include transparency, accountability and public participation."},
				"topic": "accountability",
				"sources": [{"publisher": "Kenya Law", "url": "https://example.com/article10", "retrieved": "2026-09-01", "confidence": "secondary"}]
			},
			{
				"ref": "Article 33",
				"heading": {"en": "Freedom of expression"},
				"text": {"en": "Every person has the right to freedom of expression, which includes freedom to seek or impart information."},
				"topic": "expression",
				"sources": [{"publisher": "Kenya Law", "url": "https://example.com/article33", "retrieved": "2026-09-01", "confidence": "secondary"}]
			}
		],
		"sources": [{"publisher": "Kenya Law", "url": "https://example.com/doc", "retrieved": "2026-09-01", "confidence": "secondary"}],
		"last_verified": "2026-09-01"
	}`
	gs, err := guide.Load(fstest.MapFS{
		"data/ke/test.json": &fstest.MapFile{Data: []byte(guidesJSON)},
	}, "data")
	if err != nil {
		t.Fatal(err)
	}
	rs, err := resource.Load(fstest.MapFS{
		"resources/kenya-constitution-test.json": &fstest.MapFile{Data: []byte(docJSON)},
	}, "resources")
	if err != nil {
		t.Fatal(err)
	}
	return gs, rs
}

func builderFor(t *testing.T, lang string) *Builder {
	t.Helper()
	gs, rs := testCorpus(t)
	return Build(gs, rs, lang)
}

func TestBuildChunksCarryProvenance(t *testing.T) {
	b := builderFor(t, "en")
	if len(b.Chunks) == 0 {
		t.Fatal("expected chunks to be built from the corpus")
	}
	var guideChunks, provisionChunks int
	for _, c := range b.Chunks {
		if len(c.Sources) == 0 {
			t.Errorf("chunk %s/%s has no sources — an answer citing it would be uncited", c.DocID, c.Section)
		}
		if c.Text == "" {
			t.Errorf("chunk %s/%s has empty text", c.DocID, c.Section)
		}
		switch c.Kind {
		case KindGuide:
			guideChunks++
		case KindProvision:
			if c.Ref == "" || c.Lang != "en" {
				t.Errorf("provision chunk %s/%s missing ref or wrong lang", c.DocID, c.Ref)
			}
			provisionChunks++
		}
	}
	if guideChunks == 0 || provisionChunks == 0 {
		t.Fatalf("expected both corpus halves to be chunked, got %d guide and %d provision chunks", guideChunks, provisionChunks)
	}
}

func TestSearchRanksRelevantAboveIrrelevant(t *testing.T) {
	b := builderFor(t, "en")
	res := b.Index.Search("how do I report a bribe anonymously", "KE", "en", 5)
	if len(res) == 0 {
		t.Fatal("expected results for a bribe question")
	}
	if res[0].DocID != "ke-bribery-test" {
		t.Fatalf("expected the bribery guide to rank first, got %s", res[0].DocID)
	}

	res = b.Index.Search("freedom of expression", "KE", "en", 5)
	if len(res) == 0 || res[0].Ref != "Article 33" {
		t.Fatalf("expected Article 33 for an expression question, got %+v", res)
	}
}

func TestSearchOutOfCorpusQuestionsReturnNothing(t *testing.T) {
	b := builderFor(t, "en")
	// Queries sharing nothing with the corpus must retrieve nothing above
	// the floor — the abstain path, not an improvised answer.
	for _, q := range []string{"football tournament schedule", "harvest festival traditions", "xyzzyplugh"} {
		if res := b.Index.Search(q, "KE", "en", 5); len(res) > 0 {
			t.Errorf("query %q unexpectedly retrieved %d chunks — the floor is too permissive", q, len(res))
		}
	}
}

func TestSearchJurisdictionFilter(t *testing.T) {
	b := builderFor(t, "en")
	res := b.Index.Search("bribe report toll free", "NG", "en", 5)
	if len(res) != 0 {
		t.Fatalf("expected no Kenyan chunks under an NG filter, got %d", len(res))
	}
}

func TestSearchLanguageFilterKeepsLegalTextAvailable(t *testing.T) {
	b := builderFor(t, "en")
	// A Kiswahili question must not surface English *guide* chunks...
	if res := b.Index.Search("rushwa ripoti", "KE", "sw", 5); len(res) > 0 {
		for _, sc := range res {
			if sc.Kind == KindGuide && sc.Lang != "sw" {
				t.Fatalf("English guide chunk leaked into a Kiswahili query: %s", sc.DocID)
			}
		}
	}
	// ...but the law itself stays reachable in every language, on purpose:
	// it is quoted in its authoritative English wording by design.
	res := b.Index.Search("transparency accountability", "KE", "sw", 5)
	if len(res) == 0 {
		t.Fatal("provision chunks must remain retrievable regardless of query language")
	}
}

func TestSynthesizeQuotesCorpusTextWithCitations(t *testing.T) {
	b := builderFor(t, "en")
	chunks := b.Index.Search("report bribe anonymously toll-free", "KE", "en", 5)
	ans := Synthesize("report bribe anonymously", chunks)

	if !ans.Grounded {
		t.Fatal("expected a grounded answer for a corpus-supported question")
	}
	if ans.Method != MethodExtractive {
		t.Fatalf("expected extractive method, got %s", ans.Method)
	}
	// Every cited URL must genuinely exist in the retrieved chunk set — the
	// citation cannot name a source the passages did not carry.
	urls := map[string]bool{}
	for _, sc := range chunks {
		for _, s := range sc.Sources {
			urls[s.URL] = true
		}
	}
	cited := 0
	for u := range urls {
		if strings.Contains(ans.Text, u) {
			cited++
		}
	}
	if cited == 0 {
		t.Fatalf("expected at least one real citation URL in the answer text:\n%s", ans.Text)
	}
}

func TestSynthesizeFlagsDisputedSources(t *testing.T) {
	b := builderFor(t, "en")
	chunks := b.Index.Search("call the commission phone number", "KE", "en", 5)
	ans := Synthesize("commission phone number", chunks)
	if !strings.Contains(ans.Text, "disagree") {
		t.Fatalf("expected the disputed-source note in the answer:\n%s", ans.Text)
	}
}

func TestSynthesizeAbstainsWhenNothingRetrieved(t *testing.T) {
	ans := Synthesize("what is the weather", nil)
	if ans.Grounded {
		t.Fatal("an empty retrieval must not produce a grounded answer")
	}
	if strings.ContainsAny(ans.Text, "0123456789") {
		t.Fatalf("an abstention must not contain numbers (invented or otherwise): %s", ans.Text)
	}
}

// --- LLM path ---

func llmServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, srv.Client()
}

func llmBody(content string) map[string]any {
	return map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": content}},
		},
	}
}

// setLLMEnv points the LLM path at an httptest server URL. Every test that
// calls this gets isolation from the developer's real environment (t.Setenv
// restores afterwards), which matters: a developer with RAG_LLM_ENDPOINT in
// their shell must not find their actual endpoint keyed into test traffic.
func setLLMEnv(t *testing.T, endpoint, model string) {
	t.Helper()
	t.Setenv(envEndpoint, endpoint)
	t.Setenv(envAPIKey, "test-key")
	t.Setenv(envModel, model)
}

func TestLLMAnswerAcceptedWhenSupported(t *testing.T) {
	b := builderFor(t, "en")
	chunks := b.Index.Search("report a bribe anonymously", "KE", "en", 5)
	if len(chunks) == 0 {
		t.Fatal("test setup: expected retrieval hits")
	}

	// The model answers using corpus words — "report", "anonymously", the
	// commission's name — every content word present in the passages.
	srv, client := llmServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(llmBody("You can report anonymously to the Ethics and Anti-Corruption Commission."))
	})
	setLLMEnv(t, srv.URL, "test-model")

	ans, err := AnswerWithLLM(context.Background(), client, "report a bribe anonymously", chunks)
	if err != nil {
		t.Fatalf("expected the supported LLM answer to pass verification: %v", err)
	}
	if ans.Method != MethodLLM || !ans.Grounded {
		t.Fatalf("expected a grounded LLM answer, got method=%s grounded=%v", ans.Method, ans.Grounded)
	}
}

func TestLLMAnswerFallsBackWhenFabricating(t *testing.T) {
	b := builderFor(t, "en")
	chunks := b.Index.Search("report a bribe anonymously", "KE", "en", 5)

	// The model invents a statute and a hotline number the corpus does not
	// contain — exactly the failure the verification step exists to catch.
	srv, client := llmServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(llmBody("Under the Anti-Bribery Statute section 88, call 0700 000 999 within 48 hours."))
	})
	setLLMEnv(t, srv.URL, "test-model")

	if _, err := AnswerWithLLM(context.Background(), client, "report a bribe anonymously", chunks); err == nil {
		t.Fatal("expected a fabricated answer to be rejected outright")
	}
}

func TestLLMCallFailureIsAnErrorNotAPanic(t *testing.T) {
	b := builderFor(t, "en")
	chunks := b.Index.Search("report a bribe anonymously", "KE", "en", 5)

	// A server that returns garbage instead of the expected shape: the path
	// back to extractive must be a clean error the Ask pipeline can log and
	// move past. (A panicking handler is not used here — net/http recovers
	// it, but the resulting stack trace in test output is noise, not
	// signal; a 500 with a nonsense body exercises the same client path.)
	srv, client := llmServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `not the response shape anybody promised`, http.StatusInternalServerError)
	})
	setLLMEnv(t, srv.URL, "test-model")

	if _, err := AnswerWithLLM(context.Background(), client, "report a bribe anonymously", chunks); err == nil {
		t.Fatal("expected an error from a failing LLM endpoint")
	}
}

func TestAskFallsBackToExtractiveWithoutLLM(t *testing.T) {
	// No endpoint configured: Ask must return the extractive answer, not an
	// error — this is the offline default for every deployment that has not
	// opted into an LLM.
	setLLMEnv(t, "", "test-model")
	b := builderFor(t, "en")

	ans := Ask(context.Background(), http.DefaultClient, b, "how do I report a bribe", "KE", "en")
	if !ans.Grounded || ans.Method != MethodExtractive {
		t.Fatalf("expected a grounded extractive answer with no LLM configured, got method=%s grounded=%v", ans.Method, ans.Grounded)
	}
}

func TestAskUsesLLMWhenItBehaves(t *testing.T) {
	b := builderFor(t, "en")
	// The mock echoes the corpus this exact query retrieves — the
	// well-behaved model answering from the passages it was given.
	chunks := b.Index.Search("how do I report a bribe", "KE", "en", 5)
	if len(chunks) == 0 {
		t.Fatal("test setup: expected retrieval hits")
	}
	echo := strings.Split(chunks[0].Text, ". ")[0]
	srv, client := llmServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(llmBody(echo))
	})
	setLLMEnv(t, srv.URL, "test-model")

	ans := Ask(context.Background(), client, b, "how do I report a bribe", "KE", "en")
	if ans.Method != MethodLLM {
		t.Fatalf("expected the LLM method when configured and well-behaved, got %s", ans.Method)
	}
}

func TestAskUsesExtractiveWhenLLMLies(t *testing.T) {
	b := builderFor(t, "en")
	srv, client := llmServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(llmBody("The fabricated statute 12345 section 9 grants an invented right to 555-0199."))
	})
	setLLMEnv(t, srv.URL, "test-model")

	ans := Ask(context.Background(), client, b, "how do I report a bribe", "KE", "en")
	if ans.Method != MethodExtractive || !ans.Grounded {
		t.Fatalf("expected the extractive fallback when the LLM fabricates, got method=%s grounded=%v", ans.Method, ans.Grounded)
	}
}

func TestAskAbstainsOnOutOfCorpusQuestion(t *testing.T) {
	b := builderFor(t, "en")
	ans := Ask(context.Background(), http.DefaultClient, b, "who won the football match", "KE", "en")
	if ans.Grounded {
		t.Fatal("expected an abstention for a question the corpus cannot answer")
	}
}

func TestTokenizerHandlesAccentsAndCase(t *testing.T) {
	toks := Tokenize("Dénoncer l'RUSHWA, pot-de-vin!")
	if len(toks) != 6 {
		t.Fatalf("expected 6 tokens, got %v", toks)
	}
	if toks[0] != "dénoncer" || toks[2] != "rushwa" || toks[5] != "vin" {
		t.Fatalf("unexpected tokens: %v", toks)
	}
}

func TestPromptInstructsGrounding(t *testing.T) {
	p := buildPrompt("test query", []Scored{{Chunk: Chunk{DocID: "d1", Ref: "Article 10", Text: "text"}}})
	for _, want := range []string{"ONLY", "could not find", "Article 10"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}
