package pdtt

import (
	"fmt"
	"math"
)

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

// pathWorldPoints returns an entity path's control points in world space.
func pathWorldPoints(e *Entity) []Vec {
	return pathPoints(e, e.transform())
}

// pathIsClosed reports whether a path entity is marked closed in the scene.
func pathIsClosed(e *Entity) bool {
	return e.fnum("closed") != 0
}

// Static outline sampling and morph loop extraction share pathWorldPoints but
// diverge on open paths: frozen frames draw the visible polyline (resampleOpen
// in outlinePoints, drawPath without closing), while morphing folds open paths
// into zero-area loops via thereAndBack so interpolation never toggles a
// closed flag mid-animation. That split is intentional — do not route static
// draw through thereAndBack.

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
		pts := pathWorldPoints(e)
		if len(pts) < 2 {
			return nil
		}
		if pathIsClosed(e) {
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

// morphAnim is the captured plan + run-time state for a single morph pair.
// Lifting it out of the Start/Update closures gives morph an inspectable state
// value and turns its per-frame logic into a pure step method — the typed-state
// (01) and plan/run-split (04) seam, proven on the hardest verb first.
type morphAnim struct {
	// Plan: fixed when the morph is expanded.
	rhs      Expr
	srcOpRef Ref
	srcPos   func() Vec
	srcMove  *Entity // nil when the source is a group part

	// Run-time: resolved and captured in start.
	dst      *Entity
	dstOpRef Ref
	outline  bool
	srcCtrs  [][]Vec
	dstCtrs  [][]Vec
	srcStyle shapeMorphStyle
	dstStyle shapeMorphStyle

	srcOp        float64 // source opacity at u=0
	dstOp        float64 // destination opacity at u=0
	offset       Vec     // srcPos - dst.at: the crossfade glide
	srcOffset    Vec     // source's own offset at u=0
	hasSrcOffset bool
}

// start resolves the destination and captures the values the step needs.
func (m *morphAnim) start(a *Anim, rt *Runtime) error {
	s := &Scope{rt: rt, binds: a.Binds}
	dv, err := s.Eval(m.rhs)
	if err != nil {
		return fmt.Errorf("line %d: morph target: %v", a.Line, err)
	}
	switch x := dv.(type) {
	case *Entity:
		m.dst = x
	case *Group:
		if len(x.Items) == 1 {
			m.dst = x.Items[0]
		}
	}
	if m.dst == nil {
		return fmt.Errorf("line %d: morph target is %T", a.Line, dv)
	}
	m.dstOpRef = FieldRef{E: m.dst, F: m.dst.field("opacity")}
	if m.srcMove != nil {
		m.srcMove.Active = true
	}
	dstWasActive := m.dst.Active
	m.dst.Active = true
	// The morph now owns the source's visibility: cancel any held post-tween
	// that was pinning the source opacity, so the morph's fade-out at u>=1 sticks.
	rt.clearPost(m.srcOpRef.Key())
	m.outline = m.srcMove != nil && canOutline(m.srcMove.Type) && canOutline(m.dst.Type)
	if m.outline {
		sc := morphLoops(m.srcMove)
		dc := morphLoops(m.dst)
		if len(sc) == 0 || len(dc) == 0 {
			m.outline = false
		} else {
			m.srcCtrs, m.dstCtrs = matchLoops(sc, dc, morphSamples)
		}
	}
	if m.outline {
		m.srcStyle = shapeStyleForMorph(m.srcMove)
		m.dstStyle = shapeStyleForMorph(m.dst)
		m.dstOpRef.Set(0.0)
	}
	dstStartOpacity, _ := asFloat(m.dstOpRef.Get())
	if !dstWasActive {
		dstStartOpacity = 0.0
		m.dstOpRef.Set(0.0)
	}
	m.srcOp, _ = asFloat(m.srcOpRef.Get())
	m.dstOp = dstStartOpacity
	if !m.outline {
		m.offset = m.srcPos().Sub(m.dst.fvec("at"))
		if m.srcMove != nil {
			m.srcOffset = m.srcMove.Offset
			m.hasSrcOffset = true
		}
	}
	return nil
}

// step applies the morph at interpolation parameter u. Every run-time input was
// captured in start(), so a and rt are unused: the body only reads m and writes
// the refs/entities it already resolved.
func (m *morphAnim) step(_ *Anim, _ *Runtime, u float64) {
	s0 := m.srcOp
	if m.outline && m.srcMove != nil && len(m.srcCtrs) == len(m.dstCtrs) && len(m.srcCtrs) > 0 {
		m.srcMove.MorphContours = lerpLoops(m.srcCtrs, m.dstCtrs, u)
		morphStyleAt(m.srcStyle, m.dstStyle, u).apply(m.srcMove)
		m.srcOpRef.Set(s0)
		m.dstOpRef.Set(0.0)
		if u >= 1 {
			m.srcMove.MorphContours = nil
			m.srcMove.MorphHasStroke = false
			m.srcMove.MorphStrokeW = 0
			m.srcMove.MorphHasFill = false
			m.srcOpRef.Set(0.0)
			m.srcMove.Active = false
			m.dstOpRef.Set(1.0)
			m.dst.Active = true
		}
		return
	}
	off := m.offset
	m.srcOpRef.Set(lerp(s0, 0, u))
	m.dstOpRef.Set(lerp(m.dstOp, 1, u))
	m.dst.Offset = off.Mul(1 - u)
	if m.srcMove != nil && m.hasSrcOffset {
		m.srcMove.Offset = m.srcOffset.Sub(off.Mul(u))
	}
	if u >= 1 {
		if m.srcMove != nil {
			m.srcMove.Active = false
		}
		m.dst.Active = true
	}
}

func (rt *Runtime) expandMorph(row Row, w winState, mk func(from, to float64, eb map[string]Value) *Anim) error {
	binds := mk(0, 0, nil).Binds
	srcEnts, srcParts, err := rt.verbSubjects(row.Op.LHS, binds)
	if err != nil {
		return fmt.Errorf("line %d: morph: %v", row.Line, err)
	}
	n := len(srcEnts) + len(srcParts)
	rhs := row.Op.RHS

	addPair := func(k int, srcE *Entity, srcP *PartState) {
		it := ItVal{I: k, N: n}
		if srcP != nil {
			it.Val = srcP
		} else {
			it.Val = srcE
			if iv, ok := srcE.It.(ItVal); ok {
				it = ItVal{Val: iv.Val, I: k, N: n, Cols: iv.Cols, LocalID: iv.LocalID}
			}
		}
		from := w.from + float64(k)*w.stagger
		a := mk(from, w.to, map[string]Value{"it": it})

		m := &morphAnim{rhs: rhs}
		if srcP != nil {
			m.srcOpRef = PartOpacityRef{P: srcP}
			m.srcPos = func() Vec { at, _, _ := partBox(srcP); return at }
		} else {
			e := srcE
			m.srcMove = e
			m.srcOpRef = FieldRef{E: e, F: e.field("opacity")}
			m.srcPos = func() Vec { return e.fvec("at").Add(e.Offset) }
		}

		a.Targets = []Ref{m.srcOpRef}
		a.drive(m)
		rt.Anims = append(rt.Anims, a)
	}

	for k, e := range srcEnts {
		addPair(k, e, nil)
	}
	for k, p := range srcParts {
		addPair(k+len(srcEnts), nil, p)
	}
	return nil
}

func isTextType(typ string) bool {
	switch typ {
	case "tex", "typst", "text", "decimal":
		return true
	}
	return false
}

func canOutline(typ string) bool {
	switch typ {
	case "path", "dot", "tex", "typst", "text", "decimal":
		return true
	}
	return false
}

// shapeStrokeWorldW is the stroke width (world units) the static renderer uses
// for shape outlines (path/dot): see the 0.045*ppu line widths in
// render.go. Text has no stroke, so its morph stroke width is 0 — mirroring
// manim, where Text/MathTex have stroke_width=0 and Circle has stroke_width≈4.
const shapeStrokeWorldW = 0.045

type shapeMorphStyle struct {
	EffectiveColor Color
	FillColor      Color
	StrokeA        float64
	StrokeW        float64 // stroke width in world units (manim: stroke_width)
	FillA          float64
}

func shapeStyleForMorph(e *Entity) shapeMorphStyle {
	if isTextType(e.Type) {
		col := entityColor(e)
		return shapeMorphStyle{
			EffectiveColor: col,
			FillColor:      col,
			StrokeA:        0,
			StrokeW:        0,
			FillA:          col.A,
		}
	}

	var strokeCol Color
	var strokeA float64
	switch e.Type {
	case "path", "dot":
		strokeCol = fieldColor(e, "stroke.color", fieldColor(e, "stroke", namedColors["white"]))
		strokeA = strokeCol.A
	}

	var fillCol Color
	var fillA float64
	switch e.Type {
	case "dot":
		fillCol = entityColor(e)
		if f, ok := e.Fields["fill"]; ok && f.Val != nil {
			if c, err := asColor(f.Val); err == nil {
				fillCol = c
			}
		}
		fillA = fillCol.A
	case "path":
		if c, ok := pathFillColor(e); ok {
			fillCol = c
			fillA = c.A
		}
	}

	// EffectiveColor is the stroke RGB used when blending the morph outline.
	// The fill is interpolated separately (FillColor/FillA), so the stroke
	// must track the stroke color and never fall back to the fill — otherwise
	// a stroked, filled target (e.g. WHITE stroke + PINK fill dot) drags the
	// outline toward the fill mid-morph and then snaps back at u>=1.
	eff := strokeCol
	strokeW := shapeStrokeWorldW
	if strokeA <= 0 {
		// No stroke is drawn; RGB is irrelevant but keep a sane value.
		eff = entityColor(e)
		strokeW = 0
	}
	return shapeMorphStyle{
		EffectiveColor: eff,
		FillColor:      fillCol,
		StrokeA:        strokeA,
		StrokeW:        strokeW,
		FillA:          fillA,
	}
}
