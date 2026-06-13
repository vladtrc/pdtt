package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/vladtrc/d2"
	"github.com/vladtrc/pdtt/internal/pdtt"
)

func main() {
	in := flag.String("i", "", "input .pdtt file")
	out := flag.String("o", "frames", "output directory for PNG frames and result.mp4")
	fps := flag.Float64("fps", 30, "frames per second")
	w := flag.Int("w", 960, "width in pixels")
	h := flag.Int("h", 540, "height in pixels")
	flag.Parse()

	if *in == "" {
		fmt.Fprintln(os.Stderr, "usage: pdtt -i scene.pdtt -o outdir")
		os.Exit(2)
	}

	c := d2.NewContainer()
	d2.Provide[pdtt.Config](c, pdtt.Config{
		InputPath: *in,
		OutputDir: *out,
		FPS:       *fps,
		Width:     *w,
		Height:    *h,
	})
	d2.Provide[*pdtt.SceneParser](c, pdtt.NewSceneParser)
	d2.Provide[*pdtt.SceneCompiler](c, pdtt.NewSceneCompiler)
	d2.Provide[*pdtt.FrameRenderer](c, pdtt.NewFrameRenderer)
	d2.Provide[*pdtt.MP4Encoder](c, pdtt.NewMP4Encoder)
	d2.Provide[*pdtt.App](c, pdtt.NewApp)

	if err := d2.Run(c, func(app *pdtt.App) error {
		return app.Run()
	}); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
