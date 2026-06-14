package pdtt

import (
	"math"
	"testing"
)

func TestDynamicPointTweenTracksLiveTargetDuringAndAfterWindow(t *testing.T) {
	rt := compileScene(t, `scene dynamic_point_tween

theta = 0

dot source:
  at: [-3, 0]

dot target:
  at: [2 * cos(theta), 2 * sin(theta)]

| 4s | linear
| theta -> math.tau
| source.at -> target.at

| 2s | linear
| theta -> 1.5 * math.tau
`)

	source := oneEntity(t, rt, "source")
	target := oneEntity(t, rt, "target")

	if err := rt.Step(2); err != nil {
		t.Fatalf("Step(2): %v", err)
	}
	wantMid := Vec{-2.5, 0, 0}
	assertVecNear(t, "source.at during tween", source.fvec("at"), wantMid)
	assertVecNear(t, "target.at during tween", target.fvec("at"), Vec{-2, 0, 0})

	if err := rt.Step(5); err != nil {
		t.Fatalf("Step(5): %v", err)
	}
	assertVecNear(t, "source.at after tween", source.fvec("at"), target.fvec("at"))
	assertVecNear(t, "tracked target position", target.fvec("at"), Vec{0, 2, 0})
}

func assertVecNear(t *testing.T, label string, got, want Vec) {
	t.Helper()
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Fatalf("%s[%d] = %v, want %v (got %v)", label, i, got[i], want[i], got)
		}
	}
}
