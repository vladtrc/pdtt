# PDTT full LLM reference

Write valid PDTT source only. Do not add Markdown fences or explanations.

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

text("short sentence") label:
  at: [0, 3]
  scale: 0.6
  color: color.white

| 1.0s | smooth
| label{opacity: 0} -> label

| 1.5s
```

Top-level forms:

- `scene name`
- `extern fn name(args...) -> type` declares a pure host function by name
- `name: expr` creates a live global
- `name = expr` creates a frozen capture
- `type name:` creates one record
- `text("...") name:` and `typst("...") name:` create text records with the `text`
  field already set
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

| 1.0s | smooth
| frame.w -> 9

| 1.0s | smooth
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

text("value") label:
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

Text:

```pdtt
text("A short readable sentence.") note:
  at: [0, 3.05]
  scale: 0.58
  color: color.white

typst("f(x)=x^2") formula:
  at: [0, 2.55]
  scale: 0.82
  color: color.yellow
```

Use `text(...)` for prose and `typst(...)` for formulas. Avoid LaTeX commands unless
Typst accepts them. Prefer strings like `"f(x)=x^2"`, `"x'' + x = 0"`, `"dot x"`.

`tex` and `decimal` are renderer-compatible legacy text types, but generated code
should use `text` and `typst`.

## Time Blocks

Every animation row starts with `|`. A blank line ends the current time block.

The first `|` line of a block is the clock. Rows in the same block run in parallel
against that clock unless a window or stagger changes them.

```pdtt
| 4s | linear
| theta -> math.tau
| 0-.5 | dot.opacity -> 1
```

The header may also carry its first edit inline:

```pdtt
| 1s | smooth | dot{opacity: 0, scale: 0.2} -> dot
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
| 1.5s | smooth
| p.at -> [3, 1]
| label.at -> above(p, 0.25)
```

Entry tween:

```pdtt
| 0.8s | smooth
| p{opacity: 0, scale: 0.2} -> p
| curve{draw: 0} -> curve
```

`obj{field: start, ...} -> obj` activates `obj` and tweens the listed fields from
phantom start values back to the declared field values.

Record tween:

```pdtt
home: snapshot frame

| 1s | smooth
| frame -> home
```

Morph:

```pdtt
| 1s | morph | square_a -> dot_b
```

Morph activates the target and deactivates the source at the end.

## Modifiers

Modifiers are separate `|` cells before the edit. Order is free. The edit cell must be
last.

| Modifier | Meaning |
|---|---|
| `linear`, `smooth`, `ease_in`, `ease_out`, `ease_in_out` | easing |
| `0-.5`, `.5-`, `25%-75%`, `0.2s-1.1s` | window |
| `0.6s` | window from `0` to `0.6s` |
| `after 0.5s`, `lag 0.2s`, `stagger 0.08s` | offsets |
| `morph`, `fade_in`, `draw`, `write` | transition strategy |
| `by name`, `by pos` | pairing for structural transitions |

Examples:

```pdtt
| 1.2s | smooth
| grid{draw: 0} -> grid
| axes{opacity: 0} -> axes

| 2s | smooth | stagger 0.08s
| dots[* as i]{opacity: 0, scale: 0.2} -> dots[i]
```

Do not write `| 4s linear`; the canonical form is `| 4s | linear`.

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
| 1.2s | stagger 0.12s | smooth
| p[* as i]{opacity: 0, scale: 0.2} -> p[i]
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
| 1.2s | smooth | stagger 0.08s
| roots[* as i].mark{opacity: 0, scale: 0.2} -> roots[i].mark
| roots[* as i].hit{draw: 0} -> roots[i].hit
```

If many objects derive from a list, animate the list and let live fields follow:

```pdtt
| 4s | smooth
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
- trig/math: `sin`, `cos`, `exp`, `sinh`, `cosh`, `abs`, `sqrt`
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

text("Парабола показывает квадратичный рост.") title:
  at: [0, 3.05]
  scale: 0.56
  color: color.white

typst("f(x)=0.35x^2-1") formula:
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

| 1.0s | smooth
| grid{draw: 0} -> grid
| ax{opacity: 0} -> ax

| 0.7s | smooth
| title{opacity: 0} -> title

| 2.0s

| 0.45s | smooth
| title.opacity -> 0

| 0.7s | smooth
| formula{opacity: 0} -> formula
| curve{draw: 0} -> curve
| p{opacity: 0, scale: 0.2} -> p

| 1.4s

| 4.0s | smooth
| t -> 3

| 1.5s
```

## Things That Do Not Exist

Do not generate these:

- Markdown fences or explanatory prose around the code
- `bind`, `track`, updater callbacks, `during {}`
- `tween x to y`, `animate(...)`, `wait(...)`
- camera classes; use the built-in `frame` record
- uppercase constants like `RED`, `BLUE`, `UL`, `RIGHT`
- plural type names like `dots p:` or `axeses`
- `morph` as a verb; use `| ... | morph | a -> b`
- `gone`; tween `opacity` or `draw` to `0`
- `for: from "cmd"` and `fast_after`; this prototype rejects them
- LaTeX-only commands in `typst(...)`
