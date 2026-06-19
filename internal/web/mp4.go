package web

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// EncodeFramesToMP4 writes an H.264 MP4 from PNG frames using ffmpeg.
// Frames must be named f00000.png, f00001.png, … in framesDir.
// The file is written atomically via a temp file in the same directory and rename.
func EncodeFramesToMP4(framesDir string, fps float64, destPath string) (int64, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return 0, fmt.Errorf("ffmpeg not found: %w", err)
	}

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(destPath)+".*.tmp")
	if err != nil {
		return 0, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	inputPattern := filepath.Join(framesDir, "f%05d.png")
	cmd := exec.Command(
		ffmpegPath,
		"-y",
		"-nostdin",
		"-loglevel", "error",
		"-framerate", fmt.Sprintf("%.4f", fps),
		"-i", inputPattern,
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		"-f", "mp4",
		tmpPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffmpeg: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}

	info, err := os.Stat(tmpPath)
	if err != nil {
		return 0, err
	}
	if info.Size() == 0 {
		return 0, fmt.Errorf("empty mp4 output")
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return 0, err
	}
	cleanup = false
	return info.Size(), nil
}

type mp4Encoder struct{}

func (mp4Encoder) EncodeFramesToFile(framesDir string, fps float64, _ int, destPath string) (int64, error) {
	return EncodeFramesToMP4(framesDir, fps, destPath)
}
