package pdtt

// Expression grammar of concept 36. One Pratt-ish parser used for field
// expressions, arrow RHS, and op-cell paths. Modifier cells never reach this
// parser (they are classified by regex in parse.go), which is what makes the
// optional-bound window spellings (`-.5`, `.5-`) unambiguous.

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Expr interface{}

type (
	Num   float64
	Str   string
	Ident string
	AttrE struct {
		X    Expr
		Name string
	}
)

type IndexE struct { // I == nil means [*] or [* as name]
	X        Expr
	I        Expr
	BindName string // set when [* as name]; empty for plain [*] or indexed [expr]
}
type CallE struct {
	Fn   Expr
	Args []Expr
}
type BinE struct {
	Op   string
	L, R Expr
}
type RangeE struct {
	Start, End Expr
}
type UnE struct {
	Op string
	X  Expr
}
type (
	ListE  struct{ Items []Expr }
	CondE  struct{ Then, Cond, Else Expr } // cond ? then : else
	AlphaE struct {                        // COLOR@55%
		X   Expr
		Pct float64
	}
)

type FoldE struct { // scan(init by col)
	Init Expr
	By   string
}

type SnapshotE struct { // snapshot expr — evaluates at declaration time and freezes
	X Expr
}

type token struct {
	kind string // "num" "str" "id" "op"
	s    string
	num  float64
}

func isIdentChar(c byte, first bool) bool {
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
		return true
	}
	return !first && c >= '0' && c <= '9'
}

func lexExpr(src string) ([]token, error) {
	var ts []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9' && tokenEndsValue(ts):
			ts = append(ts, token{kind: "op", s: "."})
			i++
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			j := i
			seenDot := false
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.' && !seenDot) {
				if src[j] == '.' && j+1 < len(src) && src[j+1] == '.' {
					break
				}
				if src[j] == '.' {
					if j+1 >= len(src) || src[j+1] < '0' || src[j+1] > '9' {
						break
					}
					seenDot = true
				}
				j++
			}
			n, err := strconv.ParseFloat(src[i:j], 64)
			if err != nil {
				return nil, fmt.Errorf("bad number %q", src[i:j])
			}
			ts = append(ts, token{kind: "num", s: src[i:j], num: n})
			i = j
		case c == '"':
			var b strings.Builder
			j := i + 1
			for j < len(src) && src[j] != '"' {
				if src[j] == '\\' && j+1 < len(src) && (src[j+1] == '"' || src[j+1] == '\\') {
					b.WriteByte(src[j+1])
					j += 2
					continue
				}
				b.WriteByte(src[j])
				j++
			}
			if j >= len(src) {
				return nil, fmt.Errorf("unterminated string")
			}
			ts = append(ts, token{kind: "str", s: b.String()})
			i = j + 1
		case isIdentChar(c, true):
			j := i
			for j < len(src) && isIdentChar(src[j], false) {
				j++
			}
			ts = append(ts, token{kind: "id", s: src[i:j]})
			i = j
		default:
			if i+1 < len(src) {
				two := src[i : i+2]
				if two == "==" || two == "!=" || two == "<=" || two == ">=" || two == ".." {
					ts = append(ts, token{kind: "op", s: two})
					i += 2
					continue
				}
			}
			if strings.ContainsRune("+-*/%()[],.@<>?:", rune(c)) {
				ts = append(ts, token{kind: "op", s: string(c)})
				i++
				continue
			}
			return nil, fmt.Errorf("unexpected char %q in %q", c, src)
		}
	}
	return ts, nil
}

func tokenEndsValue(ts []token) bool {
	if len(ts) == 0 {
		return false
	}
	last := ts[len(ts)-1]
	if last.kind == "id" || last.kind == "num" || last.kind == "str" {
		return true
	}
	return last.kind == "op" && (last.s == ")" || last.s == "]")
}

type exprParser struct {
	ts  []token
	pos int
}

