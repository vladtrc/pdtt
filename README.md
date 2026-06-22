# pdtt

`pdtt` compiles declarative scene files to PNG frames and then to MP4.

## Build

```bash
go build ./...
```

or:

```bash
make build
```

## Format and lint

```bash
make fmt
```

`make fmt` runs `gofumpt`, `go vet`, and `golangci-lint` on `cmd` and `internal` packages.
The external Go tools are executed with `go run`, so they do not need to be installed into `PATH`.

## Render examples

```bash
make                              # build, then render EVERY example in parallel
make render EXAMPLE=shape-morph  # render a single example
```

Outputs are written to `examples/<name>/res/`:

- `frames/f00000.png`, ...
- `result.mp4` (when `ffmpeg` is installed)

## Render the manim references

```bash
make ref                          # render every ref.py in parallel
```

Reference scenes (`examples/<name>/ref.py`) are rendered through `uv`
(`uv run --with manim`), so no global manim install is needed — just `uv`.
The reference video lands at `examples/<name>/ref/result.mp4`, mirroring `res/`.
This is best-effort: if `uv` is missing or a scene needs LaTeX that isn't
installed, it warns and skips without failing the build.

## CLI

```bash
./bin/pdtt -i examples/shape-morph/run.pdtt -o examples/shape-morph/res -fps 30 -w 960 -h 540
```

Flags:

- `-i` input `.pdtt` file
- `-o` output directory
- `-fps` frames per second
- `-w` frame width
- `-h` frame height

## Notes

- App wiring uses the `d2` DI container (`config -> parser -> compiler -> renderer -> encoder`).
- Constants are lowercase and namespaced (`color.*`, `corner.*`, `approx.*`).
- Math constants are provided under `math.*` (`math.pi`, `math.tau`).
- Linear/filled geometry uses `path`; `arrow`, `line`, `rect`, `square`, `arc`, `circle`, `ellipse`, and `polygon` are not record types.
- Arrowheads are path styling: `stroke.end: arrow`.
- Tween (`->`) keeps tracking dynamic RHS expressions after the tween window ends.
- `examples/20-dynamic-point-tween` demonstrates a tween between two moving points.
- Records start inactive. Use `in:PRESET | subject` for entrance (`in:draw`, `in:fade`, `in:pop`, …) or `transition:morph | a -> b` to morph between shapes.
- Text rendering uses `typst` (`typst compile -f svg - -`) for native glyph outlines; if `typst` is absent on `PATH`, pdtt falls back to legacy freetype text rendering.
- Text records use `text name:` with a `text:` field; math/Typst records use `typst name:` the same way. Plain text is set in "New Computer Modern", matching the math letterforms.
- Text strings take `\n` for line breaks; `in:draw | t` reveals text left to right (glyph by glyph), the text analogue of drawing a path on.
- Any substring is independently tweenable via `t.sub("phrase").{color,opacity,strike,underline,scale,wiggle}`; a plain `->` arrow sets-and-holds the effect.
- For a one-shot highlight that lights up then settles back to rest, use a transient modifier cell: `| 1s | ease:smooth | highlight:flash | t.sub("phrase")` (also `strike`, `underline`, `enlarge`, `wiggle`). See `examples/text-features`.
