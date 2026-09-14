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
verification) — see `mlinzi demo-report` for a live walkthrough. Content is
stored in plaintext in this proof of concept; encryption at rest is deferred
to Layer 3, once the guardian key-release mechanism it depends on exists.

Layer 3 in progress: check-in cadence and guardian-based escalation on
silence.

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

## Run it

```
go test ./... -cover
```

## Data sources

Every claim in `data/ke/guides.json` is attributed inline with publisher, URL,
retrieval date, and a confidence level of `official`, `secondary` or
`conflicting`.

## How AI tools were used

See [docs/AI_USAGE.md](docs/AI_USAGE.md).

## License

MIT
