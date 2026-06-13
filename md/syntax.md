# pdtt — syntax reference

## Top-level forms

```
scene name

extern fn name(arg: type, …) -> type

name = expr                           # capture: evaluates expr here, freezes result

type name:                            # record declaration
  for: list | range(n) | record | from "cmd"   # row source (optional; makes it plural)
  field: expr                         # value field
  rate field: expr                    # rate field (d/dt); may read `self`
  integrate: expr                     # history accumulator

| duration [ease]                     # block header (starts a parallel clock)
| each record [as name] dur [fast_after n dur2]

| [modifier |]… path -> expr          # edit row
| [modifier |]… transition path -> expr
```

Nothing else exists at top level.

---

## Expressions

Standard arithmetic, comparison, and boolean operators.
Ternary: `cond ? a : b` (values only, no control flow).
Index: `coll[i]`, `coll[*]` (broadcast).
Field access: `record.field`, `it.i`, `it.n`.
Builtins: `fmt(spec, val)`, `noise2(x, y)`, `range(n)`, `center(r)`, `below(r, d)`, `stack(r)`, layout helpers.

---

## Windows

In modifier position, `a-b` sets the window within the enclosing block's clock `u ∈ [0,1]`.
Units: bare fractions (`0.3`), percent (`30%`), or seconds (`1.5s` relative to block duration).

| Shorthand | Meaning |
|---|---|
| `a-b` | `from=a, to=b` |
| `-.b` | `from=0, to=b` |
| `a-` | `from=a, to=1` |
| *(absent)* | `from=0, to=1` |

Bare `-` is a compile error.

---

## Modifier cells

Each cell between `|` symbols is one modifier. Order within a row: any.
A cell in modifier position is never an arithmetic expression, so `-.5` is always a
window shorthand, never a negative number.

---

## Broadcast

`coll[*]` in a path or expression expands the row to one scalar row per element.
`it` is bound per element. Both sides of `->` can use `[*]`.

```
| stagger .1s | morph | parts[*] -> targets[it.i]
```

---

## Record merging

Multiple declarations of the same `type name:` anywhere in the file merge into one
record in the global scene database. Field order: declaration order, first wins on
conflict.

---

## `each` blocks

```
| each dots .5s
| fade_in it
```

Unrolls to N sequential `.5s` clock blocks, one per row of `dots`.
`it` is the current dot. `| each dots .5s fast_after 3 .1s` speeds remaining rows.

---

## Compile order

1. Parse
2. Merge records
3. Run `from` commands (parallel, cached, content-hashed)
4. Evaluate columns (fold expressions)
5. Expand `for:` / `each` / `[*]`
6. Liveness analysis: classify every field static / live / rate; check one-writer rule
7. Resolve `=` captures (≤1s integrator replay if rate-dependent)
8. Emit static score → render


Данный проект не поддерживает Latex! мы научимся компилировать typst но позже
