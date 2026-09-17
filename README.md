# Mlinzi

**Report safely. Know what happens next. Be protected if it goes wrong.**

*Mlinzi* is Kiswahili for "protector".

Built for the OSF × Andela *Build for Africa* invention sprint, under the theme
**information you can trust**.

## The problem

When something happens to you — a bribe demanded for a service you are entitled
to, violence at the hands of an officer, abuse at home — the obstacle is rarely
that no institution exists. It is that you do not know which one handles your
situation, what it will ask of you, what you are entitled to, or what happens
after you report. And where reporting is dangerous, going quiet afterwards means
the report goes quiet with you.

## Three layers

**1. Know.** Which institution owns your situation, how to reach it on a basic
phone with no data, what evidence to bring, what you are entitled to, and the
ordered next steps. Every fact carries its publisher, its URL, and the date it
was last verified. Where credible sources disagree, both are shown and flagged
rather than one being silently chosen.

Beside the guides sits a **Resources Center**: the primary documents
themselves — the Kenyan, Ugandan and Nigerian constitutions, and Nigeria's
ICPC Act — so that "you have a right to X" can point at the instrument that
actually says so. Each provision is quoted with the source it was checked
against, separately from the document as a whole, and every document links
out to its custodian's authoritative full text.

**2. Report.** File anonymously. The report is hash-chained and timestamped at
creation, so you can later prove exactly what you submitted and when, and the
receiving institution cannot quietly edit, backdate or delete it.

**3. Protect.** Reporting carries risk. Set a check-in cadence and name
guardians. If you go silent, your evidence escalates to them automatically.
Release requires a threshold of guardians acting together — no single party,
including whoever operates Mlinzi, can release it alone.

## Status

Layer 1 complete: domain model, validating store, seeded Kenyan dataset
(bribery, police misconduct, GBV), and a CLI browser (`list`/`search`/`show`),
with the dataset embedded into the binary for offline, install-free use.

Layer 2 complete: `internal/ledger` (generic tamper-evident hash chain) and
`internal/report` (anonymous submission, forward-only status trail, receipt
verification) — see `mlinzi demo-report`.

