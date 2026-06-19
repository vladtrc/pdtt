package pdtt

import (
	"math"
	"testing"
)

// TestAxesSizeDefault: with no `size` field the footprint is the documented 10×6.
func TestAxesSizeDefault(t *testing.T) {
	rt := compileScene(t, `scene axes_default

axes ax:
  at: [0, 0]
  x_range: [0, 10, 1]
  y_range: [0, 6, 1]
`)
	ax := oneEntity(t, rt, "ax")
	w, h := axesSize(ax)
	if w != 10 || h != 6 {
		t.Fatalf("default axesSize = %v×%v, want 10×6", w, h)
	}
}

// TestAxesSizeDecoupledFromRange: the world footprint is `size`, independent of
// the data range. Doubling `size` doubles a point's world offset for the same
// data coordinate; changing the range (not size) leaves the footprint alone.
func TestAxesSizeDecoupledFromRange(t *testing.T) {
	rt := compileScene(t, `scene axes_size

axes small:
  at: [0, 0]
  size: [10, 6]
  x_range: [0, 10, 1]
  y_range: [0, 10, 1]

axes big:
  at: [0, 0]
  size: [20, 12]
  x_range: [0, 10, 1]
  y_range: [0, 10, 1]
`)
	small := oneEntity(t, rt, "small")
	big := oneEntity(t, rt, "big")

	// data point (10,10) maps to the upper-right corner: half the footprint.
	ps := axesPoint(small, 10, 10)
	pb := axesPoint(big, 10, 10)
	if math.Abs(ps[0]-5) > 1e-9 || math.Abs(ps[1]-3) > 1e-9 {
		t.Fatalf("small corner = %v, want [5 3]", ps)
	}
	if math.Abs(pb[0]-10) > 1e-9 || math.Abs(pb[1]-6) > 1e-9 {
		t.Fatalf("big corner = %v, want [10 6] (size doubles world offset)", pb)
	}
}

// TestMathBuiltins covers pow/log/min/max added for the scale-race example.
func TestMathBuiltins(t *testing.T) {
	cases := []struct {
		expr string
		want float64
	}{
		{"pow(1.1, 2)", 1.21},
		{"pow(2, 10)", 1024},
		{"log(exp(1))", 1},
		{"min(3, 1, 2)", 1},
		{"max(3, 1, 2)", 3},
		{"max([4, 9, 2])", 9},
		{"min([4, 9, 2])", 2},
	}
	for _, c := range cases {
		rt := compileScene(t, "scene m\n\nv: "+c.expr+"\n")
		g, ok := rt.Globals["v"]
		if !ok {
			t.Fatalf("%s: global v missing", c.expr)
		}
		got, err := asFloat(g.Val)
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if math.Abs(got-c.want) > 1e-9 {
			t.Fatalf("%s = %v, want %v", c.expr, got, c.want)
		}
	}
}
