package rag

import (
	"context"
	"errors"
	"log"
	"net/http"
)

// topChunks is how many passages the synthesizer sees. Enough to stitch a
// real answer across sections (a rights claim plus the hotline plus the
// deadline), small enough that an LLM prompt stays focused and a page stays
// readable.
const topChunks = 5

// Ask runs the full pipeline: retrieve, then synthesize.
//
// The LLM path is tried first when configured; anything it says that the
// retrieved passages do not support is dropped by verification, and if that
// leaves nothing usable the extractive answer ships instead. The user gets
// a grounded answer by every path — the only variable is whether the
// phrasing was composed or model-written, which the page states.
func Ask(ctx context.Context, client *http.Client, b *Builder, query, jurisdiction, lang string) Answer {
	chunks := b.Index.Search(query, jurisdiction, lang, topChunks)
	if len(chunks) == 0 {
		return Synthesize(query, nil)
	}

	if ans, err := AnswerWithLLM(ctx, client, query, chunks); err == nil {
		return ans
	} else if !errors.Is(err, ErrNoLLM) {
		// Configured but failed (unreachable, slow, refused, unsupported
		// output). Logged, not surfaced: the extractive fallback below is
		// the user-facing answer, and the page says which method produced
		// it, so honesty is preserved without an error page.
		log.Printf("rag: llm path unavailable, using extractive answer: %v", err)
	}

	return Synthesize(query, chunks)
}
