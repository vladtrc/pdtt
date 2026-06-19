// Package render exposes pdtt's core pipeline as a public API.
package render

import (
	"os"
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
	tmpFile, err := os.CreateTemp("", "pdtt-validate-*.pdtt")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(src); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	stmts, err := pdtt.NewSceneParser().ParsePath(tmpFile.Name())
	if err != nil {
		return err
	}
	_, err = pdtt.NewSceneCompiler().CompileStmts(stmts)
	return err
}

// Scene parses, compiles, and renders a pdtt scene from src text,
// writing PNG frames into outDir. Returns the frames directory path and frame count.
func Scene(src string, outDir string, cfg Config) (*Result, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	tmpFile, err := os.CreateTemp("", "pdtt-*.pdtt")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(src); err != nil {
		tmpFile.Close()
		return nil, err
	}
	tmpFile.Close()

	pcfg := pdtt.Config{
		InputPath: tmpFile.Name(),
		OutputDir: outDir,
		FPS:       cfg.FPS,
		Width:     cfg.Width,
		Height:    cfg.Height,
	}

	stmts, err := pdtt.NewSceneParser().ParsePath(tmpFile.Name())
	if err != nil {
		return nil, err
	}
	rt, err := pdtt.NewSceneCompiler().CompileStmts(stmts)
	if err != nil {
		return nil, err
	}
	res, err := pdtt.NewFrameRenderer().Render(rt, pcfg, nil)
	if err != nil {
		return nil, err
	}
	return &Result{
		FramesDir: filepath.Join(outDir, "frames"),
		FrameCnt:  res.FrameCnt,
	}, nil
}
