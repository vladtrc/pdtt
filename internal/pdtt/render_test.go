package pdtt

import "testing"

func TestClosedPathTrimStartsEmptyAndUsesClosingSegment(t *testing.T) {
	pts := []Vec{{0, 0}, {2, 0}, {2, 2}, {0, 2}}

	if got := trimPathPoints(closePathPoints(pts), 0); got != nil {
		t.Fatalf("draw=0 returned %v, want nil", got)
	}

	got := trimPathPoints(closePathPoints(pts), 0.875)
	want := Vec{0, 1}
	if len(got) != 5 {
		t.Fatalf("trimmed point count = %d, want 5: %v", len(got), got)
	}
	if got[len(got)-1] != want {
		t.Fatalf("last trimmed point = %v, want %v", got[len(got)-1], want)
	}
}

func TestDotMorphFillPrefersFillOverStroke(t *testing.T) {
	rt := compileScene(t, `scene dot_style

dot d:
  stroke: color.white
  fill: color.pink @ 50%
`)
	style := shapeStyleForMorph(oneEntity(t, rt, "d"))
	if style.FillColor == namedColors["white"] {
		t.Fatal("dot morph fill used stroke color, want fill color")
	}
	if style.FillA < 0.49 || style.FillA > 0.51 {
		t.Fatalf("dot morph fill alpha = %v, want 0.5", style.FillA)
	}
}
