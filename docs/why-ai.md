# Why AI — and where it deliberately stops

Written to be said out loud, not just read. This design follows widely
applicable production-AI principles: deterministic financial calculation,
explicit uncertainty, constrained tools, and measured reliability — starting
with AI-by-Design step 3's most basic question: is AI actually required, and
where.

## The honest starting position

**Most of this system does not use AI at all.** Margin calculation,
reconciliation, promotion ROI, provenance, anomaly detection — all of it is
plain Go code with no model call anywhere near it. If someone asked "couldn't
a script do that," the answer is yes, and that's the point: it *is* a
script, deliberately, because financial arithmetic needs to be exact and
reproducible, and I was not going to let a model compute a number a real
owner would act on. That's not a limitation of the AI I used — it's the
first and most important design decision in the project.

## Where AI is actually used, and why each use is load-bearing

**1. The ambiguity gate** (Claude Sonnet 5 — moved from Haiku 4.5 on
2026-08-29 after Haiku proved unreliable at multi-year date comparison once
the live dataset grew past a single year; see `internal/llmclient/cost.go`)
— deciding whether a question
like "how was the weekend?" is answerable as-is, or ambiguous (does it
include Friday?), or references data that doesn't exist. This is a genuine
natural-language-understanding problem: you cannot regex your way to
detecting every form of ambiguity in free text someone might type. This is
a case where AI is required because the input is unstructured human
language — not because it's more interesting than the alternative.

**2. The explanation step** (Claude Sonnet 5) — turning an already-computed
number into a plain-language answer, including handling follow-ups and
rephrasings without me hand-writing a template for every possible way to
ask "how did we do this week." A fixed set of canned templates could cover
a narrow, closed set of question shapes — and if the question set were
small enough, that might genuinely be the right call, no model needed. It
breaks the moment a real owner asks something slightly off-template, which
is exactly the brittleness a narrow, cheap model avoids without needing to
be capable of anything more than narration.

## Why not skip AI entirely

I could ship a rules-based system with, say, ten canned question templates
and zero model calls. It would be cheaper and 100% deterministic. I didn't,
because the actual hypothesis being tested (see `docs/product-strategy.md`,
Hypothesis 3) is that owners engage with a question box, not a dashboard —
and a system that only understands ten exact phrasings isn't really
answering questions, it's a form with extra steps. The two places AI is
used are exactly the two places a deterministic function can't do the job:
understanding open-ended language in, and producing open-ended language out.
Nothing in between.

## Why not use AI for the computation, if it's already reading the data

This is the failure mode I designed against on purpose. Asking a model to
read the CSVs and compute margin directly would be faster to prototype and
would look identical in a demo — right up until it silently produces a
plausible-but-wrong number, which is worse than the system saying "I can't
compute this." That's not a hypothetical: it's a well-documented class of
error that production financial tools cannot tolerate, and it's why systems
handling financial data generally avoid light models where errors carry real
cost. This design follows that same widely applicable production-AI
principle.

## The one-sentence version, if asked directly

"AI is used exactly twice — to understand an ambiguous question, and to
narrate an answer in plain language — and nowhere near the arithmetic,
because the two things a model is actually good at are understanding and
producing open-ended language, and the one thing it must never do here is
compute a number someone's going to act on."
