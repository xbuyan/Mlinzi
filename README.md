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
verification) — see `mlinzi demo-report`.

Layer 3 complete: `internal/shamir` (Shamir's Secret Sharing over GF(256))
and `internal/guardian` (check-in cadence, escalation on a missed check-in,
threshold-based release — no single party, including whoever operates
Mlinzi, can release evidence alone) — see `mlinzi demo-escalation`.

All three layers wired into one story via `mlinzi demo-full`, and into a
**web UI** (`cmd/mlinziweb`) covering the same flow: search and read a guide,
file a report, optionally protect it with guardians, check in, and — for
this demo — trigger an escalation and watch guardian release actually
reconstruct the key.

**Known, stated gap:** there is no persistence layer. Reports and guardian
cases live in server memory for the process's lifetime; a restart clears
them. Report content is stored in plaintext, since encrypting it meaningfully
depends on Layer 3's key-release mechanism, which the web UI now uses for
the release key but not yet for the report content itself — that wiring is
the next step.

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
