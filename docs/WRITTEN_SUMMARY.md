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
- **50+ tests across five packages** were AI-drafted and human-reviewed,
  including white-box tests that directly mutate a stored ledger entry to
  prove tampering is detected, and an exhaustive test of GF(256) field
  arithmetic across all 255 nonzero elements.
- **Known limits are stated, not hidden**: Kiswahili translations are
  AI-drafted and unreviewed by a first-language speaker as of submission;
  report content is stored in plaintext in this proof of concept, with
  encryption at rest deferred to the Layer 3 key-release mechanism it
  depends on rather than faked ahead of it.

## Potential impact

The three-layer pattern (verified information → tamper-evident reporting →
guardian-based protection) is not specific to Kenya or to these three
situations. Jurisdiction-scoped data directories mean a new country's civic
information is a data change, not a rebuild; the cryptographic layers
underneath are already country-agnostic. The same architecture could extend
to land disputes, election-related violence reporting, or labor rights —
anywhere information trust and reporting safety are the two things standing
between a person and protection.
