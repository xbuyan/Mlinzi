# How AI tools were used to build Mlinzi

Kept as a running log from day one, not reconstructed at submission time.

## What was not AI-generated

The core idea. Mlinzi comes out of an existing body of work on evidence
integrity and threshold cryptography (`fileinspect`, `auditlog`, and the
Kinga/Aegis/Orizu line of projects), and the concept of pairing civic
information access with tamper-evident reporting and escalation-on-silence
predates this hackathon.

## Day 1 — 15 September 2026

**Domain modelling (AI-assisted, human-directed).** The Layer 1 data model was
designed in conversation: `Text` as a language-keyed map so multilingual
support is a data concern rather than a code concern, per-channel `Source`
provenance, and load-time validation that refuses to serve an unsourced guide.
The decisions were made by me; AI drafted the Go types and the store from
those decisions.

**Source verification (AI-assisted research, human-verified).** Contact
details, mandates and statutory bases for EACC, IPOA, KNCHR, HAK 1195, GVRC and
Childline were gathered via AI-run web search, and every fact in
`data/ke/guides.json` carries the publisher and URL it came from plus the date
it was retrieved.

This surfaced a finding that changed the design: credible sources publish two
different toll-free numbers for IPOA (0800 720 434 and 1559). Rather than pick
one, the model gained a `Conflicting` confidence level and a `Disputed()`
check, so both reach the user with the disagreement shown. A verification step
became a product feature.

**Testing (AI-drafted, human-reviewed).** 27 tests covering language fallback,
validation refusals, search across languages, staleness, and an invariant test
asserting that every guide offers at least one channel usable on a basic
handset with no mobile data.

One test failure was a genuine catch on my own assumption rather than on the
code: a Kiswahili search for *rushwa* returned both the bribery guide and the
policing guide. That is correct — an officer demanding a bribe is reportable
through both routes — so the expectation was corrected, not the behaviour.

**Translation.** Kiswahili strings are AI-drafted and pending review by a
first-language speaker before submission. Flagged here rather than presented as
verified.

## CLI browser and a real bug (AI-drafted, human-caught)

Built a thin CLI (`cmd/mlinzi`) wrapping the already-tested `guide.Store` —
`list`, `search`, `show` — as a fast, demoable presentation layer with no new
domain logic.

First version located the data directory via `go env GOMOD` at runtime. It
passed every test because tests run inside the module. It failed the first
time the compiled binary was run from outside the repository, with no Go
toolchain in the invoking shell — precisely the "basic device, no
installation step" scenario the brief asks for. Caught by manually running the
built binary from `/tmp` and then from `/`, not by the test suite.

Fixed by embedding the dataset into the binary at compile time
(`go:embed`), which also directly serves the low-bandwidth constraint: the
shipped artifact is one file, works fully offline, and needs no accompanying
data directory. Added a regression test (`assets_test.go`) documenting why the
embedding exists, so the working-directory dependency doesn't quietly return.

## Day 2 — Layer 2: anonymous reporting and the tamper-evident ledger

**Design (human-directed, AI-drafted).** Split into two packages on purpose:
`internal/ledger` is a generic, domain-agnostic hash-chain — the same
primitive proven out in the `auditlog` work — and `internal/report` builds
report submission and status tracking on top of it. A `Report` is a
projection assembled by replaying the ledger, never a mutable record, so
there is no field an institution could quietly edit.

**Scope cut, stated plainly rather than hidden.** Report content is stored in
plaintext in this proof of concept. Encrypting it without the guardian-based
key-release mechanism behind it (Layer 3) would look more finished than it
is; the honest version is to build the encryption once the release mechanism
that makes it meaningful actually exists.

**Testing (AI-drafted, human-reviewed).** White-box tests in `ledger_test.go`
directly mutate stored entries after the fact — content edit, broken
prev-hash link, backdated timestamp — to prove `Verify()` catches each one;
nothing in the ledger's public API allows this, so proving detection requires
reaching into the unexported state on purpose. `report_test.go` asserts the
one invariant that matters most for the accountability story: a status
transition from `submitted` straight to `resolved`, skipping acknowledgement,
is rejected outright rather than silently applied.

**CLI (`demo-report`).** A single-run walkthrough — submit, an attempted
shortcut correctly rejected, proper resolution, then independent
chain-integrity and receipt verification — run live from outside the repo to
confirm it. No new logic, presentation only.

## Day 3 — Layer 3: Shamir splitting and guardian escalation

