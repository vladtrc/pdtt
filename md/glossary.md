# pdtt — glossary

One term per entry. Mechanics live in `syntax.md`, `tween.md`, `types.md`.

**record** — a named typed data container, `type name:`. With a `for:` source it is plural
(one row per source element); the name stays singular. Type `data` renders nothing.

**field** — a property of a record, `name: expr`. A value field re-evaluates every frame;
a `rate name: expr` field defines d(name)/dt and is integrated. A field is just its
expression — no liveness classification.

**capture (`=`)** — `name = expr` evaluates `expr` once at that point in the score and
freezes it. `home = frame` snapshots the camera there.

**block** — a `| clock` header plus the rows under it, sharing one clock `u ∈ [0,1]`.

**clock** — the first cell of a block header: `4s`, a fraction (`0.3` / `30%`), or `each …`.

**row / edit** — a `| … | path -> expr` line. The `path -> expr` is the edit; see **tween**.

**tween (`->`)** — interpolate `path` toward `expr` over the window, then `path` *is* `expr`.

**modifier** — any cell before the `->` on a line: a window, ease, transition, offset, or
pairing. Modifiers shape the tween; they are not operations.

**window** — the `(from, to)` interval a tween runs over, as a fraction of the block clock.
Default is the whole block. Spelled `a-b` (either side optional).

**transition** — *how* a tween renders: `morph`, `fade_in`, `draw`, `write`. A modifier,
not a verb. Default is direct value interpolation.

**it** — the current element in a per-element context (broadcast `[*]`, `each`). Carries
`.i` (index), `.n` (count), and fields via dot access. Multiple `[*]` give `it.0`, `it.1`, …;
indices also read as `i j k l`. All access is dot notation — never brackets on `it`.

**self** — the previous-frame value of the field being defined; only in rate fields.

**each** — `| each record dur` unrolls into one sequential block per row of the record.

**row source (`for:`)** — makes a record plural: a list, `range(n)`, another record, or
`from "cmd"` (JSONL, cached, content-hashed).

**extern fn** — a pure host function usable at eval time. The only hole in the language;
it cannot emit score or run per frame.

**frame** — the builtin camera record. Fields `at`, `w`, `h`, `angle`. Tween or snapshot
it like any record; there is no separate camera construct.

## Constants

No uppercase magic — constants are namespaced globals:

```
color.red color.blue color.white color.black color.yellow color.green color.pink
corner.ul corner.ur corner.dl corner.dr corner.center
approx.above approx.below approx.left approx.right
math.tau math.pi
```
