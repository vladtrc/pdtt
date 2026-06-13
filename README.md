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

## Render examples

```bash
make                              # build, then render EVERY example in parallel
make render EXAMPLE=40-shape-morph  # render a single example
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
./bin/pdtt -i examples/40-shape-morph/run.pdtt -o examples/40-shape-morph/res -fps 30 -w 960 -h 540
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
- Tween (`->`) keeps tracking dynamic RHS expressions after the tween window ends.