**Design (human-directed, AI-drafted).** Split into `internal/shamir`
(domain-agnostic GF(256) Shamir splitting, rewritten fresh for this repo
rather than imported, so it could be tested on its own terms) and
`internal/guardian` (the dead-man's-switch built on it). The load-bearing
rule, stated up front before any code: a `Case` must never store enough to
reconstruct the release key by itself. Split happens once, at creation, and
the shares leave the process immediately for out-of-band distribution —
`Case` only ever tracks metadata afterward.

**A concrete threat modelled and tested against, not just assumed.** Once a
case escalates, a late check-in is rejected rather than silently cancelling
the escalation — otherwise, anyone who gained control of a reporter's phone
after they went missing could suppress a legitimate escalation by checking in
on their behalf. `TestCheckInRejectedAfterEscalation` exists specifically to
keep this behaviour from regressing.

**Testing (AI-drafted, human-reviewed).** The Shamir package carries an
exhaustive test of GF(256) multiplicative inverses across all 255 nonzero
field elements — the same style of check used on the Kinga project, kept here
because a subtly wrong field operation only breaks reconstruction for
certain byte values, which a handful of spot checks would not catch. The
guardian package's most important test, `TestNoSinglePartyCanReleaseAlone`,
asserts the one property the whole layer exists to guarantee: with a 2-of-3
threshold, one guardian's share is never sufficient, even when submitted
correctly by a legitimately named guardian.

**CLI (`demo-escalation`).** A full lifecycle in one run — case creation,
normal check-in, a missed check-in, a single guardian's share correctly
refused, a second guardian's share crossing threshold and reconstructing the
exact original key, then independent chain verification. Every claim made in
the pitch deck about this layer has a runnable command behind it.

## Day 4 — Web UI and deployment scaffolding

**Design (human-directed, AI-drafted).** The web layer (`cmd/mlinziweb`)
wraps the exact same `guide`, `report`, and `guardian` packages the CLI
uses — no new domain logic, only an HTTP presentation layer. Plain
server-rendered templates and full-page form submissions were chosen over a
JS framework or htmx, deliberately, to minimize moving parts that could go
wrong under time pressure while still meeting the brief's low-bandwidth
constraint.

**A real bug caught before it ran, not after.** The initial plan was to
parse every template file into one shared `*template.Template` and dispatch
by name. Go's `html/template` shares one namespace of named templates across
every file parsed together — so multiple page files each defining
`{{define "content"}}` would have silently overwritten each other, with only
the last-parsed page's content ever rendering, for every route. Caught during
design, before writing the page templates, by working through how
`html/template`'s associated-template model actually behaves rather than
assuming a per-file scope. Fixed by pairing `layout.html` with exactly one
page template at a time, each combination its own isolated `*template.Template`.

**The core security invariant was re-verified at the layer users actually
touch.** `internal/guardian` already proves a single guardian's share can
never trigger release. `TestWebSingleGuardianCannotRelease` proves the same
thing again, through the real HTTP handlers via `httptest` — because a
correct package underneath a buggy handler would still leave a real user
unprotected. Passing at the package level is necessary; it isn't sufficient.

**Testing.** 9 `httptest`-based tests, run entirely in-process against the
real routing table `main()` uses (no mocked router) — home and search
rendering, the disputed-source flag actually reaching the page, 404s for
unknown guides and cases, and a full report-to-protection-to-release flow
exercised exactly as a browser would drive it.

**Deployment.** `Dockerfile` (multi-stage, `CGO_ENABLED=0`, distroless
runtime — no shell, minimal attack surface) and `fly.toml`, matching an
existing Fly.io workflow from a prior project. Not deployed or build-tested
from within this environment (Fly and Docker Hub are outside its network
allowlist); the Dockerfile follows an established, standard pattern but
hasn't been run end-to-end here, and is stated as such rather than implied
verified.

## Day 4b — Institution portal

**Prompted by a direct, good question**: once a report is filed, how does
it actually reach a real institution? Honest answer: it doesn't, yet — no
institution exposes a public API to integrate with, so any claim of live
delivery would have been false. Rather than leave that gap implicit, it's
now stated in the README and demonstrated as a stand-in: an institution
portal (`/institution`) where a reviewer can acknowledge and resolve filed
reports, proving the accountability mechanism (`Advance`, the forward-only
status trail) genuinely works from the institution's side, not only shown
via hardcoded calls in a CLI demo.

**Reused, not rebuilt.** `report.Store` gained exactly two additions:
`All()` (enumerate every report, needed for a list view that never existed
before — the store only supported lookup by a specific ID) and
`Status.NextOptions()` (exposes which transitions are currently legal, so
the UI renders only real actions instead of guessing which buttons should
appear). Both are thin wrappers around logic already fully tested; no
domain rule changed.

**Re-verified the core invariant a third time, at a third layer.** The
"status can't skip acknowledgement" rule is now tested at the package level
(`internal/report`), the reporter-facing HTTP layer (Day 4), and now the
institution-facing HTTP layer (`TestInstitutionCannotSkipAcknowledgement`) —
because each layer is a place the rule could theoretically be bypassed, and
each was checked rather than assumed safe by association with the layer
below it.

**Stated plainly, not glossed over:** this page has zero authentication.
Anyone with the URL can act as any institution. That's flagged directly in
the page itself, not just in developer docs, since a judge clicking around
should not mistake a demo affordance for a finished access-control model.

## Day 5 — PWA support for the Know layer, scoped honestly

**Prompted by a direct question**: can Mlinzi work offline as a PWA? The
honest answer split the work in two, and the split itself is the design
decision worth explaining. The KNOW layer (guide browsing) can genuinely
work with zero connectivity, including on a cold start with no prior visit,
because its data is small and static. Reporting and protection cannot
honestly be made to work offline in the time available — a report's value
comes from reaching a shared, verifiable ledger at submission time, and
queuing writes locally for later sync is a real feature (local encryption,
conflict handling) that would need to be built properly, not implied by a
service worker that happens to intercept POST requests. So the service
worker explicitly does not intercept writes; they fail with the browser's
ordinary offline error, which is the honest behavior, not an oversight.

**What was added (human-directed, AI-drafted).** A web app manifest, a
brand-matched SVG icon, and a service worker that precaches the home page
and all three guide detail pages at install time — so they work offline
even for a first-time visitor with no connectivity yet — plus a visible
online/offline banner so the behavior is demoable, not just architecturally
true.

**Testing, and its real limit, stated plainly.** `httptest` can verify the
manifest, icon, and service worker are served with correct content types
and the `Service-Worker-Allowed` header the worker's scope depends on, and
a tripwire test (`TestServiceWorkerPrecachesOnlyRealGuides`) catches the
precache list drifting from the actual seed dataset. What it cannot verify
is whether a real browser actually caches these and serves them with the
network disabled — that has no meaningful httptest equivalent, since it's
browser cache-storage behavior, not server behavior. Verified manually
instead: load the site once with a connection, enable airplane mode, reload
— the home page and all three guides still render; filing a report in the
same offline state correctly fails, showing the browser's own offline
error, exactly as designed.

## Day 6 — Three targeted enhancements, chosen for judging leverage

With time remaining before submission, three additions were prioritized
over a longer wishlist, each picked because it strengthens evidence for a
specific judging criterion rather than adding a new feature for its own
sake.

**A second country (Nigeria, human-directed research, AI-assisted).**
Researched Nigeria's Independent Corrupt Practices and Other Related
Offences Commission (ICPC) — official contact channels, mandate, petition
process — with the same sourcing discipline as the Kenya data: publisher,
URL, retrieval date, confidence level. One genuine finding repeated the
pattern from IPOA: news coverage claims a toll-free ICPC line, but ICPC's
own contact page lists only standard mobile numbers, not labeled toll-free.
Rather than repeat an unconfirmed claim, that's marked `conflicting` and
explained in the data itself. This turns the "a new country is a data
folder, not a rebuild" claim from a slide into something checkable — new
tests (`TestNigerianGuideIsReachableAndScoped`,
`TestAddingASecondJurisdictionDidNotBreakTheFirst`) prove Kenya and Nigeria
are genuinely isolated and that adding one didn't touch the other. Zero
application code changed to add the country; only `data/ng/guides.json` and
a jurisdiction switcher in the web UI.

**French translations (AI-drafted, human-reviewed for consistency, not yet
reviewed by a native speaker).** Extracted all 67 unique English strings
across the three Kenya guides programmatically, translated them, and
applied them back via a script matched against the exact source text
(`internal/guide` Text maps already supported this — zero code changes,
purely a data addition, which is itself evidence for the multilingual
architecture claim). `TestFrenchTranslationResolves` proves it end to end
rather than just checking the data loaded. Same honest caveat as Kiswahili:
flagged as unreviewed by a native speaker, not presented as verified.

**Read-aloud accessibility (AI-drafted).** Uses the browser's built-in
Web Speech API — no backend change, no audio files shipped, works offline
once the page has loaded. Addresses the brief's "different literacy
levels" constraint directly. Honest limitation stated in the code comment
itself: voice availability and quality depend on the device, and a phone
with no Kiswahili or French voice installed will fall back to a default
voice that may mispronounce the text rather than failing outright.

**A real bug caught by the test suite, immediately.** The read-aloud
script initially referenced `{{.Lang}}` inside a `{{with .Guide}}` block,
where `.` is scoped to the guide value, not the page-level data — so
`.Lang` didn't exist there. `go test` failed the moment this was built,
before any manual testing, because the guide template's own test suite
runs on every change. Fixed by using the `$lang` variable the template
already captures earlier for exactly this scoping reason.

## Day 7 — Resources Center and a third country

**The Resources Center (human-directed, AI-researched).** Requested as a place
for primary documents: the Kenyan and Ugandan constitutions, kept in one
folder but each standing on its own. The shape of the answer came from the
brief rather than from the AI — one file per instrument, in one flat folder,
each with the source for *every provision* rather than one source for the
whole document, because a document-level source must not be able to launder
an unsourced article. That rule is enforced in code, not just followed by
convention: an article with no source of its own fails the load, and a
document's `id` must match its filename so no two instruments can quietly
share a file.

**Article numbers were checked against the enacted text, not recalled.** This
caught a real error that a plausible-sounding summary would have shipped:
Uganda's Article 25 is "Protection from slavery, servitude and forced
labour", not the torture provision — torture is Article 24, and Article 44 is
what makes freedom from torture non-derogable. Kenya's numbering was verified
the same way against the gazette's arrangement of articles (the ethics and
anti-corruption commission is Article 79, not an assumed "Chapter Six"
reference). Where the full article text was available only from a reproduction
rather than the custodian, the data says so, marks the source `secondary`, and
the page still sends the reader to the custodian's own copy.

