# pdtt — types

| Type | Examples |
|---|---|
| `int` | `0`, `42`, `-3` |
| `float` | `0.5`, `-.3`, `1.0` |
| `string` | `"hello"` |
| `bool` | `true`, `false` |
| `vec2` / `vec3` | `[0, 1]`, `[x, y, z]` |
| `color` | `color.red`, `[r, g, b]`, `color.pink @ 50%` |
| `dur` | `1.5s`, `0.3`, `30%` |

Types are inferred everywhere. The only explicit annotations are `extern fn` signatures
and `for: from "cmd"` feed schemas.

## dur

A duration is seconds (`1.5s`), a fraction of the parent clock (`0.3`), or percent (`30%`,
same as `0.3`). Used as block clocks and window bounds.

## Collections

`[a, b, c]` is a literal list; element type is inferred. `coll[i]` indexes it.
`coll[*]` broadcasts — see broadcast in `syntax.md`.

## Expressions

Arithmetic, comparison, boolean operators; ternary `cond ? a : b` (values only, no control
flow). Access: `record.field`, `coll[i]`, `coll[*]`. Builtins include `fmt`, `noise2`,
`range`, and layout helpers (`center`, `below`, `left`, `right_of`, `stack`, …).
