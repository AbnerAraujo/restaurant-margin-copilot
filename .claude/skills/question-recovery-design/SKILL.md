---
name: question-recovery-design
description: Design how a chat or agent interface recovers when a user's question is ambiguous, wrong, out of scope, or unanswerable — so the user stays productive instead of hitting a dead end. Use when building or reviewing an "ask anything" surface, a chatbot, an LLM-backed Q&A feature, an agent taking natural-language input, or any assistant that must sometimes say "I can't answer that." Covers pre-answer classification (answerable / ambiguous / unanswerable), refuse-rather-than-fabricate discipline, pairing every refusal or clarification with a concrete next step (a clarifying question, a reformulation, or a list of what IS answerable), proactively surfacing capabilities instead of leaving users to discover them by trial and error, and logging refusals as a backlog signal. Trigger even if the user just says their bot "gives weird answers to bad questions," "doesn't know when to say I don't know," "needs better error handling for chat," or "users get stuck when they ask something we can't do."
---

# Designing recovery from wrong or ambiguous questions

## Why this exists

Any interface that accepts free-form natural-language input will regularly
receive questions it cannot, or should not, answer confidently: the question
is ambiguous (more than one reasonable reading), it references something the
system has no data for, it's out of scope, or it's a fragment that only makes
sense in a context the system hasn't tracked. The naive failure modes are both
bad, in different directions:

- **Fabricate an answer anyway.** The system produces something plausible and
  confident. The user has no way to tell a grounded answer from a guess, so
  trust erodes the first time a confident answer turns out to be wrong — and
  it erodes silently, because the user often never finds out it was wrong.
- **Refuse with a dead end.** "I can't help with that." The system is honest
  but has now stranded the user with no path forward. They're left guessing
  what phrasing might work, which trains them to stop asking altogether.

Both failures are avoidable with the same discipline: **treat "recovering from
a bad question" as a first-class product surface, not an error case bolted on
after the happy path is built.** A wrong question handled well is often the
moment a user learns what the product can actually do — that's a chance to
build trust and competence, not just avoid embarrassment.

This is a UX and system-design methodology, not a specific classifier
implementation. It applies whether the "brain" doing classification is a
cheap LLM call, a rules engine, a search-relevance threshold, or a human
support agent's triage step.

## The five moves

### 1. Classify before you answer, cheaply

Before running the expensive path (the real reasoning call, the expensive
retrieval, the full agent loop), run a cheap, narrow pass whose *only* job is
to sort the incoming question into a small number of outcomes — typically
**answerable / ambiguous / unanswerable**. Don't let the same call that
produces the final answer also decide whether it should have answered at all;
a model (or system) that's mid-way through composing a confident response has
every incentive to rationalize answering rather than to flag its own doubt.

Keep this pass narrow on purpose:
- Give it only what it needs to classify (the question, and just enough
  grounding context — e.g. what data/date-range/scope actually exists) —
  not the whole system prompt or a full conversation history it doesn't need.
- Make its output a small fixed shape (a classification enum plus one or two
  short fields), not open-ended prose. A fixed shape is verifiable: you can
  reject a malformed response instead of accidentally treating it as an
  answer.
- Use the cheapest capable model/mechanism for this step — it's a
  classification task, not the task that needs your best reasoning. Reserve
  your most capable model for the step that actually has to think.

**Follow-ups are part of classification, not a special case.** If the user
replies to a clarifying question with a bare fragment ("yes", "the second
one", "Saturday only"), that fragment is meaningless in isolation. Classifying
it alone will confidently misfire ("no question was asked"). Carry forward
exactly the two things needed to resolve it — the original question and the
clarifying question that was asked — and classify the *resolved* question,
not the bare reply. Passing the whole conversation transcript is usually
overkill and gives the classifier more latitude than the job needs.

### 2. Never fabricate — refuse with a specific, honest reason

When the classifier says "unanswerable," resist every temptation to let the
downstream step take a guess anyway "since we're already here." A refusal
should:
- State plainly and specifically **what's missing** — not "I can't answer
  that," but "I don't have delivery-platform data for that date" or "no
  campaign matches that name in the data I have."
- Never smuggle in a fact that wasn't already established (a date, a
  quantity, an entity) just to make the refusal sound more complete.
- Be produced by the same discipline you apply to any other output the user
  might trust: if there's a fixed output shape for an answer, there should be
  a fixed output shape for a refusal too, so "I don't know" can't quietly turn
  into "I don't know, but here's a plausible number anyway."

The trust argument is asymmetric: a wrong answer confidently presented is
worse than an honest refusal, because a refusal is legible (the user knows to
distrust it) while a wrong answer isn't (the user has no signal to distrust
it). Optimize for that asymmetry, not for "answer rate."