func ParseExpr(src string) (Expr, error) {
	ts, err := lexExpr(src)
	if err != nil {
		return nil, err
	}
	p := &exprParser{ts: ts}
	e, err := p.parseCond()
	if err != nil {
		return nil, fmt.Errorf("%v in %q", err, src)
	}
	if p.pos != len(p.ts) {
		return nil, fmt.Errorf("trailing tokens after expression in %q", src)
	}
	return e, nil
}

func (p *exprParser) peek() *token {
	if p.pos < len(p.ts) {
		return &p.ts[p.pos]
	}
	return nil
}

func (p *exprParser) eatOp(s string) bool {
	if t := p.peek(); t != nil && t.kind == "op" && t.s == s {
		p.pos++
		return true
	}
	return false
}

func (p *exprParser) eatID(s string) bool {
	if t := p.peek(); t != nil && t.kind == "id" && t.s == s {
		p.pos++
		return true
	}
	return false
}

// cond ? then : else   (also accepts legacy: then if cond else elseExpr)
func (p *exprParser) parseCond() (Expr, error) {
	e, err := p.parseRange()
	if err != nil {
		return nil, err
	}
	if p.eatOp("?") {
		thenExpr, err := p.parseCond()
		if err != nil {
			return nil, err
		}
		if !p.eatOp(":") {
			return nil, fmt.Errorf("conditional missing `:`")
		}
		elseExpr, err := p.parseCond()
		if err != nil {
			return nil, err
		}
		return CondE{Cond: e, Then: thenExpr, Else: elseExpr}, nil
	}
	if p.eatID("if") {
		cond, err := p.parseCmp()
		if err != nil {
			return nil, err
		}
		if !p.eatID("else") {
			return nil, fmt.Errorf("conditional missing `else`")
		}
		els, err := p.parseCond()
		if err != nil {
			return nil, err
		}
		return CondE{Then: e, Cond: cond, Else: els}, nil
	}
	return e, nil
}

func (p *exprParser) parseRange() (Expr, error) {
	e, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	if p.eatOp("..") {
		end, err := p.parseCmp()
		if err != nil {
			return nil, err
		}
		return RangeE{Start: e, End: end}, nil
	}
	return e, nil
}

func (p *exprParser) parseCmp() (Expr, error) {
	e, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || t.kind != "op" {
			return e, nil
		}
		switch t.s {
		case "==", "!=", "<", ">", "<=", ">=":
			p.pos++
			r, err := p.parseAdd()
			if err != nil {
				return nil, err
			}
			e = BinE{Op: t.s, L: e, R: r}
		default:
			return e, nil
		}
	}
}

func (p *exprParser) parseAdd() (Expr, error) {
	e, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || t.kind != "op" || t.s != "+" && t.s != "-" {
			return e, nil
		}
		p.pos++
		r, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		e = BinE{Op: t.s, L: e, R: r}
	}
}

func (p *exprParser) parseMul() (Expr, error) {
	e, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || t.kind != "op" || t.s != "*" && t.s != "/" && t.s != "%" {
			return e, nil
		}
		p.pos++
		r, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		e = BinE{Op: t.s, L: e, R: r}
	}
}

func (p *exprParser) parseUnary() (Expr, error) {
	if p.eatOp("-") {
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return UnE{Op: "-", X: x}, nil
	}
	return p.parsePostfix()
}

