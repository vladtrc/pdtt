# PDTT full LLM reference

Return one complete PDTT scene wrapped in a single ```pdtt code fence and nothing else. The fence is required: indentation and spaces are significant in PDTT, and the fence preserves them. Do not add explanations before or after the fence.

PDTT is a small scene language for rendered math/geometry animation. A file declares
state first, then schedules animation with `|` time blocks. Declarations create values
and records. Time rows tween values with `->`. Records start inactive and appear only
after an entry tween or a morph activates them.

## File Shape

```pdtt
scene name

live_name: expr                  # live global, re-evaluates when dependencies change
frozen_name = expr               # capture, evaluated once at declaration
home: snapshot frame             # frozen snapshot of a record

type record_name:
  field: expr                    # live field
  rate field: expr               # derivative d(field)/dt; may read self

text label:
  text: "short sentence"
  at: [0, 3]
  scale: 0.6
  color: color.white

| 1.0s | ease:smooth
| in:fade | title, subtitle

| 1.5s
```

Top-level forms:

- `scene name`
- `extern fn name(args...) -> type` declares a pure host function by name
- `name: expr` creates a live global
- `name = expr` creates a frozen capture
- `type name:` creates one record
- `text name:` and `typst name:` create text records; put the string in the `text:` field
- `family[domain as i]:` creates a family of member records over an integer domain,
  commonly `family[0..n as i]:`
- Lines beginning with `|` create time blocks

Blank lines end normal record blocks and time blocks. Blank lines may separate members
inside a family.

Long expressions may span lines inside balanced `[...]` or `(...)`. Otherwise a newline
ends the expression.

```pdtt
at: [
  r * cos(a),
  0.1 + r * sin(a)
]
```

Comments start with `#` outside strings.

## Globals, Captures, And Snapshots

Use `:` for values that should keep following their dependencies:

```pdtt
t: 0
p: [2 * cos(t), 2 * sin(t)]
```

Use `=` when you need the value frozen at declaration time:

```pdtt
start = p
```

Use `snapshot` when freezing a record's fields:

```pdtt
home: snapshot frame

| 1.0s | ease:smooth
| frame.w -> 9

| 1.0s | ease:smooth
| frame -> home
```

`snapshot` is intentionally frozen even when declared with `:`.

## Records

Record syntax is singular:

```pdtt
dot p:
  at: [0, 0]
  radius: 0.12
  color: color.red
```

Declaring the same `type name:` again merges fields into the same record. First field
definition wins unless a later time event uses `set field: expr`.

Fields re-evaluate when their dependencies change. This is the normal way to make one
object follow another:

```pdtt
dot p:
  at: [x, 0]

text label:
  text: "value"
  at: above(p, 0.2)
```

Use `rate field:` for continuous updates driven by runtime:

```pdtt
dot p:
  at: [0, 0]
  rate angle: 1
```

After time has advanced, redeclare a record with `set field: expr` when you need to
replace a live field definition at that point in the score.

## Renderable Types

Common fields on visual records: `at`, `scale`, `angle`, `opacity`, `draw`.
Use dotted material fields for paths: `stroke.color`, `stroke.width`, `stroke.end`,
`fill.color`.

Supported rendered record types:

```pdtt
dot d:
  at: [0, 0]
  radius: 0.1
  color: color.blue

path edge:
  points: [[-2, 0], [2, 0]]
  stroke.color: color.yellow
  stroke.width: 0.04
  stroke.end: arrow

path area:
  at: [0, 0]
  points: [[-1, -1], [1, -1], [1, 1], [-1, 1]]
  closed: 1
  stroke.color: color.white
  fill.color: color.blue @ 25%
```

`arrow`, `line`, `rect`, `square`, `arc`, `circle`, `ellipse`, and `polygon` are not
record types. Spell them as `path` records with explicit `points`; an arrowhead is
`stroke.end: arrow`, not geometry.

Graphs:

```pdtt
plane grid:
  at: [0, -0.4]
  x_range: [-4, 4, 1]
  y_range: [-3, 3, 1]

axes ax:
  at: grid.at
  x_range: grid.x_range
  y_range: grid.y_range

plot curve:
  axes: ax
  fn: 0.35 * x * x - 1
  color: color.yellow

dot point:
  at: ax.point(t, 0.35 * t * t - 1)
  radius: 0.11
  color: color.red
```

