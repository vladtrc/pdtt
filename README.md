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

## Run an example

```bash
make render EXAMPLE=40-shape-morph
```

Outputs are written to `examples/<name>/res/`:

- `f00000.png`, ...
- `result.mp4` (when `ffmpeg` is installed)

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
