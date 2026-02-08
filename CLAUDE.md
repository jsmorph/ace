# Style Guide

## Core Principles

You are writing for an expert with no tolerance for bullshit,
junk language, guessing, fashion, or anything other that the
highest quality technical work.  No adolescent crap.  No
selling.

When you are writing docs, use modest in-line markup (for
emphasis, etc).  Do not use a hyphen when you should use a
colon.  You should probably never use hyphens.  Do not go
crazy with sub-sub-sub heading.  Consider using tables
instead of a lot of (say) level 5 subheadings.  Don't
over-use lists.  Our audience can read complete paragraphs
(and often likes to do that).

Use complete sentences and correct grammar.  No split
infinitives, vague references, slang, garbage style like
"leverage" or "journey", or any hipster crap.  You are not
selling, pitching, or persuading.  Avoid passive voice but
don't go to extremes.  Every word should be meaningful.
Remove those that aren't.  Silted, contrived, and/or
pretentious transitions are bad.  Avoid jargon or fancy terms
(unless genuinely accurate and helpful).

When writing documents, you should edit yourself like a
serious author with an old-school editor at The New Yorker.
Think like John McPhee perhaps.

Use the Oxford comma obviously.

**Absolutely no gratuitous or obvious comments in code or
elsewhere.**

Keep things as simple as possible.  Simplicity and clarify
are the greatest virtues.  Minimize third-party dependencies.
Ask for approval for any introduced dependency.  You better
have a good reason.

Do not be lazy.  When you encounter a problem, work on it
directly.  Do not rush to some hack or work-around.  Instead
of guessing, search for and use **authoritative
documentation**.  When you encounter adversity, think.

Keep all interesting development notes, including links to
authoritative documentation, in a file called `devnotes.md`,
which you update as you go.  This document should be an
organized, thoughtful journal.  Include clear links to
authoritative docs as you need them.  You can include small
plans with checkboxes.  Be sure to include background
discussion, rationales, and other explanations for important
decisions.

Do not use colors in console output.  Do not use elipsis in
logging or other messages.

Do no swallow any errors. Simply logging an error counts as
"swallowing".

For commit messages: The first line is a headline, with the first
letter capitalized unless it's a symbol that really shouldn't be.
Always less than 60 characters.  No period at the end.  The body, if
one is necessary, is very concise, grammatical, and precise.  If a
body isn't really necessary, don't provide one.


## Ask for editorial clarification

When in doubt about how to draft something, *ask*.


## Document Structure

**Headings**: Avoid deep hierarchies (no
sub-sub-sub-headings). Two or three levels suffice for most
documents.

**Tables vs. lists**: When you have many parallel items with
attributes, consider using a table instead of a list. Don't
create five level-4 subheadings when a table would be
clearer.

**Lists vs. paragraphs**: Don't overuse lists. The audience
can read complete paragraphs and often prefers them. Use
lists for genuinely enumerable items, not as a substitute for
prose. Favor real paragraphs over listicle-like bullshit.

**Markup**: Use modest inline markup for emphasis. Use
colons, not hyphens, to introduce explanatory clauses.

## No junk language

Cut sentences that announce what follows rather than saying
it.

| Cut | Replace with |
|-----|--------------|
| "Below is a specification..." | "This specification..." |
| "There is also the matter of X." | Start with X directly |
| "A further limitation is cultural." | State the limitation: "Sigma requires..." |
| "It is not advocacy." | Delete (defensive) |

The "not merely X; it is Y" construction inflates the
subject. Just say what it is.

| Before | After |
|--------|-------|
| "not merely a technique for X; it is a response to Y" | "a technique for X that addresses Y" |
| "is fundamentally about" | "determines" or state directly |

## Filler Words to Cut

These words rarely add meaning. Delete them unless they carry
genuine meaning:

- **simply** — "They simply define" -> "They define"
- **itself** — "Iota itself is reliable" -> "Iota is reliable" (exception: "truth itself" when referring to the concept of truth, not instances of it)
- **underlying** — "the underlying intuition" -> "the intuition"
- **actual** — "actual legal standards" -> "legal standards"
- **clearly** — "clearly specified" -> "specified"
- **entirely** — "entirely application-dependent" -> "application-dependent"
- **merely** — see Rhetorical Puffery above
- **given** (as filler) — "a given question" -> "a question"

## Passive Voice

Passive voice often hides the actor or weakens the sentence.
Prefer active constructions.

| Passive | Active |
|---------|--------|
| "is designed to reveal" | "reveals" |
| "is treated as a legitimate outcome" | "constitutes a legitimate outcome" |
| "arguments are presented for and against" | "advocates present arguments for and against" |
| "Amendment is allowed" | "The Rules allow amendment" |
| "are initiated concurrently" | "run concurrently" |
| "to be run" | "to run" |

## Weak Verbs and Hedges

Replace hedged verbs with direct ones.

| Weak | Strong |
|------|--------|
| "seek to determine" | "determine" |
| "could help identify" | "identifies" |
| "remain viable" | "persist" |
| "is appropriate only in" | "fits" |

## Jargon and Stilted Phrasing

Replace bureaucratic or academic jargon with plain language.

| Jargon | Plain |
|--------|-------|
| "operationally mandatory determinations" | "required decisions" |
| "evidentiary fragility" | "whether the evidence supports it" |
| "unavoidable perception effects" | "random variation in how evidence is weighed" |
| "principled reflection of the evidence" | "honest acknowledgment that the evidence is inconclusive" |
| "the degree to which" | "how well" |
| "well suited to" | "applies to" |
| "not well suited for" | "not designed for" |
| "agnostic as to domain" | "domain-agnostic" |
| "provide a framework for determining" | "determine" |
| "defining feature" | cut; just state what it does |
| "raises similar boundaries" | "faces similar limits" |

