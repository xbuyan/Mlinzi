// Package rag implements retrieval-augmented answering over Mlinzi's own
// civic corpus: the situation guides in data/ and the primary legal
// instruments in resources/.
//
// Why RAG belongs in this app, stated as a design rule: Mlinzi's whole
// argument is that a claim about your rights should carry its source with
// it. A naive chatbot over civic text is the opposite of that — a fluent
// paragraph with no provenance, which can assert a hotline number, a
// deadline, or a right that exists nowhere in the corpus. So this package
// is built cite-or-abstain: every answer names the guide or provision it
// came from, with the publisher, URL and confidence the data already
// carries; and when nothing in the corpus scores above the retrieval floor,
// the answer says so instead of improvising. The retriever cannot produce a
// claim its corpus does not contain, because the synthesizer is only shown
// retrieved chunks and the extractive path only rearranges corpus text.
//
// The pipeline has three stages, each its own file:
//
//	chunk.go     — corpus text into chunk units, each with provenance
//	index.go     — BM25 retrieval over those chunks, cross-language
//	answer.go    — extractive synthesis from the top chunks (+ llm.go for
//	               optional LLM phrasing, with the extractive path as
//	               fallback when no endpoint is configured or reachable)
//
// The corpus is small (hundreds of chunks), so a dependency-free BM25 index
// that runs in-process — cold-start in well under a millisecond, no network
// for the retrieval path, no vector database to operate — is the right
// engineering choice here, exactly as guide.Store.Search's doc comment
// argues for its simpler substring cousin.
package rag

import (
	"strings"

	"github.com/xbuyan/mlinzi/internal/guide"
	"github.com/xbuyan/mlinzi/internal/resource"
)

// Chunk is one retrievable unit of the corpus, carrying its provenance with
// it. A chunk never strays from its source: whatever the synthesizer says,
// it says about a chunk that names where the claim comes from.
type Chunk struct {
	// Kind distinguishes the two halves of the corpus: a situation guide
	// ("what do I do") or a provision of law ("what the instrument says").
	Kind ChunkKind
	// DocID is the guide ID or document ID the chunk belongs to, e.g.
	// "ke-bribery-public-service" or "kenya-constitution".
	DocID string
	// Jurisdiction is ISO 3166-1 alpha-2, inherited from the source.
	Jurisdiction string
	// Ref is the provision ref for provision chunks ("Article 10") and
	// empty for guide chunks.
	Ref string
	// Section is a human label for which part of the source this is, e.g.
	// "Your rights", "Next steps", or a document title.
	Section string
	// Lang is the language the chunk's text was taken in.
	Lang string
	// Text is the chunk's content.
	Text string
	// Sources is the provenance the chunk's own claim carries.
	Sources []SourceRef
	// Disputed reports whether any source behind this chunk flags a
	// disagreement — set per-channel for institution chunks, because a
	// disputed hotline number is the common real case and a document-level
	// flag would smear it across every chunk of the guide.
	Disputed bool
}

// ChunkKind says which half of the corpus a chunk came from.
type ChunkKind string

const (
	KindGuide     ChunkKind = "guide"
	KindProvision ChunkKind = "provision"
)

// SourceRef flattens guide.Source for the answer layer, so the answer page
// shows the same provenance shape the guide and resource pages do.
type SourceRef struct {
	Publisher  string
	URL        string
	Confidence string
}

// Builder holds the corpus and its index. Build once at startup (like the
// guide and resource stores); Index is safe for concurrent reads after
// Build.
type Builder struct {
	Chunks []Chunk
	Index  *Index
}

// Build chunks every guide and every document's provisions in lang, then
// indexes them. Guides that carry a translation in lang are chunked in that
// language, with the chunk's Lang recording what was actually used — the
// same fallback discipline Text.In applies everywhere else in the app, and
// the reason an answer page can say which language each chunk came from.
//
// Legal text is deliberately kept in its original English wording even when
// lang is sw or fr: translating a constitution produces a version that looks
// authoritative and is not — the same rule the Resources Center page
// already states in its notice.
func Build(guides *guide.Store, resources *resource.Store, lang string) *Builder {
	chunks := chunkCorpus(guides, resources, lang)
	return &Builder{
		Chunks: chunks,
		Index:  BuildIndex(chunks),
	}
}

