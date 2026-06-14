package pdtt

// Software renderer: world units → pixels through the `frame` record (the
// camera is an ordinary record, spec §6). Entities draw in declaration order.

import (
	"math"

	"github.com/fogleman/gg"
)

type Renderer struct {
	W, H int
	dc   *gg.Context
}

func NewRenderer(w, h int) *Renderer {
	return &Renderer{W: w, H: h, dc: gg.NewContext(w, h)}
}

type cam struct {
	at   Vec
	ppu  float64
	w, h int
}

func (c cam) sx(p Vec) (float64, float64) {
	return (p[0]-c.at[0])*c.ppu + float64(c.w)/2,
		float64(c.h)/2 - (p[1]-c.at[1])*c.ppu
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func rotateAround(p, center Vec, angle float64) Vec {
	if math.Abs(angle) <= 1e-9 {
		return p
	}
	dx := p[0] - center[0]
	dy := p[1] - center[1]
	c := math.Cos(angle)
	s := math.Sin(angle)
	return Vec{
		center[0] + dx*c - dy*s,
		center[1] + dx*s + dy*c,
		p[2],
	}
}

func (r *Renderer) Frame(rt *Runtime) *gg.Context {
	dc := r.dc
	dc.SetRGB(0.055, 0.066, 0.08)
	dc.Clear()

	fw := rt.Frame.fnum("w")
	if fw < 0.01 {
		fw = frameW0
	}
	c := cam{at: rt.Frame.fvec("at"), ppu: float64(r.W) / fw, w: r.W, h: r.H}

	for _, e := range rt.Entities {
		if e.Type == "data" || e.Type == "frame" {
			continue
		}
		if !e.Active {
			continue
		}
		tf := e.transform()
		if tf.Opacity <= 0.001 {
			continue
		}
		if e.MorphContours != nil {
			r.drawMorphPath(dc, c, e, tf)
			continue
		}
		switch e.Type {
		case "rect", "square":
			r.drawRect(dc, c, e, tf)
		case "dot":
			r.drawDot(dc, c, e, tf)
		case "tex", "typst", "text", "decimal":
			r.drawText(dc, c, e, tf)
		case "plane":
			r.drawPlane(dc, c, e, tf)
		case "axes":
			r.drawAxes(dc, c, e, tf)
		case "plot":
			r.drawPlot(dc, c, e, tf)
		case "arrow":
			r.drawArrow(dc, c, e, tf)
		case "arc":
			r.drawArc(dc, c, e, tf)
		}
	}
	return dc
}

func setColor(dc *gg.Context, col Color, opacity float64) {
	dc.SetRGBA(col.R, col.G, col.B, col.A*opacity)
}

func fieldColor(e *Entity, name string, fallback Color) Color {
	if f, ok := e.Fields[name]; ok && f.Val != nil {
		if c, err := asColor(f.Val); err == nil {
			return c
		}
	}
	return fallback
}

func outlinePoints(e *Entity, n int) []Vec {
	if n <= 0 {
		return nil
	}
	if isTextType(e.Type) {
		if pts := sampleContoursByLength(textOutlineContours(e), n); len(pts) == n {
			return pts
		}
	}
	tf := e.transform()
	at, angle := tf.At, tf.Angle
	pts := make([]Vec, n)
	switch e.Type {
	case "dot":
		r := e.fnum("radius")
		if r == 0 {
			r = 0.08
		}
		r *= tf.Scale
		for i := range pts {
			theta := 2 * math.Pi * float64(i) / float64(n)
			pts[i] = Vec{at[0] + r*math.Cos(theta), at[1] + r*math.Sin(theta), at[2]}
		}
	case "rect", "square":
		w, h := entitySize(e)
		hw, hh := w/2, h/2
		for i := range pts {
			theta := 2 * math.Pi * float64(i) / float64(n)
			cos, sin := math.Cos(theta), math.Sin(theta)
			dx := hw / math.Max(math.Abs(cos), 1e-9)
			dy := hh / math.Max(math.Abs(sin), 1e-9)
			d := math.Min(dx, dy)
			p := Vec{at[0] + d*cos, at[1] + d*sin, at[2]}
			pts[i] = rotateAround(p, at, angle)
		}
	default:
		w, h := entitySize(e)
		r := math.Min(w, h) / 2
		for i := range pts {
			theta := 2 * math.Pi * float64(i) / float64(n)
			pts[i] = Vec{at[0] + r*math.Cos(theta), at[1] + r*math.Sin(theta), at[2]}
		}
	}
	return pts
}

type contourEdge struct {
	a, b Vec
	len  float64
}

func sampleContoursByLength(contours [][]Vec, n int) []Vec {
	if n <= 0 {
		return nil
	}
	var edges []contourEdge
	total := 0.0
	for _, contour := range contours {
		if len(contour) < 2 {
			continue
		}
		for i := range contour {
			a := contour[i]
			b := contour[(i+1)%len(contour)]
			l := math.Hypot(b[0]-a[0], b[1]-a[1])
			if l <= 1e-9 {
				continue
			}
			edges = append(edges, contourEdge{a: a, b: b, len: l})
			total += l
		}
	}
	if len(edges) == 0 || total <= 1e-9 {
		return nil
	}

	out := make([]Vec, n)
	edgeIdx := 0
	acc := 0.0
	for i := 0; i < n; i++ {
		target := total * float64(i) / float64(n)
		for edgeIdx < len(edges)-1 && acc+edges[edgeIdx].len < target {
			acc += edges[edgeIdx].len
			edgeIdx++
		}
		ed := edges[edgeIdx]
		u := 0.0
		if ed.len > 1e-9 {
			u = (target - acc) / ed.len
		}
		out[i] = Vec{
			lerp(ed.a[0], ed.b[0], u),
			lerp(ed.a[1], ed.b[1], u),
			lerp(ed.a[2], ed.b[2], u),
		}
	}
	return out
}

// morphContours returns an entity's closed outline contours in world space,
// keeping glyphs and their counters (holes) as SEPARATE contours. Shapes are a
// single analytic ring.
func morphContours(e *Entity) [][]Vec {
	if isTextType(e.Type) {
		var out [][]Vec
		for _, ct := range textOutlineContours(e) {
			if len(ct) >= 2 {
				out = append(out, ct)
			}
		}
		return out
	}
	if pts := outlinePoints(e, 192); len(pts) >= 2 {
		return [][]Vec{pts}
	}
	return nil
}

// resampleClosed re-parameterises a closed contour to exactly n points spaced by
// cumulative arc length, so two contours can be lerped point-for-point.
func resampleClosed(contour []Vec, n int) []Vec {
	out := make([]Vec, n)
	if n <= 0 || len(contour) == 0 {
		return out
	}
	type seg struct {
		a, b Vec
		l    float64
	}
	var segs []seg
	total := 0.0
	for i := range contour {
		a := contour[i]
		b := contour[(i+1)%len(contour)]
		l := math.Hypot(b[0]-a[0], b[1]-a[1])
		if l <= 1e-12 {
			continue
		}
		segs = append(segs, seg{a, b, l})
		total += l
	}
	if len(segs) == 0 || total <= 1e-12 {
		for i := range out {
			out[i] = contour[0]
		}
		return out
	}
	idx, acc := 0, 0.0
	for i := 0; i < n; i++ {
		target := total * float64(i) / float64(n)
		for idx < len(segs)-1 && acc+segs[idx].l < target {
			acc += segs[idx].l
			idx++
		}
		s := segs[idx]
		u := (target - acc) / s.l
		out[i] = Vec{lerp(s.a[0], s.b[0], u), lerp(s.a[1], s.b[1], u), lerp(s.a[2], s.b[2], u)}
	}
	return out
}

func contourCentroid(c []Vec) Vec {
	if len(c) == 0 {
		return Vec{}
	}
	var s Vec
	for _, p := range c {
		s = Vec{s[0] + p[0], s[1] + p[1], s[2] + p[2]}
	}
	nf := float64(len(c))
	return Vec{s[0] / nf, s[1] / nf, s[2] / nf}
}

func contourSignedArea(c []Vec) float64 {
	a := 0.0
	for i := range c {
		p, q := c[i], c[(i+1)%len(c)]
		a += p[0]*q[1] - q[0]*p[1]
	}
	return a / 2
}

func pointContour(at Vec, n int) []Vec {
	out := make([]Vec, n)
	for i := range out {
		out[i] = at
	}
	return out
}

// alignContour reorients/rotates d (already same length as s) to minimise
// twisting against s: match winding, then start at the vertex nearest s[0].
func alignContour(s, d []Vec) []Vec {
	if len(s) != len(d) || len(d) == 0 {
		return d
	}
	if contourSignedArea(s)*contourSignedArea(d) < 0 {
		r := make([]Vec, len(d))
		for i := range d {
			r[i] = d[len(d)-1-i]
		}
		d = r
	}
	best, bestDist := 0, math.MaxFloat64
	for k := range d {
		dx, dy := d[k][0]-s[0][0], d[k][1]-s[0][1]
		if dd := dx*dx + dy*dy; dd < bestDist {
			bestDist, best = dd, k
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

// buildMorphPairs returns two contour lists of equal shape: matched glyph/hole
// contours are paired by nearest centroid; a glyph with no partner is paired
// with a degenerate point at its own centroid, so unmatched source glyphs shrink
// away in place and unmatched destination glyphs grow from a point in place.
func buildMorphPairs(src, dst [][]Vec, n int) (sOut, dOut [][]Vec) {
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
			dOut = append(dOut, alignContour(S[i], D[best]))
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

func (r *Renderer) drawMorphPath(dc *gg.Context, c cam, e *Entity, tf Transform) {
	if len(e.MorphContours) == 0 {
		return
	}
	op := tf.Opacity
	// Trace every contour as its own subpath so glyph counters (the holes in
	// `x`, `r`, the digit `2`) and separate glyphs stay distinct — drawn with
	// even-odd fill exactly like static text, instead of one flattened ring.
	trace := func() {
		for _, contour := range e.MorphContours {
			for i, p := range contour {
				x, y := c.sx(p)
				if i == 0 {
					dc.MoveTo(x, y)
				} else {
					dc.LineTo(x, y)
				}
			}
			dc.ClosePath()
		}
	}
	fill := func(col Color) {
		dc.SetFillRuleEvenOdd()
		setColor(dc, col, op)
		trace()
		dc.Fill()
		dc.SetFillRuleWinding()
	}
	if e.MorphHasFill {
		fill(e.MorphFill)
	} else if f, ok := e.Fields["fill"]; ok && f.Val != nil {
		if col, err := asColor(f.Val); err == nil {
			fill(col)
		}
	}
	// Stroke is fully driven by the interpolated morph style (color + width),
	// exactly like manim lerps stroke_rgba and stroke_width. There is no
	// fallback to the entity's fill colour: a text glyph has stroke width 0 and
	// must stay fill-only, otherwise the outline thickens it (the frame-29→30
	// "bolding"). A width of 0 simply draws nothing.
	if e.MorphHasStroke && e.MorphStrokeW > 1e-6 && e.MorphStroke.A > 0.001 {
		setColor(dc, e.MorphStroke, op)
		dc.SetLineWidth(math.Max(1.0, e.MorphStrokeW*c.ppu))
		trace()
		dc.Stroke()
	}
}

func drawRectStrokeProgress(dc *gg.Context, w, h, progress float64) {
	progress = clamp01(progress)
	if progress <= 0 {
		return
	}
	x0, y0 := -w/2, -h/2
	x1, y1 := w/2, h/2
	perim := 2 * (w + h)
	remain := progress * perim
	dc.MoveTo(x0, y0)
	edges := [][4]float64{
		{x0, y0, x1, y0},
		{x1, y0, x1, y1},
		{x1, y1, x0, y1},
		{x0, y1, x0, y0},
	}
	for _, ed := range edges {
		ex0, ey0, ex1, ey1 := ed[0], ed[1], ed[2], ed[3]
		elen := math.Hypot(ex1-ex0, ey1-ey0)
		if remain >= elen {
			dc.LineTo(ex1, ey1)
			remain -= elen
			continue
		}
		if remain > 0 {
			t := remain / elen
			dc.LineTo(lerp(ex0, ex1, t), lerp(ey0, ey1, t))
		}
		break
	}
}

func (r *Renderer) drawRect(dc *gg.Context, c cam, e *Entity, tf Transform) {
	op := tf.Opacity
	at := tf.At
	w, h := entitySize(e)
	angle := tf.Angle
	draw := clamp01(e.fnum("draw"))
	if draw <= 0 {
		return
	}
	x, y := c.sx(at)
	wp, hp := w*c.ppu, h*c.ppu
	dc.Push()
	dc.Translate(x, y)
	// world-space positive rotation is CCW; screen y-axis is flipped.
	dc.Rotate(-angle)
	if f, ok := e.Fields["fill"]; ok && f.Val != nil {
		if col, err := asColor(f.Val); err == nil {
			setColor(dc, Color{col.R, col.G, col.B, 1}, op*col.A)
			dc.DrawRectangle(-wp/2, -hp/2, wp, hp)
			dc.Fill()
		}
	}
	stroke := fieldColor(e, "stroke", namedColors["white"])
	if _, hasStroke := e.Fields["stroke"]; hasStroke || e.Type == "square" {
		setColor(dc, stroke, op)
		dc.SetLineWidth(math.Max(1.5, 0.045*c.ppu))
		drawRectStrokeProgress(dc, wp, hp, draw)
		dc.Stroke()
	}
	dc.Pop()
}

func (r *Renderer) drawDot(dc *gg.Context, c cam, e *Entity, tf Transform) {
	op := tf.Opacity
	at := tf.At
	rad := e.fnum("radius")
	if rad == 0 {
		rad = 0.08
	}
	x, y := c.sx(at)
	rpx := rad * tf.Scale * c.ppu
	fill := entityColor(e)
	if f, ok := e.Fields["fill"]; ok && f.Val != nil {
		if col, err := asColor(f.Val); err == nil {
			fill = col
		}
	}
	setColor(dc, fill, op)
	dc.DrawCircle(x, y, rpx)
	dc.Fill()
	if f, ok := e.Fields["stroke"]; ok && f.Val != nil {
		if stroke, err := asColor(f.Val); err == nil && stroke.A > 0.001 {
			setColor(dc, stroke, op)
			dc.SetLineWidth(math.Max(1.5, 0.045*c.ppu))
			dc.DrawCircle(x, y, rpx)
			dc.Stroke()
		}
	}
}

func (r *Renderer) drawText(dc *gg.Context, c cam, e *Entity, tf Transform) {
	op := tf.Opacity
	lay := textLayoutOf(e)
	if lay == nil {
		return
	}
	at := tf.At
	emPx := lay.Em * c.ppu
	if emPx < 1 {
		return
	}
	baseCol := entityColor(e)
	if !typstInstalled() {
		dc.SetFontFace(faceAt(emPx * refPx / 48.0)) // em world ≈ 48pt at ref scale
	}

	budget := lay.TotalRunes
	if e.Reveal < 1 {
		budget = int(e.Reveal*float64(lay.TotalRunes) + 0.5)
	}
	used := 0
	for _, line := range lay.Lines {
		for _, sg := range line.Segs {
			if used >= budget {
				return
			}
			partial := false
			text := sg.Text
			if used+sg.Runes > budget {
				partial = true
				if len(sg.Contours) > 0 {
					return
				}
				runes := []rune(text)
				text = string(runes[:budget-used])
			}
			used += sg.Runes
			col := baseCol
			segOp := op
			if sg.Part != nil {
				if sg.Part.Color != nil {
					if pc, err := asColor(sg.Part.Color); err == nil {
						col = pc
					}
				}
				segOp *= sg.Part.Opacity
			}
			if segOp <= 0.001 {
				continue
			}
			wx := at[0] - line.W/2 + sg.X
			wy := at[1] + line.Y
			setColor(dc, col, segOp)
			if len(sg.Contours) > 0 && !partial {
				dc.SetFillRuleEvenOdd()
				for _, contour := range sg.Contours {
					if len(contour) == 0 {
						continue
					}
					for i, p := range contour {
						wp := rotateAround(Vec{wx + p[0], wy + p[1], 0}, at, tf.Angle)
						x, y := c.sx(wp)
						if i == 0 {
							dc.MoveTo(x, y)
						} else {
							dc.LineTo(x, y)
						}
					}
					dc.ClosePath()
				}
				dc.Fill()
				dc.SetFillRuleWinding()
				continue
			}
			x, y := c.sx(Vec{wx, wy, 0})
			dc.DrawString(text, x, y+0.35*emPx)
		}
	}
}

// warped sample of a grid point
func gridPoint(rt *Runtime, e *Entity, p Vec) Vec {
	if e.WarpNew == nil || e.WarpBlend <= 0 {
		return p
	}
	v, err := evalWith(rt, e.WarpNew, map[string]Value{"p": p})
	if err != nil {
		return p
	}
	q, err := asVec(v)
	if err != nil {
		return p
	}
	b := e.WarpBlend
	return Vec{lerp(p[0], q[0], b), lerp(p[1], q[1], b), 0}
}

func (r *Renderer) polyline(dc *gg.Context, c cam, pts []Vec) {
	for i, p := range pts {
		x, y := c.sx(p)
		if i == 0 {
			dc.MoveTo(x, y)
		} else {
			dc.LineTo(x, y)
		}
	}
	dc.Stroke()
}

func (r *Renderer) drawPlane(dc *gg.Context, c cam, e *Entity, tf Transform) {
	op := tf.Opacity
	rt := e.rt
	draw := e.fnum("draw")
	if draw <= 0 {
		return
	}
	xr := rangeOf(e, "x_range", -7, 7)
	yr := rangeOf(e, "y_range", -4, 4)
	at := tf.At
	lineCol := hexColor("#29ABCA")
	axisCol := namedColors["white"]

	// each grid line grows from its midpoint as draw goes 0→1
	sampleLine := func(a, b Vec) []Vec {
		const n = 32
		mid := a.Add(b).Mul(0.5)
		half := b.Sub(a).Mul(0.5 * draw)
		p0, p1 := mid.Sub(half), mid.Add(half)
		var pts []Vec
		for i := 0; i <= n; i++ {
			t := float64(i) / n
			p := Vec{lerp(p0[0], p1[0], t), lerp(p0[1], p1[1], t), 0}
			p = gridPoint(rt, e, p)
			pts = append(pts, axesLocalPoint(e, p[0], p[1]).Add(at))
		}
		return pts
	}
	step := xr[2]
	if step <= 0 {
		step = 1
	}
	dc.SetLineWidth(math.Max(1, 0.018*c.ppu))
	for x := xr[0]; x <= xr[1]+1e-9; x += step {
		if math.Abs(x) < 1e-9 {
			continue
		}
		setColor(dc, lineCol, 0.55*op)
		r.polyline(dc, c, sampleLine(Vec{x, yr[0], 0}, Vec{x, yr[1], 0}))
	}
	step = yr[2]
	if step <= 0 {
		step = 1
	}
	for y := yr[0]; y <= yr[1]+1e-9; y += step {
		if math.Abs(y) < 1e-9 {
			continue
		}
		setColor(dc, lineCol, 0.55*op)
		r.polyline(dc, c, sampleLine(Vec{xr[0], y, 0}, Vec{xr[1], y, 0}))
	}
	dc.SetLineWidth(math.Max(1.5, 0.03*c.ppu))
	setColor(dc, axisCol, 0.9*op)
	r.polyline(dc, c, sampleLine(Vec{0, yr[0], 0}, Vec{0, yr[1], 0}))
	r.polyline(dc, c, sampleLine(Vec{xr[0], 0, 0}, Vec{xr[1], 0, 0}))
}

func (r *Renderer) drawAxes(dc *gg.Context, c cam, e *Entity, tf Transform) {
	op := tf.Opacity
	xr := rangeOf(e, "x_range", -7, 7)
	yr := rangeOf(e, "y_range", -4, 4)
	col := namedColors["white"]
	dc.SetLineWidth(math.Max(1.5, 0.03*c.ppu))
	setColor(dc, col, op)

	p0 := axesPoint(e, xr[0], 0)
	p1 := axesPoint(e, xr[1], 0)
	r.polyline(dc, c, []Vec{p0, p1})
	r.arrowHead(dc, c, p1, Vec{1, 0, 0}, op, col)
	q0 := axesPoint(e, 0, yr[0])
	q1 := axesPoint(e, 0, yr[1])
	r.polyline(dc, c, []Vec{q0, q1})
	r.arrowHead(dc, c, q1, Vec{0, 1, 0}, op, col)

	tick := 0.09
	for x := xr[0]; x <= xr[1]+1e-9; x += xr[2] {
		if math.Abs(x) < 1e-9 {
			continue
		}
		p := axesPoint(e, x, 0)
		r.polyline(dc, c, []Vec{{p[0], p[1] - tick, 0}, {p[0], p[1] + tick, 0}})
	}
	for y := yr[0]; y <= yr[1]+1e-9; y += yr[2] {
		if math.Abs(y) < 1e-9 {
			continue
		}
		p := axesPoint(e, 0, y)
		r.polyline(dc, c, []Vec{{p[0] - tick, p[1], 0}, {p[0] + tick, p[1], 0}})
	}
}

func (r *Renderer) arrowHead(dc *gg.Context, c cam, tip, dir Vec, op float64, col Color) {
	n := math.Hypot(dir[0], dir[1])
	if n == 0 {
		return
	}
	d := dir.Mul(1 / n)
	perp := Vec{-d[1], d[0], 0}
	size := 0.22
	b1 := tip.Sub(d.Mul(size)).Add(perp.Mul(size * 0.45))
	b2 := tip.Sub(d.Mul(size)).Sub(perp.Mul(size * 0.45))
	x0, y0 := c.sx(tip)
	x1, y1 := c.sx(b1)
	x2, y2 := c.sx(b2)
	setColor(dc, col, op)
	dc.MoveTo(x0, y0)
	dc.LineTo(x1, y1)
	dc.LineTo(x2, y2)
	dc.ClosePath()
	dc.Fill()
}

func (r *Renderer) drawPlot(dc *gg.Context, c cam, e *Entity, tf Transform) {
	op := tf.Opacity
	rt := e.rt
	draw := e.fnum("draw")
	if draw <= 0 {
		return
	}
	axf, ok := e.Fields["axes"]
	if !ok || axf.Val == nil {
		return
	}
	ax, ok := axf.Val.(*Entity)
	if !ok {
		return
	}
	fnField, ok := e.Fields["fn"]
	if !ok || fnField.Def == nil {
		return
	}
	xr := rangeOf(ax, "x_range", -7, 7)
	yr := rangeOf(ax, "y_range", -4, 4)
	const n = 240
	limit := int(draw * n)
	var pts []Vec
	for i := 0; i <= limit; i++ {
		x := xr[0] + (xr[1]-xr[0])*float64(i)/n
		binds := map[string]Value{"x": x}
		if it, ok := e.It.(ItVal); ok && (it.N > 0 || it.Cols != nil) {
			binds["it"] = it
		}
		v, err := evalWith(rt, fnField.Def, binds)
		if err != nil {
			return
		}
		y, err := asFloat(v)
		if err != nil {
			return
		}
		if math.IsNaN(y) || math.IsInf(y, 0) {
			if len(pts) > 1 {
				setColor(dc, entityColor(e), op)
				dc.SetLineWidth(math.Max(2, 0.035*c.ppu))
				r.polyline(dc, c, pts)
			}
			pts = nil
			continue
		}
		if y < yr[0] || y > yr[1] {
			if len(pts) > 1 {
				setColor(dc, entityColor(e), op)
				dc.SetLineWidth(math.Max(2, 0.035*c.ppu))
				r.polyline(dc, c, pts)
			}
			pts = nil
			continue
		}
		pts = append(pts, axesPoint(ax, x, y))
	}
	if len(pts) > 1 {
		setColor(dc, entityColor(e), op)
		dc.SetLineWidth(math.Max(2, 0.035*c.ppu))
		r.polyline(dc, c, pts)
	}
}

func (r *Renderer) drawArrow(dc *gg.Context, c cam, e *Entity, tf Transform) {
	op := tf.Opacity
	draw := e.fnum("draw")
	if draw <= 0.001 {
		return
	}
	from := e.fvec("from").Add(e.Offset)
	to := e.fvec("to").Add(e.Offset)
	tip := from.Add(to.Sub(from).Mul(draw))
	col := entityColor(e)
	setColor(dc, col, op)
	dc.SetLineWidth(math.Max(1.5, 0.035*c.ppu))
	r.polyline(dc, c, []Vec{from, tip})
	r.arrowHead(dc, c, tip, tip.Sub(from), op, col)
}

func (r *Renderer) drawArc(dc *gg.Context, c cam, e *Entity, tf Transform) {
	op := tf.Opacity
	draw := clamp01(e.fnum("draw"))
	if draw <= 0.001 {
		return
	}
	rad := e.fnum("radius")
	if rad <= 0 {
		rad = 0.5
	}
	center := tf.At
	start := e.fnum("start_angle") + tf.Angle
	end := e.fnum("end_angle") + tf.Angle
	sweep := (end - start) * draw
	steps := int(math.Ceil(math.Abs(sweep) / (math.Pi / 48)))
	if steps < 2 {
		steps = 2
	}
	if steps > 192 {
		steps = 192
	}
	rad *= tf.Scale
	pts := make([]Vec, 0, steps+1)
	for i := 0; i <= steps; i++ {
		u := float64(i) / float64(steps)
		a := start + sweep*u
		pts = append(pts, Vec{
			center[0] + rad*math.Cos(a),
			center[1] + rad*math.Sin(a),
			center[2],
		})
	}
	col := fieldColor(e, "stroke", entityColor(e))
	setColor(dc, col, op)
	dc.SetLineWidth(math.Max(1.5, 0.035*c.ppu))
	r.polyline(dc, c, pts)
	if e.fnum("arrow") > 0.5 && len(pts) >= 2 {
		tip := pts[len(pts)-1]
		prev := pts[len(pts)-2]
		r.arrowHead(dc, c, tip, tip.Sub(prev), op, col)
	}
}
