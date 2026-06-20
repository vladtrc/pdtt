# pdtt — syntax

A scene is a list of lines. Two kinds:

- **State** — declarations (no leading `|`): `scene`, `extern`, captures, records.
- **Time** — `|` lines, grouped into blocks. This is where animation happens.

## State

```
scene name

extern fn name(arg: type, …) -> type      # pure host function, eval-time only

name = expr                               # capture: evaluate once here, freeze the result

type name:                                # record (singular)
  field: expr                             #   value field — re-evaluates every frame
  rate field: expr                        #   rate field — d(field)/dt; may read `self`
  for: list | range(n) | record           #   row source ⇒ record is plural, one row each
```

Same `type name:` declared twice merges into one record (fields in declaration order, first wins).

## Time: the `|` line

Every `|` line is a list of `|`-separated **cells**. The last cell may be an **edit**
(`path -> expr` or `obj{...} -> obj`); every cell before it is a **modifier** shaping that edit:

```
| <mod> | <mod> | path -> expr
```

The first `|` line after a blank line opens a **block**. Its first cell is the **clock**;
remaining modifier cells become **block defaults** applied to every row:

```
| 4s | linear              # clock = 4s, default ease = linear
| theta -> math.tau        # a row; eases linear unless it says otherwise
| 0-.5 | x -> 1            # this row overrides the window; ease still linear
```

Rules:
- A clock is `4s` (seconds), `0.3` / `30%` (fraction of parent), or `each record [as name] dur`.
- A per-row modifier overrides a block default of the same kind.
- The opening line may carry the block's first edit inline as its last cell:
  `| 4s | linear | theta -> math.tau` ≡ the two-line form above.
- All rows in a block share the clock `u ∈ [0,1]` and run in parallel unless a window narrows them.

## Edit — the tween `->`

`path -> expr`: during the window, interpolate `path` toward `expr`; after, `path` **is** `expr`
(dynamic targets keep tracking). See `tween.md`.

## Edit — self-entry tween

Records are declared inactive: they can be referenced by expressions, but they are not rendered
until an entry or morph activates them.

`obj{field: start, ...} -> obj` is a same-object transition. The left side is a phantom copy of
`obj` with the listed field overrides; the right side is the declared object. Over the window,
each overridden field tweens from the phantom value back to the declared value:

```
| 1s | s{draw: 0} -> s
| 1s | label{opacity: 0} -> label
```

Because both sides are the same object, this is the special case that needs no `morph` modifier.

## Modifiers

A cell in modifier position is never an expression, so `-.5` is always a window, never a number.
Order within a line is free.

| Cell | Effect |
|---|---|
| `a-b` | window `(from, to)`; either side optional: `-.5` = `0-.5`, `.5-` = `.5-1`. Bare `-` is an error. |
| `linear` `smooth` `ease_in` `ease_out` `ease_out_cubic` `ease_in_out` | easing |
| `morph` `fade_in` `draw` `write` | transition strategy |
| `after r` `lag d` `stagger d` | window offsets (`stagger` is per-element) |
| `by name` `by pos` | pairing for structural transitions |

Bounds are fractions (`0.3`), percent (`30%`), or seconds (`1.5s`).

## Broadcast `[*]`

`coll[*]` expands one row into one scalar row per element. `it` is the current element,
with `.i` (index) and `.n` (count). Multiple `[*]` bind `it.0`, `it.1`, … left to right;
their indices also read as `i j k l`. Both sides of `->` may broadcast.

```
| stagger .1s | morph | dots[*] -> targets[i]
| grid[*][*].color -> (i+j) % 2 == 0 ? color.red : color.white
```

## each

`| each record dur` unrolls into one sequential clock block per row of the record;
`it` is the current row. `fast_after n dur2` speeds rows past the nth.

## Compile order

parse → merge records → run `from` commands → evaluate fields → expand `for:`/`each`/`[*]`
→ resolve `=` captures → emit static score → render.

> No LaTeX. Typst compilation comes later.
