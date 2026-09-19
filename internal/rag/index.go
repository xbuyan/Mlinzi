package rag

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

// Index is a BM25 (Okapi) retrieval index over the corpus chunks.
//
// Why BM25 rather than embeddings: the corpus is a few hundred chunks, it
// must work with zero network access (the basic-device constraint the rest
// of the app is built around), and a lexical index over multilingual civic
// text with field boosts and an explainable score is easier to audit than a
// vector space — for a project whose entire premise is auditable claims, an
// embedding's opaque similarity would be the one part of the pipeline nobody
// could inspect. If the corpus grows to thousands of documents, semantic
// retrieval becomes worth its operational cost; the Answer call signature
// below is designed so that swap would not change the layers above it.
type Index struct {
	chunks []Chunk
	dfs    map[string]int   // term -> number of chunks containing it
	tfs    []map[string]int // per-chunk term frequencies
	totals []int            // per-chunk token count (field-boosted)
	avgLen float64
}

// BM25 parameters. k1 controls how much a term's repetition keeps mattering
// (low-ish because legal text repeats terms for structure, not emphasis);
// b controls length normalisation (fairly high so a long article does not
// outrank a precise hotline chunk just by being long).
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// scoreFloor is the minimum BM25 score for a chunk to count as retrieved at
// all. Below it, the corpus has nothing meaningfully to say about the
// question, and the answer must say so rather than dress up an irrelevant
// chunk as an answer. Calibrated empirically: a query sharing one common
// word with a chunk scores under ~0.5; a query about the chunk's subject
// scores well above 2.
const scoreFloor = 0.9

// BuildIndex indexes chunks. It is safe for concurrent reads afterwards.
func BuildIndex(chunks []Chunk) *Index {
	idx := &Index{
		chunks: chunks,
		dfs:    make(map[string]int),
		tfs:    make([]map[string]int, len(chunks)),
		totals: make([]int, len(chunks)),
	}
	var totalLen float64
	for i, c := range chunks {
		tfs := make(map[string]int)
		// Field boosts: a term in the section heading or the doc's
		// jurisdiction matters more than one in flowing body text. This is
		// the cheap, inspectable version of what field-weighted retrieval
		// does in bigger systems.
		count := func(term string, weight int) {
			tfs[term] += weight
		}
		for _, t := range Tokenize(c.Section) {
			count(t, 3)
		}
		for _, t := range Tokenize(c.Ref) {
			count(t, 3)
		}
		for _, t := range Tokenize(c.Text) {
			count(t, 1)
		}
		idx.tfs[i] = tfs
		n := 0
		for _, w := range tfs {
			n += w
		}
		idx.totals[i] = n
		totalLen += float64(n)
		for term := range tfs {
			idx.dfs[term]++
		}
	}
	if len(chunks) > 0 {
		idx.avgLen = totalLen / float64(len(chunks))
	}
	return idx
}

