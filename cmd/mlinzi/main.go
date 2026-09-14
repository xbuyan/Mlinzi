// Command mlinzi is a terminal browser for the civic guide store.
//
//	mlinzi list [jurisdiction]
//	mlinzi search <term>
//	mlinzi show <guide-id>
//
// This is a presentation layer only: every guarantee (sourcing, validation,
// search) already lives in internal/guide and is already tested there. This
// file just renders it.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	mlinziassets "github.com/xbuyan/mlinzi"
	"github.com/xbuyan/mlinzi/internal/guardian"
	"github.com/xbuyan/mlinzi/internal/guide"
	"github.com/xbuyan/mlinzi/internal/report"
)

const defaultJurisdiction = "KE"

func main() {
	s, err := guide.Load(mlinziassets.DataFS, "data")
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load guide data:", err)
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "list":
		j := defaultJurisdiction
		if len(os.Args) > 2 {
			j = strings.ToUpper(os.Args[2])
		}
		cmdList(s, j)
	case "search":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: mlinzi search <term>")
			os.Exit(1)
		}
		cmdSearch(s, defaultJurisdiction, strings.Join(os.Args[2:], " "))
	case "show":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: mlinzi show <guide-id>")
			os.Exit(1)
		}
		cmdShow(s, os.Args[2])
	case "demo-report":
		cmdDemoReport()
	case "demo-escalation":
		cmdDemoEscalation()
	case "demo-full":
		cmdDemoFull()
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`Mlinzi — civic reporting guide

Usage:
  mlinzi list [jurisdiction]     List every guide (default: KE)
  mlinzi search <term>           Search guides by keyword, any language
  mlinzi show <guide-id>         Show full detail for one guide
  mlinzi demo-report             Walk through Layer 2: submit, status trail,
                                  chain verification, in one run
  mlinzi demo-escalation         Walk through Layer 3: check-ins, a missed
                                  check-in, and guardian-triggered release
  mlinzi demo-full               The complete story: know, report, protect`)
}

func cmdList(s *guide.Store, jurisdiction string) {
	guides := s.ByJurisdiction(jurisdiction)
	if len(guides) == 0 {
		fmt.Printf("No guides loaded for %s.\n", jurisdiction)
		return
	}
	fmt.Printf("%d guide(s) for %s:\n\n", len(guides), jurisdiction)
	for _, g := range guides {
		printSummaryLine(g)
	}
}

func cmdSearch(s *guide.Store, jurisdiction, term string) {
	results := s.Search(jurisdiction, term)
	if len(results) == 0 {
		fmt.Printf("No guides match %q.\n", term)
		return
	}
	fmt.Printf("%d result(s) for %q:\n\n", len(results), term)
	for _, g := range results {
		printSummaryLine(g)
	}
}

func printSummaryLine(g guide.Guide) {
	fmt.Printf("  %-30s %s\n", g.ID, g.Title.In("en"))
	fmt.Printf("  %-30s %s\n\n", "", g.Summary.In("en"))
}

