package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	mlinziassets "github.com/xbuyan/mlinzi"
	"github.com/xbuyan/mlinzi/internal/guide"
	"github.com/xbuyan/mlinzi/internal/ledger"
	"github.com/xbuyan/mlinzi/internal/persist"
	"github.com/xbuyan/mlinzi/internal/rag"
	"github.com/xbuyan/mlinzi/internal/report"
	"github.com/xbuyan/mlinzi/internal/resource"
)

// cmdDemoRAG walks the Ask pipeline the way a judge would drive it from the
// web UI: a question the corpus answers well (with citations), a question it
// answers from the law itself, a question in Kiswahili, and a question it
// must refuse — the abstention being the feature that makes the rest
// trustworthy. Every claim printed here is produced by the same code the
// /ask page runs.
func cmdDemoRAG() {
	guides, err := guide.Load(mlinziassets.DataFS, "data")
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load guide data:", err)
		os.Exit(1)
	}
	resources, err := resource.Load(mlinziassets.ResourceFS, "resources")
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load resources:", err)
		os.Exit(1)
	}

	fmt.Println("========================================")
	fmt.Println(" MLINZI — Ask Mlinzi (RAG) walkthrough")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("The corpus: every sourced guide section and every checked legal")
	fmt.Println("provision, chunked with the provenance it carries. The answer")
	fmt.Println("engine can only speak from these — it cannot improvise a number,")
	fmt.Println("a law, or a right the corpus does not contain.")
	fmt.Println()

	bEn := rag.Build(guides, resources, "en")
	bSw := rag.Build(guides, resources, "sw")
	fmt.Printf("Indexed %d chunks in English and Kiswahili, from %d guides and %d legal documents.\n\n",
		len(bEn.Chunks), guides.Len(), resources.Len())

	questions := []struct {
		query string
		lang  string
		note  string
		useSw bool
	}{
		{"Can I report a bribe without giving my name?", "en", "the everyday case — a procedure question", false},
		{"What does the constitution say about freedom of expression?", "en", "answered from the primary law itself", false},
		// Lexically anchored in the Kiswahili corpus ("rushwa", "ripoti",
		// "jina") the same way the English questions are anchored in theirs —
		// retrieval is lexical, and a question is answerable in a language
		// when its words are the words the data uses.
		{"Nimeombiwa rushwa. Naweza kuripoti bila jina?", "sw", "a Kiswahili question, answered from Kiswahili data", true},
		{"What are the lottery numbers for next week?", "en", "outside the corpus — the refusal case", false},
	}

	for i, q := range questions {
		fmt.Println(strings.Repeat("-", 60))
		fmt.Printf("Q%d (%s): %s\n", i+1, q.note, q.query)
		fmt.Println(strings.Repeat("-", 60))
		builder := bEn
		if q.useSw {
			builder = bSw
		}
		ans := rag.Ask(context.Background(), http.DefaultClient, builder, q.query, "", q.lang)
		fmt.Println(wrap(ans.Text, 60))
		fmt.Printf("\n[method: %s | grounded: %v | passages used: %d]\n", ans.Method, ans.Grounded, len(ans.Chunks))
		for _, sc := range ans.Chunks {
			ref := sc.Section
			if sc.Ref != "" {
				ref = sc.Ref + " — " + sc.Section
			}
			fmt.Printf("  -> %s (%s)\n", ref, sc.DocID)
			for _, s := range sc.Sources {
				fmt.Printf("     source: %s (%s) %s\n", s.Publisher, s.Confidence, s.URL)
			}
		}
		fmt.Println()
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("The abstention is the guarantee: a system that can say \"this is")
	fmt.Println("not in the sourced corpus\" is one whose answers you can act on.")
	fmt.Println("When an LLM endpoint is configured (RAG_LLM_ENDPOINT /")
	fmt.Println("RAG_LLM_API_KEY / RAG_LLM_MODEL), it phrases the answer against")
	fmt.Println("these same passages — and every sentence is verified against them,")
	fmt.Println("with anything unsupported dropped and the extractive answer")
	fmt.Println("shipped instead. The pitch demo works with the network cable")
	fmt.Println("pulled out.")
}

