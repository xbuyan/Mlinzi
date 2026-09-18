# Mlinzi — Written Summary

## Track

Cross-track: **Safety, Reporting & Protection**, with **Transparency &
Accountability** built into Layer 2's status trail. Layer 1 (civic
information) is the theme's direct answer — "information you can trust" —
and Layers 2–3 turn that information into something a person can safely act
on and be protected while doing so.

## The problem, briefly

People rarely fail to get help because the right institution doesn't exist.
They fail because they don't know it exists, can't verify what they're told
about it, or reporting to it puts them at risk. Mlinzi addresses all three in
one system rather than three separate tools.

## Information sources

Every fact in `data/ke/guides.json` is attributed inline with publisher, URL,
retrieval date, and a confidence level (`official`, `secondary`, or
`conflicting`). Primary sources used:

- **Ethics and Anti-Corruption Commission** (eacc.go.ke) — bribery reporting
  channels, mandate under the EACC Act 2011
- **Independent Policing Oversight Authority** (ipoa.go.ke) — police
  misconduct complaint process, mandate under the IPOA Act 2011
- **Kenya National Commission on Human Rights** — secondary reporting route
  for police misconduct
- **Healthcare Assistance Kenya / National GBV Helpline 1195** (hakgbv1195.org)
  — the national gender-based-violence helpline
- **UN Women Africa, Daily Nation, Commonwealth Says No More, Malaica** —
  secondary sources used where an official page could not be reached,
  explicitly marked `secondary` rather than presented as official

One genuine finding shaped the product itself: EACC's toll-free number is
consistently published, but IPOA's is not — two different numbers (0800 720
434 and 1559) appear across credible sources with no way to confirm which is
current. Rather than picking one, this became a designed capability: a
`Conflicting` confidence level and a `Disputed()` check that surfaces both
numbers to the user with the disagreement flagged, instead of silently
resolving it. A verification problem became a feature the brief specifically
asks for.

## Approach to trust and accuracy

Three guarantees are enforced in code, not just claimed:

1. **A guide without a source or a verification date does not load.**
   `guide.Store.Load` validates every entry and fails the whole dataset
   rather than silently serving an unsourced fact.
2. **A guide without next steps does not load.** Information that doesn't
   tell someone what to do next is exactly what this project exists to fix.
3. **A report's history cannot be quietly edited.** Reports and their status
   changes (`submitted → acknowledged → resolved`) live on a tamper-evident
   hash chain (`internal/ledger`) — there is no status field to overwrite,
   only new, chained entries. An attempt to skip straight from `submitted` to
   `resolved` is rejected outright, and is directly tested
   (`TestAdvanceRejectsSkippingAStep`).

Protection follows the same principle of provable rather than promised
security: a reporter's release key is split via Shamir's Secret Sharing
(`internal/shamir`) across named guardians, and the platform never stores
enough to reconstruct it alone. This is directly tested
(`TestNoSinglePartyCanReleaseAlone`) rather than asserted in prose.

## Evidence upload and reaching people who can't reach us

A reporter can attach photos, video, or documents to a report; a file's hash
enters the same tamper-evident ledger entry as the report text, so it is
exactly as provable and exactly as tamper-evident as the report itself.
Every image is decoded and re-encoded before storage specifically to strip
EXIF metadata — a phone photo routinely carries the GPS coordinates and
device identifiers of whoever took it, which is a materially worse leak than
the report text itself for a reporter under threat. Orientation is read and
corrected before that metadata is discarded, so stripping it doesn't
silently rotate the photo.

A second addition targets people the brief's "help people improve how they
engage with governments" goal doesn't reach by default: someone with no
phone, no literacy, or no network access — most concretely, someone already
in detention. An on-behalf-of flag lets a trusted third party file for them,
recorded openly rather than posed as a first-person account. New guides for
Kenya, Uganda, and Nigeria connect a wrongful-arrest or wrongful-imprisonment
claim to the real national legal aid body in each country, sourced from each
body's own current contact page — deliberately scoped as a directory and a
timestamped record, not a claim that Mlinzi can get an appeal filed or heard,
which it cannot and does not pretend to.

