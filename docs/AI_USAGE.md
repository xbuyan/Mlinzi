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

## Honest limits

- Kiswahili translations are unreviewed as of day 1.
- Some contact details rest on secondary sources (news outlets, NGO
  directories) where an official page could not be reached. Each is marked
  `secondary` in the data rather than presented as official.
