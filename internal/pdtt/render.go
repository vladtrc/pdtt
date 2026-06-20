package pdtt

// Software renderer: world units → pixels through the `frame` record (the
// camera is an ordinary record, spec §6). Entities draw in declaration order.

import (
	"fmt"
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
		case "path":
			r.drawPath(dc, c, e, tf)
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

func fieldString(e *Entity, names ...string) string {
	for _, name := range names {
		if s := e.fstr(name); s != "" {
			return s
		}
	}
	return ""
}

func pathStrokeColor(e *Entity) Color {
	if c := fieldColor(e, "stroke.color", Color{}); c.A > 0 {
		return c
	}
	return fieldColor(e, "stroke", entityColor(e))
}

func pathFillColor(e *Entity) (Color, bool) {
	if f, ok := e.Fields["fill.color"]; ok && f.Val != nil {
		if c, err := asColor(f.Val); err == nil {
			return c, true
		}
	}
	if f, ok := e.Fields["fill"]; ok && f.Val != nil {
		if c, err := asColor(f.Val); err == nil {
			return c, true
		}
	}
	return Color{}, false
}

func pathStrokeWidth(e *Entity) float64 {
	w := e.fnum("stroke.width")
	if w <= 0 {
		w = e.fnum("width")
	}
	if w <= 0 {
		w = 0.035
	}
	return w
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
	at := tf.At
	pts := make([]Vec, n)
	switch e.Type {
	case "path":
		pathPts := pathWorldPoints(e)
		if len(pathPts) < 2 {
			return nil
		}
		if pathIsClosed(e) {
			return resampleClosed(pathPts, n)
		}
		return resampleOpen(pathPts, n)
	case "dot":
		r := e.fnum("radius")
		if r == 0 {
			r = 0.08
		}
		r *= tf.Scale
		for i := range pts {
			theta := 2 * math.Pi * float64(i) / float64(n)
			pts[i] = Vec{at[0] + r*math.Cos(theta), at[1] + r*math.Sin(theta)}
		}
	default:
		w, h := entitySize(e)
		r := math.Min(w, h) / 2
		for i := range pts {
			theta := 2 * math.Pi * float64(i) / float64(n)
			pts[i] = Vec{at[0] + r*math.Cos(theta), at[1] + r*math.Sin(theta)}
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
		}
	}
	return out
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
		out[i] = Vec{lerp(s.a[0], s.b[0], u), lerp(s.a[1], s.b[1], u)}
	}
	return out
}

// resampleOpen re-parameterises an open polyline to exactly n points by arc length.
func resampleOpen(contour []Vec, n int) []Vec {
	out := make([]Vec, n)
	if n <= 0 || len(contour) == 0 {
		return out
	}
	if len(contour) == 1 {
		for i := range out {
			out[i] = contour[0]
		}
		return out
	}
	type seg struct {
		a, b Vec
		l    float64
	}
	var segs []seg
	total := 0.0
	for i := 0; i < len(contour)-1; i++ {
		a, b := contour[i], contour[i+1]
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
		target := total * float64(i) / float64(n-1)
		for idx < len(segs)-1 && acc+segs[idx].l < target {
			acc += segs[idx].l
			idx++
		}
		s := segs[idx]
		u := 0.0
		if s.l > 1e-12 {
			u = (target - acc) / s.l
		}
		out[i] = Vec{lerp(s.a[0], s.b[0], u), lerp(s.a[1], s.b[1], u)}
	}
	return out
}

