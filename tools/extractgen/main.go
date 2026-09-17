// Command extractgen is a ONE-OFF data generator, not part of the shipped
// application. It exists so that the full-text constitution listings in
// resources/ are mechanically derived from published sources rather than
// retyped or recalled — the only honest way to get several hundred articles
// into the Resources Center.
//
// It is deleted after generating the data. Nothing imports it and it is not
// wired into the build.
package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Source struct {
	Publisher  string `json:"publisher"`
	URL        string `json:"url"`
	Retrieved  string `json:"retrieved"`
	Confidence string `json:"confidence"`
}

type Provision struct {
	Ref     string            `json:"ref"`
	Heading map[string]string `json:"heading"`
	Text    map[string]string `json:"text"`
	Topic   string            `json:"topic,omitempty"`
	Sources []Source          `json:"sources"`
}

type Document struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	Jurisdiction string            `json:"jurisdiction"`
	Title        map[string]string `json:"title"`
	Summary      map[string]string `json:"summary"`
	Adopted      string            `json:"adopted"`
	Publisher    string            `json:"publisher"`
	FullTextURL  string            `json:"full_text_url"`
	Caveat       map[string]string `json:"caveat,omitempty"`
	Provisions   []Provision       `json:"provisions"`
	Sources      []Source          `json:"sources"`
	LastVerified string            `json:"last_verified"`
}

const retrieved = "2026-09-17"

