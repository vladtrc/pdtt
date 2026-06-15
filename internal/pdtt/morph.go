package pdtt

import "math"

// Morphing rests on one idea: a shape is a list of CLOSED loops of points, and
// nothing else. Glyphs, their counters (holes), circles, polygons are already
// closed; an open path is traced there-and-back into a zero-area loop. Because
// every loop is closed, the interpolation never has to know whether a shape was
// "open" — there is no per-contour closed flag to toggle mid-animation (the old
// toggle snapped a chord across open shapes), and a single algorithm handles
// text, shapes, open and closed uniformly.
//
// The pipeline is: loops := morphLoops(entity); s, d := matchLoops(loops_src,
// loops_dst); then lerpLoops(s, d, u) each frame. drawMorphPath always closes.

// morphSamples is the per-loop resampling resolution used for correspondence.
const morphSamples = 192

// morphLoops extracts an entity's outline as closed loops in world space.
func morphLoops(e *Entity) [][]Vec {
	if isTextType(e.Type) {
		var out [][]Vec
		for _, ct := range textOutlineContours(e) {
			if len(ct) >= 2 {
				out = append(out, ct)
			}
		}
		return out
	}
	if e.Type == "path" {
		pts := pathPoints(e, e.transform())
		if len(pts) < 2 {
			return nil
		}
		if e.fnum("closed") != 0 {
			return [][]Vec{pts}
		}
		return [][]Vec{thereAndBack(pts)}
	}
	// dot and other analytic shapes already sample as a closed ring.
	if pts := outlinePoints(e, morphSamples); len(pts) >= 2 {
		return [][]Vec{pts}
	}
	return nil
}

// thereAndBack folds an open polyline into a zero-area closed loop by walking it
// forward then back to the start. Drawn closed, the seam is a ~zero-length edge
// (first and last points coincide), so an open shape morphs to/from a closed one
// without a chord ever appearing.
func thereAndBack(pts []Vec) []Vec {
	if len(pts) < 2 {
		return pts
	}
	out := make([]Vec, 0, len(pts)*2-2)
	out = append(out, pts...)
	for i := len(pts) - 2; i >= 1; i-- {
		out = append(out, pts[i])
	}
	return out
}

// matchLoops pairs each source loop to a destination loop and returns two
// equal-length lists of equal-length, vertex-aligned loops ready to lerp.
// Pairing is greedy by nearest centroid; an unmatched loop is paired with a
// collapsed point at its own centroid, so it shrinks or grows in place.
func matchLoops(src, dst [][]Vec, n int) (sOut, dOut [][]Vec) {
	S := make([][]Vec, len(src))
	for i, c := range src {
		S[i] = resampleClosed(c, n)
	}
	D := make([][]Vec, len(dst))
	for i, c := range dst {
		D[i] = resampleClosed(c, n)
	}
	usedD := make([]bool, len(D))
	for i := range S {
		ci := contourCentroid(S[i])
		best, bestDist := -1, math.MaxFloat64
		for j := range D {
			if usedD[j] {
				continue
			}
			cj := contourCentroid(D[j])
			dx, dy := ci[0]-cj[0], ci[1]-cj[1]
			if dd := dx*dx + dy*dy; dd < bestDist {
				bestDist, best = dd, j
			}
		}
		sOut = append(sOut, S[i])
		if best >= 0 {
			usedD[best] = true
			dOut = append(dOut, alignLoop(S[i], D[best]))
		} else {
			dOut = append(dOut, pointContour(contourCentroid(S[i]), n))
		}
	}
	for j := range D {
		if usedD[j] {
			continue
		}
		sOut = append(sOut, pointContour(contourCentroid(D[j]), n))
		dOut = append(dOut, D[j])
	}
	return sOut, dOut
}

// alignLoop reindexes d (same length as s) so it lerps into s with the least
// travel: it matches winding, then picks the cyclic start offset that minimises
// total squared displacement. Minimising over every rotation — not just the
// offset that aligns s[0] — is what removes the rotational "swirl" the old
// single-vertex heuristic left behind. O(n^2) with an early-out, run once per
// pair at animation start, not per frame.
func alignLoop(s, d []Vec) []Vec {
	if len(s) != len(d) || len(d) == 0 {
		return d
	}
	if contourSignedArea(s)*contourSignedArea(d) < 0 {
		d = reverseContour(d)
	}
	best, bestCost := 0, math.MaxFloat64
	for k := range d {
		var sum float64
		for i := range s {
			p := d[(i+k)%len(d)]
			dx, dy := s[i][0]-p[0], s[i][1]-p[1]
			sum += dx*dx + dy*dy
			if sum >= bestCost {
				break
			}
		}
		if sum < bestCost {
			bestCost, best = sum, k
		}
	}
	if best == 0 {
		return d
	}
	out := make([]Vec, len(d))
	for i := range d {
		out[i] = d[(i+best)%len(d)]
	}
	return out
}

// lerpLoops linearly interpolates matched loops at progress u.
func lerpLoops(s, d [][]Vec, u float64) [][]Vec {
	out := make([][]Vec, len(s))
	for ci := range s {
		a, b := s[ci], d[ci]
		pc := make([]Vec, len(a))
		for i := range pc {
			pc[i] = Vec{
				lerp(a[i][0], b[i][0], u),
				lerp(a[i][1], b[i][1], u),
				lerp(a[i][2], b[i][2], u),
			}
		}
		out[ci] = pc
	}
	return out
}

// morphRenderStyle is the interpolated paint a morph hands the renderer.
type morphRenderStyle struct {
	Stroke    Color
	StrokeW   float64
	HasStroke bool
	Fill      Color
	HasFill   bool
}

// morphStyleAt blends two shape styles at progress u. Stroke RGB and width lerp
// directly (manim's interpolate_color / stroke_width); fill lerps in
// premultiplied alpha so a fade from a transparent side does not pull the colour
// toward black.
func morphStyleAt(s, d shapeMorphStyle, u float64) morphRenderStyle {
	stroke := Color{
		R: lerp(s.EffectiveColor.R, d.EffectiveColor.R, u),
		G: lerp(s.EffectiveColor.G, d.EffectiveColor.G, u),
		B: lerp(s.EffectiveColor.B, d.EffectiveColor.B, u),
		A: lerp(s.StrokeA, d.StrokeA, u),
	}
	strokeW := lerp(s.StrokeW, d.StrokeW, u)

	fillA := lerp(s.FillA, d.FillA, u)
	fill := d.FillColor
	if fillA > 1e-6 {
		fill = Color{
			R: lerp(s.FillColor.R*s.FillA, d.FillColor.R*d.FillA, u) / fillA,
			G: lerp(s.FillColor.G*s.FillA, d.FillColor.G*d.FillA, u) / fillA,
			B: lerp(s.FillColor.B*s.FillA, d.FillColor.B*d.FillA, u) / fillA,
			A: fillA,
		}
	}
	return morphRenderStyle{
		Stroke:    stroke,
		StrokeW:   strokeW,
		HasStroke: strokeW > 1e-6 && stroke.A > 1e-6,
		Fill:      fill,
		HasFill:   fillA > 1e-6,
	}
}

// apply writes an interpolated style onto an entity's morph render fields.
func (ms morphRenderStyle) apply(e *Entity) {
	e.MorphHasStroke = ms.HasStroke
	e.MorphStroke = ms.Stroke
	e.MorphStrokeW = ms.StrokeW
	e.MorphHasFill = ms.HasFill
	e.MorphFill = ms.Fill
}
