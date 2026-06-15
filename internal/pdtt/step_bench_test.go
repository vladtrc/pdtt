package pdtt

import (
	"os"
	"path/filepath"
	"testing"
)

func compileHeartScene(b *testing.B) *Runtime {
	b.Helper()
	path := filepath.Join("..", "..", "examples", "heart", "run.pdtt")
	src, err := os.ReadFile(path)
	if err != nil {
		b.Fatalf("read heart scene: %v", err)
	}
	stmts, err := ParseFile(string(src))
	if err != nil {
		b.Fatalf("ParseFile: %v", err)
	}
	rt, err := Compile(stmts)
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	return rt
}

func BenchmarkHeartStepAllFrames(b *testing.B) {
	rt := compileHeartScene(b)
	const fps = 5.0
	nFrames := int(rt.Total*fps) + 1
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for k := 0; k < nFrames; k++ {
			t := float64(k) / fps
			if err := rt.Step(t); err != nil {
				b.Fatalf("Step(%v): %v", t, err)
			}
		}
	}
}
