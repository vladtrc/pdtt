# Concepts to question

Foundational assumptions in this repo worth revisiting. These are not bugs to
fix today — they are design decisions that keep generating bugs and should be
challenged before the next big feature lands.

The meta-pattern behind most of them: **the repo keeps encoding structural
decisions as runtime data** — string-valued types, positional `[]Value` slices,
booleans read mid-interpolation. Each is cheap to add and expensive to debug.
The morph rewrite (`internal/pdtt/morph.go`) fixed one instance of this (the
`closed` flag leaking into interpolation); these are the rest.

| # | Concept | Severity | Contained? |
|---|---------|----------|------------|
| 01 | [Untyped animation state (`a.start []Value`)](01-untyped-anim-state.md) | high | yes — **done** (morph migrated; rest is legitimate) |
| 02 | [`Vec` is 3D in a 2D system](02-vec-is-3d-in-a-2d-system.md) | medium | no (wide) |
| 03 | [Magic-string types (`"__family_proxy__"`)](03-magic-string-types.md) | medium | yes |
| 04 | [`compile.go` mixes plan-time and run-time](04-compile-mixes-plan-and-runtime.md) | high | seam **established** on morph; rest incremental |
| 05 | [Open/closed split still in the renderer](05-open-closed-split-in-renderer.md) | low | yes |


u think not as smart llm could handle these?

● Honestly? It depends less on raw "smartness" than on three things: is the decision already made,
  is the blast radius bounded, and do tests catch mistakes if it goes wrong. A weaker model is fine
  when all three hold and dangerous when they don't. By that test:

  - 03 (magic-string types) — yes. Contained, the fix is named, and the compiler flags every missed
  call site. A cheaper model would do this cleanly.
  - 05 (open/closed in renderer) — yes, but only after you answer the question the doc poses ("is
  static draw legitimately different?"). The edit is tiny; the judgment isn't. Decide it, then a
  weaker model executes.
  - 02 (Vec → 2D) — maybe. It's wide but mechanical: flip the type, then chase compiler errors to
  zero. A weaker model can grind that if it doesn't give up or get creative halfway — the risk is a
  40-file diff where it starts "fixing" things that weren't broken. Bounded by the compiler, so
  recoverable.
  - 01 (typed anim state) — risky. The failure mode here is silent positional/type bugs, which is
  exactly what weaker models produce and exactly what won't surface unless tests cover that verb.
  Sliceable to one-verb-at-a-time, but each slice needs a test guarding it first.
  - 04 (compile split) — no. That's architecture and taste; the doc itself says "deliberate refactor
  with its own design pass." A weaker model would more likely entrench the mess than fix it.

  The real tell: I wrote these as decision-and-rationale docs, not task lists. They say "question
  this," not "do step 1, 2, 3." A weaker model needs the opposite — the call pre-made and the work
  pre-sliced, with a test as a tripwire.

  So the useful move, if you want to hand any of these off cheaply: let me convert the ones you pick
  (03 and 05 are the natural first two) into tight execution specs — decision stated, steps listed,
  the test to add named up front. Want me to do that for any of them?


  What this hands to the cheaper model now

  04's remaining verbs are no longer a design problem — they're mechanical: copy the xAnim{ start;
  step } shape, one verb per slice, each behind a stepping test like the two I added. The judgment
  call is spent; the grind is offloadable. That's the clean line you wanted between what needed
  taste and what doesn't.

  I did not commit — say the word and I'll commit this slice (and it's a natural standalone commit).
