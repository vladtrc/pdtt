package pdtt

// Text handling. Happy path: compile text with typst and render filled vector
// outlines. Fallback path (only when typst is unavailable): legacy freetype
// rendering with rough TeX cleanup.

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
)

var (
	ttfFont     *truetype.Font
	faceCache   = map[int]font.Face{}
	measureFace font.Face
)

const (
	refPx                 = 64.0
	typstWorldPerPtAtBase = 0.0115
)

func initFonts() error {
	f, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return err
	}
	ttfFont = f
	measureFace = faceAt(refPx)
	return nil
}

func faceAt(px float64) font.Face {
	key := int(px + 0.5)
	if key < 4 {
		key = 4
	}
	if key > 600 {
		key = 600
	}
	if f, ok := faceCache[key]; ok {
		return f
	}
	f := truetype.NewFace(ttfFont, &truetype.Options{Size: float64(key), DPI: 72})
	faceCache[key] = f
	return f
}

func measurePx(s string) float64 {
	if measureFace == nil || s == "" {
		return 0
	}
	adv := font.MeasureString(measureFace, s)
	return float64(adv) / 64
}

// ---------- tex cleanup ----------

var (
	fracRe   = regexp.MustCompile(`\\frac\{([^{}]*)\}\{([^{}]*)\}`)
	textbfRe = regexp.MustCompile(`\\text(?:bf|it|rm)?\{([^{}]*)\}`)
	supRe    = regexp.MustCompile(`\^\{([^{}]*)\}`)
	subRe    = regexp.MustCompile(`_\{([^{}]*)\}`)
)

var texSymbols = strings.NewReplacer(
	`\sum`, "Σ", `\infty`, "∞", `\pi`, "π", `\nabla`, "∇",
	`\cdot`, "·", `\times`, "×", `\alpha`, "α", `\beta`, "β",
	`\theta`, "θ", `\LaTeX`, "LaTeX", `\rightarrow`, "→",
)

var supDigits = strings.NewReplacer("0", "⁰", "1", "¹", "2", "²", "3", "³", "4", "⁴",
	"5", "⁵", "6", "⁶", "7", "⁷", "8", "⁸", "9", "⁹")

func cleanTex(s string) string {
	s = strings.ReplaceAll(s, `\ `, "\n") // bare `\` + space: line break
	s = fracRe.ReplaceAllString(s, "$1/$2")
	s = textbfRe.ReplaceAllString(s, "$1")
	s = texSymbols.Replace(s)
	s = supRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := supRe.FindStringSubmatch(m)[1]
		if regexp.MustCompile(`^\d+$`).MatchString(inner) {
			return supDigits.Replace(inner)
		}
		return "^" + inner
	})
	s = subRe.ReplaceAllString(s, "_$1")
	// bare ^N
	s = regexp.MustCompile(`\^(\d)`).ReplaceAllStringFunc(s, func(m string) string {
		return supDigits.Replace(m[1:])
	})
	s = strings.ReplaceAll(s, "$", "")
	s = strings.NewReplacer("{", "", "}", "", "\\", "").Replace(s)
	return s
}

var (
	typstFracRe   = regexp.MustCompile(`\\frac\{([^{}]*)\}\{([^{}]*)\}`)
	typstTextBfRe = regexp.MustCompile(`\\textbf\{([^{}]*)\}`)
	typstTextItRe = regexp.MustCompile(`\\textit\{([^{}]*)\}`)
	typstTextRmRe = regexp.MustCompile(`\\textrm\{([^{}]*)\}`)
	typstCmdRe    = regexp.MustCompile(`\\([a-zA-Z]+)`)
	spaceRe       = regexp.MustCompile(`[ \t]+`)
)

func normalizeTexForTypst(s string) string {
	// Preserve old `\ ` linebreak shorthand from examples.
	s = strings.ReplaceAll(s, `\ `, "\n")
	s = typstFracRe.ReplaceAllString(s, "frac($1, $2)")
	s = typstTextBfRe.ReplaceAllString(s, " bold($1)")
	s = typstTextItRe.ReplaceAllString(s, " italic($1)")
	s = typstTextRmRe.ReplaceAllString(s, " $1")
	s = typstCmdRe.ReplaceAllString(s, "$1")
	s = spaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// ---------- segmentation ----------

type texSeg struct {
	Part *PartState // nil for plain text
	Text string
}

// segmentBySub splits raw into plain runs and emphasised spans, one span per
// part whose substring is found in the text. Earliest match wins; later parts
// that would overlap an accepted span are skipped.
func segmentBySub(raw string, parts []*PartState) []texSeg {
	type span struct {
		start, end int
		p          *PartState
	}
	var spans []span
	for _, p := range parts {
		if p.Sub == "" {
			continue
		}
		if idx := strings.Index(raw, p.Sub); idx >= 0 {
			spans = append(spans, span{idx, idx + len(p.Sub), p})
		}
	}
	if len(spans) == 0 {
		return []texSeg{{Text: raw}}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })

	var segs []texSeg
	pos := 0
	for _, sp := range spans {
		if sp.start < pos {
			continue // overlaps an already-emitted span
		}
		if sp.start > pos {
			segs = append(segs, texSeg{Text: raw[pos:sp.start]})
		}
		segs = append(segs, texSeg{Part: sp.p, Text: raw[sp.start:sp.end]})
		pos = sp.end
	}
	if pos < len(raw) {
		segs = append(segs, texSeg{Text: raw[pos:]})
	}
	return segs
}

