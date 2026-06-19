# pdtt — project guide for the coding agent

**pdtt** is a small declarative animation language. It compiles text scene files
(`.pdtt`) to PNG frames with [`github.com/fogleman/gg`](https://github.com/fogleman/gg),
then encodes them to MP4 via `ffmpeg`.

## Sources of truth

**`md/` is the authoritative language spec.** Where anything disagrees with it, `md/` wins.

- `md/glossary.md` — every term (record, field, window, tween `->`, modifier, `it`, `self`, capture `=`, block, each, `for:`, `frame`).
- `md/syntax.md` — top-level forms, expressions, windows, modifier cells, broadcast, compile order.
- `md/tween.md` — semantics of `->` (the "tween" operator).
- `md/types.md` — primitive types, `dur`, collections, broadcast.
- `md/why-not.md` — terms that intentionally do NOT exist.

## Language conventions

- **Lowercase, namespaced constants** — no UPPERCASE magic values:
  - colors: `color.red color.blue color.white color.black color.yellow color.green color.pink ...`
  - corners: `corner.ul corner.ur corner.dl corner.dr corner.center`
  - approx: `approx.above approx.below approx.left approx.right`
  - `[0,0]` for origin; math constants under `math.*` (`math.tau`, `math.pi`).
- **`morph` is a modifier, not a verb.** Syntax: `| morph | s -> d` (between pipes, before the tween). Same for `fade_in`, `draw`, `write`.
- **`->` is the "tween".** `path -> expr`: during the window interpolate; after the window `path` *is* `expr` (see `md/tween.md`). The tween keeps tracking a live/dynamic RHS after the window ends, including when both sides move (`a.at -> b.at`).
- Supported features: records (`type name:`), value & rate fields, `for:` row sources, broadcast `[*]` with `it`/`it.0..`/`i j k l`, `each` blocks, captures `=`, the builtin `frame` camera record, windows/easing/modifiers.

## Architecture

The application is assembled with the **`github.com/vladtrc/d2`** reflection-based DI
container (wired via a `replace` to `../d2` in `go.mod`), not hand-wired in `main`. The
pipeline stages are providers run via `d2.Run`: `config → parser → compiler → renderer → encoder`.

The CLI reads a `.pdtt` file → parses → compiles → renders PNG frames → encodes to MP4.
`cmd/pdtt` is the entrypoint, with flags `-i`, `-o`, `-fps`, `-w`, `-h`.

## Examples layout

Each example under `examples/<name>/`:

```
examples/<name>/run.pdtt     # the pdtt scene
examples/<name>/ref.py       # the manim scene it mirrors
examples/<name>/res/         # OUR rendered output (frames + result.mp4)   [gitignored]
examples/<name>/ref/         # manim's rendered output                      [gitignored]
```

`make` builds and renders every example into its `res/`. Manim rendering of `ref.py`
(`make ref`) is best-effort and guarded so the build works without `uv`/manim.

## Quality gates (verify before claiming done)

1. `go build ./...` succeeds.
2. `go vet ./...` and `make lint` are clean.
3. The app is assembled through the d2 container.
4. Every `examples/<name>/run.pdtt` renders to `examples/<name>/res/` (PNG frames + an `.mp4` when `ffmpeg` is present).
5. Tests pass: `go test ./...`.

Work in stages, keep the build green, and prefer small composable files.