`axes`/`plane` keep two independent scales. `size: [w, h]` is the physical
footprint in world units (default `[10, 6]`); `x_range`/`y_range` are the data
window mapped into that footprint. Animate `size` to grow or shrink the whole
graph; animate the ranges to zoom the data without moving the footprint. Both
are plain live fields, so a tween on either rescales every `plot` and every
`ax.point(...)` that reads the axes. `frame: 1` outlines the footprint so its
scene size stays visible while the data window zooms underneath it.

Text:

```pdtt
text note:
  text: "A short readable sentence."
  at: [0, 3.05]
  scale: 0.58
  color: color.white

typst formula:
  text: "f(x)=x^2"
  at: [0, 2.55]
  scale: 0.82
  color: color.yellow
```

Use `text` for prose and `typst` for formulas. Put the string in the `text:` field.
Avoid LaTeX commands unless Typst accepts them. Prefer strings like `"f(x)=x^2"`,
`"x'' + x = 0"`, `"dot x"`.

`tex` and `decimal` are renderer-compatible legacy text types, but generated code
should use `text` and `typst`. Plain `text` is drawn in the same Computer-Modern
letterforms as `typst` math, so prose and formulas share one visual style.

### Multiline, write-on, and emphasis

Text strings take the `\n` escape for line breaks, so one record can hold several
stacked lines. For longer prose, the `|` block scalar keeps the shape of the text
visible in the source — the indented body is dedented and its line breaks kept
verbatim (interior blank lines become empty text lines, surrounding blanks are
trimmed), with no quoting or `\n` by hand:

```pdtt
text intro:
  text: |
    first line
    second line
```

The `draw` field (default 1) reveals text left to right, one line after another —
the text analogue of drawing a path on. Use the same self-transition as any shape:

```pdtt
| 1.5s | in:draw | intro  # write the text on, glyph by glyph
```

Any substring is independently tweenable through `<text>.sub("phrase").<attr>`.
There is no markup in the text — the span is selected by its literal characters,
so the prose stays plain (the first match wins; later spans that would overlap an
earlier one are skipped):

| attr | effect |
|---|---|
| `color` | recolour the span |
| `opacity` | fade the span |
| `strike` | 0..1 strikethrough rule, drawn left to right |
| `underline` | 0..1 underline rule, drawn left to right |
| `scale` | size multiplier about the span centre (1 = normal) |
| `wiggle` | 0..1 shake amplitude (0 = still) |

A plain `->` arrow **sets and holds**: the span stays recoloured, struck, or
enlarged after the window.

```pdtt
text line:
  text: "you can emphasise a word"

| 0.6s | line.sub("emphasise").color -> color.yellow
| 0.6s | line.sub("emphasise").scale -> 1.4
```

For a one-shot highlight that **lights up, then settles back to rest**, use a
`highlight:` modifier cell instead — a channel and a target span, no arrow and no
number. The channel runs through a `0 → peak → 0` envelope over the window and is
left at its resting value:

| modifier | effect over the window |
|---|---|
| `highlight:flash` | flash the span to yellow, then back to its colour |
| `highlight:strike` | sweep a strikethrough in, then out |
| `highlight:underline` | sweep an underline in, then out |
| `highlight:enlarge` | swell the span to ~1.5×, then back |
| `highlight:wiggle` | a self-contained shake that settles |

```pdtt
| 1.0s | ease:smooth | highlight:flash | line.sub("emphasise")
| 1.0s | ease:smooth | highlight:wiggle | line.sub("emphasise")
```

See `examples/text-features` for both forms — modifiers and arrows — side by side.

## Time Blocks

Every animation row starts with `|`. A blank line ends the current time block.

The first `|` line of a block is the clock. Rows in the same block run in parallel
against that clock unless a window or stagger changes them.

```pdtt
| 4s | ease:linear
| theta -> math.tau
| 0-.5 | dot.opacity -> 1
```

The header may also carry its first edit inline:

```pdtt
| 1s | ease:smooth | in:pop | dot
```

A standalone clock with no rows is a pause:

```pdtt
| 1.5s
```

Use `| each record dur` to create one sequential block per row in a record/group:

```pdtt
| each points 0.4s
| it.opacity -> 1
```

`| each record as name dur` binds the row as `name`.

## Tweens

`path -> expr` is the core operation. During the row window, PDTT interpolates the
left side toward the right side. After the row, the left side is assigned to the right
side. If the right side is dynamic, it keeps tracking.

```pdtt
| 1.5s | ease:smooth
| p.at -> [3, 1]
| label.at -> above(p, 0.25)
```

