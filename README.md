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
out to its custodian's authoritative full text. The Resources Center is also
where **Ask Mlinzi** answers from — see [RAG: Ask Mlinzi](#rag-ask-mlinzi).

**2. Report.** File anonymously. The report is hash-chained and timestamped at
creation, so you can later prove exactly what you submitted and when, and the
receiving institution cannot quietly edit, backdate or delete it.

**3. Protect.** Reporting carries risk. Set a check-in cadence and name
guardians. If you go silent, your evidence escalates to them automatically.
Release requires a threshold of guardians acting together — no single party,
including whoever operates Mlinzi, can release it alone.

**4. Ask.** A retrieval-augmented assistant answers questions about rights,
reporting procedures and the law **using only the sourced corpus this app
already publishes** — the guides and the legal documents — and cites the
passages it used. When the corpus does not contain the answer, it says so
rather than improvising. Details and limits below.

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

Layer 4 complete: `internal/rag` (retrieval-augmented answering over the
corpus, cite-or-abstain) — see `mlinzi demo-rag` and the `/ask` page.

All layers wired into one story via `mlinzi demo-full`, and into a
**web UI** (`cmd/mlinziweb`) covering the same flow: search and read a guide,
ask the corpus a question, file a report, optionally protect it with
guardians, check in, and — for this demo — trigger an escalation and watch
guardian release actually reconstruct the key. An **institution portal**
(`/institution`) lets a reviewer acknowledge and resolve filed reports,
proving the accountability mechanism works from both sides.

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

**Known, stated gap:** there is no authentication on the institution portal
— anyone with the URL can currently advance any report's status. It is fine
for a hackathon proof of concept and would need to be fixed before any real
deployment.

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

## RAG: Ask Mlinzi

The `/ask` page answers questions about rights, procedures and the law. The
design constraint comes from the project itself: an answer without its
source is exactly what Mlinzi is against, so the assistant is built
**cite-or-abstain**.

The pipeline (`internal/rag`) has three stages, all in-process and
offline by default:

1. **Chunking.** Every guide section (summary, rights, steps, timeline,
   each institution with its channels spelled out) and every legal provision
   becomes a retrieval chunk carrying its provenance — publisher, URL,
   confidence — and its language. Guides are chunked per language; the law
   is chunked in its authoritative English in every index, the same rule
   the Resources Center pages state.
2. **Retrieval.** BM25 over those chunks with field boosts, an absolute
   score floor, a term-coverage gate, and a relative cut keeping only
   passages within 40% of the best hit. Each gate exists because of a real
   failure the live corpus produced — a query about lotteries retrieving a
   helpline schedule on one shared word, a bribery answer drifting into an
   article about judicial power — and each is pinned by a test against the
   real corpus (`internal/rag/corpus_test.go`), not only against fixtures.
3. **Synthesis.** The default synthesizer is extractive: it composes the
   answer from the retrieved passages' own text, each with its citation,
   and cannot emit a sentence the corpus does not contain. When the corpus
   has nothing relevant, it refuses: *"I could not find this in the sourced
   guides or legal documents."* That refusal is a feature — it is what
   makes the other answers worth acting on.

**Optional LLM phrasing, verified.** Setting `RAG_LLM_ENDPOINT`,
`RAG_LLM_API_KEY` and `RAG_LLM_MODEL` (any OpenAI-compatible endpoint —
Groq, OpenAI, Together, Mistral) lets a language model phrase the answer
against the same retrieved passages. Every sentence of its output is then
verified against the passages it saw; anything unsupported is dropped, and
if nothing survives, the extractive answer ships instead. The page states
which method produced the answer you are reading. With no endpoint
configured — the default — the binary stays fully offline, and the pitch
demo cannot be broken by a third-party API having a bad minute.

```
# offline by default:
go run ./cmd/mlinziweb

# or with LLM phrasing (example: Groq's free tier):
RAG_LLM_ENDPOINT=https://api.groq.com/openai/v1/chat/completions
RAG_LLM_API_KEY=gsk_...
RAG_LLM_MODEL=llama-3.3-70b-versatile
go run ./cmd/mlinziweb
```

A CLI walkthrough of the whole pipeline, including the refusal case:

```
mlinzi demo-rag
```

**Stated limits:** retrieval is lexical (BM25), not semantic — it matches
the words the corpus uses, which is why a Kiswahili question is answered
from Kiswahili data and an out-of-domain question abstains; a query phrased
in completely different words from the data may miss. The LLM path is
phrasing with verification, not independent reasoning — it can only speak
from the retrieved passages, same as the extractive path. The assistant
cites sources but is not legal advice; the guides and documents it links to
remain the authoritative pages.

## Persistence

The biggest stated gap is now closed. Setting `MLINZI_DATA_DIR` gives every
store a durable home:

- **The report and guardian ledgers** go to disk as snapshots and are
  *verified on restore* — `ledger.NewFromEntries` re-walks the hash chain,
  so a snapshot that was edited, truncated or reordered refuses to load
  rather than serving altered history.
- **Evidence files** go to disk one file per content hash and are
  re-verified against that hash on restore — a swapped file is refused.
- Reports keep their IDs, contents, status trails and verification codes
  across restarts; verification codes are derived from the ledger hashes,
  so they come back exactly. Guardian cases restore with their status,
  check-in clocks and report links. (A guardian whose share was submitted
  but not yet threshold-crossing at restart simply submits again — key
  material is never persisted, by the layer's founding rule.)
- Snapshots are written **atomically** (temp file, fsync, rename) after
  every mutating request, so a crash mid-write leaves the previous
  snapshot intact. The directory is created `0700`; files are `0600`.

Without the variable set, the app runs memory-only exactly as before —
the default for throwaway demos.

`mlinzi demo-persistence` proves the property that makes this more than
"we save to disk": it files reports, restores them, then edits history on
disk and shows the restore refusing the altered snapshot.

```
MLINZI_DATA_DIR=./mlinzi-data go run ./cmd/mlinziweb
mlinzi demo-persistence
```

**Stated limits:** this is single-instance disk persistence, and on Fly it
depends on a Fly volume actually being attached at `/data` — without one,
`MLINZI_DATA_DIR` still works locally (it's a real directory on your own
disk), but on Fly it would just be part of the machine's own image, which
`flyctl deploy` replaces from scratch. `fly.toml` now declares a
`mlinzi_data` volume mounted at `/data`; that volume has to be created once
before the first deploy that uses it (`flyctl volumes create mlinzi_data
--region jnb --size 1`) — Fly does not create it for you. With the volume
attached, data survives restarts and redeploys of this one machine; it does
not survive loss of the machine's zone or a move to multi-machine/multi-region
serving, since a Fly volume is single-zone and not replicated. Restores are
verified but not encrypted at rest; disk access remains outside the threat
model this proof of concept addresses.

## Evidence upload

A reporter can attach photos, video, or documents to a report. The two
guarantees the rest of the app makes for text carry over rather than being
reinvented for files: integrity (a file's hash enters the same ledger entry
as the report text, so a file swapped after submission is exactly as
detectable as edited report text already is) and zero trust in the client
(declared filename and declared content type are both discarded; the actual
bytes are sniffed against an allowlist — JPEG, PNG, PDF, MP4, WebM,
QuickTime — and anything else, including a file that merely claims to be an
allowed type but doesn't decode as one, is rejected outright).

One risk is treated as stricter than the rest of the app's accepted
plaintext-storage posture: a phone photo routinely carries the GPS
coordinates and device identifiers of whoever took it. That is silent in a
way exposed report text is not, so every image is decoded and re-encoded
before storage, which drops EXIF/XMP/ICC metadata entirely — verified
against a real GPS- and device-ID-bearing JPEG fixture, not assumed. Blindly
re-encoding would also silently rotate photos from phones that store pixels
in sensor orientation and rely on the EXIF orientation tag to display them
upright, so orientation is read (via a small from-scratch parser — Go's
standard library has no EXIF support) and applied to the pixels before the
tag carrying it is discarded. All 8 EXIF orientation values are checked
against hand-derived pixel positions in `internal/evidence`.

**Known, stated limits:** evidence files follow the app's persistence story
(see [Persistence](#persistence)) — with `MLINZI_DATA_DIR` set they are
restored after re-verifying their content hashes, and without it they are
in-memory only. Files are capped at 8MB each,
20MB and 4 files per report, 150MB across the whole process, sized for a
small single-instance deployment rather than for what someone might
reasonably want to upload. Video and PDF metadata is not stripped — Go's
standard library has no video or PDF parsing to build that on in the time
available, and the upload form says so plainly rather than implying a
guarantee that isn't there. Evidence is served back at `/evidence/{hash}`
with the same lack of authentication the institution portal already has and
states — not a new risk, the existing one extended to a new content type.

## Reporting for someone who can't

Not everyone affected by corruption, abuse, or wrongful detention can file
their own report — no phone, no literacy, no network access, most
concretely someone already in custody. A checkbox on the report form lets a
trusted third party (a family member, a paralegal, a visitor) file on that
person's behalf, recorded openly rather than the proxy silently posing as
the person affected — an institution reading the report afterward can see
it arrived this way, which matters for how much weight to give a
first-person claim relayed by someone else.

New guides for exactly this — "I, or someone I know, was wrongly arrested or
imprisoned and cannot afford a lawyer" — exist for Kenya, Uganda, and
Nigeria. This is scoped deliberately narrowly: Mlinzi cannot get anyone's
appeal filed or heard, and nothing in the guide text claims otherwise. What
it can honestly do is connect someone to the real body that can act, with a
timestamped record that a claim was raised on a given date. Every
institution listed is sourced from its own current official contact page —
Kenya's National Legal Aid Service and KNCHR, Uganda's LASPNET and Human
Rights Commission, Nigeria's Legal Aid Council — and the constitutional
rights cited are quoted from this repo's own `resources/*-constitution.json`
rather than asserted from memory. Kenya's 14-day appeal-notice deadline is
cited to the actual Criminal Procedure Code section, since an unsourced
deadline in a guide meant to help someone not miss one would be worse than
not stating it at all.

## Multilingual, demonstrated

Kenya's **and Uganda's** guides are available in English, Kiswahili and
French — try `?lang=fr` on any Kenya or Uganda guide page, or the language
switcher in the web UI. Adding French required zero code changes, since
`guide.Text` is already a language-keyed map; this was a pure data addition,
run through every guide (67 strings for Kenya, 70 for Uganda, each verified
to resolve). Nigeria remains English-only, and its data says so rather than
half-translating it.

**The interface is translated too**, not just the content. Navigation,
buttons, field labels, headings, statuses, the offline banner and the footer
all come from a string catalogue (`internal/ui/strings.json`) with the same
language-keyed shape as the civic data, so adding a language stays a data
change. Chrome used to be literal English in the templates, which meant a
French guide rendered inside an English frame.

Four separate mechanisms carry a language through the app, and each of them
was a place it could be dropped — all four are now covered by tests:

- **Links** keep it: every header link carries `?lang=`, so clicking away no
  longer resets you to English.
- **The switcher** preserves the rest of the query. It was a form posting to
  `action=""`, which replaces the whole query string — so changing language on
  the home page silently threw away the chosen country and the search term.
  It is now plain links, which also means it needs no JavaScript.
- **Forms** submit it and the server reads it. The hidden `lang` field was
  being sent all along and never read, so every POST result page — a filed
  report, a status lookup, a case action — came back in English however the
  person had set the language.
- **Offline copies** exist per language, because offline caching is keyed by
  exact URL and language lives in the URL. Precaching only the bare paths
  meant someone reading in Kiswahili who lost their connection got the
  browser's offline error; the offline claim held in English only.

Completeness is a tested property rather than a convention. Three tripwires
cover the ways a translated page can quietly go wrong: every string in a
translated jurisdiction must resolve
(`TestTranslatedJurisdictionsCoverEveryString`), every catalogue key a
template names must exist (`TestEveryTemplateStringKeyExistsInCatalog` — a typo
otherwise renders a blank heading, since templates are text and no compiler
sees it), and every label looked up from data — channel kinds, categories,
statuses, document kinds — must exist too (`TestDynamicLabelKeysExist`, which
enumerates them from the real dataset).

**Two things are deliberately not translated, and the pages say so.** The law
itself stays in its authoritative English wording, with a visible notice on
the page explaining that the surrounding translation does not extend to the
articles; and a guide with no translation in the requested language says so
instead of silently presenting English. As with Kiswahili, the French is
AI-drafted and not yet reviewed by a native speaker — stated here rather than
implied verified.

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

The precache covers every supported language, and the bare paths alongside
the `?lang=` variants, because a cache key is an exact URL: the manifest's
`start_url` is `/`, and a bookmark or typed address is the bare path too.
Precaching language variants without the bare paths is a mistake this repo
actually made, and the installed app could not open offline until a test
pinned both forms.

The Resources Center documents are precached once, in English, rather than
in three languages each: they render to about 450 KB apiece, so the variants
would put several megabytes in the cache of the basic phones this is built
for, to duplicate a body of legal text that does not change between them.
The guides — what someone actually needs in the moment — are offline in every
language.

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

With persistence (state survives restarts):

```
MLINZI_DATA_DIR=./mlinzi-data go run ./cmd/mlinziweb
```

## Deploy (Fly.io)

A `Dockerfile` and `fly.toml` are included.

`fly.toml` mounts a Fly volume (`mlinzi_data`) at `/data` for persistence —
create it once before the first deploy that references it, or the deploy
fails looking for a volume that doesn't exist:

```
flyctl volumes create mlinzi_data --region jnb --size 1
flyctl launch    # first time — creates the app, uses fly.toml as-is
flyctl deploy    # subsequent deploys
```

If you deliberately want to run without persistence on Fly, remove the
`[[mounts]]` block and the `MLINZI_DATA_DIR` line from `fly.toml` first —
otherwise the app will point at a mount that isn't there.

The Fly region defaults to Johannesburg (closest to East Africa). The app is
kept at `min_machines_running = 1` so it stays warm through the hackathon's
judging window rather than cold-starting on a judge's first click — safe to
scale back to 0 afterward.

**Verify it for real before the deadline:** deploy, submit a test report,
redeploy (`flyctl deploy` again with no code change), and confirm the report
is still there. That's the one thing that actually proves the persistence
claim on Fly rather than just on your own machine.

## Design guarantees, enforced in code

- **A guide without sources or a verification date will not load.** Serving
  civic information that cannot show its provenance is worse than serving none,
  so `Load` fails the whole dataset rather than skipping bad entries.
- **A guide without next steps will not load.** Information that does not tell
  you what to do next is what this project exists to replace.
- **An answer cannot exist without its source.** The RAG pipeline retrieves
  provenance-carrying chunks and refuses to answer outside them; a question
  the corpus cannot support gets a refusal, not a guess.
- **A tampered snapshot will not load.** Persistence restores through the
  ledger's chain verification, so editing history on disk fails loudly
  instead of loading quietly.
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

## A limit worth stating

The web UI is fully translated. The CLI (`cmd/mlinzi`) is not: its output
labels are English, and it has no language switch. It reads the same
translated data, so guide content in Kiswahili or French is reachable there,
but the surrounding presentation is English. On the basic-device story the
CLI is meant to serve, adding a language flag is the obvious next step rather
than something done here.

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
