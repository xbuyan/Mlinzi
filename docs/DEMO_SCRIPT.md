# Mlinzi — demo video script (v4, ~175 seconds)

v4 change from v3: covers every shipped feature, not just Know/Report/
Protect — adds Ask (RAG), evidence upload, and the on-behalf-of/detention
guides. That makes this noticeably longer than a typical 60–90s hackathon
clip. **Check the actual submission rules for a stated time limit before
finalizing** — nothing in the handoff notes one, but "comprehensive" and
"under the judges' limit" can conflict, and a video that gets cut off or
skipped for running long costs more on Presentation than a missing feature
would. If there is a hard cap, cut in this order: on-behalf-of/detention
(1:40–1:55) first, then the second Ask question (1:00–1:10) — both are
real features but the least likely to be what a judge remembers.

Two windows needed: a terminal (most of the video) and a browser, for the
two segments with no CLI equivalent (evidence upload, detention guides).
Cut between them live rather than editing in post if you can — it reads as
more honest and is one less thing to get wrong before the deadline.

```
./mlinzi show ke-police-misconduct
./mlinzi list UG
./mlinzi resource uganda-constitution
./mlinzi demo-rag
./mlinzi demo-full
./mlinzi demo-persistence
```

---

**[0:00 – 0:12] — Before running anything. Black terminal, or prompt only.**

> "Someone's stopped at a stage in Kisumu. An officer wants two hundred
> shillings to let them pass. This happens constantly — and the real
> problem is they don't know IPOA exists, don't know it's free, don't know
> it works without airtime. Nothing gets reported, because the information
> gap comes first."

**[0:12 – 0:18] — Run `./mlinzi show ke-police-misconduct`. Let it scroll to institutions.**

> "This is Mlinzi answering that gap. Sourced, dated, and it runs from a
> single binary with zero network calls."

**[0:18 – 0:32] — Pause on the two disputed IPOA toll-free numbers.**

> "And here's something we didn't design, we found it. Two official
> sources publish two different IPOA hotlines. So we show both, flagged,
> instead of picking one and hoping. That's what happened when we actually
> checked."

**[0:32 – 0:50] — Run `./mlinzi list UG`, then `./mlinzi resource uganda-constitution`.**

> "Scalability isn't a slide claim — watch it happen. Uganda: same three
> guides, its own institutions, its own constitution — 288 articles,
> sourced, with a caveat on exactly where the text came from. Adding a
> country was a data folder. Nigeria's in too, with its constitution
> explicitly marked incomplete rather than padded to look finished."

**[0:50 – 1:10] — Run `./mlinzi demo-rag`. Let the first question and its citation print, then let the abstention question print.**

> "Ask Mlinzi answers questions in plain language — but only from this same
> sourced corpus, nothing improvised. Every answer carries its citations.
> And when a question falls outside what's sourced — here, something with
> no civic content at all — it says so instead of guessing. That refusal
> is the feature: an assistant that can admit it doesn't know is one whose
> answers you can actually act on."

**[1:10 – 1:25] — Cut to `./mlinzi demo-full`, let Step 1 and Step 2 print.**

> "Reporting is the harder part, because it's traceable, and sometimes
> dangerous. A report is hash-chained the moment it's filed. No name
> required. No institution, including the one receiving it, can quietly
> edit or delete it afterward."

**[1:25 – 1:40] — Cut to the browser: submit a report with a photo attached.**

> "Reports can carry evidence — photos, video, documents. Every photo is
> stripped of its metadata before it's stored, because a phone photo's GPS
> location is a worse leak than the report text for someone under threat.
> The file's hash chains into the same ledger entry as the report — a
> swapped photo is exactly as detectable as edited text."

**[1:40 – 1:55] — Still in the browser: open a wrongful-detention guide, scroll to the legal aid contact.**

> "Not everyone who needs this can file it themselves. Someone can report
> on behalf of a person in detention, openly recorded as such — pointing to
> the real national legal aid body, not a form that pretends to file an
> appeal it can't file. We were explicit about that limit because
> overstating it here would be worse than not building it."

**[1:55 – 2:15] — Back to terminal: `./mlinzi demo-escalation` (or the escalation step of `demo-full`), let check-in, missed check-in, and threshold release print.**

> "If reporting puts someone at risk, they set a dead man's switch. Miss
> your check-in, and it escalates. Release needs a threshold of
> guardians acting together — no single guardian, and not us either, can
> reconstruct that key alone. Wiring that key to unlock the reporter's
> actual evidence, not just prove the mechanism, is the next milestone —
> stated here, not implied as already done."

**[2:15 – 2:45] — Run `./mlinzi demo-persistence`. Let the full output print
without cutting away — the round trip and the tamper-refusal are the same
command, back to back, so this is one continuous take, not two.**

> "None of this lives only in memory. Reports and cases survive a restart —
> we tested that on the actual deployed server, not just locally. And if
> someone edits history on the disk itself, the chain refuses to load it —
> not a claim in a deck, a command anyone can run."

**[2:45 – 2:55] — Closing. Terminal or repo page.**

> "Back to that officer at the stage. Now there's a path — and if the path
> is dangerous, there's a floor under it. Built with AI tools, logged
> honestly, including the limits we haven't closed yet."

---

**Timing note:** the bracketed ranges above sum to ~175s and assume brisk
cuts with no dead air waiting for output to scroll — rehearse once with a
stopwatch before recording for real, since CLI output speed and browser
page-load time will shift these numbers in practice more than narration
pace will.
