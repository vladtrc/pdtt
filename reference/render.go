package main

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
		op := e.fnum("opacity")
		if op <= 0.001 {
			continue
		}
		if e.MorphPath != nil {
			r.drawMorphPath(dc, c, e, op)
			continue
		}
		switch e.Type {
		case "rect", "square":
			r.drawRect(dc, c, e, op)
		case "dot":
			r.drawDot(dc, c, e, op)
		case "tex", "text", "decimal":
			r.drawText(dc, c, e, op)
		case "plane":
			r.drawPlane(dc, c, e, op)
		case "axes":
			r.drawAxes(dc, c, e, op)
		case "plot":
			r.drawPlot(dc, c, e, op)
		case "arrow":
			r.drawArrow(dc, c, e, op)
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
	at := e.fvec("at").Add(e.Offset)
	angle := e.fnum("angle")
	pts := make([]Vec, n)
	switch e.Type {
	case "dot":
		r := e.fnum("radius")
		if r == 0 {
			r = 0.08
		}
		scale := e.fnum("scale")
		if scale == 0 {
			scale = 1
		}
		r *= scale
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

func (r *Renderer) drawMorphPath(dc *gg.Context, c cam, e *Entity, op float64) {
	if len(e.MorphPath) == 0 {
		return
	}
	trace := func() {
		for i, p := range e.MorphPath {
			x, y := c.sx(p)
			if i == 0 {
				dc.MoveTo(x, y)
			} else {
				dc.LineTo(x, y)
			}
		}
		dc.ClosePath()
	}
	if e.MorphHasFill {
		setColor(dc, e.MorphFill, op)
		trace()
		dc.Fill()
	} else if f, ok := e.Fields["fill"]; ok && f.Val != nil {
		if col, err := asColor(f.Val); err == nil {
			setColor(dc, col, op)
			trace()
			dc.Fill()
		}
	}
	stroke := fieldColor(e, "stroke", entityColor(e))
	if e.MorphHasStroke {
		stroke = e.MorphStroke
	}
	if stroke.A > 0.001 {
		setColor(dc, stroke, op)
		dc.SetLineWidth(math.Max(1.5, 0.045*c.ppu))
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

func (r *Renderer) drawRect(dc *gg.Context, c cam, e *Entity, op float64) {
	at := e.fvec("at").Add(e.Offset)
	w, h := entitySize(e)
	angle := e.fnum("angle")
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
	stroke := fieldColor(e, "stroke", namedColors["WHITE"])
	if _, hasStroke := e.Fields["stroke"]; hasStroke || e.Type == "square" {
		setColor(dc, stroke, op)
		dc.SetLineWidth(math.Max(1.5, 0.045*c.ppu))
		drawRectStrokeProgress(dc, wp, hp, draw)
		dc.Stroke()
	}
	dc.Pop()
}

func (r *Renderer) drawDot(dc *gg.Context, c cam, e *Entity, op float64) {
	at := e.fvec("at").Add(e.Offset)
	rad := e.fnum("radius")
	if rad == 0 {
		rad = 0.08
	}
	scale := e.fnum("scale")
	if scale == 0 {
		scale = 1
	}
	x, y := c.sx(at)
	rpx := rad * scale * c.ppu
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

func (r *Renderer) drawText(dc *gg.Context, c cam, e *Entity, op float64) {
	lay := textLayoutOf(e)
	if lay == nil {
		return
	}
	at := e.fvec("at").Add(e.Offset)
	emPx := lay.Em * c.ppu
	if emPx < 1 {
		return
	}
	dc.SetFontFace(faceAt(emPx * refPx / 48.0)) // em world ≈ 48pt at ref scale
	baseCol := entityColor(e)

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
			text := sg.Text
			if used+sg.Runes > budget {
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
			x, y := c.sx(Vec{wx, wy, 0})
			setColor(dc, col, segOp)
			dc.DrawString(text, x, y+0.35*emPx)
		}
	}
}

// warped sample of a grid point
func gridPoint(rt *Runtime, e *Entity, p Vec) Vec {
	if e.WarpNew == nil || e.WarpBlend <= 0 {
		return p
	}
	v, err := evalWith(rt, e.WarpNew, "p", p)
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

func (r *Renderer) drawPlane(dc *gg.Context, c cam, e *Entity, op float64) {
	rt := e.rt
	draw := e.fnum("draw")
	if draw <= 0 {
		return
	}
	xr := rangeOf(e, "x_range", -7, 7)
	yr := rangeOf(e, "y_range", -4, 4)
	at := e.fvec("at").Add(e.Offset)
	lineCol := hexColor("#29ABCA")
	axisCol := namedColors["WHITE"]

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
			pts = append(pts, gridPoint(rt, e, p).Add(at))
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

func (r *Renderer) drawAxes(dc *gg.Context, c cam, e *Entity, op float64) {
	xr := rangeOf(e, "x_range", -7, 7)
	yr := rangeOf(e, "y_range", -4, 4)
	col := namedColors["WHITE"]
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

func (r *Renderer) drawPlot(dc *gg.Context, c cam, e *Entity, op float64) {
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
		v, err := evalWith(rt, fnField.Def, "x", x)
		if err != nil {
			return
		}
		y, err := asFloat(v)
		if err != nil {
			return
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

func (r *Renderer) drawArrow(dc *gg.Context, c cam, e *Entity, op float64) {
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