## How AI tools were used

Used throughout the build, with a running log kept from day one rather than
reconstructed at submission time (`docs/AI_USAGE.md` in the repo). In brief:

- **Design decisions were made first, by me; AI drafted the code from them.**
  The core idea, the three-layer architecture, and the working principles
  (source-tracked data, forward-only status, no single party can release
  alone) came from prior work on tamper-evident evidence systems, not from
  the AI.
- **Civic contact information was researched via AI-run web search**, with
  every fact independently attributed rather than recalled from model memory.
- **A real bug was caught and fixed, not hidden.** The first CLI build
  located its data directory via the Go toolchain at runtime — it passed
  every test (which run inside the module) and then failed the moment the
  compiled binary ran on a machine with no Go installed, which is exactly the
  "basic device, no install step" case this hackathon calls out. Caught by
  manually running the built binary outside the repo, fixed by embedding the
  dataset into the binary, which also made the shipped tool fully offline.
- **A second real bug, later in the build:** switching the report handler to
  support file uploads required parsing multipart form data, which broke
  every existing plain-text report submission — a non-multipart POST makes
  Go's `ParseMultipartForm` return an error even though it has already
  parsed the form fields correctly, and that error was initially treated as
  fatal. Caught only because the full pre-existing test suite was re-run
  after the change, not assumed still-passing because the new tests passed.
- **136 test functions across nine packages** were AI-drafted and
  human-reviewed, including white-box tests that directly mutate a stored
  ledger entry to prove tampering is detected, an exhaustive test of GF(256)
  field arithmetic across all 255 nonzero elements, and — after the Resources
  Center was expanded to whole constitutions — a coverage report the extractor
  prints on every run, so "this document is complete" and "this one is not"
  are measured claims rather than hand-written caveats.
- **Known limits are stated, not hidden**: Kiswahili translations are
  AI-drafted and unreviewed by a first-language speaker as of submission;
  report content is stored in plaintext in this proof of concept, with
  encryption at rest deferred to the Layer 3 key-release mechanism it
  depends on rather than faked ahead of it.

## Accessibility and offline support

The CLI runs from a single binary with zero network calls — the guide
dataset is embedded at compile time, so looking up a guide, filing a demo
report, and running the full guardian-escalation walkthrough all work with
no connection at all. The web UI is installable and its Know layer (the
home page and every guide) works fully offline via a service worker,
including on a first visit with no prior connectivity. Reporting and
protection are deliberately not offline-capable in the web UI: a report's
value depends on reaching a shared, verifiable ledger at the moment it's
submitted, and building real offline-write support (local queuing,
encryption at rest, sync conflict handling) is a substantial feature in its
own right that wasn't attempted rather than faked.

## Potential impact

The three-layer pattern (verified information → tamper-evident reporting →
guardian-based protection) is not specific to Kenya or to these three
situations. Three countries (Kenya, Nigeria, Uganda) now load from the same
code path, and the Resources Center carries the Kenyan, Ugandan and Nigerian
constitutions plus Nigeria's ICPC Act as primary documents a guide can cite
directly — every article of the first two, which the page states as a measured
count rather than a claim. Jurisdiction-scoped data directories
mean a new country's civic information is a data change, not a rebuild; the cryptographic layers
underneath are already country-agnostic, and a document is a file in one flat
folder the same way a country is. The same architecture could extend
to land disputes, election-related violence reporting, or labor rights —
anywhere information trust and reporting safety are the two things standing
between a person and protection. The wrongful-detention guides are one
concrete step in that direction already taken: the same guide/institution
pattern that serves a bribery report today serves someone who cannot file
for themselves tomorrow, with no new architecture required.
