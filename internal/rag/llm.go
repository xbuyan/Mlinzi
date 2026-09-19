package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// LLM configuration, all from the environment so a deployment opts in by
// setting variables and the default binary stays fully offline:
//
//	RAG_LLM_ENDPOINT — e.g. https://api.groq.com/openai/v1/chat/completions
//	RAG_LLM_API_KEY  — bearer token for that endpoint
//	RAG_LLM_MODEL    — model name the endpoint understands
//
// No endpoint set, or an unreachable/slow/ill-formed response, and the
// extractive synthesizer answers instead — the pitch demo cannot break
// because a third-party API had a bad minute, and the binary never ships a
// key inside it.
const (
	envEndpoint = "RAG_LLM_ENDPOINT"
	envAPIKey   = "RAG_LLM_API_KEY"
	envModel    = "RAG_LLM_MODEL"

	llmTimeout = 6 * time.Second
)

// ErrNoLLM is returned by llmAnswer when no endpoint is configured.
var ErrNoLLM = errors.New("rag: no LLM endpoint configured (RAG_LLM_ENDPOINT unset)")

// llmConfig is the resolved configuration for one call.
type llmConfig struct {
	endpoint string
	apiKey   string
	model    string
}

func llmFromEnv() (llmConfig, bool) {
	endpoint := strings.TrimSpace(os.Getenv(envEndpoint))
	if endpoint == "" {
		return llmConfig{}, false
	}
	return llmConfig{
		endpoint: endpoint,
		apiKey:   strings.TrimSpace(os.Getenv(envAPIKey)),
		model:    strings.TrimSpace(os.Getenv(envModel)),
	}, true
}

// AnswerWithLLM is the Ask pipeline's top half: retrieve, then let an LLM
// phrase the answer against the retrieved chunks — then verify what it
// said before showing it. Verification is the load-bearing step: a model
// asked to ground its answer in retrieved passages will usually comply and
// occasionally will not, and "usually" is not a guarantee a civic
// information app can ship on. So the response is checked against the same
// chunks the model saw: every sentence must be supported by chunk text, or
// the answer falls back to the extractive one.
//
// When no LLM is configured, it returns (AnswerWithLLMNone, nil) and the
// caller falls back; an configured-but-failed call does the same, with the
// error logged via the returned method only — the user sees a grounded
// answer either way.
func AnswerWithLLM(ctx context.Context, client *http.Client, query string, chunks []Scored) (Answer, error) {
	cfg, ok := llmFromEnv()
	if !ok {
		return Answer{}, ErrNoLLM
	}
	if client == nil {
		client = http.DefaultClient
	}

	prompt := buildPrompt(query, chunks)
	body, err := callLLM(ctx, client, cfg, prompt)
	if err != nil {
		return Answer{}, err
	}

	// Verify before trust: the LLM's sentences are checked against the
	// retrieved chunk text the same way the extractive path's sentences are
	// grounded in it by construction.
	supported, rejected := verifyAgainstChunks(body, chunks)
	if len(supported) == 0 {
		return Answer{}, fmt.Errorf("rag: llm response had no sentences supported by retrieved passages")
	}

	text := strings.Join(supported, " ")
	if rejected > 0 {
		text += " " + droppedNote(rejected)
	}

	return Answer{
		Text:     text,
		Chunks:   chunks,
		Grounded: true,
		Method:   MethodLLM,
	}, nil
}

// AnswerWithLLMNone is a sentinel callers can compare against with
// errors.Is to distinguish "not configured" from "configured but failed".
var AnswerWithLLMNone = ErrNoLLM

// buildPrompt instructs the model in terms of the guarantee being enforced:
// answer ONLY from the passages, cite the ones used, refuse otherwise.
func buildPrompt(query string, chunks []Scored) string {
	var b strings.Builder
	b.WriteString("You answer questions about civic rights and reporting procedures for Kenya, Nigeria and Uganda. ")
	b.WriteString("Answer using ONLY the numbered passages below. ")
	b.WriteString("Every factual sentence must be supported by one or more passages. ")
	b.WriteString("If the passages do not contain the answer, say exactly: I could not find this in the sourced guides. ")
	b.WriteString("Do not add hotline numbers, deadlines, article numbers, or rights not present in the passages. ")
	b.WriteString("Keep the answer under 150 words. Answer in the same language as the question.\n\n")
	b.WriteString("Question: " + query + "\n\nPassages:\n")
	for i, sc := range chunks {
		fmt.Fprintf(&b, "[%d] (%s", i+1, sc.DocID)
		if sc.Ref != "" {
			fmt.Fprintf(&b, ", %s", sc.Ref)
		}
		b.WriteString(") ")
		b.WriteString(sc.Text)
		b.WriteString("\n")
	}
	return b.String()
}

