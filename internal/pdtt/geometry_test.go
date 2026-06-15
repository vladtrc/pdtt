package pdtt

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestGeometryConstructorCompact(t *testing.T) {
	rt := compileScene(t, `scene geom_compact

path c:
  points: circle{radius: 2}
  closed: 1
`)
	c := oneEntity(t, rt, "c")
	pts, err := resolvePoints(c.Fields["points"].Val)
	if err != nil {
		t.Fatalf("resolvePoints: %v", err)
	}
	if len(pts) < 60 {
		t.Fatalf("circle points = %d, want ~64", len(pts))
	}
	r := math.Hypot(pts[0][0], pts[0][1])
	if math.Abs(r-2) > 0.01 {
		t.Fatalf("circle radius = %v, want 2", r)
	}
}

func TestGeometryConstructorMultiline(t *testing.T) {
	rt := compileScene(t, `scene geom_multiline

path p:
  points:
    regular_polygon:
      sides: 6
      radius: 1.5
      angle: math.pi / 6
  closed: 1
`)
	p := oneEntity(t, rt, "p")
	pts, err := resolvePoints(p.Fields["points"].Val)
	if err != nil {
		t.Fatalf("resolvePoints: %v", err)
	}
	if len(pts) != 6 {
		t.Fatalf("hexagon points = %d, want 6", len(pts))
	}
}

func TestAllGeometryConstructors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"circle", "points: circle{radius: 2}", 64},
		{"ellipse", "points: ellipse{rx: 2, ry: 1}", 64},
		{"arc", "points: arc{radius: 2, start: 0, end: math.pi / 2}", 32},
		{"sector", "points: sector{radius: 2, start: 0, end: math.pi / 3}", 34},
		{"rect", "points: rect{w: 3, h: 1.4, corner_radius: 0.12}", 0},
		{"regular_polygon", "points: regular_polygon{sides: 6, radius: 1.5}", 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := compileScene(t, "scene geom\n\npath p:\n  "+tc.src+"\n  closed: 1\n")
			p := oneEntity(t, rt, "p")
			pts, err := resolvePoints(p.Fields["points"].Val)
			if err != nil {
				t.Fatalf("resolvePoints: %v", err)
			}
			if tc.want > 0 && len(pts) != tc.want {
				t.Fatalf("points = %d, want %d", len(pts), tc.want)
			}
			if tc.want == 0 && len(pts) < 4 {
				t.Fatalf("points = %d, want rounded rect with many vertices", len(pts))
			}
			NewRenderer(320, 180).Frame(rt)
		})
	}
}

