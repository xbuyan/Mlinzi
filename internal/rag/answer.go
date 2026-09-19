package rag

import (
	"fmt"
	"strings"
)

// Answer is the finished product: what the ask page shows, with the
// provenance that makes it checkable.
type Answer struct {
	// Text is the answer itself. When Grounded is false it is an explicit
	// refusal to answer, not a guess.
	Text string
	// Chunks are the retrieved chunks the answer was built from.
	Chunks []Scored
	// Grounded reports whether the corpus actually supported an answer.
	Grounded bool
	// Method records how the text was produced, so the page can be honest
	// about it: composed by the deterministic synthesizer, or phrased by an
	// LLM against the same retrieved chunks.
	Method string
}

// Answer methods, surfaced on the page rather than hidden in a log.
const (
	MethodExtractive = "extractive"
	MethodLLM        = "llm"
)

// Synthesize builds the grounded answer from retrieved chunks, without any
// language model: sentences from the corpus, rearranged, each carrying the
// citation of the chunk it came from.
//
// This path cannot hallucinate a fact, structurally: it only emits sentences
// that literally occur in chunks retrieved from the corpus. That is why it
// is the fallback and the floor — when no LLM is configured, or the LLM is
// unreachable, or the LLM says something the retrieved chunks do not support
// (see llm.go's verification), this is what the user gets.
func Synthesize(query string, chunks []Scored) Answer {
	if len(chunks) == 0 {
		return Answer{
			Text:     notFoundText(),
			Chunks:   chunks,
			Grounded: false,
			Method:   MethodExtractive,
		}
	}

	lead := chunks[0]
	var b strings.Builder
	b.WriteString(introSentence(lead))

	// Each cited sentence quotes its chunk's own text, cut to a couple of
	// sentences so the answer stays readable. The citation — (source: …) —
	// travels with every quoted passage, which is the property that makes
	// the answer checkable rather than merely confident. Passages are
	// deduplicated by content, not just by identity: the same summary text
	// retrieved under two jurisdictions must not print twice.
	seen := make(map[string]bool, len(chunks))
	used := 0
	for _, sc := range chunks {
		if used >= 3 {
			break
		}
		passage := trimmed(sc.Text)
		if seen[passage] {
			continue
		}
		seen[passage] = true
		used++
		b.WriteString(" ")
		b.WriteString(passage)
		b.WriteString(" ")
		b.WriteString(cite(sc.Chunk))
	}

	// Disagreement is surfaced, never resolved silently — the same rule the
	// guide page applies to a disputed hotline number. Carried per-chunk by
	// the chunker, so a disputed phone number flags the answer that cites
	// it without smearing unrelated sections.
	disputed := false
	for _, sc := range chunks {
		if sc.Disputed {
			disputed = true
			break
		}
	}
	if disputed {
		b.WriteString(" ")
		b.WriteString(disputeNote())
	}

	return Answer{
		Text:     b.String(),
		Chunks:   chunks,
		Grounded: true,
		Method:   MethodExtractive,
	}
}

// introSentence leads with the strongest chunk, naming the guide or
// article that answered the question — the citation first, before any
// claim, which is the order the whole app argues for.
func introSentence(lead Scored) string {
	if lead.Ref != "" {
		return fmt.Sprintf("From %s, %s (%s):", lead.Jurisdiction, lead.Section, lead.Ref)
	}
	return fmt.Sprintf("From the guide %q:", lead.DocID)
}

// trimmed cuts a chunk to its first sentences — enough to answer, small
// enough to stay readable. Sentence splitting is deliberately naive: corpus
// text is ours, and legal abbreviations that would fool a clever splitter
// are rare enough that the cost of getting it wrong (mangled statute text)
// outweighs the benefit.
func trimmed(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	sentences := splitSentences(text)
	if len(sentences) <= 2 {
		return text
	}
	return strings.Join(sentences[:2], " ")
}

func splitSentences(text string) []string {
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '.' || text[i] == '!' || text[i] == '?' {
			// Swallow trailing quotes/brackets so a sentence doesn't end
			// with a dangling paren.
			for i+1 < len(text) && strings.ContainsRune("\")]", rune(text[i+1])) {
				i++
			}
			if i+1 >= len(text) {
				out = append(out, text[start:i+1])
				start = i + 1
				break
			}
			if text[i+1] == ' ' || text[i+1] == '\n' {
				out = append(out, strings.TrimSpace(text[start:i+1]))
				start = i + 1
			}
		}
	}
	if start < len(text) {
		out = append(out, strings.TrimSpace(text[start:]))
	}
	return out
}

// cite renders a chunk's provenance inline. Provision chunks cite the
// article; guide chunks cite the guide. Every answer sentence carries one,
// because an uncited claim is exactly what this app exists to prevent.
func cite(c Chunk) string {
	for _, s := range c.Sources {
		if s.URL != "" {
			return fmt.Sprintf("(source: %s, %s)", s.Publisher, s.URL)
		}
	}
	if c.Ref != "" {
		return fmt.Sprintf("(source: %s, %s)", c.DocID, c.Ref)
	}
	return fmt.Sprintf("(source: %s)", c.DocID)
}

func disputeNote() string {
	return "Note: sources disagree on some of the details above — the guide page shows both, so you can decide what to try first."
}

func notFoundText() string {
	return "I could not find this in the sourced guides or legal documents. Rather than guess, try the search on the home page — or rephrase with the words you would use to describe the situation."
}
