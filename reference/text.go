package main

// Text handling. LaTeX is approximated, not rendered: known commands map to
// unicode, the rest is stripped (the honesty-audit cut: "whole-glyph
// re-render, no per-glyph diffing"). Named markup `{c2}...{/c2}` carves parts;
// when a record declares parts but the text has no markup, `$...$` math spans
// are assigned to the declared parts in order (benchmark 07's question).

import (
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

const refPx = 64.0

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
	Part  *PartState // nil for plain text
	Text  string
	X, W  float64 // world units, X from line left edge
	Runes int
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

func textLayoutOf(e *Entity) *TextLayout {
	raw := entityText(e)
	if raw == "" {
		return nil
	}
	scale := e.fnum("scale")
	if scale == 0 {
		scale = 1
	}
	fs := e.fnum("font_size")
	if fs == 0 {
		fs = 48
	}
	key := fmt.Sprintf("%s|%g|%g", raw, scale, fs)
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

	segs := segmentText(raw, partNames)
	var lines []LayLine
	cur := LayLine{}
	flush := func() {
		lines = append(lines, cur)
		cur = LayLine{}
	}
	total := 0
	for _, sg := range segs {
		clean := cleanTex(sg.Text)
		pieces := strings.Split(clean, "\n")
		for pi, piece := range pieces {
			if pi > 0 {
				flush()
			}
			if piece == "" {
				continue
			}
			wWorld := measurePx(piece) / refPx * em
			ls := LaySeg{Text: piece, X: cur.W, W: wWorld, Runes: len([]rune(piece))}
			if sg.PartName != "" {
				ls.Part = partByName[sg.PartName]
			}
			total += ls.Runes
			cur.W += wWorld
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
	at := e.fvec("at").Add(e.Offset)
	for _, line := range lay.Lines {
		for _, sg := range line.Segs {
			if sg.Part == p {
				cx := at[0] - line.W/2 + sg.X + sg.W/2
				cy := at[1] + line.Y
				return Vec{cx, cy, 0}, sg.W, lay.Em
			}
		}
	}
	return at, 0.5, lay.Em
}
