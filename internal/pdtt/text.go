package pdtt

// Text handling. Happy path: compile text with typst and render filled vector
// outlines. Fallback path (only when typst is unavailable): legacy freetype
// rendering with rough TeX cleanup.

import (
	"errors"
	"fmt"
	"regexp"
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
	PartName string
	Text     string
}

var (
	markupRe   = regexp.MustCompile(`\{(\w+)\}(.*?)\{/\w+\}`)
	mathSpanRe = regexp.MustCompile(`\$([^$]*)\$`)
)

func segmentText(raw string, partNames []string) []texSeg {
	if locs := markupRe.FindAllStringSubmatchIndex(raw, -1); len(locs) > 0 {
		var segs []texSeg
		pos := 0
		for _, m := range locs {
			if m[0] > pos {
				segs = append(segs, texSeg{Text: raw[pos:m[0]]})
			}
			segs = append(segs, texSeg{PartName: raw[m[2]:m[3]], Text: raw[m[4]:m[5]]})
			pos = m[1]
		}
		if pos < len(raw) {
			segs = append(segs, texSeg{Text: raw[pos:]})
		}
		return segs
	}
	if len(partNames) > 0 {
		if locs := mathSpanRe.FindAllStringSubmatchIndex(raw, -1); len(locs) >= len(partNames) {
			var segs []texSeg
			pos := 0
			for i, m := range locs {
				name := ""
				if i < len(partNames) {
					name = partNames[i]
				}
				if m[0] > pos {
					segs = append(segs, texSeg{Text: raw[pos:m[0]]})
				}
				segs = append(segs, texSeg{PartName: name, Text: raw[m[2]:m[3]]})
				pos = m[1]
			}
			if pos < len(raw) {
				segs = append(segs, texSeg{Text: raw[pos:]})
			}
			return segs
		}
	}
	return []texSeg{{Text: raw}}
}

// ---------- layout ----------

type LaySeg struct {
	Part     *PartState // nil for plain text
	Text     string
	X, W     float64 // world units, X from line left edge
	Runes    int
	Contours [][]Vec // local world-space contours, centered on the line
}

type LayLine struct {
	Segs []LaySeg
	W, Y float64 // Y = line center relative to layout center (world, +up)
}

type TextLayout struct {
	Lines      []LayLine
	W, H       float64
	Em         float64 // world units per reference em
	TotalRunes int
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

func transformContoursToSegment(contours [][]Vec, bbox Box, worldPerPt float64) [][]Vec {
	if len(contours) == 0 {
		return nil
	}
	cy := 0.5 * (bbox.Min[1] + bbox.Max[1]) * worldPerPt
	out := make([][]Vec, 0, len(contours))
	for _, contour := range contours {
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
		out = append(out, dst)
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
	contours, bbox, err := typstGlyphs(markup, textIsMath(e))
	if err != nil {
		return LaySeg{}, err
	}
	out := LaySeg{
		Text:     piece,
		Runes:    len([]rune(piece)),
		W:        bbox.Width() * worldPerPt,
		Contours: transformContoursToSegment(contours, bbox, worldPerPt),
	}
	if !bbox.Valid && strings.TrimSpace(piece) == "" {
		// typst emits no glyph path for pure whitespace; keep spacing stable.
		em := textEm(e)
		out.W = measurePx(piece) / refPx * em
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
	key := fmt.Sprintf("%s|%s|%g|%g|%t", e.Type, raw, scale, fs, typstInstalled())
	if e.layoutCache != nil && e.layoutKey == key {
		return e.layoutCache
	}

	var partNames []string
	for _, p := range e.Parts {
		partNames = append(partNames, p.Name)
	}
	partByName := map[string]*PartState{}
	for _, p := range e.Parts {
		partByName[p.Name] = p
	}

	em := 0.62 * fs / 48 * scale // world height of one em
	lineH := 1.45 * em

	segRaw := raw
	if textIsMath(e) {
		segRaw = strings.ReplaceAll(segRaw, `\ `, "\n")
	}
	segs := segmentText(segRaw, partNames)
	useTypst := typstInstalled()

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
			if sg.PartName != "" {
				ls.Part = partByName[sg.PartName]
			}
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
	lay := &TextLayout{Lines: lines, W: maxW, H: H, Em: em, TotalRunes: total}
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
			if len(sg.Contours) == 0 {
				continue
			}
			x0 := at[0] - line.W/2 + sg.X
			y0 := at[1] + line.Y
			for _, contour := range sg.Contours {
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
	return out
}