// cmdDemoPersistence proves the durability story end to end, including the
// part that makes it credible: tampering with the snapshot on disk and
// watching the restore refuse it, exactly as the chain verification is
// designed to.
func cmdDemoPersistence() {
	dir, err := os.MkdirTemp("", "mlinzi-demo-persistence")
	if err != nil {
		fmt.Fprintln(os.Stderr, "temp dir:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	fmt.Println("========================================")
	fmt.Println(" MLINZI — persistence walkthrough")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Printf("Data directory for this run: %s\n\n", dir)

	fmt.Println("--- Boot 1: file two anonymous reports ---")
	reports := report.NewStore()
	r1, err := reports.Submit("bribery", "ke-bribery-public-service",
		"Officer demanded KES 200 at the counter, 14 Sep.")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	r2, err := reports.Submit("policing", "ke-police-misconduct",
		"Detained overnight without charge, 15 Sep.")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := reports.Advance(r1.ID, report.Acknowledged, "eacc", "Received and logged."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("  %s  acknowledged by eacc\n", r1.ID)
	fmt.Printf("  %s  submitted\n", r2.ID)

	if err := persist.SaveJSON(filepath.Join(dir, "reports-ledger.json"), ledgerSnapshot(reports)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("\nSnapshot written to disk. Every status change, check-in and")
	fmt.Println("escalation in the app writes the same way, atomically.")

	fmt.Println("\n--- Boot 2: the restart restores from the snapshot ---")
	var snap ledgerSnapshotFile
	if err := persist.LoadJSON(filepath.Join(dir, "reports-ledger.json"), &snap); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	l, err := ledger.NewFromEntries(snap.Entries)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	restored, err := report.NewStoreFromLedger(l)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	got, err := restored.Get(r1.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("  %s -> status %s, verification code %s (identical: derived from the restored ledger)\n",
		got.ID, got.Status, got.VerificationCode)
	got2, err := restored.Get(r2.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("  %s -> status %s\n", got2.ID, got2.Status)

	fmt.Println("\n--- Now someone edits history: the report content is rewritten on disk ---")
	entries := snap.Entries
	original := string(entries[0].Data)
	tampered := strings.Replace(original, "KES 200", "KES 0.00 — nothing happened", 1)
	if tampered == original {
		fmt.Println("  (test setup could not alter the payload — nothing to demonstrate)")
		return
	}
	entries[0].Data = []byte(tampered)

	fmt.Println("\n--- Boot 3: the restore refuses the altered snapshot ---")
	if _, err := ledger.NewFromEntries(entries); err != nil {
		fmt.Printf("  REFUSED: %v\n", err)
	} else {
		fmt.Println("  NOT REFUSED — this would be a serious bug")
	}
	fmt.Println()
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("This is the difference between \"we save to disk\" and")
	fmt.Println("\"persistence that keeps the tamper-evidence promise\": the")
	fmt.Println("ledger's chain verification runs over the restored entries, so")
	fmt.Println("editing history on disk fails loudly instead of loading quietly.")
	fmt.Println("Known, stated limits: the guardian platform never persists key")
	fmt.Println("material (shares are re-submitted after a restart), and evidence")
	fmt.Println("files are restored only after re-verifying their content hashes.")
}

// ledgerSnapshotFile is the on-disk shape of the report ledger, mirroring
// the web app's persistedLedger.
type ledgerSnapshotFile struct {
	Entries []ledger.Entry `json:"entries"`
}

func ledgerSnapshot(s *report.Store) ledgerSnapshotFile {
	return ledgerSnapshotFile{Entries: s.Ledger().Entries()}
}

// wrap does crude word wrapping for terminal output.
func wrap(s string, width int) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var b strings.Builder
	line := 0
	for _, w := range words {
		if line > 0 && line+1+len(w) > width {
			b.WriteByte('\n')
			b.WriteString("  ")
			line = 0
		}
		if line > 0 {
			b.WriteByte(' ')
			line++
		}
		b.WriteString(w)
		line += len(w)
	}
	return b.String()
}
