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

## Honest limits

- Kiswahili translations are unreviewed as of day 1.
- Some contact details rest on secondary sources (news outlets, NGO
  directories) where an official page could not be reached. Each is marked
  `secondary` in the data rather than presented as official.