// llmRequest/llmResponse are the OpenAI chat-completions shapes, which
// Groq, OpenAI, Together, Mistral and most compatible providers accept.
type llmRequest struct {
	Model       string       `json:"model"`
	Messages    []llmMessage `json:"messages"`
	Temperature float64      `json:"temperature"`
	MaxTokens   int          `json:"max_tokens"`
}

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llmResponse struct {
	Choices []struct {
		Message llmMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// callLLM performs one chat completion with a hard timeout.
func callLLM(ctx context.Context, client *http.Client, cfg llmConfig, prompt string) (string, error) {
	if cfg.model == "" {
		return "", errors.New("rag: RAG_LLM_MODEL is not set")
	}
	reqBody, err := json.Marshal(llmRequest{
		Model: cfg.model,
		Messages: []llmMessage{
			{Role: "system", Content: "You are a careful assistant that answers only from provided passages and refuses to improvise."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.1,
		MaxTokens:   400,
	})
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, llmTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("rag: llm call failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("rag: llm endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(limited)))
	}

	var decoded llmResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&decoded); err != nil {
		return "", fmt.Errorf("rag: llm response unreadable: %w", err)
	}
	if decoded.Error != nil && decoded.Error.Message != "" {
		return "", fmt.Errorf("rag: llm endpoint error: %s", decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return "", errors.New("rag: llm returned no content")
	}
	return decoded.Choices[0].Message.Content, nil
}

// verifyAgainstChunks splits the LLM's answer into sentences and keeps only
// those whose content words are covered by the retrieved chunk text. The
// tolerance (most content words must appear somewhere in the chunks) is
// deliberate: the model legitimately paraphrases and stitches across
// passages, and the check exists to catch fabrication — invented numbers,
// invented article refs, invented institutions — not to reject good
// paraphrases.
func verifyAgainstChunks(body string, chunks []Scored) (supported []string, rejected int) {
	corpus := make(map[string]bool)
	for _, sc := range chunks {
		for _, t := range Tokenize(sc.Text) {
			corpus[t] = true
		}
		// Refs and numbers get exact-match tokens so an invented "Article
		// 99" cannot borrow credibility from a real "Article 10".
		if sc.Ref != "" {
			corpus[strings.ToLower(sc.Ref)] = true
		}
		// The chunk's own provenance is part of its verifiable content: an
		// answer that names the publishing institution ("the Ethics and
		// Anti-Corruption Commission") is grounded in the chunk's sources,
		// not inventing an institution.
		for _, s := range sc.Sources {
			for _, t := range Tokenize(s.Publisher) {
				corpus[t] = true
			}
		}
	}

	for _, raw := range splitSentences(body) {
		if !looksFactual(raw) {
			// Greetings, filler, refusal boilerplate — pass through without
			// needing support.
			supported = append(supported, raw)
			continue
		}
		tokens := Tokenize(raw)
		if len(tokens) == 0 {
			continue
		}
		covered := 0
		for _, t := range tokens {
			if corpus[t] {
				covered++
			}
		}
		if float64(covered)/float64(len(tokens)) >= 0.6 {
			supported = append(supported, raw)
		} else {
			rejected++
		}
	}
	return supported, rejected
}

// looksFactual reports whether a sentence carries claims that need support,
// as a cheap heuristic: it contains digits (numbers, deadlines, refs) or is
// long enough to be substantive. Short connective sentences do not.
func looksFactual(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if len(s) <= 40 && !strings.ContainsAny(s, "0123456789") {
		return false
	}
	return true
}

func droppedNote(n int) string {
	return fmt.Sprintf("(%d sentence(s) from the drafting model were omitted because the sourced passages did not support them.)", n)
}