func cmdShow(s *guide.Store, id string) {
	g, err := s.Get(id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	langs := strings.Join(g.Langs(), ", ")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("%s  [%s]\n", g.Title.In("en"), g.ID)
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Available in: %s   Last verified: %s", langs, g.LastVerified)
	if g.StaleAfter(90*24*time.Hour, time.Now()) {
		fmt.Print("  [older than 90 days — verify before relying on this]")
	}
	fmt.Println()
	fmt.Println()

	fmt.Println(g.Summary.In("en"))
	fmt.Println()

	if len(g.Rights) > 0 {
		fmt.Println("Your rights:")
		for _, r := range g.Rights {
			fmt.Printf("  - %s\n", r.In("en"))
		}
		fmt.Println()
	}

	if len(g.Evidence) > 0 {
		fmt.Println("Bring, if you can:")
		for _, e := range g.Evidence {
			fmt.Printf("  - %s\n", e.In("en"))
		}
		fmt.Println()
	}

	fmt.Println("Next steps:")
	for i, st := range g.Steps {
		mark := " "
		if st.Critical {
			mark = "!"
		}
		fmt.Printf(" %s %d. %s\n", mark, i+1, st.Action.In("en"))
		if d := st.Detail.In("en"); d != "" {
			fmt.Printf("      %s\n", d)
		}
		if dl := st.Deadline.In("en"); dl != "" {
			fmt.Printf("      Deadline: %s\n", dl)
		}
	}
	fmt.Println()

	fmt.Println("Where to go:")
	for _, inst := range g.Institutions {
		fmt.Printf("\n  %s\n", inst.Name)
		fmt.Printf("  %s\n", inst.Role.In("en"))
		for _, c := range inst.Channels {
			flags := flagString(c)
			fmt.Printf("    [%s] %s%s\n", c.Kind, c.Value, flags)
			if n := c.Note.In("en"); n != "" {
				fmt.Printf("        %s\n", n)
			}
		}
	}
	fmt.Println()

	if t := g.Timeline.In("en"); t != "" {
		fmt.Println("What to expect next:")
		fmt.Println(" ", t)
		fmt.Println()
	}

	fmt.Println("Sources:")
	for _, src := range g.Sources {
		fmt.Printf("  - %s (%s) — %s\n", src.Publisher, src.Confidence, src.URL)
	}
}

func flagString(c guide.Channel) string {
	var flags []string
	if c.TollFree {
		flags = append(flags, "toll-free")
	}
	if c.Kind.LowBandwidth() {
		flags = append(flags, "no data needed")
	}
	if c.Anonymous {
		flags = append(flags, "anonymous")
	}
	if c.Disputed() {
		flags = append(flags, "SOURCES DISAGREE — see note")
	}
	if len(flags) == 0 {
		return ""
	}
	return "  [" + strings.Join(flags, ", ") + "]"
}

func cmdDemoReport() {
	// A single-process walkthrough of Layer 2. There is no persistence
	// layer yet (that is intentionally deferred, alongside encryption at
	// rest, to Layer 3), so this demonstrates the lifecycle in one run
	// rather than pretending a report survives between separate CLI
	// invocations when nothing is actually saved to disk.
	s := report.NewStore()

	fmt.Println("--- Filing an anonymous report ---")
	r, err := s.Submit("policing", "ke-police-misconduct",
		"Officer at Kondele stage demanded KES 200 to let me pass with my motorbike, 14 Sep evening.")
	if err != nil {
		fmt.Fprintln(os.Stderr, "submit failed:", err)
		os.Exit(1)
	}
	fmt.Printf("Report ID:          %s\n", r.ID)
	fmt.Printf("Status:             %s\n", r.Status)
	fmt.Printf("Verification code:  %s   (keep this — it proves what you submitted, without giving your name)\n\n", r.VerificationCode)

	fmt.Println("--- IPOA acknowledges it ---")
	if _, err := s.Advance(r.ID, report.Acknowledged, "ipoa", "Received. Case opened for investigation."); err != nil {
		fmt.Fprintln(os.Stderr, "advance failed:", err)
		os.Exit(1)
	}

	fmt.Println("--- An attempt to skip straight to 'resolved' from 'submitted' would be rejected ---")
	r2, _ := s.Submit("policing", "ke-police-misconduct", "second, unrelated report")
	if _, err := s.Advance(r2.ID, report.Resolved, "ipoa", "closing quietly"); err != nil {
		fmt.Printf("  rejected, as expected: %v\n\n", err)
	}

	fmt.Println("--- IPOA resolves the original case ---")
	if _, err := s.Advance(r.ID, report.Resolved, "ipoa", "Officer identified and referred for disciplinary action."); err != nil {
		fmt.Fprintln(os.Stderr, "advance failed:", err)
		os.Exit(1)
	}

	final, _ := s.Get(r.ID)
	fmt.Printf("\n--- Status trail for %s ---\n", r.ID)
	fmt.Printf("  [submitted]    %s\n", final.SubmittedAt.Format(time.RFC3339))
	for _, ev := range final.Events {
		fmt.Printf("  [%-11s] %s   actor=%-6s note=%q   (ledger entry #%d, hash %s...)\n",
			ev.Status, ev.Timestamp.Format(time.RFC3339), ev.Actor, ev.Note, ev.EntrySeq, ev.EntryHash[:10])
	}

	fmt.Println("\n--- Independent checks ---")
	if err := s.VerifyChain(); err != nil {
		fmt.Printf("  chain integrity: FAILED — %v\n", err)
	} else {
		fmt.Println("  chain integrity: OK — no entry has been altered, reordered, or backdated")
	}

	ok, _ := s.VerifyReceipt(r.ID, r.VerificationCode)
	fmt.Printf("  receipt check with the real code:  %v\n", ok)
	bad, _ := s.VerifyReceipt(r.ID, "0000000000")
	fmt.Printf("  receipt check with a fabricated code: %v\n", bad)
}

func cmdDemoEscalation() {
	// A single-process walkthrough of Layer 3: check-ins, a missed
	// check-in, and guardian-triggered release. As with demo-report, there
	// is no persistence layer yet, so this runs the whole lifecycle in one
	// process rather than pretending state survives between invocations
	// when nothing is saved to disk.
	s := guardian.NewStore()

	fmt.Println("--- A reporter opens a case, naming three guardians (2-of-3 threshold) ---")
	key, err := guardian.ReleaseKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, "key generation failed:", err)
		os.Exit(1)
	}
	c, shares, err := s.NewCase("rpt_23f0714766d1765d", key,
		[]string{"lawyer", "journalist", "family"}, 2, 48*time.Hour)
	if err != nil {
		fmt.Fprintln(os.Stderr, "case creation failed:", err)
		os.Exit(1)
	}
	fmt.Printf("Case ID:    %s\n", c.ID)
	fmt.Printf("Guardians:  %s\n", strings.Join(c.GuardianIDs, ", "))
	fmt.Printf("Threshold:  %d of %d\n", c.Threshold, len(c.GuardianIDs))
	fmt.Println("Shares generated and handed to each guardian out of band — the case itself now holds none of them.")
	fmt.Println()

	fmt.Println("--- The reporter checks in normally, on schedule ---")
	if err := s.CheckIn(c.ID); err != nil {
		fmt.Fprintln(os.Stderr, "check-in failed:", err)
		os.Exit(1)
	}
	fmt.Println("Check-in recorded and logged to the ledger.")
	fmt.Println()

	fmt.Println("--- Time passes. The reporter goes silent past the check-in window ---")
	overdueAt := time.Now().Add(72 * time.Hour) // past the 48h interval
	if err := s.Escalate(c.ID, overdueAt); err != nil {
		fmt.Fprintln(os.Stderr, "escalation failed:", err)
		os.Exit(1)
	}
	fmt.Println("Case escalated. Guardians are now able to submit their shares.")
	fmt.Println()

	fmt.Println("--- A single guardian alone cannot release anything ---")
	_, err = s.SubmitShare(c.ID, "lawyer", shares[0])
	fmt.Printf("Lawyer submits their share: %v\n\n", err)

	fmt.Println("--- A second guardian submits independently, crossing the 2-of-3 threshold ---")
	recovered, err := s.SubmitShare(c.ID, "journalist", shares[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "reconstruction failed:", err)
		os.Exit(1)
	}
	match := "does not match"
	if string(recovered) == string(key) {
		match = "matches exactly"
	}
	fmt.Printf("Release key reconstructed from 2 independent shares — %s the original.\n\n", match)

	final, _ := s.Get(c.ID)
	fmt.Printf("Final case status: %s\n\n", final.Status)

	fmt.Println("--- Independent check ---")
	if err := s.VerifyChain(); err != nil {
		fmt.Printf("chain integrity: FAILED — %v\n", err)
	} else {
		fmt.Println("chain integrity: OK — every check-in, escalation, and release step is verifiable and untampered")
	}
}

