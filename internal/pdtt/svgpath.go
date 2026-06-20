package pdtt

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
)

type svgMatrix struct {
	a, b, c, d, e, f float64
}

func identityMatrix() svgMatrix {
	return svgMatrix{a: 1, d: 1}
}

func (m svgMatrix) apply(p Vec) Vec {
	return Vec{
		m.a*p[0] + m.c*p[1] + m.e,
		m.b*p[0] + m.d*p[1] + m.f,
	}
}

// glyphUse is a deferred <use> placement. Typst emits the glyph <symbol> defs
// AFTER the <use> elements that reference them, so we cannot resolve a use at
// the moment we see it — we collect placements and resolve them after EOF, once
// every symbol is known.
type glyphUse struct {
	id     string
	x, y   float64
	matrix svgMatrix
}

// parseTypstSVG returns the glyphs of the rendered markup, each glyph being its
// own group of closed contours (outer outline plus any counters/holes). Keeping
// glyphs grouped — rather than flattening every contour into one list — lets the
// renderer reveal text one whole letter at a time (manim's Write) instead of
// wiping a hard vertical edge across half-drawn glyphs.
func parseTypstSVG(src string) ([][][]Vec, Box, error) {
	dec := xml.NewDecoder(strings.NewReader(src))

	symbols := map[string][][]Vec{}
	var curSymbol string

	textDepth := 0
	textMatrix := identityMatrix()

	var uses []glyphUse

	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, Box{}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "symbol":
				curSymbol = attr(t.Attr, "id")
			case "path":
				if curSymbol == "" {
					continue
				}
				d := attr(t.Attr, "d")
				cs, err := parseSVGPathContours(d)
				if err != nil {
					return nil, Box{}, err
				}
				symbols[curSymbol] = append(symbols[curSymbol], cs...)
			case "g":
				if textDepth > 0 {
					textDepth++
					continue
				}
				if class := attr(t.Attr, "class"); strings.Contains(class, "typst-text") {
					textDepth = 1
					m, err := parseMatrix(attr(t.Attr, "transform"))
					if err != nil {
						return nil, Box{}, err
					}
					textMatrix = m
				}
			case "use":
				if textDepth == 0 {
					continue
				}
				href := attr(t.Attr, "href")
				if href == "" {
					continue
				}
				ux, _ := parseAttrFloat(attr(t.Attr, "x"))
				uy, _ := parseAttrFloat(attr(t.Attr, "y"))
				uses = append(uses, glyphUse{
					id:     strings.TrimPrefix(href, "#"),
					x:      ux,
					y:      uy,
					matrix: textMatrix,
				})
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "symbol":
				curSymbol = ""
			case "g":
				if textDepth > 0 {
					textDepth--
				}
			}
		}
	}

	var glyphs [][][]Vec
	var bbox Box
	for _, u := range uses {
		glyph := symbols[u.id]
		var placedGlyph [][]Vec
		for _, contour := range glyph {
			if len(contour) == 0 {
				continue
			}
			placed := make([]Vec, len(contour))
			for i, p := range contour {
				q := Vec{p[0] + u.x, p[1] + u.y}
				q = u.matrix.apply(q)
				placed[i] = q
				bbox.Include(q)
			}
			placedGlyph = append(placedGlyph, placed)
		}
		if len(placedGlyph) > 0 {
			glyphs = append(glyphs, placedGlyph)
		}
	}
	return glyphs, bbox, nil
}

func attr(attrs []xml.Attr, key string) string {
	for _, a := range attrs {
		if a.Name.Local == key {
			return a.Value
		}
	}
	return ""
}

func parseAttrFloat(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}

func parseMatrix(s string) (svgMatrix, error) {
	if s == "" {
		return identityMatrix(), nil
	}
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "matrix(") || !strings.HasSuffix(s, ")") {
		return svgMatrix{}, fmt.Errorf("unsupported svg transform %q", s)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(s, "matrix("), ")")
	fields := strings.Fields(strings.ReplaceAll(body, ",", " "))
	if len(fields) != 6 {
		return svgMatrix{}, fmt.Errorf("bad matrix transform %q", s)
	}
	vals := [6]float64{}
	for i := 0; i < 6; i++ {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return svgMatrix{}, err
		}
		vals[i] = v
	}
	return svgMatrix{
		a: vals[0], b: vals[1], c: vals[2],
		d: vals[3], e: vals[4], f: vals[5],
	}, nil
}

type svgPathToken struct {
	cmd   byte
	num   float64
	isCmd bool
}

