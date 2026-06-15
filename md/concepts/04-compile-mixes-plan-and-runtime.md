# 04 — `compile.go` mixes plan-time and run-time

## Status

**Seam established on morph (the wedge).** Morph's `Start`/`Update` are no
longer 100 lines of closure over captured locals. They're thin adapters over a
`morphAnim` value (`Anim.state`): `morphAnim.start` resolves the plan and
captures run-time state; `morphAnim.step(u)` is a **pure** stepper — reads the
struct, writes refs/entities, no closure captures, no `rt`. This is exactly the
plan/eval split this doc asks for, proven on the hardest verb first and paired
with [01 — typed animation state](01-untyped-anim-state.md).

The big-bang 2900-line split is still not worth doing. The remaining verbs can
follow the same `xAnim{ start; step }` pattern **incrementally**, one verb per
slice, each behind a stepping test — a shape now exists to copy. Those follow-up
migrations are mechanical and offloadable; the design call (this seam) is done.

## The assumption

`compile.go` is ~2900 lines and "compile" means two different jobs at once:

1. **Plan-time** — lower the parsed scene into an animation plan: resolve
   references, expand verbs (`expandMorph`), build `Anim` values.
2. **Run-time** — the per-frame `Update`/`Start` closures that actually move
   pixels each tick.

Both live in the same file, often in the same function: `expandMorph` builds the
plan *and* defines the closure that runs every frame.

## Why it's a problem

- **No seam to test or reason about.** You can't inspect "what is the plan for
  this scene?" separately from "what does frame 37 look like?" — they're fused in
  closures over captured locals.
- **Giant functions.** The morph closure was ~100 lines inline because the
  run-time logic had nowhere else to go. That's a symptom, not the disease.
- **Hard to change safely.** Touching how a verb is planned and how it ticks are
  different risks, but they share a blast radius because they share a function
  and captured variables.

## The thing to question

Is "compile" one phase or two? The name suggests lowering; the file also does
evaluation.

## Sketch of a fix

Split along the seam:

- **Planning** produces explicit, inspectable plan structs (per verb), free of
  closures where possible.
- **Evaluation** is a separate stepper that takes a plan + `u` and applies it,
  pairing naturally with [01 — typed animation state](01-untyped-anim-state.md).

This is **large**. Don't attempt it opportunistically — it's a deliberate
refactor with its own design pass and review, not a side effect of another task.
