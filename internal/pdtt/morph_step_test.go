package pdtt

import (
	"math"
	"testing"
)

// Tripwire for the non-outline crossfade morph path: opacity cross-fade plus
// the positional dst/src offset interpolation that currently lives in
// a.start[0..3]. Guards behavior while morph's captured state is refactored
// into a typed struct on Anim (concepts 01 + 04).
func TestMorphCrossfadeStepsOpacityAndOffset(t *testing.T) {
	rt := compileScene(t, `scene morph_crossfade

dot src:
  at: [0, 0]
  radius: 0.2
  color: color.white

plane dst:
  at: [2, 0]
  x_range: [-1, 1, 1]
  y_range: [-1, 1, 1]

| 1s
| transition:morph | src -> dst
`)

	src := oneEntity(t, rt, "src")
	dst := oneEntity(t, rt, "dst")

	type want struct {
		srcOp, dstOp   float64
		dstOff, srcOff Vec
	}
	check := func(at float64, w want) {
		t.Helper()
		if err := rt.Step(at); err != nil {
			t.Fatalf("Step(%v): %v", at, err)
		}
		near := func(label string, got, exp float64) {
			if math.Abs(got-exp) > 1e-9 {
				t.Errorf("u=%v %s = %v, want %v", at, label, got, exp)
			}
		}
		nearVec := func(label string, got, exp Vec) {
			for i := range got {
				if math.Abs(got[i]-exp[i]) > 1e-9 {
					t.Errorf("u=%v %s = %v, want %v", at, label, got, exp)
					return
				}
			}
		}
		near("src.opacity", src.fnum("opacity"), w.srcOp)
		near("dst.opacity", dst.fnum("opacity"), w.dstOp)
		nearVec("dst.Offset", dst.Offset, w.dstOff)
		nearVec("src.Offset", src.Offset, w.srcOff)
	}

	// src at [0,0], dst at [2,0] ⇒ offset = srcPos - dst.at = [-2,0].
	check(0.0, want{srcOp: 1, dstOp: 0, dstOff: Vec{-2, 0}, srcOff: Vec{0, 0}})
	check(0.5, want{srcOp: 0.5, dstOp: 0.5, dstOff: Vec{-1, 0}, srcOff: Vec{1, 0}})
	check(1.0, want{srcOp: 0, dstOp: 1, dstOff: Vec{0, 0}, srcOff: Vec{2, 0}})

	if src.Active {
		t.Error("u=1: src still active, want inactive")
	}
	if !dst.Active {
		t.Error("u=1: dst inactive, want active")
	}
}

// Guards the outline (contour-interpolation) morph branch of step: two path
// shapes morph by their outlines, the source carries interpolated contours
// mid-tween, and the handoff to the destination completes at u=1.
func TestMorphOutlineStepsContoursAndHandoff(t *testing.T) {
	rt := compileScene(t, `scene morph_outline

path a:
  at: [0, 0]
  points: circle{radius: 0.85}
  closed: 1
  stroke.color: color.white

path b:
  at: [0, 0]
  points: rect{w: 1.5, h: 1.5}
  closed: 1
  stroke.color: color.gold

| 1s
| transition:morph | a -> b
`)
	a := oneEntity(t, rt, "a")
	b := oneEntity(t, rt, "b")

	if err := rt.Step(0.5); err != nil {
		t.Fatalf("Step(0.5): %v", err)
	}
	if len(a.MorphContours) == 0 {
		t.Error("u=0.5: source has no interpolated contours, outline branch not taken")
	}

	if err := rt.Step(1.0); err != nil {
		t.Fatalf("Step(1.0): %v", err)
	}
	if a.MorphContours != nil {
		t.Error("u=1: source contours not cleared")
	}
	if a.Active {
		t.Error("u=1: source still active")
	}
	if !b.Active {
		t.Error("u=1: destination not active")
	}
	if op := b.fnum("opacity"); math.Abs(op-1) > 1e-9 {
		t.Errorf("u=1: destination opacity = %v, want 1", op)
	}
}
