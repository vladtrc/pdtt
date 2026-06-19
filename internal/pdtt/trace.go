package pdtt

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Tracer writes structured JSON lines to traces.jsonl in the output directory.
type Tracer struct {
	log *slog.Logger
	f   *os.File
}

func NewTracer(outputDir string) (*Tracer, error) {
	path := filepath.Join(outputDir, "traces.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	h := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})
	return &Tracer{log: slog.New(h), f: f}, nil
}

func (t *Tracer) Close() error {
	if t == nil || t.f == nil {
		return nil
	}
	return t.f.Close()
}

func (t *Tracer) Info(msg string, args ...any) {
	if t != nil && t.log != nil {
		t.log.Info(msg, args...)
	}
}

func durMs(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
