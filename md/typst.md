# Typst-native text + text morphing (task spec)

Goal (from `humantxt/4`): add an example/test where **text morphs into another text and
then into a shape**. The Python reference (`ref.py`) uses **LaTeX** (manim `MathTex`). Our
`pdtt` implementation uses **Typst NATIVELY — no approximation hacks**.

Today `internal/pdtt/text.go` does NOT render real markup: it maps a handful of `\commands`
to unicode and strips the rest (`cleanTex`), then draws goregular glyphs with freetype. That
is the "hack" we are removing. Typst gives us **real glyph vector outlines**, which also makes
a *true* outline morph possible (text→text and text→shape) instead of the cross-fade hack in
`compile.go`'s `expandMorph` (the `textMorph` branch).

## Pieces to build

### 1. `internal/pdtt/typst.go` — invoke typst, get glyph outlines
- Function `typstGlyphs(markup string, math bool) (contours [][]Vec, bbox Box, err error)`.
- Build a `.typ` source:
  ```
  #set page(width: auto, height: auto, margin: 0pt, fill: none)
  #set text(fill: black)
  <body>
  ```
  For `math=true`, wrap body in `$ ... $`. The body is the entity's `text` field.
- Run `typst compile -f svg - -` (markup on **stdin**, SVG on **stdout**). Resolve the binary
  from `$PATH` (`exec.LookPath("typst")`). If typst is missing, return an error the caller can
  fall back on (see §4).
- Parse the SVG (next file). Coordinates come out in **pt**, Y-down in the symbol; the
  `<g class="typst-text" transform="matrix(1 0 0 -1 0 H)">` flips to Y-up — apply it so the
  returned contours are **Y-up, origin at glyph-run baseline-ish, units = pt**.
- **Cache** by `(markup,math)` — typst spawn is ~50ms; never call it twice for the same string.

### 2. `internal/pdtt/svgpath.go` — SVG `<path>` d parser + flattener
- Parse the typst SVG: collect `<symbol id=..><path d=..></symbol>` glyph defs, then the
  `<use xlink:href="#id" x=... y=...>` placements inside `<g class="typst-text" transform=..>`.
- Implement an SVG-path `d` tokenizer supporting `M m L l H h V v C c Q q Z z` (typst emits
  `M`, `m`, `l`, `c`, `Z` — relative-heavy; absolute `M` then relative `m`). Flatten cubic/
  quadratic beziers to polylines at a fixed tolerance (e.g. subdivide each curve into ~12
  segments, or adaptive). Each subpath (between `M`/`Z`) is one **contour** (`[]Vec`).
- Apply: glyph-local coords → `+ (use.x, use.y)` → text-group `matrix(1 0 0 -1 0 H)` (i.e.
  `y' = H - y`) so output is Y-up. Return all glyphs' contours concatenated, plus bbox.

### 3. Render text from outlines — `render.go` `drawText`
- Replace freetype drawing for `tex`/`text` entities with **filled glyph contours**: for each
  contour `dc.MoveTo/LineTo...; dc.ClosePath()`, then `dc.SetFillRuleEvenOdd()` + `dc.Fill()`
  using the entity color/opacity so counters (the hole in `A`, `B`, `o`) render correctly.
- Map pt→world units and position via the entity transform (`at`, `scale`, `angle`,
  `opacity`) and `font_size`. Tune the pt→world scale so existing text examples stay legible
  (roughly match the old `em = 0.62*fs/48*scale` sizing — pick a constant `ptToWorld` so a
  48pt-ish glyph height matches old output; verify visually).
- Layout caching: cache the contour set on the entity keyed by `(text,font_size,scale)` the
  same way `textLayoutOf` caches today, so we don't re-parse per frame.

### 4. `outlinePoints` for text + unified outline morph — `render.go` + `compile.go`
- Extend `outlinePoints(e, n)` so that for `tex`/`text`/`decimal` it returns `n` points sampled
  **by cumulative arc length** over *all* glyph contours concatenated into one ordered ring
  (resample so src and dst both have exactly `n` points). This is the naive-but-smooth morph
  (same family as manim's Transform) and plugs straight into the existing point-lerp loop.
- In `expandMorph`: **delete the `textMorph` cross-fade hack**. Make the morph use the
  outline-point path whenever **both** sides can produce outlines. So:
  - text → text  : outline morph (glyph A outline → glyph B outline)
  - text → shape : outline morph (glyph outline → closed path/dot outline)
  - shape → shape: unchanged (already outline morph)
  Generalize `isShapeType`→`canOutline(typ)` covering `path dot tex text decimal`.
  Keep the opacity hand-off (`src.opacity→0`, `dst.opacity→1` at `u>=1`) and the stroke/fill
  blend already there. Bump the sample count `n` if 64 looks too coarse for glyphs.

### 5. Example `examples/45-text-morph/`
- `run.pdtt` (NEW lowercase syntax): a `tex` "A" morphs to a `tex` "B", then to a shape
  (`dot` or a closed `path`). Sketch:
  ```
  scene text_morph

  tex a:
    at: [0, 0]
    text: "A"
    stroke: color.white

  tex b:
    at: [0, 0]
    text: "B"
    stroke: color.white

  dot c:
    at: [0, 0]
    radius: 1.2
    stroke: color.white
    fill: color.pink @ 50%
    opacity: 0

  | 1s
  | a.opacity -> 1     # or | write | a -> a  if write modifier exists; keep it simple

  | 1s
  | morph | a -> b

  | 1s
  | morph | b -> c
  ```
  (Adjust to whatever the parser actually supports — check `md/syntax.md` and the other
  examples. The point: text→text→shape via `| morph |`.)
- `ref.py`: manim mirror using **LaTeX**: `MathTex("A")` → `Transform` to `MathTex("B")` →
  `Transform` to `Circle()`/`Dot()`. Guard like the other refs (manim/latex may be absent).

## Constraints / definition of done
1. `go build ./...` and `go vet ./...` clean.
2. App still assembled via the d2 container; pipeline unchanged in shape.
3. **All existing examples still render** (09, 14, 20, 40) — don't regress their output.
4. `examples/45-text-morph/run.pdtt` renders PNG frames (+ mp4 if ffmpeg present) into `res/`.
5. Text is rendered through **typst** (real outlines), not `cleanTex` unicode substitution.
   Keep a graceful error/fallback if `typst` is not on PATH, but the happy path is typst.
6. typst binary: assume `typst` on `$PATH` (v0.14+, installed at `~/.local/bin/typst`).
   Add a note to `README.md` that typst is required for text.
