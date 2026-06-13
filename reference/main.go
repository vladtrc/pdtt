package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fogleman/gg"
)

func main() {
	in := flag.String("i", "", "input .pipe file")
	out := flag.String("o", "frames", "output directory for PNG frames")
	fps := flag.Float64("fps", 30, "frames per second")
	w := flag.Int("w", 960, "width px")
	h := flag.Int("h", 540, "height px")
	flag.Parse()

	if *in == "" {
		fmt.Fprintln(os.Stderr, "usage: pipe36 -i scene.pipe -o outdir")
		os.Exit(2)
	}
	if err := run(*in, *out, *fps, *w, *h); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(in, out string, fps float64, w, h int) error {
	if err := initFonts(); err != nil {
		return err
	}
	src, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	stmts, err := ParseFile(string(src))
	if err != nil {
		return err
	}
	rt, err := Compile(stmts)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}

	r := NewRenderer(w, h)
	nFrames := int(rt.Total*fps) + 1
	fmt.Printf("%s: %s — %.1fs, %d frames, %d anims, %d live fields\n",
		filepath.Base(in), rt.SceneName, rt.Total, nFrames, len(rt.Anims), len(rt.liveFields))

	for k := 0; k < nFrames; k++ {
		t := float64(k) / fps
		if err := rt.Step(t); err != nil {
			return fmt.Errorf("t=%.2fs: %v", t, err)
		}
		dc := r.Frame(rt)
		path := filepath.Join(out, fmt.Sprintf("f%05d.png", k))
		if err := gg.SavePNG(path, dc.Image()); err != nil {
			return err
		}
	}
	return nil
}