**A third country (Uganda).** Researched the Inspectorate of Government,
the Uganda Human Rights Commission, the Uganda Police Force, and the Sauti
116 helpline (Ministry of Gender, Labour and Social Development) with the
same sourcing discipline as Kenya and Nigeria — publisher, URL, retrieval
date, confidence, taken from the institutions' own pages wherever they
answered. Two custodian sites (Kenya Law and ULII) could not be read from the
build environment, so nothing is claimed from them: they appear as the
pointer to the authoritative text, not as a source we verified.

**A tripwire caught an old gap while it was being widened.** The
service-worker precache test had always been scoped to Kenya, so it could not
notice that Nigeria's guide was never precached — meaning an offline user in
Nigeria got nothing on a cold start. Widening it to every loaded jurisdiction
made the test fail immediately, which is the outcome to want: the guarantee
was wrong for an existing country and a test written for a new one found it.

## Day 8 — Full legal texts, Uganda's translations, and a generator that had to be distrusted

**Expanding the Resources Center to whole instruments, and the failure that
produced the honest version.** The Resources Center began with a curated
selection of provisions per constitution. Expanding that to the entire
instrument by hand (~670 articles) was not realistic, so the first move was to
write an extractor (`tools/extractgen`) against published sources and let it do
the mechanical work.

