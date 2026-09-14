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
	"github.com/xbuyan/mlinzi/internal/guide"
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
  mlinzi show <guide-id>         Show full detail for one guide`)
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
