# pdtt — syntax

A scene is a list of lines. Two kinds:

- **State** — declarations (no leading `|`): `scene`, `extern`, captures, records.
- **Time** — `|` lines, grouped into blocks. This is where animation happens.

## State

```
scene name

extern fn name(arg: type, …) -> type      # pure host function, eval-time only

name = expr                               # capture: evaluate once here, freeze the result
scene name:                               # reusable scene block
run name                                  # splice that scene block here

type name:                                # record (singular)
  field: expr                             #   value field — re-evaluates every frame
  rate field: expr                        #   rate field — d(field)/dt; may read `self`
  for: list | range(n) | record           #   row source ⇒ record is plural, one row each
```

Same `type name:` declared twice merges into one record (fields in declaration order, first wins).
When compiling a file path, sibling `.pdtt` files in the same directory are visible too; the input
file is parsed first.

## Scene blocks

`scene name` names the output scene. `scene name:` defines a reusable block of state/time forms.
`run name` compiles that block at the current point in the timeline, sharing the same records and
globals as the caller:

```
scene tour

text narrator:
  text: "..."

run graph

scene graph:
plot p:
  fn: sin(x)

| 1s | in:draw | p
```

## Time: the `|` line

Every `|` line is a list of `|`-separated **cells**. The last cell is either an **edit**
(`path -> expr`) or the **subject** of a verb modifier (`in:`/`ou:`/`highlight:`); every cell
before it is a **modifier** shaping that edit:

```
| <mod> | <mod> | path -> expr
```

The first `|` line after a blank line opens a **block**. Its first cell is the **clock**;
remaining modifier cells become **block defaults** applied to every row:

```
| 4s | ease:linear  # clock = 4s, default ease = linear
| theta -> math.tau  # a row; eases linear unless it says otherwise
| 0-.5 | x -> 1  # this row overrides the window; ease still linear
```

Rules:
- A clock is `4s` (seconds), `0.3` / `30%` (fraction of parent), or `each record [as name] dur`.
- A per-row modifier overrides a block default of the same kind.
- The opening line may carry the block's first edit inline as its last cell:
  `| 4s | ease:linear | theta -> math.tau` ≡ the two-line form above.
- All rows in a block share the clock `u ∈ [0,1]` and run in parallel unless a window narrows them.

## Edit — the tween `->`

`path -> expr`: during the window, interpolate `path` toward `expr`; after, `path` **is** `expr`
(dynamic targets keep tracking). See `tween.md`.

## Entrance / exit — `in:` and `ou:`

Records are declared inactive: they can be referenced by expressions, but they are not rendered
until an entrance (or a morph) activates them.

`in:PRESET | subject` brings a subject on screen: it snaps the preset's hidden fields, then tweens
them back to the declared values over the window. `ou:PRESET | subject` is the reverse — it tweens
declared → hidden, then leaves the subject inactive. Subjects may be comma-separated. Presets:

| Preset | Hidden fields |
|---|---|
| `draw` | `draw: 0` (stroke wipes on) |
| `fade` | `opacity: 0` |
| `pop` | `opacity: 0`, `scale: 0.2` |
| `draw_fade` | `draw: 0`, `opacity: 0` |

```
| 1s | in:draw | s
| 1s | in:fade | label
| 1s | in:fade | title, capL, capR
| 1s | ou:fade | label   # dismiss it again
```

The subject is the last cell and may broadcast (`in:pop | ring[* as i].p`). It is a per-field
self-transition, never a morph.

## Modifiers

A cell in modifier position is never an expression, so `-.5` is always a window, never a number.
Order within a line is free.

| Cell | Effect |
|---|---|
| `a-b` | window `(from, to)`; either side optional: `-.5` = `0-.5`, `.5-` = `.5-1`. Bare `-` is an error. |
| `ease:NAME` | easing — `linear` `smooth` `in` `out` `out_cubic` `in_out` |
| `transition:NAME` | transition strategy for a `->` edit — `morph` `fade_in` `draw` `write` |
| `in:PRESET` / `ou:PRESET` | entrance / exit of the subject cell — `draw` `fade` `pop` `draw_fade` |
| `highlight:CHANNEL` | transient emphasis of the subject span — `flash` `strike` `underline` `enlarge` `wiggle` |
| `after r` `lag d` `stagger d` | window offsets (`stagger` is per-element) |
| `by name` `by pos` | pairing for structural transitions |

Bounds are fractions (`0.3`), percent (`30%`), or seconds (`1.5s`).

## Broadcast `[*]`

`coll[*]` expands one row into one scalar row per element. `it` is the current element,
with `.i` (index) and `.n` (count). Multiple `[*]` bind `it.0`, `it.1`, … left to right;
their indices also read as `i j k l`. Both sides of `->` may broadcast.

```
| stagger .1s | transition:morph | dots[*] -> targets[i]
| grid[*][*].color -> (i+j) % 2 == 0 ? color.red : color.white
```

## each

`| each record dur` unrolls into one sequential clock block per row of the record;
`it` is the current row. `fast_after n dur2` speeds rows past the nth.

## Compile order

parse → merge records → run `from` commands → evaluate fields → expand `for:`/`each`/`[*]`
→ resolve `=` captures → emit static score → render.

> No LaTeX. Typst compilation comes later.