### 3. Ambiguous is not automatically a dead end — resolve it two ways

An ambiguous question has more than one reasonable reading, but that doesn't
always mean you must stop and ask. Give the classification step exactly two
levers, and require it to use exactly one:
- **Ask one specific clarifying question**, when the interpretations genuinely
  diverge and getting it wrong would matter (e.g. "does 'the weekend' include
  Friday?"). Make the question concrete enough that the user can answer it in
  one sentence or one tap — not "could you clarify?" but "did you mean Friday
  through Sunday, or just Saturday and Sunday?"
- **State the assumption and proceed**, when a reasonable default exists and
  getting it wrong is low-stakes and easy to correct (e.g. "week" defaults to
  the trailing 7 days ending on the last day of available data). Say the
  assumption out loud in the response so the user can correct it if it's
  wrong — silently picking an interpretation is just a quieter version of
  fabrication.

Never do both, and never do neither — a classifier that hedges by doing a
little of each produces a mushy, unhelpful answer that satisfies nobody.

### 4. Every refusal or clarification carries a next step

A correct refusal is still a dead end unless it hands the user a way back in.
Pair every non-answer with at least one of:
- **Quick-reply options for a clarifying question** — 2–4 short, complete
  phrasings the user can select with one tap rather than having to compose a
  reply from scratch. Cap the count; a wall of eight buttons defeats the point
  of a fast choice.
- **A short, concrete list of what the system actually can answer** — drawn
  from the same real, tool-backed set of capabilities the rest of the product
  uses (never the model freelancing a description of itself), so a refusal
  doubles as a capability lesson instead of a wall.
- **A reformulation hint** — when the failure is "almost right" (a near-miss
  entity name, a date just outside range), say what would need to change,
  not just that it failed.

The options offered back are *phrasings of a reply*, not new facts — treat
whichever one the user picks exactly as if they had typed it themselves and
re-run it through the same classification step. Don't let the presence of
convenient buttons become a second, less-scrutinized path into the system.

### 5. Show capabilities proactively, not just reactively

Don't make discovery something a user only gets after they've already hit a
wall. Surface a small set of concrete example questions or capability
descriptions on the *empty state* — before any failure has occurred — using
the exact same presentation (same component, same visual language) that
later appears after a refusal. A returning user should recognize "oh, this is
the same kind of suggestion I saw before" rather than meeting three different
lookalike mechanisms for the same underlying idea. This does double duty:
new users ramp faster, and the reactive refusal-time suggestions feel
familiar rather than like a consolation prize.

### 6. Log every refusal and clarification as a backlog signal

An invisible refusal is a silent frustration; a logged one is product data.
Record, for every interaction, at minimum:
- which outcome fired (answerable / ambiguous-asked / ambiguous-assumed /
  unanswerable),
- the resolved question text,
- cost/latency of the classification (and any downstream) call.

Do this from the very first real call your system makes, not as a
retrofit — instrumentation added after the fact almost always has gaps right
where the interesting failures are. Persisting these interaction records lets
you answer "what are people asking that we can't do yet?" with real numbers
instead of anecdotes, which is exactly the input a roadmap needs. Treat a
spike in one particular kind of refusal as a feature request, not noise to
suppress.

## Checklist

Use this when designing or reviewing a free-form-input surface:

- [ ] Is there a classification step that runs *before* the expensive/final
      path, on narrow input, producing a fixed output shape?
- [ ] Does that classifier use the cheapest model/mechanism capable of the
      job, reserving stronger reasoning for the step that actually needs it?
- [ ] Can a malformed or unparseable classification response be rejected
      outright, rather than silently defaulting to "answerable"?
- [ ] Does "unanswerable" always produce a specific, honest reason instead of
      a generic "I can't help with that"?
- [ ] Is it structurally impossible for a refusal to also carry a fabricated
      answer, number, or provenance? (Validate this, don't just intend it.)
- [ ] For "ambiguous," is exactly one of {ask a specific question, state an
      assumption and proceed} chosen — never both, never neither?
- [ ] Does every clarifying question offer short, complete, one-tap reply
      options where practical?
- [ ] Does every refusal hand back a concrete list of what IS answerable,
      drawn from the real capability set — not the model improvising?
- [ ] Do follow-up replies to a clarifying question get resolved against the
      original question + the question that was asked, rather than
      classified as a bare fragment?
- [ ] Are capabilities shown proactively on the empty state, using the same
      presentation that appears reactively after a refusal?
- [ ] Is every interaction — including clarifications and refusals — logged
      with enough detail to mine "what can't we answer yet" as a backlog
      signal, from the first real call onward?

## Anti-patterns

| Anti-pattern | Why it fails |
|---|---|
| One model call both decides *and* answers | The model composing a confident answer is a poor judge of its own uncertainty; it rationalizes rather than refuses. |
| Generic refusal copy ("I can't help with that") | Honest but useless — gives the user nothing to correct or try next. |
| Classifying a bare follow-up reply ("yes") on its own | Meaningless in isolation; produces a confidently wrong classification (e.g. "no question was asked") and turns every clarification into a dead end. |
| Silently picking an interpretation for an ambiguous question | Quieter version of fabrication — the user has no signal their question was reinterpreted. |
| Capability hints that only appear after a failure | Forces trial-and-error discovery; a proactive empty state would have prevented the failure in the first place. |
| Refusals that aren't logged | Turns a real, minable backlog signal ("users keep asking for X") into invisible, cumulative user frustration. |
| Unbounded clarifying-option lists | A wall of choices defeats the purpose of a fast, one-tap resolution. |

## Worked example (case study, not a template to copy verbatim)

This methodology was generalized from a real implementation in a restaurant
margin-reconciliation copilot, referenced here to make the moves concrete —
adapt the *shape* of the pattern to your own stack, not these exact files.

- **Move 1 (cheap classification gate):** a small classifier call sorts every
  incoming question into `answerable` / `ambiguous` / `unanswerable` before
  any expensive tool-calling or reasoning step runs, using a cheap model tier
  reserved for classification while a stronger model tier is reserved for the
  narration/explanation step downstream. Its output is a fixed JSON shape; an
  unparseable reply is treated as a hard failure, never silently defaulted to
  "answerable."
- **Follow-up handling:** a bare reply to a clarifying question is composed,
  deterministically in plain code (not a second model call), with the
  original question and the clarifying question that was asked, so the
  classifier always sees a self-contained, resolvable question — never a
  fragment.
- **Move 2 (refuse with a reason, structurally enforced):** the persistence
  layer itself rejects any record where a refusal carries an answer or
  provenance data — the "no fabricated answer alongside a refusal" rule is a
  validated invariant, not just a convention someone might forget.
- **Move 3 (ambiguous → ask or assume, never both):** the classifier's output
  shape has separate fields for a clarifying question and a stated
  assumption; a response validator rejects an output that sets both or
  neither.
- **Move 4 (next step attached to every non-answer):** a clarification
  renders with 2–4 tappable reply options; a refusal renders with a specific
  missing-data list *and* a short set of example questions drawn from the
  product's real, typed capabilities — the same presentation component used
  elsewhere, not custom copy invented per refusal.
- **Move 5 (proactive capability surfacing):** the same suggested-question
  component that appears after a refusal also appears on the empty state and
  as a persistent "ideas" affordance, so a user meets one consistent
  mechanism rather than three different ones.
- **Move 6 (refusals as backlog signal):** every interaction — answerable,
  clarified, or refused — is persisted with its classification outcome,
  resolved question text, and cost/latency, from the very first real call,
  specifically so refusal patterns become a queryable product signal rather
  than an invisible support burden.

A second-pass detail worth calling out: the cheap classifier's own
first-draft wording for a clarifying question or refusal reason is sometimes
serviceable but not well-written. Rather than asking the cheap model to also
be a good writer, a second, narrow pass — using the stronger model tier
already reserved for narration — rewrites *only the prose* of an ambiguous/
unanswerable message, while being structurally prevented from re-deciding the
classification (its output shape has no classification field at all). This
is an optional refinement, not a required part of the methodology: apply it
only if you're finding that your cheap classifier's raw wording is
noticeably harming the recovery experience, since it's a real, disclosed
second cost for exactly the hardest cases.

## Related skills

- **proactive-guidance-design** — the mirror-image skill, covering the
  success half of this same surface: the zero-state capability list before
  the first question, and the post-answer follow-up suggestion after one
  succeeds. This skill's move 5 (proactive capability surfacing) is that
  skill's move 1, generalized to also fire reactively on a refusal; read it
  for how the same `SuggestionChips`-style component should extend to a
  post-answer placement so guidance stays one voice across the whole
  interaction, not just the failure half of it.
- **api-design** — for the equivalent discipline at a pure API boundary
  (structured error responses, RFC 9457 problem details) when there's no chat
  surface to render a clarification or suggestion chip into.
- **ux-writing** — for the tone and wording of refusal/error/empty-state copy
  once you've decided what the message needs to say.
- **observability** — for structuring the "log every interaction" instinct
  in move 6 into real wide-event/telemetry practice at scale.

## When this doesn't apply

Skip this for single-purpose tools with a fixed, narrow input schema (a form,
a typed API parameter) where "ambiguous input" isn't really possible because
the input space is already constrained — validation error handling there is
better served by **api-design**. This methodology is for surfaces that
accept genuinely free-form natural-language questions, where the space of
"things the user might type" is larger than the space of "things the system
can actually do."
