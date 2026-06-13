# pdtt — types

## Primitives

| Type | Examples |
|---|---|
| `int` | `0`, `42`, `-3` |
| `float` | `0.5`, `-.3`, `1.0` |
| `string` | `"hello"`, `"\\LaTeX"` |
| `bool` | `true`, `false` |
| `vec2` | `[0, 1]`, `[x, y]` |
| `vec3` | `[0, 1, 0]`, `[x, y, z]` |
| `color` | `color.red`, `color.blue`, `[r, g, b]` |
| `dur` | `1.5s`, `0.3`, `30%` |

## `dur` — duration

Used in block headers and modifiers. Three equivalent spellings:

```
| 1.5s        # seconds
| 0.3         # fraction of the parent clock
| 30%         # same as 0.3
```

In a window modifier, `dur` values set the bounds:
```
| .2-.8 |     # from 20% to 80% of the block
| -.5s  |     # from 0 to 0.5s into the block
```

## `tween` — the `->` operator

Not a value type. `path -> expr` is a statement in a block row.
Both `path` and `expr` can be any type; they must match.

## Collections

`[a, b, c]` — literal list. Type is inferred from elements.

`coll[i]` — index access.

`coll[*]` — broadcast. Expands the row into N parallel tween rows, one per element.
Inside the broadcast expression, `it` is the current element, `it.i` is its index,
`it.n` is the total count.

```
| parts[*].opacity -> 0          # fade out every part
| parts[*] -> targets[it.i]      # pair by index
| parts[*].color -> it.i % 2 == 0 ? color.red : color.blue
```

**Multiple `[*]` in one row** — each `[*]` left-to-right binds to `it.0`, `it.1`, `it.2`, …
Each has `.i`, `.n`, and the element's fields via dot access.

```
| a[*].x + b[*].y -> 0      # it.0 = current element of a, it.1 = current element of b
                             # it.0.i, it.1.i — their indices
```

Single `[*]` is `it.0`, also accessible as plain `it` for convenience.

Index shorthands — `i` `j` `k` `l` for `it.0.i` `it.1.i` `it.2.i` `it.3.i`:

```
| parts[*] -> targets[i]
| a[*] + b[*] -> 0              # i = index in a, j = index in b
| grid[*][*].color -> (i+j) % 2 == 0 ? color.red : color.white
```

## Type inference

Types are inferred everywhere. Explicit type annotations appear only in `extern fn`
signatures and `for: from "cmd"` feed schemas.

```
extern fn noise2(x: float, y: float) -> float

data rounds:
  for: from "go run ./gen"
  pick: int
  score: float
```
