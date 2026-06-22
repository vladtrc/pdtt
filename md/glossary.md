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

**row / edit** — a `| ... | path -> expr` line (edit last cell) or a `| ... | VERB | subject`
line where `VERB` is `in:`/`ou:`/`highlight:` and the subject is the last cell.

**tween (`->`)** — interpolate `path` toward `expr` over the window, then `path` *is* `expr`.

**entrance / exit** — `in:PRESET | obj` activates `obj`, snaps the preset's hidden fields, then
tweens them back to the declared values; `ou:PRESET | obj` tweens them to hidden and leaves `obj`
inactive. Presets: `draw`, `fade`, `pop`, `draw_fade`. It is the special case of morphing an
object into itself, so it needs no `transition:` modifier or structural pairing.

**presence** — records are declared inactive: they exist for references and layout, but render
nothing until an `in:` entrance or `morph` activates them. `opacity` stays a visual field, not the
presence flag.

**modifier** — any cell that is not the last edit/subject cell: a window, ease, transition,
offset, or pairing, each spelled `key:value` where applicable. Modifiers shape the tween; they
are not operations.

**window** — the `(from, to)` interval a tween runs over, as a fraction of the block clock.
Default is the whole block. Spelled `a-b` (either side optional).

**transition** — *how* a `->` tween renders, spelled `transition:NAME`: `morph`, `fade_in`,
`draw`, `write`. A modifier, not a verb. Default is direct value interpolation.

**it** — the current element in a per-element context (broadcast `[*]`, `each`). Carries
`.i` (index), `.n` (count), and fields via dot access. Multiple `[*]` give `it.0`, `it.1`, …;
indices also read as `i j k l`. All access is dot notation — never brackets on `it`.

**self** — the previous-frame value of the field being defined; only in rate fields.

**each** — `| each record dur` unrolls into one sequential block per row of the record.

**row source (`for:`)** — makes a record plural: a list, `range(n)`, another record, or
`from "cmd"` (JSONL, cached, content-hashed).

**extern fn** — a pure host function usable at eval time. The only hole in the language;
it cannot emit score or run per frame.

**scene block** — `scene name:` names a reusable list of state/time forms. Sibling `.pdtt`
files in the same directory are visible when compiling a file path.

**run** — `run name` splices a scene block at the current timeline position. The block shares
records, globals, and the frame with the caller.

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