// ---------- layout ----------

type LaySeg struct {
	Part   *PartState // nil for plain text
	Text   string
	X, W   float64 // world units, X from line left edge
	Runes  int
	Glyphs [][][]Vec // local world-space glyphs (each a list of contours), centered on the line
}

type LayLine struct {
	Segs []LaySeg
	W, Y float64 // Y = line center relative to layout center (world, +up)
}

type TextLayout struct {
	Lines       []LayLine
	W, H        float64
	Em          float64 // world units per reference em
	TotalRunes  int
	TotalGlyphs int // vector glyph count across all lines, for the Write cascade
}

func entityText(e *Entity) string {
	if e.Type == "decimal" {
		dec := int(e.fnum("decimals"))
		return fmt.Sprintf("%.*f", dec, e.fnum("value"))
	}
	return e.fstr("text")
}

func textIsMath(e *Entity) bool {
	return e.Type == "tex" || e.Type == "typst"
}

func textWorldPerPt(e *Entity) float64 {
	scale := e.transform().Scale
	fs := e.fnum("font_size")
	if fs == 0 {
		fs = 48
	}
	return typstWorldPerPtAtBase * (fs / typstBaseTextSizePt) * scale
}

func textEm(e *Entity) float64 {
	fs := e.fnum("font_size")
	if fs == 0 {
		fs = 48
	}
	return 0.62 * fs / 48 * e.transform().Scale
}

func normalizeSegmentForTypst(e *Entity, s string) string {
	if textIsMath(e) {
		return normalizeTexForTypst(s)
	}
	return s
}

func transformGlyphsToSegment(glyphs [][][]Vec, bbox Box, worldPerPt float64) [][][]Vec {
	if len(glyphs) == 0 {
		return nil
	}
	cy := 0.5 * (bbox.Min[1] + bbox.Max[1]) * worldPerPt
	out := make([][][]Vec, 0, len(glyphs))
	for _, glyph := range glyphs {
		dstGlyph := make([][]Vec, 0, len(glyph))
		for _, contour := range glyph {
			if len(contour) == 0 {
				continue
			}
			dst := make([]Vec, len(contour))
			for i, p := range contour {
				dst[i] = Vec{
					(p[0] - bbox.Min[0]) * worldPerPt,
					// Typst SVG y grows downward; world y grows up. Flip about the
					// glyph's vertical center so text reads upright.
					cy - p[1]*worldPerPt,
				}
			}
			dstGlyph = append(dstGlyph, dst)
		}
		if len(dstGlyph) > 0 {
			out = append(out, dstGlyph)
		}
	}
	return out
}

func segmentLayoutTypst(e *Entity, piece string) (LaySeg, error) {
	worldPerPt := textWorldPerPt(e)
	markup := normalizeSegmentForTypst(e, piece)
	if strings.TrimSpace(markup) == "" {
		em := textEm(e)
		return LaySeg{
			Text:  piece,
			Runes: len([]rune(piece)),
			W:     measurePx(piece) / refPx * em,
		}, nil
	}
	// Typst's glyph bbox excludes leading/trailing spaces, so a segment rendered
	// in isolation (e.g. the " a word" sitting next to an emphasised "{x}..{/x}"
	// span) would lose its edge spacing and butt against its neighbour. Render the
	// trimmed core and re-add the measured edge-space width, shifting the glyphs
	// right by the leading pad so the segment occupies its full advance.
	core := strings.Trim(markup, " ")
	lead := len(markup) - len(strings.TrimLeft(markup, " "))
	trail := len(markup) - len(strings.TrimRight(markup, " "))
	em := textEm(e)
	spaceW := measurePx(" ") / refPx * em
	leadW := float64(lead) * spaceW
	trailW := float64(trail) * spaceW

	glyphs, bbox, err := typstGlyphs(core, textIsMath(e))
	if err != nil {
		return LaySeg{}, err
	}
	segGlyphs := transformGlyphsToSegment(glyphs, bbox, worldPerPt)
	if leadW > 0 {
		for _, glyph := range segGlyphs {
			for _, contour := range glyph {
				for i := range contour {
					contour[i][0] += leadW
				}
			}
		}
	}
	out := LaySeg{
		Text:   piece,
		Runes:  len([]rune(piece)),
		W:      leadW + bbox.Width()*worldPerPt + trailW,
		Glyphs: segGlyphs,
	}
	return out, nil
}