// Search returns the top n chunks matching query for jurisdiction ("",
// means all), filtered to the query's language when lang is set.
//
// Two gates stand between a query and a result, and both exist because of
// failures the real corpus produced, not hypothetical ones:
//
//  1. The score floor — a chunk must score at least scoreFloor, so queries
//     sharing nothing with the corpus retrieve nothing.
//  2. Term coverage — a chunk must share at least two *meaningful* query
//     terms, where meaningful means not a closed-class stopword and not
//     ubiquitous across the corpus. The floor alone did not hold on the
//     real corpus: "lottery numbers for next week" cleared it by matching
//     "next" against the section label "Next steps", which every guide
//     repeats mechanically. One coincidentally shared word is not
//     retrieval, however well it scores. Coverage drops to one term only
//     when the question has exactly one meaningful term, so a one-word
//     query ("bribe") still works.
func (ix *Index) Search(query, jurisdiction string, lang string, n int) []Scored {
	if n <= 0 || len(ix.chunks) == 0 {
		return nil
	}
	terms := Tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	// A term is meaningful if it is neither a closed-class function word
	// nor present in more than a tenth of the corpus. The threshold is not
	// a quarter: "people" sits in roughly that share of the corpus through
	// constitutional text, and a query about a bribe once drifted into an
	// article about judicial power on the strength of that one shared word.
	// Meaningful terms drive the coverage gate; every term — including weak
	// ones — still contributes to the BM25 score, where IDF keeps their
	// weight small.
	// Vocabulary gate, measured on the real corpus: how many of the
	// question's meaningful terms exist anywhere in it. "lottery numbers
	// for next week" has three meaningful terms and the corpus contains
	// exactly one of them ("numbers", inside helpline schedules); "capital
	// of France" has two, of which the corpus holds one ("capital", in a
	// list of states) — both questions are outside the corpus whatever one
	// coincidental overlap scores, so strictly more than half of the
	// meaningful terms must be in-vocabulary. A question about the corpus's
	// actual domain ("bribe", "rushwa ripoti") clears it comfortably; an
	// out-of-domain one abstains with the refusal message inviting a
	// rephrase.
	vocabHits := 0
	for _, t := range terms {
		if !isStopword(t) && ix.dfs[t] > 0 {
			vocabHits++
		}
	}
	meaningfulCount := 0
	for _, t := range terms {
		if !isStopword(t) {
			meaningfulCount++
		}
	}
	if vocabHits*2 <= meaningfulCount {
		return nil
	}

	meaningfulMax := len(ix.chunks) / 10
	// A term is *rare* — and therefore precise — when it sits in almost no
	// other chunk: a single rare-term match is strong enough evidence to
	// qualify a chunk on its own, where two generic matches are not. The
	// floor of 2 keeps the rule meaningful on tiny corpora (a test fixture
	// of 8 chunks would otherwise deem nothing rare).
	rareMax := len(ix.chunks) / 50
	if rareMax < 2 {
		rareMax = 2
	}
	// A new slice, deliberately not terms[:0]: filtering in place would
	// overwrite the backing array score() still reads, silently re-weighting
	// the query. (Found by instrumenting a drift case on the real corpus.)
	meaningful := make([]string, 0, len(terms))
	for _, t := range terms {
		if isStopword(t) {
			continue
		}
		meaningful = append(meaningful, t)
	}
	if len(meaningful) == 0 {
		return nil
	}

	var scored []Scored
	for i, c := range ix.chunks {
		if jurisdiction != "" && !strings.EqualFold(c.Jurisdiction, jurisdiction) {
			continue
		}
		if lang != "" && c.Lang != lang && !c.isLegal() {
			// A Kiswahili question should not surface an English guide
			// chunk — the app has real translations and the fallback rule
			// ("guide says it's English-only") applies. Provision chunks are
			// exempt: the law is quoted in English on purpose, in every
			// language, and the page says so.
			continue
		}
		coverage := 0
		rare := false
		for _, t := range meaningful {
			if ix.tfs[i][t] == 0 {
				continue
			}
			if ix.dfs[t] <= meaningfulMax {
				coverage++
			}
			if ix.dfs[t] <= rareMax {
				rare = true
			}
		}
		if coverage < 2 && !rare {
			continue
		}
		s := ix.score(i, meaningful)
		if s < scoreFloor {
			continue
		}
		scored = append(scored, Scored{Chunk: c, Score: s})
	}

	sort.Slice(scored, func(a, b int) bool {
		if scored[a].Score != scored[b].Score {
			return scored[a].Score > scored[b].Score
		}
		// Deterministic tie-break: shorter chunk first (denser), then doc
		// order. Map iteration elsewhere is randomised; ranking must not be.
		if len(scored[a].Text) != len(scored[b].Text) {
			return len(scored[a].Text) < len(scored[b].Text)
		}
		return scored[a].DocID < scored[b].DocID
	})

	// Relative cut: keep only passages scoring within 40% of the best hit.
	// Absolute gates cannot finish the job, because legal corpora are dense
	// with shared boilerplate — a passage about the President's functions
	// once cleared every absolute gate on two generic words it happened to
	// share with a bribery question, and would then sit in the answer as if
	// it belonged. Whatever the top hit is about, the rest must be about
	// too, at least at 0.4 of its weight.
	if len(scored) > 0 {
		best := scored[0].Score
		kept := scored[:0]
		for _, sc := range scored {
			if sc.Score >= 0.4*best {
				kept = append(kept, sc)
			}
		}
		scored = kept
	}
	if len(scored) > n {
		scored = scored[:n]
	}
	return scored
}

func (c Chunk) isLegal() bool { return c.Kind == KindProvision }

// score computes BM25 for one chunk against the query terms. Exported logic
// lives in Search; this is the arithmetic.
func (ix *Index) score(i int, terms []string) float64 {
	tf := ix.tfs[i]
	dl := float64(ix.totals[i])
	var score float64
	for _, t := range terms {
		f := float64(tf[t])
		if f == 0 {
			continue
		}
		dfi := float64(ix.dfs[t])
		n := float64(len(ix.chunks))
		idf := ln1p((n - dfi + 0.5) / (dfi + 0.5))
		if idf < 0 {
			idf = 0
		}
		norm := f * (bm25K1 + 1) / (f + bm25K1*(1-bm25B+bm25B*dl/ix.avgLen))
		score += idf * norm
	}
	return score
}

// ln1p is log(1+x), so idf stays non-negative for terms in more than half
// the corpus.
func ln1p(x float64) float64 {
	return math.Log1p(x)
}

// Scored pairs a chunk with the retrieval score that surfaced it. Chunk is
// embedded, so chunk fields promote onto Scored values.
type Scored struct {
	Chunk
	Score float64
}

// Tokenize lowercases, strips punctuation and digits, and splits on
// non-letter boundaries. Unicode-aware so Kiswahili and French (with their
// accented characters) tokenise correctly, not just ASCII English.
//
// No stemming and no stopword list, on purpose: stemming helps English and
// quietly breaks French/Kiswahili morphology; a stopword list is a second
// place language assumptions hide. BM25's IDF handles stopword-like terms
// (common terms score near zero), and the score floor catches the rest.
func Tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// DebugTermHits reports where a term matched within a chunk (section, text,
// or nowhere), for the retrieval debugger in tools/. It is test/diagnostic
// scaffolding, not part of the retrieval contract.
func DebugTermHits(ix *Index, c Chunk, term string) string {
	for i, cc := range ix.chunks {
		if cc.DocID == c.DocID && cc.Ref == c.Ref && cc.Section == c.Section && cc.Text == c.Text {
			hits := []string{}
			if ix.tfs[i][term] > 0 {
				hits = append(hits, fmt.Sprintf("tf=%d df=%d", ix.tfs[i][term], ix.dfs[term]))
			}
			if len(hits) == 0 {
				return "-"
			}
			return strings.Join(hits, " ")
		}
	}
	return "chunk not in index"
}
