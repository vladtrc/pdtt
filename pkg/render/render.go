// Package render exposes pdtt's core pipeline as a public API.
package render

import (
	"path/filepath"

	"github.com/vladtrc/pdtt/internal/pdtt"
)

// Config mirrors pdtt.Config for callers outside the module.
type Config struct {
	FPS    float64
	Width  int
	Height int
}

// Result holds the output of a render call.
type Result struct {
	FramesDir string
	FrameCnt  int
}

// Validate parses and compiles a pdtt scene without rendering frames.
func Validate(src string) error {
	stmts, err := pdtt.ParseFile(src)
	if err != nil {
		return err
	}
	_, err = pdtt.Compile(stmts)
	return err
}

// Scene parses, compiles, and renders a pdtt scene from src text, writing PNG
// frames into outDir. Returns the frames directory path and frame count.
func Scene(src string, outDir string, cfg Config) (*Result, error) {
	stmts, err := pdtt.ParseFile(src)
	if err != nil {
		return nil, err
	}
	rt, err := pdtt.Compile(stmts)
	if err != nil {
		return nil, err
	}
	pcfg := pdtt.Config{
		InputPath: "scene.pdtt",
		OutputDir: outDir,
		FPS:       cfg.FPS,
		Width:     cfg.Width,
		Height:    cfg.Height,
	}
	// No mp4 stream: this entry point only writes PNG frames.
	result, err := pdtt.NewFrameRenderer().Render(rt, pcfg, nil, nil)
	if err != nil {
		return nil, err
	}
	if err := result.WaitDebug(); err != nil {
		return nil, err
	}
	return &Result{
		FramesDir: filepath.Clean(result.FramesDir),
		FrameCnt:  result.FrameCnt,
	}, nil
}