func contourCentroid(c []Vec) Vec {
	if len(c) == 0 {
		return Vec{}
	}
	var s Vec
	for _, p := range c {
		s = Vec{s[0] + p[0], s[1] + p[1]}
	}
	nf := float64(len(c))
	return Vec{s[0] / nf, s[1] / nf}
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

func reverseContour(c []Vec) []Vec {
	out := make([]Vec, len(c))
	for i := range c {
		out[i] = c[len(c)-1-i]
	}
	return out
}

func (r *Renderer) drawMorphPath(dc *gg.Context, c cam, e *Entity, tf Transform) {
	if len(e.MorphContours) == 0 {
		return
	}
	op := tf.Opacity
	// Every morph contour is a closed loop (open paths are folded there-and-back
	// in morphLoops), so each is traced as its own closed subpath. That keeps
	// glyph counters (the holes in `x`, `r`, the digit `2`) and separate glyphs
	// distinct under even-odd fill, and — because openness is geometric, not a
	// flag — there is never a chord snapping across a once-open shape.
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
	em := lay.Em
	emPx := em * c.ppu
	if emPx < 1 {
		return
	}
	baseCol := entityColor(e)
	if !typstInstalled() {
		dc.SetFontFace(faceAt(emPx * refPx / 48.0)) // em world ≈ 48pt at ref scale
	}

	// `draw` (default 1) reveals the text like manim's Write: glyphs are traced
	// on as outlines and then filled, cascading left to right (and top to bottom),
	// each letter overlapping the next. This replaces a crude horizontal wipe — at
	// no point is a glyph cut by a hard vertical edge.
	draw := clamp01(e.fnum("draw"))
	var t float64
	if e.rt != nil {
		t = e.rt.T
	}

	// Fallback (no typst → no vector glyphs): keep the simple rune-proportional
	// horizontal wipe, one line after another.
	if lay.TotalGlyphs == 0 {
		r.drawTextWiped(dc, c, lay, at, em, emPx, draw, baseCol, op, t, tf.Angle)
		return
	}

	strokeW := math.Max(1.0, 0.035*em*c.ppu)
	glyphIdx := 0
	for _, line := range lay.Lines {
		wy := at[1] + line.Y
		for _, sg := range line.Segs {
			col := baseCol
			segOp := op
			xf := identityXform
			wx := at[0] - line.W/2 + sg.X
			if sg.Part != nil {
				p := sg.Part
				if p.Color != nil {
					if pc, err := asColor(p.Color); err == nil {
						col = pc
					}
				}
				segOp *= p.Opacity
				xf = partXform(p, Vec{wx + sg.W/2, wy}, em, t)
			}
			if segOp <= 0.001 {
				glyphIdx += len(sg.Glyphs)
				continue
			}
			toScreen := func(p Vec) (float64, float64) {
				wp := xf(Vec{wx + p[0], wy + p[1]})
				wp = rotateAround(wp, at, tf.Angle)
				return c.sx(wp)
			}
			sumGp := 0.0
			for _, glyph := range sg.Glyphs {
				gp := 1.0
				if draw < 1 {
					gp = writeGlyphProgress(draw, glyphIdx, lay.TotalGlyphs)
				}
				glyphIdx++
				sumGp += gp
				if gp <= 0 {
					continue
				}
				r.drawGlyphWrite(dc, glyph, toScreen, col, segOp, gp, strokeW)
			}
			if sg.Part != nil {
				// Rules retreat with the span's reveal (mean glyph progress) so
				// strike/underline don't outlive the letters when text un-writes.
				reveal := 1.0
				if draw < 1 && len(sg.Glyphs) > 0 {
					reveal = sumGp / float64(len(sg.Glyphs))
				}
				r.drawPartRules(dc, c, sg.Part, Vec{wx, wy}, sg.W, em, col, segOp, xf, at, tf.Angle, reveal)
			}
		}
	}
}

// writeLagRatio controls how much consecutive glyphs overlap as they write on.
// 1 ⇒ fully sequential (one finishes before the next starts); smaller ⇒ more
// overlap. ~0.4 gives a lively cascade that still reads letter by letter.
const writeLagRatio = 0.4

// writeGlyphProgress maps the entity-wide draw value (0..1) to the local 0..1
// reveal progress of glyph i of n, staggered so glyphs overlap by writeLagRatio.
func writeGlyphProgress(draw float64, i, n int) float64 {
	if n <= 1 {
		return draw
	}
	span := 1.0 / (1.0 + float64(n-1)*writeLagRatio)
	start := float64(i) * writeLagRatio * span
	return clamp01((draw - start) / span)
}

// drawGlyphWrite renders one glyph at local progress gp (0..1): the outline is
// traced as a stroke over the first writeBorderPortion of progress, then the fill
// fades in while the stroke fades out — manim's DrawBorderThenFill.
const writeBorderPortion = 0.6

func (r *Renderer) drawGlyphWrite(dc *gg.Context, glyph [][]Vec, toScreen func(Vec) (float64, float64), col Color, segOp, gp, strokeW float64) {
	fillGlyph := func(alpha float64) {
		setColor(dc, col, alpha)
		dc.SetFillRuleEvenOdd()
		for _, contour := range glyph {
			if len(contour) == 0 {
				continue
			}
			for i, p := range contour {
				x, y := toScreen(p)
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
	}

	if gp >= 1 {
		fillGlyph(segOp)
		return
	}

	traceFrac := clamp01(gp / writeBorderPortion)
	fillOp := clamp01((gp - writeBorderPortion) / (1 - writeBorderPortion))
	if fillOp > 0.001 {
		fillGlyph(segOp * fillOp)
	}
	// Stroke the outline up to traceFrac of its length, fading out as the fill
	// arrives so the two phases hand off cleanly.
	strokeOp := segOp * (1 - fillOp)
	if strokeOp <= 0.001 {
		return
	}
	strokes := glyphStrokePartial(glyph, traceFrac)
	if len(strokes) == 0 {
		return
	}
	setColor(dc, col, strokeOp)
	dc.SetLineWidth(strokeW)
	for _, s := range strokes {
		for i, p := range s {
			x, y := toScreen(p)
			if i == 0 {
				dc.MoveTo(x, y)
			} else {
				dc.LineTo(x, y)
			}
		}
		dc.Stroke()
	}
}

// closedLoop returns ct with its first point repeated at the end so the closing
// segment counts toward the traced outline.
func closedLoop(ct []Vec) []Vec {
	if len(ct) < 2 || ct[0] == ct[len(ct)-1] {
		return ct
	}
	out := make([]Vec, len(ct)+1)
	copy(out, ct)
	out[len(ct)] = ct[0]
	return out
}

func contourPerimeter(ct []Vec) float64 {
	loop := closedLoop(ct)
	total := 0.0
	for i := 1; i < len(loop); i++ {
		total += math.Hypot(loop[i][0]-loop[i-1][0], loop[i][1]-loop[i-1][1])
	}
	return total
}

// glyphStrokePartial returns the polylines tracing the glyph's outline up to
// fraction frac of its total perimeter. Each contour is a separate pen stroke
// (the pen lifts between counters), drawn in order so the trace flows naturally.
func glyphStrokePartial(glyph [][]Vec, frac float64) [][]Vec {
	frac = clamp01(frac)
	if frac <= 0 {
		return nil
	}
	total := 0.0
	for _, ct := range glyph {
		total += contourPerimeter(ct)
	}
	if total <= 0 {
		return nil
	}
	target := frac * total
	var out [][]Vec
	acc := 0.0
	for _, ct := range glyph {
		if len(ct) < 2 {
			continue
		}
		loop := closedLoop(ct)
		seg := []Vec{loop[0]}
		done := false
		for i := 1; i < len(loop); i++ {
			d := math.Hypot(loop[i][0]-loop[i-1][0], loop[i][1]-loop[i-1][1])
			if d <= 0 {
				continue
			}
			if acc+d >= target {
				s := (target - acc) / d
				seg = append(seg, Vec{
					loop[i-1][0] + (loop[i][0]-loop[i-1][0])*s,
					loop[i-1][1] + (loop[i][1]-loop[i-1][1])*s,
				})
				done = true
				break
			}
			acc += d
			seg = append(seg, loop[i])
		}
		if len(seg) >= 2 {
			out = append(out, seg)
		}
		if done {
			break
		}
	}
	return out
}

// drawTextWiped is the no-typst fallback reveal: a rune-proportional horizontal
// wipe of the freetype-rendered string, one line after another.
func (r *Renderer) drawTextWiped(dc *gg.Context, c cam, lay *TextLayout, at Vec, em, emPx, draw float64, baseCol Color, op, t, angle float64) {
	totalRunes := lay.TotalRunes
	if totalRunes <= 0 {
		totalRunes = 1
	}
	revealRunes := draw * float64(totalRunes)
	canClip := math.Abs(angle) < 1e-6
	consumed := 0.0
	for _, line := range lay.Lines {
		lineRunes := 0
		for _, sg := range line.Segs {
			lineRunes += sg.Runes
		}
		frac := 1.0
		if draw < 1 {
			if lineRunes <= 0 {
				frac = 1
			} else {
				frac = clamp01((revealRunes - consumed) / float64(lineRunes))
			}
		}
		consumed += float64(lineRunes)
		if frac <= 0 {
			continue
		}
		wy := at[1] + line.Y
		clipped := false
		if frac < 1 && canClip {
			left := at[0] - line.W/2
			right := left + frac*line.W
			x0, y0 := c.sx(Vec{left, wy + em})
			x1, y1 := c.sx(Vec{right, wy - em})
			dc.DrawRectangle(math.Min(x0, x1), math.Min(y0, y1), math.Abs(x1-x0), math.Abs(y1-y0))
			dc.Clip()
			clipped = true
		}
		for _, sg := range line.Segs {
			col := baseCol
			segOp := op
			xf := identityXform
			wx := at[0] - line.W/2 + sg.X
			if sg.Part != nil {
				p := sg.Part
				if p.Color != nil {
					if pc, err := asColor(p.Color); err == nil {
						col = pc
					}
				}
				segOp *= p.Opacity
				xf = partXform(p, Vec{wx + sg.W/2, wy}, em, t)
			}
			if segOp <= 0.001 {
				continue
			}
			setColor(dc, col, segOp)
			x, y := c.sx(xf(Vec{wx, wy}))
			dc.DrawString(sg.Text, x, y+0.35*emPx)
			if sg.Part != nil {
				r.drawPartRules(dc, c, sg.Part, Vec{wx, wy}, sg.W, em, col, segOp, xf, at, angle, 1)
			}
		}
		if clipped {
			dc.ResetClip()
		}
	}
}

// xform maps a world point through a per-segment affine (scale + wiggle).
type xform func(Vec) Vec

func identityXform(p Vec) Vec { return p }

// partXform builds the scale + wiggle transform for an emphasised span, centred
// on the span. Wiggle is a plain 0..1 amplitude: the shimmer phase is wall-clock
// driven, while the rise-and-settle envelope is owned by whatever animates the
// channel (the transient `| wiggle |` modifier sweeps it 0→1→0).
func partXform(p *PartState, center Vec, em, t float64) xform {
	s := p.scaleOr1()
	rot := 0.0
	if p.Wiggle > 1e-6 {
		amp := clamp01(p.Wiggle)
		phase := t * 2 * math.Pi * 6          // 6 Hz shimmer driven by wall-clock
		rot = amp * 0.16 * math.Sin(phase)    // ±~9° rock
		s *= 1 + amp*0.12*math.Sin(phase+1.7) // slight breathing
	}
	if math.Abs(s-1) < 1e-6 && math.Abs(rot) < 1e-9 {
		return identityXform
	}
	cs, sn := math.Cos(rot), math.Sin(rot)
	return func(q Vec) Vec {
		dx := (q[0] - center[0]) * s
		dy := (q[1] - center[1]) * s
		return Vec{center[0] + dx*cs - dy*sn, center[1] + dx*sn + dy*cs}
	}
}

// drawPartRules strokes the strikethrough / underline rules across an emphasised
// span. Each grows left to right with its 0..1 progress, in the span's colour,
// and rides the same scale/wiggle transform as the glyphs. `reveal` (the span's
// Write progress, 0..1) caps how far the rule extends so it retreats together
// with the glyphs when the text un-writes, instead of lingering as a floating line.
func (r *Renderer) drawPartRules(dc *gg.Context, c cam, p *PartState, leftMid Vec, w, em float64, col Color, op float64, xf xform, at Vec, angle, reveal float64) {
	// head/tail are the rule's right/left edges as 0..1 fractions of the span;
	// a persistent fill is tail=0, head=v, while the transient sweep drives both.
	rule := func(head, tail, yOff float64) {
		head = clamp01(math.Min(head, reveal))
		tail = clamp01(tail)
		if head-tail <= 0.001 || w <= 0 {
			return
		}
		y := leftMid[1] + yOff*em
		a := xf(Vec{leftMid[0] + w*tail, y})
		b := xf(Vec{leftMid[0] + w*head, y})
		a = rotateAround(a, at, angle)
		b = rotateAround(b, at, angle)
		ax, ay := c.sx(a)
		bx, by := c.sx(b)
		setColor(dc, col, op)
		dc.SetLineWidth(math.Max(1.4, 0.05*em*c.ppu))
		dc.MoveTo(ax, ay)
		dc.LineTo(bx, by)
		dc.Stroke()
	}
	rule(p.Strike, p.StrikeTail, -0.03)       // through the x-height middle
	rule(p.Underline, p.UnderlineTail, -0.34) // just under the baseline
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
	return Vec{lerp(p[0], q[0], b), lerp(p[1], q[1], b)}
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
	r.drawAxesFrame(dc, c, e, tf)
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
			p := Vec{lerp(p0[0], p1[0], t), lerp(p0[1], p1[1], t)}
			p = gridPoint(rt, e, p)
			pts = append(pts, axesLocalPoint(e, p[0], p[1]).Add(at))
		}
		return pts
	}
	step := niceStep(xr[2])
	dc.SetLineWidth(math.Max(1, 0.018*c.ppu))
	for x := xr[0]; x <= xr[1]+1e-9; x += step {
		if math.Abs(x) < 1e-9 {
			continue
		}
		setColor(dc, lineCol, 0.55*op)
		r.polyline(dc, c, sampleLine(Vec{x, yr[0]}, Vec{x, yr[1]}))
	}
	step = niceStep(yr[2])
	for y := yr[0]; y <= yr[1]+1e-9; y += step {
		if math.Abs(y) < 1e-9 {
			continue
		}
		setColor(dc, lineCol, 0.55*op)
		r.polyline(dc, c, sampleLine(Vec{xr[0], y}, Vec{xr[1], y}))
	}
	dc.SetLineWidth(math.Max(1.5, 0.03*c.ppu))
	setColor(dc, axisCol, 0.9*op)
	r.polyline(dc, c, sampleLine(Vec{0, yr[0]}, Vec{0, yr[1]}))
	r.polyline(dc, c, sampleLine(Vec{xr[0]}, Vec{xr[1]}))
}

// drawAxesFrame outlines the world-space footprint of an axes/plane when its
// `frame` field is set. It makes the graph's scene size visible and distinct
// from its data scale — animating `size` visibly grows this box.
func (r *Renderer) drawAxesFrame(dc *gg.Context, c cam, e *Entity, tf Transform) {
	if e.fnum("frame") <= 0 {
		return
	}
	w, h := axesSize(e)
	at := tf.At
	box := []Vec{
		{at[0] - w/2, at[1] - h/2},
		{at[0] + w/2, at[1] - h/2},
		{at[0] + w/2, at[1] + h/2},
		{at[0] - w/2, at[1] + h/2},
		{at[0] - w/2, at[1] - h/2},
	}
	dc.SetLineWidth(math.Max(1, 0.02*c.ppu))
	setColor(dc, namedColors["white"], 0.22*tf.Opacity)
	r.polyline(dc, c, box)
}

func (r *Renderer) drawAxes(dc *gg.Context, c cam, e *Entity, tf Transform) {
	op := tf.Opacity
	r.drawAxesFrame(dc, c, e, tf)
	xr := rangeOf(e, "x_range", -7, 7)
	yr := rangeOf(e, "y_range", -4, 4)
	col := namedColors["white"]
	dc.SetLineWidth(math.Max(1.5, 0.03*c.ppu))
	setColor(dc, col, op)

	p0 := axesPoint(e, xr[0], 0)
	p1 := axesPoint(e, xr[1], 0)
	r.polyline(dc, c, []Vec{p0, p1})
	r.arrowHead(dc, c, p1, Vec{1}, op, col)
	q0 := axesPoint(e, 0, yr[0])
	q1 := axesPoint(e, 0, yr[1])
	r.polyline(dc, c, []Vec{q0, q1})
	r.arrowHead(dc, c, q1, Vec{0, 1}, op, col)

	tick := 0.09
	xTicks := axisTickValues(xr[0], xr[1], xr[2])
	for _, x := range xTicks {
		if math.Abs(x) < 1e-9 {
			continue
		}
		p := axesPoint(e, x, 0)
		r.polyline(dc, c, []Vec{{p[0], p[1] - tick}, {p[0], p[1] + tick}})
	}
	yTicks := axisTickValues(yr[0], yr[1], yr[2])
	for _, y := range yTicks {
		if math.Abs(y) < 1e-9 {
			continue
		}
		p := axesPoint(e, 0, y)
		r.polyline(dc, c, []Vec{{p[0] - tick, p[1]}, {p[0] + tick, p[1]}})
	}
	r.drawAxisTickLabels(dc, c, e, xTicks, yTicks, tick, op)
}

func axisTickValues(lo, hi, step float64) []float64 {
	step = niceStep(step)
	start := math.Ceil((lo+1e-9)/step) * step
	if math.Abs(lo) < 1e-9 {
		start = step
	}
	var out []float64
	for v := start; v <= hi+1e-9; v += step {
		out = append(out, snapAxisTick(v, step))
	}
	return out
}

// niceStep snaps an arbitrary step to the nearest "nice" value of the form
// 1/2/5 × 10^k. Static authored steps (0.5, 1, 2) are already nice and pass
// through unchanged; the point is to round the ugly fractional steps that fall
// out of animating a range (e.g. 88.9 → 100) so tick labels stay legible.
func niceStep(step float64) float64 {
	if step <= 0 {
		return 1
	}
	mag := math.Pow(10, math.Floor(math.Log10(step)))
	norm := step / mag
	switch {
	case norm < 1.5:
		return mag
	case norm < 3.5:
		return 2 * mag
	case norm < 7.5:
		return 5 * mag
	default:
		return 10 * mag
	}
}

func snapAxisTick(v, step float64) float64 {
	if step <= 0 {
		step = 1
	}
	return math.Round(v/step) * step
}

func formatAxisTick(v, step float64) string {
	step = niceStep(step)
	v = snapAxisTick(v, step)
	if math.Abs(v) < 1e-12 {
		return "0"
	}
	abs := math.Abs(v)
	// compact large magnitudes so labels stay short across order-of-magnitude
	// zooms (14000 → "14k", 5e7 → "50M"); small ranges are unaffected.
	switch {
	case abs >= 1e9:
		return compactTick(v, 1e9, "B")
	case abs >= 1e6:
		return compactTick(v, 1e6, "M")
	case abs >= 1e3:
		return compactTick(v, 1e3, "k")
	}
	dec := axisTickDecimals(step)
	if dec == 0 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.*f", dec, v)
}

func compactTick(v, divisor float64, suffix string) string {
	x := v / divisor
	if math.Abs(x-math.Round(x)) < 0.05 {
		return fmt.Sprintf("%.0f%s", math.Round(x), suffix)
	}
	return fmt.Sprintf("%.1f%s", x, suffix)
}

func axisTickDecimals(step float64) int {
	if step <= 0 || math.Abs(step-math.Round(step)) < 1e-6 {
		return 0
	}
	d := int(math.Ceil(-math.Log10(step)))
	if d < 1 {
		return 1
	}
	if d > 4 {
		return 4
	}
	return d
}

func (r *Renderer) drawAxisTickLabels(dc *gg.Context, c cam, e *Entity, xTicks, yTicks []float64, tick, op float64) {
	px := math.Max(14, 0.28*c.ppu)
	dc.SetFontFace(faceAt(px))
	col := namedColors["white"]
	labelOp := 0.85 * op
	xr := rangeOf(e, "x_range", -7, 7)
	yr := rangeOf(e, "y_range", -4, 4)
	tickPx := tick * c.ppu
	// x labels sit just under the axis line (data y = 0), centered on each tick
	for _, x := range xTicks {
		if math.Abs(x) < 1e-9 {
			continue
		}
		sx, sy := c.sx(axesPoint(e, x, 0))
		s := formatAxisTick(x, xr[2])
		w, h := dc.MeasureString(s)
		setColor(dc, col, labelOp)
		dc.DrawString(s, sx-w/2, sy+tickPx+h)
	}
	// y labels sit just left of the axis line (data x = 0), vertically centered
	for _, y := range yTicks {
		if math.Abs(y) < 1e-9 {
			continue
		}
		sx, sy := c.sx(axesPoint(e, 0, y))
		s := formatAxisTick(y, yr[2])
		w, h := dc.MeasureString(s)
		setColor(dc, col, labelOp)
		dc.DrawString(s, sx-tickPx-w-6, sy+h*0.35)
	}
}

func (r *Renderer) arrowHead(dc *gg.Context, c cam, tip, dir Vec, op float64, col Color) {
	n := math.Hypot(dir[0], dir[1])
	if n == 0 {
		return
	}
	d := dir.Mul(1 / n)
	perp := Vec{-d[1], d[0]}
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

func pathPoints(e *Entity, tf Transform) []Vec {
	f, ok := e.Fields["points"]
	if !ok || f.Val == nil {
		return nil
	}
	pts, err := resolvePoints(f.Val)
	if err != nil || len(pts) == 0 {
		return nil
	}
	out := make([]Vec, len(pts))
	for i, p := range pts {
		p = p.Mul(tf.Scale).Add(tf.At)
		out[i] = rotateAround(p, tf.At, tf.Angle)
	}
	return out
}

func trimPathPoints(pts []Vec, draw float64) []Vec {
	draw = clamp01(draw)
	if draw >= 1 || len(pts) < 2 {
		return pts
	}
	if draw <= 0 {
		return nil
	}
	total := 0.0
	lengths := make([]float64, len(pts)-1)
	for i := 0; i < len(pts)-1; i++ {
		l := math.Hypot(pts[i+1][0]-pts[i][0], pts[i+1][1]-pts[i][1])
		lengths[i] = l
		total += l
	}
	if total <= 1e-9 {
		return pts[:1]
	}
	remain := total * draw
	out := []Vec{pts[0]}
	for i, l := range lengths {
		if remain >= l {
			out = append(out, pts[i+1])
			remain -= l
			continue
		}
		if remain > 0 {
			u := remain / l
			out = append(out, Vec{
				lerp(pts[i][0], pts[i+1][0], u),
				lerp(pts[i][1], pts[i+1][1], u),
			})
		}
		break
	}
	return out
}

func closePathPoints(pts []Vec) []Vec {
	if len(pts) == 0 {
		return nil
	}
	out := make([]Vec, 0, len(pts)+1)
	out = append(out, pts...)
	out = append(out, pts[0])
	return out
}

func (r *Renderer) tracePath(dc *gg.Context, c cam, pts []Vec, closed bool) {
	for i, p := range pts {
		x, y := c.sx(p)
		if i == 0 {
			dc.MoveTo(x, y)
		} else {
			dc.LineTo(x, y)
		}
	}
	if closed {
		dc.ClosePath()
	}
}

func (r *Renderer) drawPath(dc *gg.Context, c cam, e *Entity, tf Transform) {
	op := tf.Opacity
	draw := clamp01(e.fnum("draw"))
	closed := pathIsClosed(e)
	pts := pathWorldPoints(e)
	if len(pts) < 2 || draw <= 0 {
		return
	}
	drawPts := pts
	if closed {
		if draw < 1 {
			drawPts = trimPathPoints(closePathPoints(pts), draw)
		}
	} else {
		drawPts = trimPathPoints(pts, draw)
	}
	if len(drawPts) < 2 {
		return
	}
	if closed && draw >= 1 {
		if fill, ok := pathFillColor(e); ok && fill.A > 0.001 {
			setColor(dc, Color{fill.R, fill.G, fill.B, 1}, op*fill.A)
			r.tracePath(dc, c, drawPts, true)
			dc.Fill()
		}
	}
	stroke := pathStrokeColor(e)
	if stroke.A > 0.001 {
		setColor(dc, stroke, op)
		dc.SetLineWidth(math.Max(1.0, pathStrokeWidth(e)*c.ppu))
		r.tracePath(dc, c, drawPts, closed && draw >= 1)
		dc.Stroke()
	}
	end := fieldString(e, "stroke.end", "end", "end_marker")
	if end == "arrow" && len(drawPts) >= 2 {
		tip := drawPts[len(drawPts)-1]
		prev := drawPts[len(drawPts)-2]
		r.arrowHead(dc, c, tip, tip.Sub(prev), op, stroke)
	}
}
