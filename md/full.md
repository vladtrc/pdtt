# PDTT quick reference for scene generation

Write only valid PDTT source code. No Markdown fences, no explanation.

Target render is a square 640x640 video. The default camera frame is about `14.2 x 8.0` world units, centered at `[0, 0]`; keep important content inside roughly `x = -6.7..6.7`, `y = -3.6..3.6`. Use `frame` only when a camera move is needed. Normal title/narration text scale is `0.48..0.70`; formulas `0.65..0.95`; labels near points `0.28..0.42`. Avoid long text lines; split ideas into several short sequential cards.

## Scene quality rules

- Make scenes readable: introduce objects sequentially, then animate relationships.
- Do not show more than one visible explanatory text card at the same time. Fade the old text out before showing the next.
- Do not morph text. Use opacity/write transitions for text; reserve `morph` for shape-to-shape or visual object transformations.
- After text appears, pause long enough to read it: usually `1.2s..2.8s` depending on length.
- Parallel animation is powerful, but use it mostly for related non-text objects. Text beats should usually be sequential.
- Prefer clean layouts: title near top (`y` around `3.0`), main geometry centered or slightly below, labels close to objects.
- Use colors sparingly: `color.white` for main text, `color.yellow` for emphasis, and a few distinct accent colors such as `color.blue`, `color.green`, `color.red`, `color.pink`, `color.magenta`, `color.teal`, `color.cyan`, `color.orange`, or `color.purple`.
- End with a short hold or gentle fade-out; avoid abrupt final frames.

## Top-level syntax

```
scene name

name: expr                  # live global; re-evaluates when dependencies change
name = expr                 # frozen capture
home: snapshot frame        # frozen snapshot of record state

type name:
  field: expr
  rate field: expr
  for: list_or_range

text("short text") label:
  at: [0, 3]
  scale: 0.6
  color: color.white

typst("x^2 + y^2 = r^2") formula:
  at: [0, 2.7]
  scale: 0.8
  color: color.yellow
```

Records start inactive and are not rendered until an entry or morph activates them. Same `type name:` declared again merges fields; first field wins.

## Time syntax

Animation lines start with `|`. A blank line ends a block. First `|` line of a block is the clock; following rows in that block run in parallel unless windows/stagger change them.

```
| 1.0s | smooth
| obj{opacity: 0, scale: 0.2} -> obj

| 2.0s | linear
| value -> 5

| 0.6s | smooth
| old_text.opacity -> 0

| 0.6s | smooth
| new_text{opacity: 0} -> new_text

| 1.8s
```

`path -> expr` tweens a field/global to a target. After the tween, the path is the target; if the target is dynamic, it keeps following.

`obj{field: start, ...} -> obj` is an entry/self-transition: activates `obj` and tweens overridden fields from the phantom start values to declared values. Common entries:

```
| dot{opacity: 0, scale: 0.2} -> dot
| line{draw: 0} -> line
| label{opacity: 0} -> label
```

Modifiers before `->`:

- Windows: `0-.5`, `.5-`, `25%-75%`, `0.2s-1.1s`
- Easing: `linear`, `smooth`, `ease_in`, `ease_out`, `ease_in_out`
- Transitions: `morph`, `fade_in`, `draw`, `write`
- Offsets: `after 0.5s`, `lag 0.2s`, `stagger 0.08s`
- Pairing: `by name`, `by pos`

Blocks:

```
| 1.2s | smooth              # rows below run together
| grid{draw: 0} -> grid
| axes{opacity: 0} -> axes

| 2.0s                       # pause/hold
```

## Collections and broadcast

```
xs: [-2, 0, 2]
cols: [color.red, color.green, color.blue]

dot p:
  for: xs
  at: [it, 0]
  radius: 0.1
  color: cols[it.i]

| 1.2s | stagger 0.12s | smooth
| p[*]{opacity: 0, scale: 0.2} -> p[i]
```

Broadcast `record[*]` expands one row per element. Use `it.i`/`i` for index and `it.n` for count. Use `record[* as i]` when RHS needs the same index. `| each record 0.6s` creates sequential blocks per element.

## Useful primitives

Common fields: `at`, `scale`, `angle`, `opacity`, `color`, `stroke`, `fill`, `draw`.

Shapes:

```
dot d:
  at: [0, 0]
  radius: 0.12
  color: color.blue

square s:
  at: [0, 0]
  side: 1.2
  stroke: color.white
  fill: color.blue @ 25%

rect r:
  at: [0, 0]
  w: 2
  h: 1
  stroke: color.white

arrow a:
  from: [-2, 0]
  to: [2, 0]
  color: color.yellow
```

Graphs:

```
plane grid:
  at: [0, -0.4]
  x_range: [-4, 4, 1]
  y_range: [-3, 3, 1]

axes ax:
  at: grid.at
  x_range: grid.x_range
  y_range: grid.y_range

plot parabola:
  axes: ax
  fn: 0.35 * x * x - 1
  color: color.yellow

dot point:
  at: ax.point(t, 0.35 * t * t - 1)
  radius: 0.11
  color: color.red
```

Text:

```
text("A short readable sentence.") note:
  at: [0, 3.05]
  scale: 0.58
  color: color.white

typst("f(x)=x^2") formula:
  at: [0, 2.55]
  scale: 0.82
  color: color.yellow
```

Use `text(...)` for prose, `typst(...)` for formulas/math-like labels. Do not use LaTeX commands unless Typst understands them; prefer plain Typst-ish math strings like `"f(x)=x^2"`, `"x'' + x = 0"`, `"dot x"`.

## Expressions and helpers

Types: int/float/string/bool, vec2/vec3 (`[x, y]`), color, duration. Operators: arithmetic, comparisons, booleans, ternary `cond ? a : b`.

Builtins/namespaces: `fmt`, `range`, `sum`, `prod`, `pair_sum`, `sqrt`, `sin`, `cos`, `tan`, `sinh`, `cosh`, `abs`, `center(obj)`, `below(obj, gap)`, `above(obj, gap)`, `right_of(obj, gap)`, `corner(corner.ul)`, `edge(approx.right)`, `math.pi`, `math.tau`.

Colors: `color.white`, `color.black`, `color.gray`/`color.grey`, `color.light_gray`, `color.dark_gray`, `color.blue`, `color.blue_e`, `color.cyan`, `color.teal`, `color.green`, `color.lime`, `color.yellow`, `color.gold`, `color.orange`, `color.red`, `color.red_b`, `color.maroon`, `color.pink`, `color.magenta`, `color.purple`, `color.violet`, `color.brown`. Use opacity with `@`, for example `color.magenta @ 60%`.

## Things that do not exist

No `bind`, `track`, updater callbacks, `during {}`, `tween x to y`, camera class, uppercase constants (`RED`, `UL`), plural type names, or morph as a verb. Use `->` with modifiers, dynamic expressions, the builtin `frame`, namespaced constants, and singular record names.

## Reliable scene pattern

1. Declare globals, grid/axes/objects/text.
2. Enter grid/axes or background.
3. Show one short text card, pause, fade it out.
4. Animate the main object(s).
5. Show the next text/formula, pause, fade it out.
6. Finish with the final visual state and a hold.

Example:

```
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