## Academic Hand-Waving

These verbs hide what something actually does:

| Hand-waving | Direct |
|-------------|--------|
| "draws on a tradition" | name the source or cut |
| "reflects X's contention that" | "follows X:" or just state the idea |
| "embodies the intuition" | "implements the idea" |
| "rests on commitments" | "assumes" |
| "has roots in" | cut; name-dropping without substance |

Fields don't do things; people do:

| Wrong | Right |
|-------|-------|
| "Social epistemology has documented" | "Research shows" or cite specific work |
| "Epistemology has long recognized" | cut; just state the point |

Avoid jargon that sounds impressive but says little:

- "convergent truth tracking" — say "independent confirmation"
- "institutionalized epistemic humility" — say what the institution actually does
- "epistemological commitments" — "assumptions"

When tempted to cite a philosopher, ask: does the name add
information, or is it decoration? If decoration, cut it and
state the idea directly.

## Redundancy

Combine repetitive constructions.

| Redundant | Tighter |
|-----------|---------|
| "The Rules treat X. The Rules allow Y." | "The Rules treat X, allowing Y." |
| "the particular personnel or the particular trajectory" | "personnel or trajectory" |
| "within a trial, within a chain, within the proceeding" | "in a trial, in a chain, in the proceeding" |

## Vague references

Avoid vague references.

| Vague | Specific |
|-------|----------|
| "This is not always desirable." ("This" *what*?) | "This orientation is not universally desirable." |
| "In this regard..." | Cut or be specific |

## Grammar and Mechanics

- **Complete sentences**: Use them. Avoid fragments for
  effect.
- **Split infinitives**: "to thoroughly evaluate" ->
  "to evaluate thoroughly"
- **Articles**: "A Iota" -> "An Iota"
- **Parallelism**: "Neither X, nor Y" with clauses ->
  "Neither X or Y" when both follow a single verb
- **Hyphens**: "ill posed" -> "ill-posed" when used as
  compound adjective
- **Semicolons**: Use periods, not semicolons, to separate
  independent clauses. Semicolons are acceptable in
  legal-style enumerated lists: (a) first; (b) second; or
  (c) third.
- **Spacing**: Single space after periods; no double blank
  lines between paragraphs
- **Vague references**: Ensure "this", "that", "it" have
  clear antecedents

Consider using singular instead of plural when describing
some behavior.  Singular can avoid some confusion about
one-to-many.  Example: Instead of writing "X gimzos are
associated with Y gizmos" consider "An X gizmo is associated
with a Y gizmo." That way we don't wonder if we're talking
about one-to-one or one-to-many.  Not a hard rule but instead
something to consider.

## Banned Words and Phrases

These words are corporate-speak, hipster jargon, or empty:

- **leverage** (as verb)
- **journey**
- **utilize** (use "use")
- **impactful**
- **learnings**
- **cadence**
- **space** (as in "the AI space")
- **ecosystem**
- **synergy**
- **stakeholder** (unless genuinely appropriate)
- **robust** (often meaningless; be specific)
- **holistic**
- **streamline**
- **actionable**
- **best-in-class**
- **surface** (as verb) — use "reveal" or "expose"

## Acceptable Constructions

Not everything that looks like a pattern needs fixing. These
are often fine:

- **"not only...but also"** when making a substantive
  contrast (e.g., "reflects not only the evidence but also
  the evaluator's training")
- **"In summary"** at the end of a document when genuinely
  summarizing
- **"not"** when stating honest limitations (e.g., "Sigma
  does not eliminate these problems")
- **"serves two functions: First...Second..."** — clear
  enumeration is fine
- **"itself"** when genuinely emphasizing identity (e.g.,
  "Peirce defined truth itself as...")

## Review Process

When reviewing a document:

1. **First pass**: Scan for throat-clearing openings and
   rhetorical puffery
2. **Second pass**: Search for filler words (simply, itself,
   underlying, actual, clearly, entirely, merely)
3. **Third pass**: Identify passive constructions and weak
   verbs
4. **Fourth pass**: Flag jargon and stilted phrasing
5. **Fifth pass**: Check for redundancy and vague pointing
6. **Final pass**: Grammar and mechanics

Then repeat this entire process at least once.

Read sentences aloud. If a sentence sounds like it's selling
something or warming up to say something, it needs revision.

## Examples of Good Revision

**Before**: "Procedure Sigma is not merely a technique for
aggregating judgments; it is a response to fundamental
problems in epistemology."

**After**: "Procedure Sigma is a judgment aggregation
technique that addresses fundamental problems in
epistemology."

---

**Before**: "There is also the matter of independence. Sigma
assumes that the independent chains do not share interpretive
biases."

**After**: "Sigma assumes that the independent chains do not
share interpretive biases."

---

**Before**: "The procedure could help identify theses that
hold up under repeated scrutiny and distinguish them from
theses that depend on fragile or highly contingent
assumptions. It could also help expose situations in which
equally coherent but incompatible interpretations exist,
which is relevant for risk management."

**After**: "The procedure identifies theses that hold up
under repeated scrutiny and distinguishes them from theses
that depend on fragile or contingent assumptions. It also
exposes situations in which equally coherent but incompatible
interpretations exist."

---

**Before**: "These Rules provide a framework for determining
whether a question yields a stable adjudicative answer when
subjected to repeated, independent analysis."

**After**: "These Rules determine whether a question yields a
stable adjudicative answer when subjected to repeated,
independent analysis."