Entrance:

```pdtt
| 0.8s | ease:smooth
| in:pop | p
| in:draw | curve, axis
```

`in:PRESET | obj` activates `obj`, snaps the preset's hidden fields, and tweens them
back to the declared values. Presets: `draw`, `fade`, `pop`, `draw_fade`. `ou:PRESET | obj`
reverses it and leaves `obj` inactive. For `in:`, `ou:`, and `highlight:`, the subject cell
may list multiple top-level subjects separated by commas: `| in:fade | title, capL, capR`.

Record tween:

```pdtt
home: snapshot frame

| 1s | ease:smooth
| frame -> home
```

Morph:

```pdtt
| 1s | transition:morph | square_a -> dot_b
```

Morph activates the target and deactivates the source at the end.

## Modifiers

Modifiers are separate `|` cells before the edit. Order is free. The edit cell must be
last.

| Modifier | Meaning |
|---|---|
| `ease:linear`, `ease:smooth`, `ease:in`, `ease:out`, `ease:out_cubic`, `ease:in_out` | easing |
| `0-.5`, `.5-`, `25%-75%`, `0.2s-1.1s` | window |
| `0.6s` | window from `0` to `0.6s` |
| `after 0.5s`, `lag 0.2s`, `stagger 0.08s` | offsets |
| `transition:morph`, `transition:fade_in`, `transition:draw`, `transition:write` | transition strategy for a `->` edit |
| `in:PRESET`, `ou:PRESET` | entrance / exit of the subject (`draw`, `fade`, `pop`, `draw_fade`) |
| `highlight:CHANNEL` | transient emphasis of the subject span (`flash`, `strike`, `underline`, `enlarge`, `wiggle`) |
| `by name`, `by pos` | pairing for structural transitions |

Examples:

```pdtt
| 1.2s | ease:smooth
| in:draw | grid
| in:fade | axes, title

| 2s | ease:smooth | stagger 0.08s
| in:pop | dots[* as i]
```

Do not write `| 4s linear`; the canonical form is `| 4s | ease:linear`.

## Collections, Broadcast, And Families

Lists use `[a, b, c]`. Index them with `[i]`. `list.indices` returns `[0, 1, ...]`;
`list.len` returns the length.

Simple plural record:

```pdtt
xs: [-2, 0, 2]
cols: [color.red, color.green, color.blue]

dot p:
  for: xs
  at: [it, 0]
  radius: 0.1
  color: cols[i]
```

Inside a `for:` record, `it` is the current element, `i` is the index, and `it.n` is
the count.

Broadcast `[*]` expands one row per element:

```pdtt
| 1.2s | stagger 0.12s | ease:smooth
| in:pop | p[* as i]
```

Use `[* as i]` when the RHS needs the same index. Plain `[*]` also binds canonical
`i` and `it` for simple cases.

Families group several member records per domain key. The domain may be a range or a
variable that evaluates to unique integers; `0..n` means `0` through `n-1`.

```pdtt
val: [-3, -1, 3]
cols: [color.red, color.green, color.blue]

roots[val.indices as i]:
  x: val[i]
  label_text: fmt("{:.2f}", x)

  dot mark:
    at: ax.point(x, 0)
    radius: 0.095
    color: cols[i]

  path hit:
    points: [ax.point(x, -1.2), mark.at]
    stroke.color: cols[i]
    stroke.end: arrow

  text n:
    text: label_text
    at: below(mark, 0.2)
    color: cols[i]
```

Access family members with `roots[i].mark`, `roots[i].hit`, `roots[i].n`.
Indented `name: expr` lines directly inside a family are local live bindings for that
family element. Member records may reuse them, and they update when their dependencies
change.

Broadcast family members like this:

```pdtt
| 1.2s | ease:smooth | stagger 0.08s
| in:pop | roots[* as i].mark
| in:draw | roots[* as i].hit
```

If many objects derive from a list, animate the list and let live fields follow:

```pdtt
| 4s | ease:smooth
| val[* as i] -> [-2, 1, 3][i]
```

Family members may reference other members, including through computed indices. This is
useful for neighbors:

```pdtt
n = 18
phase: 0

ring[0..n as i]:
  a: math.tau * i / n + phase
  prev_i: (i - 1 + n) % n

  dot p:
    at: [2 * cos(a), 2 * sin(a)]

  path chord:
    points: [ring[prev_i].p.at, p.at]
    stroke.end: arrow
```