Layer 3 complete: `internal/shamir` (Shamir's Secret Sharing over GF(256))
and `internal/guardian` (check-in cadence, escalation on a missed check-in,
threshold-based release — no single party, including whoever operates
Mlinzi, can release evidence alone) — see `mlinzi demo-escalation`.

All three layers wired into one story via `mlinzi demo-full`, and into a
**web UI** (`cmd/mlinziweb`) covering the same flow: search and read a guide,
file a report, optionally protect it with guardians, check in, and — for
this demo — trigger an escalation and watch guardian release actually
reconstruct the key. An **institution portal** (`/institution`) lets a
reviewer acknowledge and resolve filed reports, proving the accountability
mechanism works from both sides.

**How a report reaches a real institution — stated honestly:** Mlinzi does
not have a live integration with EACC's, IPOA's, or any institution's actual
case-management systems — none expose a public API to integrate with. The
institution portal is a stand-in for what a real partner institution or NGO
relay would use, not a live pipe. Two realistic models for a next iteration:
(1) Mlinzi verifies and directs the person to the institution's own real
channel, and the person logs the outcome back into Mlinzi as their permanent
record; (2) a partner NGO relays reports into the institution's existing
channel on the reporter's behalf and updates status here. Neither is built;
both are honest next steps, not claimed capabilities.

**Known, stated gap:** there is no persistence layer and no authentication
on the institution portal — anyone with the URL can currently advance any
report's status. Both are fine for a hackathon proof of concept and would
need to be fixed before any real deployment.

## Scalability, demonstrated

`data/ke/`, `data/ng/` and `data/ug/` are all loaded and fully isolated —
search or browse Nigeria's ICPC bribery-reporting guide with `mlinzi list NG`
(CLI) or the country switcher on the web home page. Adding a country required
zero application code changes, only a new data file, which is the actual
proof behind "a new country is a data folder, not a rebuild." Uganda was
added exactly the same way Nigeria was.

Widening the offline-precache test from Kenya to every loaded jurisdiction
while doing that immediately caught a real gap: Nigeria's guide had never
been added to the service worker's precache list, so a Nigerian user with no
connection could not read it on a cold start. That is fixed and now covered
by a tripwire that fails if any jurisdiction's guides, or any Resources
Center document, drops out of the precache list.

## The Resources Center

`resources/` holds one JSON file per legal instrument, all in a single flat
folder, each standing entirely on its own: Kenya's constitution and Uganda's
share the folder and nothing else — no cross-references, no shared
provisioning step, no ordering dependency. A document's `id` must equal its
filename, so each instrument has exactly one home, and each one is published
with the provisions it was checked on, the source for each provision, and a
plain statement of what the document is and is not.

Four instruments today, with the coverage each one actually has:

| Document | Coverage | Source actually used |
| --- | --- | --- |
| Kenya, Constitution 2010 | all 264 articles | text reproduced by the Comparative Constitutions Project (`secondary`) |
| Uganda, Constitution 1995 | all 288 articles | Wikisource transcription (`secondary`) |
| Nigeria, Constitution 1999 | sections 1–111 only — **not the whole Constitution** | Wikisource transcription (`secondary`) |
| Nigeria, ICPC Act 2000 | 8 key sections, a selection | the Commission's own published copy (`official`) |

Coverage is a measured fact in the data, not an assumption written into a
caveat by hand. The extractor reports how many articles it found against how
many it expected, on every run, and the caveats and the counts above are
written from that report — which is how "sections 1–111 only" and "section
107 is absent from the source" became stated facts rather than things a
reader has to discover for themselves.

Where the custodian's own copy could not be read from the build environment
(Kenya Law, ULII), the data says so in the document's own caveat, marks the
source `secondary`, and the page still sends the reader to the custodian for
the authoritative wording. Nigeria's constitution is left at partial coverage
with that stated on the page rather than padded to look complete.

Regenerating this does not mean retyping ~670 articles:

```
go run ./tools/extractgen
```

`tools/extractgen` fetches the source texts, parses them, preserves the
topical labels, writes `resources/*.json`, and prints a per-document coverage
report. Re-running it against unchanged sources reproduces the committed
constitutions byte-for-byte — verified, not assumed — and it does not touch
`nigeria-icpc-act.json`, which is curated by hand from the Commission's own
PDF.

Adding a constitution is adding a file there, the same way adding a country
is adding a folder to `data/`.

## Multilingual, demonstrated

Kenya's **and Uganda's** guides are available in English, Kiswahili and
French — try `?lang=fr` on any Kenya or Uganda guide page, or the language
switcher in the web UI. Adding French required zero code changes, since
`guide.Text` is already a language-keyed map; this was a pure data addition,
run through every guide (67 strings for Kenya, 70 for Uganda, each verified
to resolve). Nigeria remains English-only, and its data says so rather than
half-translating it.

That completeness is now a tested property rather than a convention:
`TestTranslatedJurisdictionsCoverEveryString` walks the domain type — not the
JSON — and fails naming the exact guide, field and language if any string in
a translated jurisdiction is missing a translation. Adding a field without a
translation, or a country claiming full coverage without it, fails the build.

As with Kiswahili, French is AI-drafted and not yet reviewed by a native
speaker — stated here rather than implied verified.

## Accessibility: read-aloud

Every guide page has a "Read this guide aloud" button using the browser's
built-in text-to-speech (Web Speech API) — no server round-trip, no audio
files shipped, works offline once the page has loaded. Voice availability
and quality depend on the device; a browser with no Kiswahili or French
voice installed falls back to its default voice rather than failing.

## Offline support (PWA)

The web UI is installable and works offline for the **Know** layer: the
home page and every guide's detail page are precached by a service worker
at install time, so they render with zero connectivity — including on a
first-ever visit with no prior connection. This is a genuine, low-bandwidth
capability, not a claim: try it — load the site once, then disable your
network and reload.

**Reporting and protection are not offline-capable, by design.** A report's
value depends on reaching a shared, tamper-evident ledger the moment it's
submitted; a real offline-write feature (local queueing, encryption at
rest, sync-conflict handling) is a substantial feature in its own right and
isn't built here. The service worker deliberately does not intercept POST
requests — filing a report while offline fails with the browser's ordinary
network error, which is the honest behavior rather than a silently broken
one.

## Run the web UI locally

```
go run ./cmd/mlinziweb
```

Then open http://localhost:8080.

## Deploy (Fly.io)

A `Dockerfile` and `fly.toml` are included.

```
flyctl launch    # first time — creates the app, uses fly.toml as-is
flyctl deploy    # subsequent deploys
```

The Fly region defaults to Johannesburg (closest to East Africa). The app is
kept at `min_machines_running = 1` so it stays warm through the hackathon's
judging window rather than cold-starting on a judge's first click — safe to
scale back to 0 afterward.

## Design guarantees, enforced in code

- **A guide without sources or a verification date will not load.** Serving
  civic information that cannot show its provenance is worse than serving none,
  so `Load` fails the whole dataset rather than skipping bad entries.
- **A guide without next steps will not load.** Information that does not tell
  you what to do next is what this project exists to replace.
- **Every guide is reachable on a basic handset.** Asserted by test.
- **Multilingual by construction.** Every human-readable string is a
  language-keyed map with fallback; adding a language is a data change.
- **Scalable by directory.** `data/<jurisdiction>/` — another country is a new
  folder, not a new build.
- **A legal document whose provisions cannot show their sources will not
  load.** The Resources Center validates every provision's own source
  independently of the document around it, so an article cannot be laundered
  by a well-sourced file. One document per file, `id` matching its filename.

## Run it

```
go test ./... -cover
```

## Data sources

Every claim in `data/ke/guides.json`, `data/ng/guides.json`,
`data/ug/guides.json` and `resources/*.json` is attributed inline with
publisher, URL, retrieval date, and a confidence level of `official`,
`secondary` or `conflicting`. Where an official page could not be reached from
the build environment, the source actually used is named as such — the
constitution extracts say plainly that their wording was checked against a
reproduced text and point to the custodian's own copy for the authoritative
version.

Three of the four documents rest on `secondary` sources (Constitute,
Wikisource) because the custodians — Kenya Law, ULII, Nigeria's own law
portal — could not be read from the build environment (bot filters, a broken
TLS chain). Only the ICPC Act is sourced from the body it is about. Every
document page therefore links to the custodian's copy, and says which parts
of the wording should be confirmed there before being relied on.

## How AI tools were used

See [docs/AI_USAGE.md](docs/AI_USAGE.md).

## License

MIT
