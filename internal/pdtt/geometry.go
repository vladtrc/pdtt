package pdtt

import (
	"fmt"
	"math"
)

// GeomE is a geometry constructor expression: circle{radius: 2} or the
// multiline `points: circle: radius: 2` form parsed into the same shape.
type GeomE struct {
	Name   string
	Fields []FieldDef
}

// GeomVal is a evaluated geometry constructor awaiting point expansion.
type GeomVal struct {
	Name   string
	Fields map[string]Value
}

func geomFloat(g GeomVal, name string, def float64) float64 {
	if v, ok := g.Fields[name]; ok && v != nil {
		if f, err := asFloat(v); err == nil {
			return f
		}
	}
	return def
}

// resolvePoints expands a point list or geometry constructor to world-local
// vertices (before entity transform).
func resolvePoints(v Value) ([]Vec, error) {
	if pts, err := asPoints(v); err == nil {
		return pts, nil
	}
	g, ok := v.(GeomVal)
	if !ok {
		return nil, fmt.Errorf("not a point list or geometry constructor: %T", v)
	}
	return buildGeomPoints(g)
}

func buildGeomPoints(g GeomVal) ([]Vec, error) {
	switch g.Name {
	case "circle":
		r := geomFloat(g, "radius", 1)
		if r <= 0 {
			return nil, fmt.Errorf("circle.radius must be > 0")
		}
		return circlePoints(r, 64), nil
	case "ellipse":
		rx := geomFloat(g, "rx", 1)
		ry := geomFloat(g, "ry", 1)
		if rx <= 0 || ry <= 0 {
			return nil, fmt.Errorf("ellipse.rx and ellipse.ry must be > 0")
		}
		return ellipsePoints(rx, ry, 64), nil
	case "arc":
		r := geomFloat(g, "radius", 1)
		start := geomFloat(g, "start", 0)
		end := geomFloat(g, "end", math.Pi/2)
		if r <= 0 {
			return nil, fmt.Errorf("arc.radius must be > 0")
		}
		return arcPoints(r, start, end, 32), nil
	case "sector":
		r := geomFloat(g, "radius", 1)
		start := geomFloat(g, "start", 0)
		end := geomFloat(g, "end", math.Pi/3)
		if r <= 0 {
			return nil, fmt.Errorf("sector.radius must be > 0")
		}
		return sectorPoints(r, start, end, 32), nil
	case "rect":
		w := geomFloat(g, "w", 2)
		h := geomFloat(g, "h", 1)
		cr := geomFloat(g, "corner_radius", 0)
		if w <= 0 || h <= 0 {
			return nil, fmt.Errorf("rect.w and rect.h must be > 0")
		}
		if cr < 0 {
			return nil, fmt.Errorf("rect.corner_radius must be >= 0")
		}
		return rectPoints(w, h, cr), nil
	case "regular_polygon":
		sides := int(geomFloat(g, "sides", 6))
		r := geomFloat(g, "radius", 1)
		angle := geomFloat(g, "angle", 0)
		if sides < 3 {
			return nil, fmt.Errorf("regular_polygon.sides must be >= 3")
		}
		if r <= 0 {
			return nil, fmt.Errorf("regular_polygon.radius must be > 0")
		}
		return regularPolygonPoints(sides, r, angle), nil
	default:
		return nil, fmt.Errorf("unknown geometry constructor %q", g.Name)
	}
}

func circlePoints(r float64, n int) []Vec {
	return ellipsePoints(r, r, n)
}

func ellipsePoints(rx, ry float64, n int) []Vec {
	if n < 3 {
		n = 3
	}
	pts := make([]Vec, n)
	for i := 0; i < n; i++ {
		t := 2 * math.Pi * float64(i) / float64(n)
		pts[i] = Vec{rx * math.Cos(t), ry * math.Sin(t), 0}
	}
	return pts
}

func arcPoints(r, start, end float64, n int) []Vec {
	if n < 2 {
		n = 2
	}
	if end < start {
		end += 2 * math.Pi
	}
	span := end - start
	if span <= 1e-9 {
		return []Vec{{r * math.Cos(start), r * math.Sin(start), 0}}
	}
	pts := make([]Vec, n)
	for i := 0; i < n; i++ {
		t := start + span*float64(i)/float64(n-1)
		pts[i] = Vec{r * math.Cos(t), r * math.Sin(t), 0}
	}
	return pts
}

func sectorPoints(r, start, end float64, n int) []Vec {
	arc := arcPoints(r, start, end, n)
	out := make([]Vec, 0, len(arc)+2)
	out = append(out, Vec{})
	out = append(out, arc...)
	out = append(out, Vec{})
	return out
}

func rectPoints(w, h, cornerRadius float64) []Vec {
	hw, hh := w/2, h/2
	maxCR := math.Min(hw, hh)
	if cornerRadius > maxCR {
		cornerRadius = maxCR
	}
	if cornerRadius <= 1e-9 {
		return []Vec{
			{-hw, -hh, 0},
			{hw, -hh, 0},
			{hw, hh, 0},
			{-hw, hh, 0},
		}
	}
	const cornerSeg = 6
	var pts []Vec
	addArc := func(cx, cy, a0, a1 float64) {
		for i := 0; i <= cornerSeg; i++ {
			t := a0 + (a1-a0)*float64(i)/float64(cornerSeg)
			pts = append(pts, Vec{
				cx + cornerRadius*math.Cos(t),
				cy + cornerRadius*math.Sin(t),
				0,
			})
		}
	}
	// bottom edge start -> bottom-right corner -> right -> top-right -> top -> top-left -> left -> bottom-left
	pts = append(pts, Vec{-hw + cornerRadius, -hh, 0})
	addArc(hw-cornerRadius, -hh, -math.Pi/2, 0)
	pts = append(pts, Vec{hw, hh - cornerRadius, 0})
	addArc(hw-cornerRadius, hh-cornerRadius, 0, math.Pi/2)
	pts = append(pts, Vec{-hw + cornerRadius, hh, 0})
	addArc(-hw+cornerRadius, hh-cornerRadius, math.Pi/2, math.Pi)
	pts = append(pts, Vec{-hw, -hh + cornerRadius, 0})
	addArc(-hw+cornerRadius, -hh+cornerRadius, math.Pi, 3*math.Pi/2)
	return pts
}

func regularPolygonPoints(sides int, r, angle float64) []Vec {
	pts := make([]Vec, sides)
	step := 2 * math.Pi / float64(sides)
	for i := 0; i < sides; i++ {
		t := angle + step*float64(i)
		pts[i] = Vec{r * math.Cos(t), r * math.Sin(t), 0}
	}
	return pts
}