The dependency graph follows these indexed references, so if `phase` tweens, both `p`
and `chord` update during the tween.

## Expressions

Supported expression forms:

- numbers: `0`, `42`, `-.3`, `1.0`
- strings: `"hello"`
- lists/vectors: `[0, 1]`, `[x, y, z]`
- ranges: `0..n`
- multiline lists/parentheses while brackets are balanced
- arithmetic: `+`, `-`, `*`, `/`, `%`
- interpolation: `mix(from, to, amount)` for numbers, vectors, colors, and compatible lists
- comparisons: `==`, `!=`, `<`, `>`, `<=`, `>=`
- ternary: `cond ? a : b`
- legacy ternary: `a if cond else b`
- attributes: `p.at`, `p.at.x`, `list.indices`, `list.len`
- indexing: `list[i]`, `family[i].member`
- calls: `fmt("x = {:.2f}", x)`, `sin(t)`, `ax.point(x, y)`
- color alpha: `color.red @ 60%`
- snapshots: `snapshot frame`

There are no boolean literals in the current parser. Use comparisons or numeric
truthiness in conditionals.

Builtins and namespaces:

- math: `math.pi`, `math.tau`, `math.e`
- trig/math: `sin`, `cos`, `exp`, `log`, `pow(base, exp)`, `sinh`, `cosh`, `abs`, `sqrt`
- reducers: `min(a, b, ...)` / `max(a, b, ...)` (also accept a single list)
- lists: `range(n)`, `sum(list)`, `prod(list)`, `pair_sum(list)`
- strings: `fmt(format, ...)`
- layout: `center(obj)`, `below(obj, gap)`, `above(obj, gap)`, `right_of(obj, gap)`,
  `left(d)`, `right(d)`, `corner(corner.ul)`, `edge(approx.right)`, `stack().top`,
  `stack().bottom`, `stack().center`
- axes method: `ax.point(x, y)`
- namespaces: `color.*`, `corner.ul/ur/dl/dr/center`, `approx.above/below/left/right`

Colors:

`color.white`, `color.black`, `color.gray`, `color.grey`, `color.light_gray`,
`color.dark_gray`, `color.blue`, `color.blue_e`, `color.cyan`, `color.teal`,
`color.green`, `color.lime`, `color.yellow`, `color.gold`, `color.orange`,
`color.red`, `color.red_b`, `color.maroon`, `color.pink`, `color.magenta`,
`color.purple`, `color.violet`, `color.brown`.

## Complete Example

```pdtt
scene parabola_intro

t: -3

text title:
  text: "Парабола показывает квадратичный рост."
  at: [0, 3.05]
  scale: 0.56
  color: color.white

typst formula:
  text: "f(x)=0.35x^2-1"
  at: [0, 2.55]
  scale: 0.82
  color: color.yellow

plane grid:
  at: [0, -0.55]
  x_range: [-4, 4, 1]
  y_range: [-2.5, 3.5, 1]

axes ax:
  at: grid.at
  x_range: grid.x_range
  y_range: grid.y_range

plot curve:
  axes: ax
  fn: 0.35 * x * x - 1
  color: color.yellow

dot p:
  at: ax.point(t, 0.35 * t * t - 1)
  radius: 0.12
  color: color.red

| 1.0s | ease:smooth
| in:draw | grid
| in:fade | ax

| 0.7s | ease:smooth
| in:fade | title

| 2.0s

| 0.45s | ease:smooth
| title.opacity -> 0

| 0.7s | ease:smooth
| in:fade | formula
| in:draw | curve
| in:pop | p

| 1.4s

| 4.0s | ease:smooth
| t -> 3

| 1.5s
```

## Things That Do Not Exist

Do not generate these:

- explanatory prose, comments, or extra text outside the single ```pdtt fence (the outer fence itself is required)
- `bind`, `track`, updater callbacks, `during {}`
- `tween x to y`, `animate(...)`, `wait(...)`
- camera classes; use the built-in `frame` record
- uppercase constants like `RED`, `BLUE`, `UL`, `RIGHT`
- plural type names like `dots p:` or `axeses`
- `morph` as a verb; use `| ... | transition:morph | a -> b`
- `gone`; use `ou:fade | obj` to dismiss an object
- `for: from "cmd"` and `fast_after`; this prototype rejects them
- LaTeX-only commands in `typst` records
- `text("...") name:` and `typst("...") name:` constructor syntax; use a `text:` field
