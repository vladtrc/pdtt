package web

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

func TestEncodeFramesToMP4AtomicOnError(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	out := filepath.Join(t.TempDir(), "out.mp4")

	_, err := EncodeFramesToMP4(dir, 10, out)
	if err == nil {
		t.Fatal("expected error for missing frames")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("partial dest should not exist, stat err=%v", err)
	}
}

func TestEncodeFramesToMP4WritesVideo(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	writeFramePNG(t, filepath.Join(dir, "f00000.png"), 64, 64)
	writeFramePNG(t, filepath.Join(dir, "f00001.png"), 64, 64)

	out := filepath.Join(t.TempDir(), "out.mp4")
	size, err := EncodeFramesToMP4(dir, 10, out)
	if err != nil {
		t.Fatalf("EncodeFramesToMP4: %v", err)
	}
	if size <= 0 {
		t.Fatalf("size = %d", size)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != size {
		t.Fatalf("stat size = %d, returned %d", info.Size(), size)
	}
}

func writeFramePNG(t *testing.T, path string, width, height int) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create PNG: %v", err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode PNG: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close PNG: %v", err)
	}
}