The first version's output looked plausible and was wrong. Article numbering
came out scrambled, with large gaps. The cause took measuring rather than
reading: the source page's structure conflates article headings with topic
labels under a naive parse, and one source is genuinely partial in one form
(Uganda's Wikisource wikitext) while complete in another (its rendered page).
That is a worse failure mode than an error — a plausible-looking dataset of
legal text with the wrong numbers on it, which is exactly what this project
exists to argue against.

The fix was to make coverage a measurement instead of a belief. The extractor
now reports how many articles it found against how many it expected
(`refs 1..288 | empty text 0 | missing []`), and nothing went into a caveat by
hand that the report had not established. That is also what turned "Nigeria's
constitution is partial" from something that would have been asserted into a
specific measured fact — sections 1–111 of 320, with section 107 absent from
the source — stated on the page rather than left for a reader to discover.
Kenya and Uganda came out whole: 264/264 and 288/288.

Two further defects were caught by reading the output rather than the summary:
the last article of each generated document had swallowed the page's scripts,
styles and footer, and article bodies ran past the schedules boundary into
unrelated text. Both were found by inspecting the tail of the generated JSON.

**Reproducibility as the check on all of it.** Re-running the extractor
against unchanged sources reproduces the committed constitution files
byte-for-byte, and leaves the hand-curated ICPC Act untouched. Committing
generated legal text is only defensible if the generation is reproducible and
reviewable, so that property is verified rather than asserted.

**The statute (human-curated, AI-assisted).** Nigeria's ICPC Act is a
selection of 8 key sections taken from the copy the Commission publishes
itself — the one document in the Resources Center that is `official` rather
than `secondary`. The Act's own text prints its commencement date as blanks,
so no commencement date is asserted, and the caveat says why.

**Uganda translated (AI-drafted, unreviewed by native speakers).** The three
Ugandan guides were brought to Kenya's standard — English, Kiswahili and
French, 70 strings — as a pure data change with no code touched. Rather than
check completeness by eye, it became a test that walks the domain type (not
the JSON) and fails naming the exact guide, field and language for any gap. I
confirmed the test actually bites by deleting one Kiswahili string and watching
it fail, because a completeness check that cannot fail proves nothing.

**Verified in a real browser, after a check that was invalid.** An earlier
attempt at this verification was worthless and is worth recording: killing
`go run` leaves the compiled server alive, so the "offline" reload was still
being served over the network and the comparison proved nothing. Re-run
properly — build the binary, run it directly, kill it, confirm the port
refuses connections — headless Chrome renders the Uganda guides and the
Resources Center from the service worker cache with the server genuinely dead,
at byte-identical sizes to the online pages. The offline claim is now
something observed rather than inferred from HTTP status codes.

**A limit this work exposed, stated rather than left to be hit.** Kenya's and
Uganda's constitution pages are ~450 KB of server-rendered HTML each — fine on
a decent connection, heavy for the basic handsets this project is built for.
Completeness was the goal and was achieved, but on those two pages
completeness is now in tension with low bandwidth, and that tension is real;
it is recorded here rather than presented as solved.

## Day 9 — Localization that actually reaches the page

**A vague bug report, taken seriously rather than guessed at.** "The French
and Swahili translations aren't working well" could have meant anything, so
the first move was to reproduce it rather than assume. Four separate defects
turned up, and none of them was a translation problem:

1. Header links carried no `?lang=`, so the language was lost on the first
   click anywhere.
2. The switcher was a form posting to `action=""`, which replaces the entire
   query string — changing language on the home page silently discarded the
   chosen country and the search term.
3. All interface chrome was literal English in the templates: a French guide
   rendered inside an English frame.
4. The Resources Center was English-only, and every form submitted a hidden
   `lang` field that the server never read — so each POST result page came
   back in English.

The fourth was only visible because a test failed on a message the page was
supposed to have translated. The hidden field had been sent all along; nothing
read it.

**Chrome as data, not as templates.** Interface strings moved into a
catalogue (`internal/ui`) with the same language-keyed shape as the civic
data, deliberately kept out of `data/` and `resources/`: those folders are
validated as sourced civic facts, and UI labels have no publisher, URL or
retrieval date. Put them together and one of two bad things happens — the
validation has to be loosened for everything, or chrome borrows the
credibility of a file that was verified. The catalogue validates on its own
terms instead (a key with no English text fails the load, because a blank
button is worse than a refused start).

**Three tripwires, because templates are text.** Nothing compiles a template's
string lookup, so a typo, a renamed key, or a label for a value that only
exists in the data silently renders as an empty string — a heading or button
that simply vanishes. So: every key a template names must exist; every label
looked up from data (channel kinds, categories, statuses, document kinds) must
exist, enumerated from the real dataset rather than from a list someone
remembered to update; and every string in a translated jurisdiction must
resolve in every language. I confirmed the first two actually fail — by
typo-ing a key and by deleting a category label — because a completeness check
that cannot fail proves nothing.

**The fix that broke something else, caught by verifying rather than
assuming.** The offline copy is keyed by exact URL and language lives in the
URL, so precaching only the bare paths meant a reader in Kiswahili who lost
their connection got the browser's offline error: the offline claim held in
English only. Adding the `?lang=` variants then broke the installed app's
offline launch, because the manifest's `start_url` is `/` — a bare URL that no
longer matched anything in the cache. Both forms are now precached, and a test
compares the precache list against the manifest's `start_url` so it cannot
regress a third time.

**A verification harness that had to be fixed before it could be trusted.**
The earlier offline check used `--dump-dom`, which exits as soon as the DOM is
produced and can cut the service worker's install short. It no longer
reproduced, and rather than report a failure that might not exist — or, worse,
report a success by accident — the check was rewritten to drive Chrome over
the DevTools protocol and wait for `navigator.serviceWorker.ready`, then
confirm the specific French URL was really in the cache before killing the
server. With the server genuinely dead, the French and Kiswahili guides and
the French home page render from the cache with no English chrome left on
them. Two failed attempts, one of them producing a stream of confidently
irrelevant numbers, were discarded rather than reported.

**The legal text stays in English, and the page says so.** Translating a
constitution's articles would produce a version that looks authoritative and
is not, so the provisions keep their original wording and a visible notice
explains that the translation covers the page around them and not the law
itself. A guide with no translation says that too, rather than quietly
presenting English.

## Day 10 — Evidence upload, and reporting for someone who can't

**Two features requested together, one line drawn between them.** The ask
was file upload for evidence, plus "cater for the unprivileged population
like prisoners" whose appeals go unheard. The first is a real, scoped
engineering problem. The second, taken literally — an app that gets a
wrongful conviction appeal processed — is not honestly buildable, and
building something that looked like it did would be the worst possible
thing to ship into a hackathon about trustworthy information: a system that
implies it can get someone's case heard when it can't is actively dangerous
to someone acting on that implication. What got built instead: an
on-behalf-of submission flag for people with no way to file for themselves,
and a guide connecting them to the real legal aid bodies that can act —
recorded and sourced with the same discipline as every other guide, none of
it claiming to do more than that.

**Metadata stripping was treated as a correctness problem, not a checkbox.**
A phone photo's GPS coordinates and device serial number are exactly the
kind of silent risk this app's reporter-anonymity story exists to prevent,
so every image is decoded and re-encoded before storage — Go's own JPEG and
PNG encoders never write EXIF, so this is enough on its own. But re-encoding
blind would also silently rotate any photo from a phone that stores pixels
in sensor orientation and relies on the EXIF orientation tag to display
correctly — stripping the tag without reading it first would trade one
silent corruption for another. Go's standard library has no EXIF support at
all, so a minimal from-scratch reader was written for the one tag that
matters, and the pixel transform for all 8 EXIF orientation values was
verified against hand-derived expected positions on a synthetic image,
independent of JPEG compression noise.

**A test failure that was the fixture, not the code — caught before it
became false confidence.** The first orientation test used a single marked
pixel in a 4x2 image; it failed, and the instinct to "fix the rotation
math" was wrong. The math was already independently verified by hand for
all 8 cases; a single pixel simply doesn't survive JPEG's block-based
compression at that image size. Rebuilding the fixture with larger,
block-aligned color regions made the same test pass for the right reason,
and a separate table-driven test against synthetic (uncompressed) images
now checks all 8 orientation values directly, so compression artifacts
can't hide a real regression in either direction again.

**A real regression, caught by running the full suite, not just the new
code.** Switching the report handler from `ParseForm` to
`ParseMultipartForm` — necessary for file uploads — broke every existing
plain-text report submission, because a non-multipart request makes that
call return `ErrNotMultipart` even though it has already populated the form
fields correctly. Treating that as a hard failure rejected every report
with no evidence attached, which is still the overwhelming majority of
submissions. Fixed by explicitly tolerating that one error value; caught
only because the pre-existing report tests were run again after the change,
not assumed still-passing because the new tests passed.

**A second regression, in the test harness itself.** The new
`GET /evidence/{hash}` route was registered in `cmd/mlinziweb/main.go` but
not in the separate route table the test suite builds in
`handlers_test.go` — an existing duplication in this codebase, not
something introduced here. The upload-path test failed with a 404 that
looked like a broken store lookup; it was a missing route in the test
double. Worth naming because the fix (add the route to both places) is
easy, but assuming the simpler explanation without checking would have
sent the debugging in the wrong direction entirely.

**Same access-control posture as before, not a new one.** Evidence is
served from `/evidence/{hash}` with no authentication, exactly like the
institution portal already reads report text with none. That is not a new
risk introduced by this feature; it is the existing, already-stated one
extended to a new content type, and the handler and the upload form both
say so rather than implying a protection that isn't there.

## Honest limits

- Kiswahili translations are unreviewed by a first-language speaker, as of day
  1, and both the Kiswahili and French additions since then are AI-drafted in
  a single pass with no separate reviewer — including the interface strings
  added on day 9, which are the most visible of the lot.
- The CLI remains English-only: it reads the translated data, but its own
  labels have no language switch.
- Some contact details rest on secondary sources (news outlets, NGO
  directories) where an official page could not be reached. Each is marked
  `secondary` in the data rather than presented as official.
- Only one of the four Resources Center documents — Nigeria's ICPC Act — comes
  from the body it is about. The three constitutions rest on reproduced texts
  (the Comparative Constitutions Project, Wikisource) because the custodians
  (Kenya Law, ULII) could not be read from the build environment — bot
  filters and a broken TLS chain. Their wording should be confirmed against
  the custodian's own copy before a specific provision is relied on, and the
  documents say so on the page.
- Nigeria's constitution is deliberately incomplete: sections 1–111 of 320,
  with section 107 absent from the source used. The page states this rather
  than padding it.
- Kenya's and Uganda's constitution pages are ~450 KB of HTML each.
  Complete, and heavy on a low-bandwidth handset.
- Evidence files are stored in memory only, like every other store in this
  app — gone on restart, not encrypted at rest, and served with no
  authentication (the same stated gap the institution portal already has).
  Video and PDF metadata is not stripped; only JPEG and PNG are, because
  Go's standard library gives no way to parse the other two formats'
  metadata in the time available. The upload form states this rather than
  implying a guarantee for file types it doesn't cover.
- The on-behalf-of guides connect someone to a real legal aid body and
  create a timestamped record that a claim was raised. They do not, and
  cannot, get an appeal filed or a case heard — that would require a live
  integration with each country's legal aid and court systems that does not
  exist and was not built here.

## Day 11 — RAG, and a persistence layer that keeps the tamper-evidence promise

**Two features, one constraint.** The ask was a RAG system for the Resources
Center plus a pitch-ready demo, and the second ask — "production ready" —
pointed straight at the repo's own stated gap list, whose largest entry was
the missing persistence layer. Both were built with the same rule: a feature
that undermines the project's central claim is worse than a missing feature.

**The RAG design decision was made before any code.** A chatbot over civic
text is the opposite of what Mlinzi argues for — a fluent paragraph with no
provenance, able to invent a hotline, a deadline, a right. So the package is
cite-or-abstain by construction: chunks carry their provenance from the
existing stores, the extractive synthesizer can only emit corpus sentences
with citations, retrieval failures produce a visible refusal, and the
optional LLM path is phrasing-with-verification, not free generation. BM25
over an in-process index rather than embeddings: a few hundred chunks, zero
network on the default path, and a scoring function a reviewer can read —
an opaque similarity would have been the one unauditable part of the
pipeline in a project about auditable claims.

**Every retrieval gate in the shipped code was found by running the demo
against the real corpus, not by design.** The fixture tests all passed
while the live demo drifted — "lottery numbers for next week" retrieved a
helpline schedule on one shared word; a bribery answer drifted into an
article about judicial power on generic vocabulary; a Kiswahili question
was fed the English index because the demo (and, initially, the test)
built only one index. The fixes — a closed-class stopword list small
enough to read at a glance, a term-coverage gate, a rare-term exemption
so one-word queries still work, a vocabulary gate (at least half the
question's meaningful terms must exist in the corpus), and a relative cut
keeping only passages within 40% of the best hit — are each pinned by
tests that run against the embedded corpus (`corpus_test.go`), because a
fixture corpus of eight chunks cannot catch what 769 chunks of real legal
text does.

**A real bug found by instrumenting, in code I had just written.** Tracing
the drift led to `meaningful := terms[:0]` — filtering a slice in place
while another function still iterated the original backing array,
silently re-weighting the query. Fixed with a fresh slice; the comment in
`index.go` records why, because this is exactly the kind of subtle aliasing
a later edit would reintroduce.

**The persistence design decision mirrored the RAG one.** "We save to disk"
is easy; the honest version is "persistence that keeps the tamper-evidence
promise". Ledger snapshots verify through `NewFromEntries` before a store
rebuilds on top of them; evidence files re-verify against their content
hashes; key material is never persisted, so in-flight guardian shares are
re-submitted after a restart rather than written down. The demo command
(`mlinzi demo-persistence`) edits history on disk and shows the restore
refusing it — the same shape as the ledger's own white-box tamper tests,
extended to the disk boundary.

**A test caught a bug the design missed, and it was the test that was
right.** The first round-trip test failed with "ledger tampered at entry
0" on an untampered chain: `SaveJSON` used `MarshalIndent`, which
re-indents embedded `json.RawMessage`, so the bytes a ledger hash commits
to changed across the snapshot round-trip. Snapshots are now compact JSON,
with a comment explaining that this is an integrity requirement, not a
formatting preference.

**Process note, recorded because it cost an hour.** Early in the session I
overwrote `internal/report/report_test.go` (247 lines of existing tests)
with a new file containing only the persistence tests. Recovered from git
history and merged rather than rewritten from memory — the originals
include tests whose exact expectations I could not have reconstructed
faithfully. All further test additions were made by appending to or
 editing existing files.

**What the LLM path is, stated so nobody oversells it.** Verification
checks that each sentence's content words appear in the retrieved passages
(a 60% coverage threshold), plus exact-match tokens for refs and numbers.
That catches fabrication — invented statutes, invented hotlines — not
subtle misparaphrasing, and it is phrasing support, not reasoning. The
extractive path remains the floor in every failure mode: endpoint
unreachable, slow, ill-formed, or quietly hallucinating. The page states
which method produced the answer, in all three languages.

**Testing.** New suites in `internal/persist`, `internal/rag` (fixtures +
real-corpus), and `cmd/mlinziweb` (ask page, persistence round-trips
through the real HTTP handlers, corrupt-snapshot boot refusal, evidence
byte-identity across restart). The round-trip tests boot a second app over
the same directory and assert user-visible state — not bytes written.
`go test ./...` is green across all packages.

**A real limit found by reading the verification code, not by a test
failing: `verifyAgainstChunks` cannot catch a negation flip.** It checks
token overlap between each generated sentence and the retrieved source
text — roughly 60% of a sentence's words need to appear in the corpus for
it to pass. This reliably catches an invented number, name or citation,
since the invented token is simply absent from the corpus. It does not
catch a sentence whose polarity is wrong: "you do not need a lawyer to
file this" and its true opposite score almost identically, because nearly
every content word overlaps and small words like "not" barely move a
60%-coverage ratio. Every test written for this function checks whether it
rejects unsupported *content* (wrong numbers, fabricated institutions) —
correctly — but none of them tests whether it rejects an inverted true
statement, because that gap wasn't visible until the verification logic
was read end to end looking specifically for what a bag-of-words check
structurally cannot express. No amount of more fixture data would have
caught this; it needed someone to ask what the check was actually
comparing, not just whether it passed.

Given the stakes — an inverted civic or legal right is close to the worst
failure mode this project could produce — the honest fix within the time
available wasn't to patch the verifier under deadline pressure, but to not
carry the risk into the submission at all: `RAG_LLM_ENDPOINT` is left
unset on the deployed instance, so `/ask` always answers extractively.
Nothing is generated, so nothing can be inverted. A real fix — at minimum
a negation/polarity check comparing each sentence's polarity against its
best-matching chunk before counting it as supported — is recorded as
future work, not shipped as solved.

**A real gap found by re-reading the code, not by a test: anyone could attach guardians to anyone's report.** `handleProtectCreate` took only the report ID from the URL path and never checked that the caller actually held the reporter's verification code — the one piece of proof this whole app treats as "you are the reporter" everywhere else (status lookup already required it). Since the institution portal displays every report's ID in plain text and is itself zero-auth, this meant anyone who opened `/institution` could immediately POST their own guardian names onto someone else's real report, no proof of authorship required at all. That's a materially worse gap than the already-stated "institution portal has no auth" limit — it doesn't just let someone advance a status, it lets a stranger insert themselves into the trust mechanism the reporter is depending on for their own safety. No existing test caught it because every test that exercises this path is written by someone who already has the code in hand, the same way a real reporter does — the gap only shows up when you ask what a caller *without* the code could do, which nothing was asking.

Fixed by requiring the verification code on `/report/{id}/protect`, checked via the same `VerifyReceipt` used for status lookup. The code is shown exactly once, on the reporter's own confirmation page, never on the institution portal — so the fix costs the legitimate reporter nothing (their own page now sends it as a hidden field) while actually closing the door for anyone else. Updated the three tests that exercise this path (`TestFullReportToProtectionFlow`, `TestWebSingleGuardianCannotRelease`, `TestPersistenceRoundTripKeepsGuardianCases`) to supply the code, since all three were, correctly, written as the legitimate reporter and so never exercised the missing check.

**A real gap found by using the app, not by a test: guardian release
reconstructs a key that unlocks nothing.** `guardian.ReleaseKey()` generates
32 random bytes at protection setup, with no cryptographic tie to the
report or its evidence — Shamir splits and later reconstructs those same
random bytes, correctly, but there was never anything on the other end for
the reconstructed key to decrypt. Report text and evidence remain plaintext
in their stores throughout, exactly as before protection was set up. This
matters more than a typical scope cut because it's not a feature that was
never started — it's a promise made explicitly on Day 2 ("encryption at
rest is scoped out and deferred to Layer 3's guardian key-release
mechanism") that Layer 3, as built, doesn't keep: it demonstrates the
threshold-reconstruction math in isolation, not the mechanism the Day 2
note implied. No test caught this because the guardian package's own tests
correctly verify Shamir reconstruction against whatever key they're handed
— they were never wrong about what they check, the gap is one layer up, in
what the reconstructed key is connected to.

Fixed the honest way rather than the fast way: encrypting content against
this key and decrypting it on release would make the promise true, but it's
real engineering work this close to a deadline, so instead every place that
implied otherwise was corrected to say what actually happens — the case
dashboard (a stated-scope note shown on release), the README (the Protect
layer description, the Layer 3 status line, and a new stated-gap entry),
and the demo video script, which said aloud that "evidence is split across
guardians" and "guardians... release it" before a single frame was
recorded. Wiring the key to actually gate access to content is recorded as
the next step, not claimed as done.

**A real gap caught in review, not by a test: `fly.toml` claimed
persistence it didn't have.** Setting `MLINZI_DATA_DIR = "/data"` in
`fly.toml` with no `[[mounts]]` volume attached means `/data` is just part
of the machine's own disk image — wiped on every `flyctl deploy`, since a
deploy rebuilds the machine from the new image rather than reusing its old
disk. The original comment said this survived "redeploys of the same
machine," which was not true for Fly specifically; only restarts of an
already-running machine would have survived. Go tests could not have caught
this — it's a platform-behavior gap, not a code bug, and the round-trip
tests all boot against a local directory. Fixed by adding a real Fly volume
(`mlinzi_data`) mounted at `/data`, with the one-time `flyctl volumes
create` step called out in the README so it isn't skipped. Recorded here
because it's exactly the kind of claim this project's own principles say
should be measured, not asserted — and this one very nearly shipped
unverified.