func fetch(url string) string {
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64)")
	resp, err := client.Do(req)
	if err != nil {
		panic(fmt.Sprintf("fetch %s: %v", url, err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	if resp.StatusCode != 200 {
		panic(fmt.Sprintf("fetch %s: status %d", url, resp.StatusCode))
	}
	return string(body)
}

var (
	reAllTags      = regexp.MustCompile(`(?s)<[^>]*>`)
	reTopicDiv     = regexp.MustCompile(`(?s)<div class="section-topic">.*?</div>`)
	reTopicSpan    = regexp.MustCompile(`(?s)<span class="topic"[^>]*>.*?</span>`)
	reAnyH3        = regexp.MustCompile(`(?s)<h3[^>]*>.*?</h3>`)
	reMarker       = regexp.MustCompile(`list-style-type: '([^']*)'`)
	reSpaces       = regexp.MustCompile(`[ \t\x{00a0}]+`)
	reEditTag      = regexp.MustCompile(`\s*\[\s*edit\s*\]\s*$`)
	reScript       = regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>`)
	reFooters      = regexp.MustCompile(`(?i)<div class="printfooter"|<div id="catlinks"|<footer`)
	reSchedule     = regexp.MustCompile(`(?i)^(schedule|schedules|(first|second|third|fourth|fifth|sixth|seventh|eighth|ninth|tenth)\s+schedule)\b`)
	reTailJunk     = regexp.MustCompile(`(?i)^(retrieved from|this work is in the public domain|public domainpublic domain|category:|privacy|terms & conditions|twitter|facebook|linkedin|email)$`)
	reScheduleCut  = regexp.MustCompile(`(?i)<h2[^>]*>\s*SCHEDULES\s*</h2>|<h3[^>]*>\s*(first|second|third|fourth|fifth|sixth|seventh|eighth|ninth|tenth)\s+schedule`)
	reNumberedText = regexp.MustCompile(`^\d{1,3}\.\s+[A-Z]`)
)

// trimToContent cuts a fetched page at its own footer marker and drops
// scripts and styles. Without this, the last article of a document swallows
// the page's JavaScript, licence text and navigation links as if they were
// part of the law.
func trimToContent(page string) string {
	if loc := reFooters.FindStringIndex(page); loc != nil {
		page = page[:loc[0]]
	}
	return reScript.ReplaceAllString(page, " ")
}

func clean(s string) string {
	s = html.UnescapeString(s)
	return strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
}

// spacedLines mirrors what a reader sees: every tag becomes a space, so
// text that sits adjacent in the HTML stays on one line, while the source's
// own line breaks survive. Heading patterns are matched against this.
func spacedLines(page string) []string {
	page = trimToContent(page)
	text := html.UnescapeString(reAllTags.ReplaceAllString(page, " "))
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if c := clean(l); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// --- Kenya: Constitute, articles at mixed heading depths ---

var reConstituteH3 = regexp.MustCompile(`<h3 class="depth-[0-9]">(\d{1,3})\.\s*([^<]*)</h3>`)

func extractConstitute(page string) []Provision {
	page = trimToContent(page)
	// The Schedules follow the last article. They are not articles, and their
	// contents would otherwise be swallowed by the article before them.
	if loc := reScheduleCut.FindStringIndex(page); loc != nil {
		page = page[:loc[0]]
	}
	locs := reConstituteH3.FindAllStringSubmatchIndex(page, -1)

	type sel struct {
		start, end int
		num        int
		heading    string
	}
	var sels []sel
	for _, m := range locs {
		n, _ := strconv.Atoi(page[m[2]:m[3]])
		sels = append(sels, sel{start: m[0], end: m[1], num: n, heading: clean(page[m[4]:m[5]])})
	}

	// Keep only the headings that continue the article sequence. Articles run
	// 1..N in order; anything numbered at or below what we have already seen
	// (the Schedules restart at 1) is not an article, and body extraction must
	// run between *selected* headings, or a rejected heading would truncate
	// the article before it.
	var selected []sel
	prev := 0
	for _, s := range sels {
		if s.num <= prev || s.num > prev+3 {
			continue
		}
		prev = s.num
		selected = append(selected, s)
	}

	var out []Provision
	for i, s := range selected {
		bodyEnd := len(page)
		if i+1 < len(selected) {
			bodyEnd = selected[i+1].start
		}
		body := page[s.end:bodyEnd]
		body = reTopicDiv.ReplaceAllString(body, " ")
		body = reTopicSpan.ReplaceAllString(body, " ")
		body = reAnyH3.ReplaceAllString(body, " ")

		var lines []string
		for _, chunk := range strings.Split(strings.ReplaceAll(body, "<li", "\n<li"), "\n") {
			marker := ""
			if strings.HasPrefix(strings.TrimSpace(chunk), "<li") {
				if m := reMarker.FindStringSubmatch(chunk); m != nil {
					marker = strings.TrimSpace(m[1])
				}
			}
			t := clean(reAllTags.ReplaceAllString(chunk, " "))
			// The Schedules follow the last article and are not articles.
			if reSchedule.MatchString(t) || reTailJunk.MatchString(t) {
				break
			}
			if t == "" || t == "." {
				continue
			}
			if marker != "" {
				t = marker + " " + t
			}
			lines = append(lines, t)
		}
		if len(lines) == 0 {
			continue
		}
		out = append(out, Provision{
			Ref:     "Article " + strconv.Itoa(s.num),
			Heading: map[string]string{"en": s.heading},
			Text:    map[string]string{"en": strings.Join(lines, "\n")},
		})
	}
	return out
}

// --- Uganda and Nigeria: Wikisource rendered text ---

var (
	reUgandaHead  = regexp.MustCompile(`^(\d{1,3})\.\s+([A-Z].*)$`)
	reNigeriaHead = regexp.MustCompile(`^\((\d{1,3})\)\s+(.+?)\s*\[\s*edit\s*\]$`)
)

// extractNumbered walks lines looking for headings and accepts one as a new
// provision only when its number continues the sequence already seen. That is
// what keeps schedule items, footnotes and cross-references inside article
// bodies from being mistaken for the start of a new article.
func extractNumbered(lines []string, re *regexp.Regexp, refPrefix string, stopAtNumberedText bool) []Provision {
	type head struct {
		num     int
		heading string
		line    int
	}
	var heads []head
	prev := 0
	for i, l := range lines {
		m := re.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= prev || n > prev+3 {
			continue
		}
		prev = n
		heads = append(heads, head{num: n, heading: clean(m[2]), line: i})
	}

	var out []Provision
	for i, h := range heads {
		end := len(lines)
		if i+1 < len(heads) {
			end = heads[i+1].line
		}
		var body []string
		for _, l := range lines[h.line+1 : end] {
			l = clean(reEditTag.ReplaceAllString(l, ""))
			if l == "" || l == "↑" {
				continue
			}
			if reTailJunk.MatchString(l) || reSchedule.MatchString(l) {
				break
			}
			// Some transcriptions switch numbering style part-way through; a
			// "112. Text" line is the next section, not this one's body.
			if stopAtNumberedText && reNumberedText.MatchString(l) {
				break
			}
			body = append(body, l)
		}
		if len(body) == 0 {
			continue
		}
		out = append(out, Provision{
			Ref:     refPrefix + strconv.Itoa(h.num),
			Heading: map[string]string{"en": h.heading},
			Text:    map[string]string{"en": strings.Join(body, "\n")},
		})
	}
	return out
}

func sourceFor(publisher, url string) []Source {
	return []Source{{Publisher: publisher, URL: url, Retrieved: retrieved, Confidence: "secondary"}}
}

func report(doc Document) {
	fmt.Printf("\n=== %s: %d provisions\n", doc.ID, len(doc.Provisions))
	var nums []int
	empty := 0
	for _, p := range doc.Provisions {
		f := strings.Fields(p.Ref)
		if n, err := strconv.Atoi(f[len(f)-1]); err == nil {
			nums = append(nums, n)
		}
		if strings.TrimSpace(p.Text["en"]) == "" {
			empty++
		}
	}
	sort.Ints(nums)
	if len(nums) == 0 {
		return
	}
	seen := map[int]bool{}
	for _, n := range nums {
		seen[n] = true
	}
	var missing []int
	for n := nums[0]; n <= nums[len(nums)-1]; n++ {
		if !seen[n] {
			missing = append(missing, n)
		}
	}
	fmt.Printf("    refs %d..%d | empty text %d | missing %v\n", nums[0], nums[len(nums)-1], empty, missing)
	fmt.Printf("    first: %s | %s\n", doc.Provisions[0].Ref, doc.Provisions[0].Heading["en"])
	fmt.Printf("    last:  %s | %s\n", doc.Provisions[len(doc.Provisions)-1].Ref, doc.Provisions[len(doc.Provisions)-1].Heading["en"])
	fmt.Printf("    sample body: %q\n", truncate(doc.Provisions[0].Text["en"], 160))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func write(doc Document) {
	report(doc)
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		panic(err)
	}
	b = append(b, '\n')
	path := "resources/" + doc.ID + ".json"
	if err := os.WriteFile(path, b, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("    wrote %s (%d KB)\n", path, len(b)/1024)
}

func loadTopics(path string) map[string]string {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("note: no existing %s; no curated topics carried over\n", path)
		return map[string]string{}
	}
	var d Document
	if err := json.Unmarshal(b, &d); err != nil {
		panic(err)
	}
	out := map[string]string{}
	for _, p := range d.Provisions {
		if p.Topic != "" {
			out[p.Ref] = p.Topic
		}
	}
	fmt.Printf("carried over %d curated topic(s) from %s\n", len(out), path)
	return out
}

func applyTopics(ps []Provision, topics map[string]string) {
	for i := range ps {
		if t, ok := topics[ps[i].Ref]; ok {
			ps[i].Topic = t
		}
	}
}

func main() {
	// --- Kenya: full text, Constitute (headings at mixed depths) ---
	keURL := "https://www.constituteproject.org/constitution/Kenya_2010"
	ke := extractConstitute(fetch(keURL))
	applyTopics(ke, loadTopics("resources/kenya-constitution.json"))
	for i := range ke {
		ke[i].Sources = sourceFor("Comparative Constitutions Project (Constitute)", keURL)
	}
	write(Document{
		ID:           "kenya-constitution",
		Kind:         "constitution",
		Jurisdiction: "KE",
		Title:        map[string]string{"en": "The Constitution of Kenya, 2010"},
		Summary: map[string]string{"en": "Kenya's supreme law, promulgated on 27 August 2010 — every article, in order. Chapter Four is the " +
			"Bill of Rights; Chapter Six binds State officers to leadership and integrity rules and required Parliament to establish an " +
			"independent ethics and anti-corruption commission."},
		Adopted:     "2010-08-27",
		Publisher:   "National Council for Law Reporting (Kenya Law)",
		FullTextURL: "https://new.kenyalaw.org/",
		Caveat: map[string]string{"en": "Every article is included, taken from the text reproduced by the Comparative Constitutions Project — " +
			"not from the custodian's own copy, which could not be read when this was compiled. Confirm the exact wording against the custodian " +
			"before relying on a specific phrase. Not legal advice."},
		Provisions:   ke,
		Sources:      sourceFor("Comparative Constitutions Project (Constitute)", keURL),
		LastVerified: retrieved,
	})

	// --- Uganda: full text, Wikisource rendered page ---
	ugURL := "https://en.wikisource.org/wiki/Constitution_of_the_Republic_of_Uganda"
	ugLines := spacedLines(fetch(ugURL))
	ug := extractNumbered(ugLines, reUgandaHead, "Article ", false)
	applyTopics(ug, loadTopics("resources/uganda-constitution.json"))
	for i := range ug {
		ug[i].Sources = sourceFor("Wikisource — Constitution of the Republic of Uganda, 1995", ugURL)
	}
	write(Document{
		ID:           "uganda-constitution",
		Kind:         "constitution",
		Jurisdiction: "UG",
		Title:        map[string]string{"en": "The Constitution of the Republic of Uganda, 1995"},
		Summary: map[string]string{"en": "Uganda's supreme law, adopted by the Constituent Assembly on 22 September 1995 and commenced on " +
			"8 October 1995 — every article, in order. Chapter Four is the Bill of Rights; Chapter Thirteen establishes the Inspectorate of " +
			"Government, the body that investigates corruption, abuse of authority and abuse of public office."},
		Adopted:     "1995-09-22",
		Publisher:   "Uganda Legal Information Institute (ULII)",
		FullTextURL: "https://ulii.org/",
		Caveat: map[string]string{"en": "Every article is included, taken from the transcription on Wikisource — not from the custodian's own " +
			"copy, which could not be read when this was compiled. Confirm the exact wording against the custodian before relying on a specific " +
			"phrase. Not legal advice."},
		Provisions:   ug,
		Sources:      sourceFor("Wikisource — Constitution of the Republic of Uganda, 1995", ugURL),
		LastVerified: retrieved,
	})

	// --- Nigeria: the sections the only reachable transcription carries ---
	ngURL := "https://en.wikisource.org/wiki/Constitution_of_Nigeria_(1999)"
	ng := extractNumbered(spacedLines(fetch(ngURL)), reNigeriaHead, "Section ", true)
	for i := range ng {
		ng[i].Sources = sourceFor("Wikisource — Constitution of the Federal Republic of Nigeria, 1999", ngURL)
	}
	write(Document{
		ID:           "nigeria-constitution",
		Kind:         "constitution",
		Jurisdiction: "NG",
		Title:        map[string]string{"en": "The Constitution of the Federal Republic of Nigeria, 1999"},
		Summary: map[string]string{"en": "Nigeria's supreme law, in force since 29 May 1999. This listing covers sections 1-111 as transcribed " +
			"in the source used: the supremacy of the Constitution, citizenship, the fundamental rights chapter (sections 33-46) and the " +
			"National Assembly. Later sections and the Schedules are not included — read those at the custodian's copy."},
		Adopted:     "1999-05-29",
		Publisher:   "Policy and Legal Advocacy Centre (LawsofNigeria)",
		FullTextURL: "https://placng.org/lawsofnigeria/",
		Caveat: map[string]string{"en": "NOT the whole Constitution: sections 1-111 only, as transcribed in the source used. The sections a " +
			"person reporting wrongdoing most often needs — the supremacy of the Constitution, the fundamental rights chapter and the National " +
			"Assembly — are here; the executive, judiciary, state government and the Schedules are not. Section 107 is absent from the source " +
			"used, and nothing has been invented to fill it. Read the rest, and confirm any wording you rely on, at the custodian's copy. Not " +
			"legal advice."},
		Provisions:   ng,
		Sources:      sourceFor("Wikisource — Constitution of the Federal Republic of Nigeria, 1999", ngURL),
		LastVerified: retrieved,
	})
}
