# pdtt — build task for the coding agent

You are implementing **pdtt**, a small declarative animation language that compiles
text scenes to PNG frames and then to an MP4. This is a **rename + cleanup port** of an
existing working Go implementation. Your job is to produce a clean, building Go project
in this directory that can run pdtt examples end-to-end.

## Sources of truth (read these first)

1. **`md/`** — the canonical language spec. THIS IS AUTHORITATIVE. Where it disagrees
   with the reference implementation, `md/` wins.
   - `md/glossary.md` — every term (record, field, window, tween `->`, modifier, `it`, `self`, capture `=`, block, each, `for:`, `frame`).
   - `md/syntax.md` — top-level forms, expressions, windows, modifier cells, broadcast, compile order. **No LaTeX. Typst comes later, not now.**
   - `md/tween.md` — semantics of `->` (the "tween" operator).
   - `md/types.md` — primitive types, `dur`, collections, broadcast.
   - `md/why-not.md` — terms that intentionally do NOT exist.
2. **`reference/*.go`** — the existing working implementation (module was `pipe36`).
   Use it as your starting code. Port it; don't rewrite from scratch unless a file is small.
   It is a READ-ONLY reference — write the real implementation into the repo root / `internal/`.
3. **`reference/examples/*.pipe`** — old example scenes (OLD syntax — uppercase consts, `morph` as verb).
4. **`reference/manim-src/*.py`** — the manim scenes the examples mirror; these become `ref.py`.

## What changes vs the reference (apply md/ decisions)

- **Rename** everything `pipe36` → `pdtt`. File extension `.pipe` → `.pdtt`.
- **Lowercase everything.** No UPPERCASE magic constants. Replace `WHITE`, `PINK`,
  `ORIGIN`, `TAU`, `UL`, `DOWN`, etc. with namespaced globals:
  - colors: `color.red color.blue color.white color.black color.yellow color.green color.pink ...`
  - corners: `corner.ul corner.ur corner.dl corner.dr corner.center`
  - approx: `approx.above approx.below approx.left approx.right`
  - `[0,0]` for origin. Provide a math namespace for `tau`/`pi` (e.g. `math.tau`, `math.pi`) OR builtin lowercase `tau`/`pi` — pick one, document it.
- **`morph` is a MODIFIER, not a verb.** Syntax: `| morph | s -> d` (between pipes, before the tween). Same for `fade_in`, `draw`, `write`.
- **The `->` operator is called the "tween".** `path -> expr`: during the window interpolate; after the window `path` *is* `expr` (see md/tween.md).
- **Fix dynamic-point tween** — the reference does not properly support tweening between
  two *dynamic* points (e.g. `a.at -> b.at` where both move). Make `path -> expr` work when
  the RHS is a live/dynamic expression: during the window interpolate current→(live target),
  and after the window keep tracking the live target. Add an example that exercises this.
- Keep features that md/ documents: records (`type name:`), value & rate fields, `for:` row
  sources, broadcast `[*]` with `it`/`it.0..`/`i j k l`, `each` blocks, captures `=`, the
  builtin `frame` camera record, windows/easing/modifiers.

## Architecture requirement: assemble the app with the d2 DI container

Use **`github.com/vladtrc/d2`** (a reflection-based DI container; already wired via a
`replace` to `../d2` in `go.mod`) to assemble the application. Pattern:
`d2.NewContainer()`, `d2.Provide[T](c, provider)`, `d2.Get[T](c)`, `d2.Run(c, fn)`.
Wire the pipeline stages (config → parser → compiler → renderer → encoder) as providers and
run via `d2.Run`. Read `../d2/container.go` and its tests for the exact API.

## CLI / pipeline

The tool should: read a `.pdtt` file → parse → compile → render PNG frames (using
`github.com/fogleman/gg`, as the reference does) → encode to MP4 via `ffmpeg`.
Keep the reference's frame-rendering approach. Provide a `cmd/pdtt` main (or root `main.go`)
with flags compatible with the reference (`-i`, `-o`, `-fps`, `-w`, `-h`) plus an MP4 output.

## Examples layout (from `asd`)

Core repo has `examples/`. Each example:

```
examples/<name>/run.pdtt     # the pdtt scene (NEW lowercase syntax)
examples/<name>/ref.py       # the manim scene it mirrors
examples/<name>/res/         # OUR rendered output (frames + result.mp4)   [gitignored]
examples/<name>/ref/         # manim's rendered output                      [gitignored]
```

Port the old `reference/examples/*.pipe` scenes into `examples/<name>/run.pdtt` using the
NEW syntax, and copy the matching `reference/manim-src/*.py` to `examples/<name>/ref.py`.
At minimum get **`40-shape-morph`** building and rendering with our compiler.

Provide a `Makefile` (or `scripts/run.sh`) with targets to build the binary and render an
example into its `res/` dir. Manim rendering of `ref.py` is optional/best-effort (manim may
not be installed) — guard it so the build still works without manim.

## Definition of done (verify before claiming done)

1. `go build ./...` succeeds.
2. `go vet ./...` is clean.
3. The app is assembled through the d2 container (not hand-wired `main`).
4. `examples/40-shape-morph/run.pdtt` exists in NEW syntax and renders to
   `examples/40-shape-morph/res/` (PNG frames + an `.mp4` if ffmpeg is present).
5. At least 2–3 examples ported to new syntax, including one that demonstrates the
   dynamic-point tween fix.
6. A short `README.md` documents how to build and run an example.

Work in stages, keep the build green, and prefer small composable files.