func TestShapeMorphShowcaseAllShapes(t *testing.T) {
	srcPath := filepath.Join("..", "..", "examples", "shape-morph", "run.pdtt")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	stmts, err := ParseFile(string(src))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	rt, err := Compile(stmts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	shapes := []string{
		"stick", "circ", "circ_f", "ell", "ell_f",
		"arc_p", "sector_p", "rect_p", "rect_f", "poly", "poly_f",
	}
	for _, name := range shapes {
		if rt.Groups[name] == nil {
			t.Fatalf("missing path record %q", name)
		}
	}

	if err := rt.Step(0); err != nil {
		t.Fatalf("Step(0): %v", err)
	}
	stick := oneEntity(t, rt, "stick")
	if !stick.Active {
		t.Fatal("stick not active after entry")
	}

	// Arrow is a stroke-end modifier on the same path, not a separate record.
	if err := rt.Step(1.2); err != nil {
		t.Fatalf("Step(1.2): %v", err)
	}
	if got := stick.fstr("stroke.end"); got != "arrow" {
		t.Fatalf("stick.stroke.end = %q, want arrow", got)
	}

	if err := rt.Step(27); err != nil {
		t.Fatalf("Step(27): %v", err)
	}
	polyF := oneEntity(t, rt, "poly_f")
	if !polyF.Active {
		t.Fatal("poly_f not active after morph chain")
	}
	if stick.Active {
		t.Fatal("stick still active after morph chain")
	}

	r := NewRenderer(320, 180)
	for _, tSec := range []float64{0, 1, 3, 5, 7, 15, 25, 30} {
		if err := rt.Step(tSec); err != nil {
			t.Fatalf("Step(%v): %v", tSec, err)
		}
		r.Frame(rt)
	}
}

func TestRegularPolygonClosedOutline(t *testing.T) {
	rt := compileScene(t, `scene hex

path poly:
  at: [0, 0]
  points: regular_polygon{sides: 6, radius: 1.25, angle: math.pi / 6}
  closed: 1
  stroke.color: color.cyan
`)
	poly := oneEntity(t, rt, "poly")
	pts, err := resolvePoints(poly.Fields["points"].Val)
	if err != nil {
		t.Fatalf("resolvePoints: %v", err)
	}
	openLen := pathPerimeter(pts, false)
	closedLen := pathPerimeter(pts, true)
	if closedLen <= openLen*1.05 {
		t.Fatalf("closed perimeter %v should exceed open %v (missing closing edge)", closedLen, openLen)
	}
	world := outlinePoints(poly, 192)
	if len(world) < 2 {
		t.Fatal("expected closed hexagon outline")
	}
}

func pathPerimeter(pts []Vec, closed bool) float64 {
	if len(pts) < 2 {
		return 0
	}
	total := 0.0
	for i := 0; i < len(pts)-1; i++ {
		total += math.Hypot(pts[i+1][0]-pts[i][0], pts[i+1][1]-pts[i][1])
	}
	if closed && len(pts) > 2 {
		last, first := pts[len(pts)-1], pts[0]
		total += math.Hypot(first[0]-last[0], first[1]-last[1])
	}
	return total
}

func TestClosedToOpenMorphNoChord(t *testing.T) {
	rt := compileScene(t, `scene closed_open

path ell_f:
  at: [0, 0]
  points: ellipse{rx: 1.15, ry: 1.55}
  closed: 1
  stroke.color: color.gold
  stroke.width: 0.035
  fill.color: color.orange @ 40%

path arc_p:
  at: [0, 0]
  points: arc{radius: 1.45, start: 0, end: math.pi / 2}
  closed: 0
  stroke.color: color.white
  stroke.width: 0.04
  opacity: 0

| 2.55s | smooth | morph | ell_f -> arc_p
`)
	ellF := oneEntity(t, rt, "ell_f")
	arc := oneEntity(t, rt, "arc_p")
	sc := morphLoops(ellF)
	dc := morphLoops(arc)
	if len(sc) != 1 || len(dc) != 1 {
		t.Fatalf("loops = %d / %d, want 1/1", len(sc), len(dc))
	}
	srcPairs, dstPairs := matchLoops(sc, dc, 64)
	if len(srcPairs) != 1 || len(dstPairs) != 1 {
		t.Fatalf("pairs = %d / %d, want 1/1", len(srcPairs), len(dstPairs))
	}
	// The open arc is folded into a closed there-and-back loop, so no single edge
	// of the destination loop is a chord: every consecutive gap stays small
	// relative to the perimeter. (The old model toggled a closed flag at u=0.5
	// and snapped a chord across the shape — that is the regression this guards.)
	d := dstPairs[0]
	var perim, maxGap float64
	for i := range d {
		j := (i + 1) % len(d)
		g := math.Hypot(d[j][0]-d[i][0], d[j][1]-d[i][1])
		perim += g
		if g > maxGap {
			maxGap = g
		}
	}
	if maxGap > perim*0.2 {
		t.Fatalf("chord in destination loop: maxGap=%v perim=%v", maxGap, perim)
	}
	if err := rt.Step(2.55); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if ellF.Active || !arc.Active {
		t.Fatalf("after morph: ell_f.Active=%v arc.Active=%v", ellF.Active, arc.Active)
	}
	if len(arc.MorphContours) != 0 {
		t.Fatal("morph contours should be cleared after tween")
	}
}

func TestOpenPathOutlineMorph(t *testing.T) {
	rt := compileScene(t, `scene open_morph

path line:
  points: [[-1, 0], [1, 0]]
  closed: 0
  stroke.color: color.white

path circ:
  at: [0, 0]
  points: circle{radius: 1}
  closed: 1
  stroke.color: color.gold
  opacity: 0

| 0.5s | morph | line -> circ
`)
	line := oneEntity(t, rt, "line")
	circ := oneEntity(t, rt, "circ")
	if pts := outlinePoints(line, 64); len(pts) != 64 {
		t.Fatalf("open path outline = %d points, want 64", len(pts))
	}
	if err := rt.Step(0.5); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if line.Active || !circ.Active {
		t.Fatalf("after morph: line.Active=%v circ.Active=%v", line.Active, circ.Active)
	}
	NewRenderer(320, 180).Frame(rt)
}