func (p *exprParser) parsePostfix() (Expr, error) {
	e, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.eatOp("."):
			t := p.peek()
			if t == nil || t.kind != "id" && t.kind != "num" {
				return nil, fmt.Errorf("expected attribute name after `.`")
			}
			if t.kind == "num" && math.Trunc(t.num) != t.num {
				return nil, fmt.Errorf("numeric attribute after `.` must be an integer")
			}
			p.pos++
			e = AttrE{X: e, Name: t.s}
		case p.eatOp("("):
			var args []Expr
			if !p.eatOp(")") {
				for {
					a, err := p.parseCond()
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					if p.eatOp(")") {
						break
					}
					if !p.eatOp(",") {
						return nil, fmt.Errorf("expected `,` or `)` in call args")
					}
				}
			}
			e = CallE{Fn: e, Args: args}
		case p.eatOp("["):
			if p.eatOp("*") {
				// [*] or [* as name]
				bindName := ""
				if p.eatID("as") {
					t2 := p.peek()
					if t2 == nil || t2.kind != "id" {
						return nil, fmt.Errorf("expected identifier after `* as`")
					}
					bindName = t2.s
					p.pos++
				}
				if !p.eatOp("]") {
					return nil, fmt.Errorf("expected `]` after `[*`")
				}
				e = IndexE{X: e, I: nil, BindName: bindName}
				continue
			}
			idx, err := p.parseCond()
			if err != nil {
				return nil, err
			}
			if !p.eatOp("]") {
				return nil, fmt.Errorf("expected `]`")
			}
			e = IndexE{X: e, I: idx}
		case p.eatOp("@"):
			t := p.peek()
			if t == nil || t.kind != "num" {
				return nil, fmt.Errorf("expected percent after `@`")
			}
			p.pos++
			if !p.eatOp("%") {
				return nil, fmt.Errorf("expected `%%` after `@%v`", t.num)
			}
			e = AlphaE{X: e, Pct: t.num}
		default:
			return e, nil
		}
	}
}

func (p *exprParser) parsePrimary() (Expr, error) {
	t := p.peek()
	if t == nil {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	switch t.kind {
	case "num":
		p.pos++
		return Num(t.num), nil
	case "str":
		p.pos++
		return Str(t.s), nil
	case "id":
		p.pos++
		if t.s == "snapshot" {
			// snapshot expr — freeze the expression at evaluation time
			x, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return SnapshotE{X: x}, nil
		}
		return Ident(t.s), nil
	case "op":
		if t.s == "(" {
			p.pos++
			e, err := p.parseCond()
			if err != nil {
				return nil, err
			}
			if !p.eatOp(")") {
				return nil, fmt.Errorf("expected `)`")
			}
			return e, nil
		}
		if t.s == "[" {
			p.pos++
			var items []Expr
			if !p.eatOp("]") {
				for {
					e, err := p.parseCond()
					if err != nil {
						return nil, err
					}
					items = append(items, e)
					if p.eatOp("]") {
						break
					}
					if !p.eatOp(",") {
						return nil, fmt.Errorf("expected `,` or `]` in list")
					}
				}
			}
			return ListE{Items: items}, nil
		}
	}
	return nil, fmt.Errorf("unexpected token %q", t.s)
}

// exprDeps collects the identifier paths an expression reads, for the
// liveness pass. Paths are reported as "name" or "name.field"; deeper or
// indexed paths collapse to the entity name (conservative: depending on the
// whole record).
func exprDeps(e Expr, out map[string]bool) {
	switch v := e.(type) {
	case nil, Num, Str:
	case Ident:
		out[string(v)] = true
	case AttrE:
		if id, ok := v.X.(Ident); ok {
			out[string(id)+"."+v.Name] = true
			return
		}
		exprDeps(v.X, out)
	case IndexE:
		exprDeps(v.X, out)
		if v.I != nil {
			exprDeps(v.I, out)
		}
	case CallE:
		exprDeps(v.Fn, out)
		for _, a := range v.Args {
			exprDeps(a, out)
		}
	case BinE:
		exprDeps(v.L, out)
		exprDeps(v.R, out)
	case RangeE:
		exprDeps(v.Start, out)
		exprDeps(v.End, out)
	case UnE:
		exprDeps(v.X, out)
	case ListE:
		for _, it := range v.Items {
			exprDeps(it, out)
		}
	case CondE:
		exprDeps(v.Then, out)
		exprDeps(v.Cond, out)
		exprDeps(v.Else, out)
	case AlphaE:
		exprDeps(v.X, out)
	case FoldE:
		exprDeps(v.Init, out)
		out[v.By] = true
	case SnapshotE:
		// snapshot is frozen — intentionally not a live dependency
	}
}
