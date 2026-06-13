# pdtt — glossary

Canonical definitions. One term per entry. No history.

---

## Primitives

### record
A named typed data container. Declared with `type name:` at top level.
A record with `for:` is plural — it holds one instance per row of the row source.
The type `data` renders nothing (pure data).

```
circle dot:
  at: [0, 0]
  r: .3

data rounds:
  for: range(8)
  pick: int
```

### field
A named property of a record. Declared inside a record block with `name: expr`.

Two kinds:
- **value field** — `name: expr` — evaluates its expression every frame.
- **rate field** — `rate name: expr` — defines d(name)/dt; integrated by the runtime.

A field's expression can reference any other field, builtin, or extern fn.
If it references something time-varying, it updates. If not, it's constant.
No special classification needed — a field is just its expression.

### window
The time interval `(from, to)` over which a tween runs, plus easing and pairing hints.
Expressed as modifier cells between pipes. Both bounds are optional fractions of the
enclosing block's clock. Default: full block `(0, 1)`.

```
| 2s
| .2-.8 | smooth | x -> 1    # tween runs from 20% to 80% of the 2s block
| -.5   |                     # shorthand for 0-.5
|  .5-  |                     # shorthand for .5-1
```

### tween (`->`)
The tween operator. `path -> expr` does two things:
1. **During the window:** interpolates `path` from its current value to `expr`.
2. **After the window:** `path` is `expr`. The previous value of `path` is forgotten.

Both sides are just values — static literal, dynamic expression, captured snapshot, anything.
After `x -> y`, x is y. That's it.

```
| 1.5s smooth
| dot.at -> [3, 1]              # after: dot.at is [3, 1]
| label.at -> center(dot)       # after: label.at follows center(dot)
| frame.at -> home.at           # after: frame.at follows home.at
```

### modifier
Everything between pipes except the final tween. A modifier rewrites the tuple
`(from, to, ease, pairing, transition)` of the current tween.

Closed vocabulary:

| Modifier | Effect |
|---|---|
| `a-b` | set `(from, to)` (either bound optional) |
| `after r` | `from` = end of previous row in this block |
| `lag d` | shift `from` by `d` |
| `stagger d` | per-element: shift `from` by `it.i * d` with proportional compression |
| `linear`, `smooth`, `ease_in`, … | set `ease` |
| `by name`, `by pos` | set `pairing` for structural transitions |
| `morph`, `fade_in`, `draw`, `write` | set `transition` strategy |

Modifiers are not operations. They set up the window; the tween is the operation.

### extern fn
A pure, eval-time function implemented outside pdtt (in the host language).
The only hole in the language. Cannot emit score, cannot run per frame.

```
extern fn noise2(x: float, y: float) -> float
```

---

## Binders

Exactly two binders exist.

### `it`
The current element in any per-element context: broadcast RHS and `each` blocks.
Carries `.i` (zero-based index), `.n` (total count), and the element's fields via dot access.

When a row contains multiple `[*]`, iterators are `it.0`, `it.1`, `it.2`, … left-to-right.
Single `[*]` can use plain `it` as shorthand for `it.0`.

Index shorthands for the first four iterators: `i` `j` `k` `l`
(equivalent to `it.0.i` `it.1.i` `it.2.i` `it.3.i`).

```
| parts[*] -> targets[i]                        # pair by index
| a[*] + b[*] -> c[i]                           # i = a's index, j = b's index
| grid[*][*].color -> (i+j) % 2 == 0 ? color.red : color.white
```

All field access uses dot notation: `it.field`, `it.0.field`, `record.field` — never brackets.

### `self`
The previous-frame value of the field currently being defined.
Available only in rate field expressions.

---

## Other terms

### capture (`=`)
`name = expr` at top level evaluates `expr` at that point in the score and freezes
the result. The name is a static alias for that snapshot — it does not re-evaluate.

```
home = frame      # snapshot of the camera at this moment in the timeline
```

### block
A `| duration` header plus the op rows that follow it. All rows in a block share one
clock `u = 0..1`. Rows run in parallel unless a modifier narrows the window.

### each
`| each record dur` unrolls into one sequential clock block per row of the record.
`fast_after n dur2` speeds up rows beyond the nth.

### row source (`for:`)
Declares that a record is plural. The source can be a literal list, `range(n)`,
another record, or `from "cmd"` (JSONL, cached, content-hashed).

### `frame`
Builtin record representing the camera. Fields: `at`, `w`, `h`, `angle`.
Tween them, snapshot them, restore them. No special camera form exists.

---

## Constants

No uppercase magic. Constants live in namespaced global objects.

```
color.red    color.blue   color.white   color.black   color.yellow   color.green
corner.ul    corner.ur    corner.dl     corner.dr     corner.center
approx.above approx.below approx.left   approx.right
```
