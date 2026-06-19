package pdtt

import (
	"os"
	"path/filepath"
	"testing"
)

func compileDNAScene(b *testing.B) *Runtime {
	b.Helper()
	path := filepath.Join("..", "..", "examples", "dna-to-mobius", "run.pdtt")
	src, err := os.ReadFile(path)
	if err != nil {
		b.Fatalf("read scene: %v", err)
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

func BenchmarkDNAStepSingle(b *testing.B) {
	rt := compileDNAScene(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt.T = 0
		rt.Dt = 0
		for _, a := range rt.Anims {
			a.started = false
			a.done = false
		}
		for _, ev := range rt.Events {
			ev.done = false
		}
		if err := rt.Step(5.0); err != nil {
			b.Fatalf("Step: %v", err)
		}
	}
}

func BenchmarkDNAStepAllFrames(b *testing.B) {
	rt := compileDNAScene(b)
	const fps = 30.0
	nFrames := int(rt.Total*fps) + 1
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt2 := compileDNAScene(b)
		for k := 0; k < nFrames; k++ {
			t := float64(k) / fps
			if err := rt2.Step(t); err != nil {
				b.Fatalf("Step(%v): %v", t, err)
			}
		}
	}
	_ = rt
}