func segmentLayoutFallback(e *Entity, piece string) LaySeg {
	em := textEm(e)
	clean := cleanTex(piece)
	return LaySeg{
		Text:  clean,
		Runes: len([]rune(clean)),
		W:     measurePx(clean) / refPx * em,
	}
}

func textLayoutOf(e *Entity) *TextLayout {
	raw := entityText(e)
	if raw == "" {
		return nil
	}
	scale := e.transform().Scale
	fs := e.fnum("font_size")
	if fs == 0 {
		fs = 48
	}
	var subKey strings.Builder
	for _, p := range e.Parts {
		subKey.WriteString(p.Sub)
		subKey.WriteByte('\x00')
	}
	useTypst := typstInstalled()
	key := fmt.Sprintf("%s|%s|%g|%g|%t|%s", e.Type, raw, scale, fs, useTypst, subKey.String())
	if e.layoutCache != nil && e.layoutKey == key {
		return e.layoutCache
	}

	em := 0.62 * fs / 48 * scale // world height of one em
	lineH := 1.45 * em

	segRaw := raw
	if textIsMath(e) {
		segRaw = strings.ReplaceAll(segRaw, `\ `, "\n")
	}
	segs := segmentBySub(segRaw, e.Parts)

	var lines []LayLine
	cur := LayLine{}
	flush := func() {
		lines = append(lines, cur)
		cur = LayLine{}
	}
	total := 0
	for _, sg := range segs {
		pieces := strings.Split(sg.Text, "\n")
		for pi, piece := range pieces {
			if pi > 0 {
				flush()
			}
			if piece == "" {
				continue
			}
			var (
				ls  LaySeg
				err error
			)
			if useTypst {
				ls, err = segmentLayoutTypst(e, piece)
				if err != nil {
					if errors.Is(err, errTypstUnavailable) {
						useTypst = false
						ls = segmentLayoutFallback(e, piece)
					} else {
						if e.rt != nil {
							e.rt.warnOnce("typst text compile failed: " + err.Error())
						}
						return nil
					}
				}
			} else {
				ls = segmentLayoutFallback(e, piece)
			}
			ls.X = cur.W
			ls.Part = sg.Part
			total += ls.Runes
			cur.W += ls.W
			cur.Segs = append(cur.Segs, ls)
		}
	}
	flush()

	maxW := 0.0
	for _, l := range lines {
		if l.W > maxW {
			maxW = l.W
		}
	}
	H := float64(len(lines)) * lineH
	for i := range lines {
		lines[i].Y = H/2 - (float64(i)+0.5)*lineH
	}
	totalGlyphs := 0
	for _, l := range lines {
		for _, sg := range l.Segs {
			totalGlyphs += len(sg.Glyphs)
		}
	}
	lay := &TextLayout{Lines: lines, W: maxW, H: H, Em: em, TotalRunes: total, TotalGlyphs: totalGlyphs}
	e.layoutCache = lay
	e.layoutKey = key
	return lay
}

// partBox in world coordinates (absolute).
func (lay *TextLayout) partBox(p *PartState) (Vec, float64, float64) {
	e := p.E
	at := e.transform().At
	for _, line := range lay.Lines {
		for _, sg := range line.Segs {
			if sg.Part == p {
				cx := at[0] - line.W/2 + sg.X + sg.W/2
				cy := at[1] + line.Y
				return Vec{cx, cy}, sg.W, lay.Em
			}
		}
	}
	return at, 0.5, lay.Em
}

func textOutlineContours(e *Entity) [][]Vec {
	lay := textLayoutOf(e)
	if lay == nil {
		return nil
	}
	tf := e.transform()
	at := tf.At
	angle := tf.Angle

	var out [][]Vec
	for _, line := range lay.Lines {
		for _, sg := range line.Segs {
			if len(sg.Glyphs) == 0 {
				continue
			}
			x0 := at[0] - line.W/2 + sg.X
			y0 := at[1] + line.Y
			for _, glyph := range sg.Glyphs {
				for _, contour := range glyph {
					if len(contour) == 0 {
						continue
					}
					dst := make([]Vec, len(contour))
					for i, p := range contour {
						q := Vec{x0 + p[0], y0 + p[1]}
						dst[i] = rotateAround(q, at, angle)
					}
					out = append(out, dst)
				}
			}
		}
	}
	return out
}