func cmdDemoFull() {
	// The full story in one run: a person finds their situation (Layer 1),
	// reports it anonymously (Layer 2), and is protected if reporting puts
	// them at risk (Layer 3). This is the walkthrough the demo video follows
	// shot for shot.
	guides, err := guide.Load(mlinziassets.DataFS, "data")
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load guide data:", err)
		os.Exit(1)
	}
	reports := report.NewStore()
	cases := guardian.NewStore()

	fmt.Println("========================================")
	fmt.Println(" MLINZI — full walkthrough")
	fmt.Println("========================================")
	fmt.Println()

	fmt.Println("STEP 1 — Know")
	fmt.Println("A person searches for what happened to them.")
	fmt.Println()
	g, err := guides.Get("ke-police-misconduct")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("  %q\n", g.Title.In("en"))
	fmt.Printf("  Route: %s (toll-free, works with no data)\n", g.Institutions[0].Name)
	fmt.Printf("  Last verified: %s\n\n", g.LastVerified)

	fmt.Println("STEP 2 — Report")
	fmt.Println("They file anonymously, linked to that guide.")
	fmt.Println()
	r, err := reports.Submit("policing", g.ID,
		"Officer at Kondele stage demanded KES 200 to let me pass with my motorbike, 14 Sep evening.")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("  Report %s filed against guide %q\n", r.ID, r.GuideID)
	fmt.Printf("  Verification code: %s (theirs to keep — no name required)\n\n", r.VerificationCode)

	fmt.Println("STEP 3 — Protect")
	fmt.Println("Because this is a safety report, they set a dead-man's switch.")
	fmt.Println()
	key, _ := guardian.ReleaseKey()
	c, shares, err := cases.NewCase(r.ID, key, []string{"lawyer", "journalist", "family"}, 2, 48*time.Hour)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("  Case %s protects report %s\n", c.ID, c.ReportID)
	fmt.Printf("  Guardians: %s (2-of-3 must act together to release)\n\n", strings.Join(c.GuardianIDs, ", "))

	fmt.Println("--- Time passes. They go silent. ---")
	fmt.Println()
	overdueAt := time.Now().Add(72 * time.Hour)
	cases.Escalate(c.ID, overdueAt)
	cases.SubmitShare(c.ID, "lawyer", shares[0])
	recovered, err := cases.SubmitShare(c.ID, "journalist", shares[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	match := string(recovered) == string(key)
	fmt.Printf("Two guardians combine independently. Key reconstructed: %v.\n", match)
	fmt.Println("The report — and who was responsible for it going unresolved — reaches the guardians.")
	fmt.Println()

	fmt.Println("--- Every step above is independently checkable ---")
	if err := reports.VerifyChain(); err != nil {
		fmt.Println("report chain: FAILED")
	} else {
		fmt.Println("report chain:   OK")
	}
	if err := cases.VerifyChain(); err != nil {
		fmt.Println("guardian chain: FAILED")
	} else {
		fmt.Println("guardian chain: OK")
	}
}
