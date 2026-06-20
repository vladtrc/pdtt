package render

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSceneWritesDebugFrames(t *testing.T) {
	outDir := t.TempDir()
	res, err := Scene(`scene api

dot p:
  at: [0, 0]
`, outDir, Config{FPS: 1, Width: 80, Height: 60})
	if err != nil {
		t.Fatalf("Scene: %v", err)
	}
	if res.FrameCnt != 1 {
		t.Fatalf("FrameCnt = %d, want 1", res.FrameCnt)
	}
	if res.FramesDir != filepath.Join(outDir, "frames") {
		t.Fatalf("FramesDir = %q, want %q", res.FramesDir, filepath.Join(outDir, "frames"))
	}
	if _, err := os.Stat(filepath.Join(res.FramesDir, "f00000.png")); err != nil {
		t.Fatalf("rendered frame missing: %v", err)
	}
}