func parseSVGPathContours(d string) ([][]Vec, error) {
	toks, err := tokenizeSVGPath(d)
	if err != nil {
		return nil, err
	}
	var (
		out     [][]Vec
		curPoly []Vec
		cur     Vec
		start   Vec
		cmd     byte
		i       int
	)

	flush := func() {
		if len(curPoly) > 0 {
			out = append(out, curPoly)
			curPoly = nil
		}
	}
	hasNum := func() bool { return i < len(toks) && !toks[i].isCmd }
	nextNum := func() (float64, error) {
		if !hasNum() {
			return 0, fmt.Errorf("svg path: expected number")
		}
		v := toks[i].num
		i++
		return v, nil
	}

	for i < len(toks) {
		if toks[i].isCmd {
			cmd = toks[i].cmd
			i++
		} else if cmd == 0 {
			return nil, fmt.Errorf("svg path: missing command")
		}

		switch cmd {
		case 'M', 'm':
			first := true
			for hasNum() {
				x, err := nextNum()
				if err != nil {
					return nil, err
				}
				y, err := nextNum()
				if err != nil {
					return nil, err
				}
				p := Vec{x, y}
				if cmd == 'm' {
					p = cur.Add(p)
				}
				if first {
					flush()
					cur = p
					start = p
					curPoly = append(curPoly, cur)
					first = false
					continue
				}
				cur = p
				curPoly = append(curPoly, cur)
			}
		case 'L', 'l':
			for hasNum() {
				x, err := nextNum()
				if err != nil {
					return nil, err
				}
				y, err := nextNum()
				if err != nil {
					return nil, err
				}
				p := Vec{x, y}
				if cmd == 'l' {
					p = cur.Add(p)
				}
				cur = p
				curPoly = append(curPoly, cur)
			}
		case 'H', 'h':
			for hasNum() {
				x, err := nextNum()
				if err != nil {
					return nil, err
				}
				if cmd == 'h' {
					x += cur[0]
				}
				cur = Vec{x, cur[1]}
				curPoly = append(curPoly, cur)
			}
		case 'V', 'v':
			for hasNum() {
				y, err := nextNum()
				if err != nil {
					return nil, err
				}
				if cmd == 'v' {
					y += cur[1]
				}
				cur = Vec{cur[0], y}
				curPoly = append(curPoly, cur)
			}
		case 'C', 'c':
			for hasNum() {
				x1, _ := nextNum()
				y1, _ := nextNum()
				x2, _ := nextNum()
				y2, _ := nextNum()
				x3, _ := nextNum()
				y3, _ := nextNum()
				c1 := Vec{x1, y1}
				c2 := Vec{x2, y2}
				p3 := Vec{x3, y3}
				if cmd == 'c' {
					c1 = cur.Add(c1)
					c2 = cur.Add(c2)
					p3 = cur.Add(p3)
				}
				seg := flattenCubic(cur, c1, c2, p3, 12)
				curPoly = append(curPoly, seg...)
				cur = p3
			}
		case 'Q', 'q':
			for hasNum() {
				x1, _ := nextNum()
				y1, _ := nextNum()
				x2, _ := nextNum()
				y2, _ := nextNum()
				c1 := Vec{x1, y1}
				p2 := Vec{x2, y2}
				if cmd == 'q' {
					c1 = cur.Add(c1)
					p2 = cur.Add(p2)
				}
				seg := flattenQuadratic(cur, c1, p2, 12)
				curPoly = append(curPoly, seg...)
				cur = p2
			}
		case 'Z', 'z':
			cur = start
			flush()
		default:
			return nil, fmt.Errorf("unsupported svg path command %q", string(cmd))
		}
	}
	flush()
	return out, nil
}

func flattenCubic(p0, p1, p2, p3 Vec, steps int) []Vec {
	if steps < 2 {
		steps = 2
	}
	out := make([]Vec, 0, steps)
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		mt := 1 - t
		x := mt*mt*mt*p0[0] + 3*mt*mt*t*p1[0] + 3*mt*t*t*p2[0] + t*t*t*p3[0]
		y := mt*mt*mt*p0[1] + 3*mt*mt*t*p1[1] + 3*mt*t*t*p2[1] + t*t*t*p3[1]
		out = append(out, Vec{x, y})
	}
	return out
}

func flattenQuadratic(p0, p1, p2 Vec, steps int) []Vec {
	if steps < 2 {
		steps = 2
	}
	out := make([]Vec, 0, steps)
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		mt := 1 - t
		x := mt*mt*p0[0] + 2*mt*t*p1[0] + t*t*p2[0]
		y := mt*mt*p0[1] + 2*mt*t*p1[1] + t*t*p2[1]
		out = append(out, Vec{x, y})
	}
	return out
}

func tokenizeSVGPath(d string) ([]svgPathToken, error) {
	var out []svgPathToken
	for i := 0; i < len(d); {
		c := d[i]
		switch {
		case unicode.IsSpace(rune(c)) || c == ',':
			i++
			continue
		case unicode.IsLetter(rune(c)):
			out = append(out, svgPathToken{cmd: c, isCmd: true})
			i++
			continue
		default:
			j := i
			if d[j] == '+' || d[j] == '-' {
				j++
			}
			digits := false
			for j < len(d) && d[j] >= '0' && d[j] <= '9' {
				j++
				digits = true
			}
			if j < len(d) && d[j] == '.' {
				j++
				for j < len(d) && d[j] >= '0' && d[j] <= '9' {
					j++
					digits = true
				}
			}
			if !digits {
				return nil, fmt.Errorf("bad svg path number near %q", d[i:])
			}
			if j < len(d) && (d[j] == 'e' || d[j] == 'E') {
				j++
				if j < len(d) && (d[j] == '+' || d[j] == '-') {
					j++
				}
				expDigits := false
				for j < len(d) && d[j] >= '0' && d[j] <= '9' {
					j++
					expDigits = true
				}
				if !expDigits {
					return nil, fmt.Errorf("bad svg path exponent near %q", d[i:])
				}
			}
			num, err := strconv.ParseFloat(d[i:j], 64)
			if err != nil || math.IsNaN(num) || math.IsInf(num, 0) {
				return nil, fmt.Errorf("bad svg path number %q", d[i:j])
			}
			out = append(out, svgPathToken{num: num})
			i = j
		}
	}
	return out, nil
}