func chunkCorpus(guides *guide.Store, resources *resource.Store, lang string) []Chunk {
	var chunks []Chunk
	for _, jur := range guides.Jurisdictions() {
		for _, g := range guides.ByJurisdiction(jur) {
			chunks = append(chunks, chunkGuide(g, lang)...)
		}
	}
	for _, d := range resources.All() {
		chunks = append(chunks, chunkDocument(d)...)
	}
	return chunks
}

func sourceRefs(srcs []guide.Source) []SourceRef {
	out := make([]SourceRef, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, SourceRef{
			Publisher:  s.Publisher,
			URL:        s.URL,
			Confidence: string(s.Confidence),
		})
	}
	return out
}

func chunkGuide(g guide.Guide, lang string) []Chunk {
	var chunks []Chunk
	add := func(section, text string, srcs []guide.Source, disputed bool) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		chunks = append(chunks, Chunk{
			Kind:         KindGuide,
			DocID:        g.ID,
			Jurisdiction: g.Jurisdiction,
			Section:      section,
			Lang:         lang,
			Text:         text,
			Sources:      sourceRefs(srcs),
			Disputed:     disputed,
		})
	}

	// A guide's sections are chunked separately because a person's question
	// usually wants one part of one guide — the steps, or the rights — not
	// the whole page. Each section keeps the guide's own top-level sources.
	add("Summary", g.Title.In(lang)+". "+g.Summary.In(lang), g.Sources, false)
	for _, r := range g.Rights {
		add("Your rights", r.In(lang), g.Sources, false)
	}
	for _, st := range g.Steps {
		text := st.Action.In(lang)
		if d := st.Detail.In(lang); d != "" {
			text += " " + d
		}
		if dl := st.Deadline.In(lang); dl != "" {
			text += " Deadline: " + dl + "."
		}
		add("Next steps", text, g.Sources, false)
	}
	if t := g.Timeline.In(lang); t != "" {
		add("What to expect next", t, g.Sources, false)
	}
	// Institutions get one chunk each with their channels spelled out, so
	// "who do I call" queries retrieve the number itself, not a summary of
	// it. Disputed numbers keep their disagreement flag — the answer layer
	// surfaces it rather than picking a winner.
	for _, inst := range g.Institutions {
		var b strings.Builder
		b.WriteString(inst.Name)
		b.WriteString(". ")
		b.WriteString(inst.Role.In(lang))
		disputed := false
		for _, c := range inst.Channels {
			b.WriteString(" ")
			b.WriteString(string(c.Kind))
			b.WriteString(": ")
			b.WriteString(c.Value)
			if c.TollFree {
				b.WriteString(" (toll-free)")
			}
			if c.Anonymous {
				b.WriteString(" (anonymous)")
			}
			if c.Disputed() {
				b.WriteString(" (sources disagree on this number)")
				disputed = true
			}
			if n := c.Note.In(lang); n != "" {
				b.WriteString(" — ")
				b.WriteString(n)
			}
			b.WriteString(".")
		}
		add("Where to go", b.String(), g.Sources, disputed)
	}
	return chunks
}

func chunkDocument(d resource.Document) []Chunk {
	var chunks []Chunk
	for _, p := range d.Provisions {
		text := strings.TrimSpace(p.Text.In(resource.DefaultLang))
		if text == "" {
			continue
		}
		chunks = append(chunks, Chunk{
			Kind:         KindProvision,
			DocID:        d.ID,
			Jurisdiction: d.Jurisdiction,
			Ref:          p.Ref,
			Section:      p.Heading.In(resource.DefaultLang),
			Lang:         resource.DefaultLang,
			Text:         text,
			Sources:      sourceRefs(p.Sources),
			Disputed:     p.Disputed(),
		})
	}
	return chunks
}
