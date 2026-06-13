package main

// Line-based parser for the top level: the two visual categories of the spec
// (`|` lines are time, everything else is state). Top-level forms are
// distinguished by first tokens exactly as checklist.md §1 describes.

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type Stmt interface{}

type (
	SceneStmt  struct{ Name string }
	ExternStmt struct{ Name string }
)

type CaptureStmt struct {
	Name string
	E    Expr
	Line int
}

type FieldDef struct {
	Name      string
	E         Expr
	Rate, Set bool
	Line      int
}

type RecordStmt struct {
	Type, Name string
	ForE       Expr // row source: list / range(n) / record name
	Fields     []FieldDef
	Line       int
}

type RowMod struct {
	Kind string  // "win" "after" "lag" "stagger" "ease" "pair"
	A, B float64 // window bounds; NaN = absent side
	ASec bool    // bound/offset spelled in seconds
	BSec bool
	D    float64 // after/lag/stagger amount
	DSec bool
	Name string // ease name or pairing mode
}

type RowOp struct {
	Kind string // "arrow" "write" "fade_in" "morph"
	LHS  Expr   // arrow target path / verb subject / morph source
	RHS  Expr   // arrow RHS / morph target
	Arg  string // fade_in direction
}

type Row struct {
	Mods []RowMod
	Op   RowOp
	Line int
}

type BlockStmt struct {
	DurS float64
	Ease string
	Each string // record name for `each` headers
	As   string
	Rows []Row
	Line int
}

var (
	recordRe  = regexp.MustCompile(`^(\w+) (\w+):$`)
	captureRe = regexp.MustCompile(`^(\w+)\s*=\s*(.+)$`)
	durRe     = regexp.MustCompile(`^(\d*\.?\d+)s$`)
	winRe     = regexp.MustCompile(`^(\d*\.?\d+(?:%|s)?)?-(\d*\.?\d+(?:%|s)?)?$`)
	foldRe    = regexp.MustCompile(`^scan\((.+) by (\w+)\)$`)
	timeAmtRe = regexp.MustCompile(`^(\d*\.?\d+)(s?)$`)
)

var easeNames = map[string]bool{
	"linear": true, "smooth": true, "ease_in": true, "ease_out": true, "ease_in_out": true,
}

func stripComment(s string) string {
	inStr := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inStr = !inStr
		case '#':
			if !inStr {
				return s[:i]
			}
		}
	}
	return s
}

func ParseFile(src string) ([]Stmt, error) {
	rawLines := strings.Split(src, "\n")
	var stmts []Stmt
	var curRecord *RecordStmt
	var curBlock *BlockStmt
	flushRecord := func() {
		if curRecord != nil {
			stmts = append(stmts, *curRecord)
			curRecord = nil
		}
	}
	flushBlock := func() {
		if curBlock != nil {
			stmts = append(stmts, *curBlock)
			curBlock = nil
		}
	}

	for n, raw := range rawLines {
		ln := n + 1
		line := strings.TrimRight(stripComment(raw), " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushRecord()
			flushBlock()
			continue
		}
		indented := line[0] == ' ' || line[0] == '\t'

		if indented {
			if curRecord == nil {
				return nil, fmt.Errorf("line %d: indented line outside a record", ln)
			}
			fd, err := parseFieldLine(trimmed, ln)
			if err != nil {
				return nil, err
			}
			curRecord.Fields = append(curRecord.Fields, fd)
			continue
		}

		if strings.HasPrefix(trimmed, "|") {
			flushRecord()
			body := strings.TrimSpace(trimmed[1:])
			if curBlock == nil {
				b, err := parseBlockHeader(body, ln)
				if err != nil {
					return nil, err
				}
				curBlock = b
				continue
			}
			row, err := parseRow(body, ln)
			if err != nil {
				return nil, err
			}
			curBlock.Rows = append(curBlock.Rows, row)
			continue
		}

		flushRecord()
		flushBlock()

		switch {
		case strings.HasPrefix(trimmed, "scene "):
			stmts = append(stmts, SceneStmt{Name: strings.TrimSpace(trimmed[6:])})
		case strings.HasPrefix(trimmed, "extern fn "):
			rest := strings.TrimSpace(trimmed[10:])
			name := rest
			if i := strings.IndexByte(rest, '('); i >= 0 {
				name = rest[:i]
			}
			stmts = append(stmts, ExternStmt{Name: name})
		default:
			if m := recordRe.FindStringSubmatch(trimmed); m != nil {
				curRecord = &RecordStmt{Type: m[1], Name: m[2], Line: ln}
				continue
			}
			if m := captureRe.FindStringSubmatch(trimmed); m != nil && !strings.HasPrefix(m[2], "=") {
				e, err := ParseExpr(m[2])
				if err != nil {
					return nil, fmt.Errorf("line %d: %v", ln, err)
				}
				stmts = append(stmts, CaptureStmt{Name: m[1], E: e, Line: ln})
				continue
			}
			return nil, fmt.Errorf("line %d: unrecognized top-level form: %q", ln, trimmed)
		}
	}
	flushRecord()
	flushBlock()
	return stmts, nil
}

func parseFieldLine(s string, ln int) (FieldDef, error) {
	fd := FieldDef{Line: ln}
	if strings.HasPrefix(s, "rate ") {
		fd.Rate = true
		s = strings.TrimSpace(s[5:])
	} else if strings.HasPrefix(s, "set ") {
		fd.Set = true
		s = strings.TrimSpace(s[4:])
	}
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return fd, fmt.Errorf("line %d: field line missing `:`", ln)
	}
	fd.Name = strings.TrimSpace(s[:i])
	body := strings.TrimSpace(s[i+1:])
	if fd.Name == "for" && strings.HasPrefix(body, "from ") {
		return fd, fmt.Errorf("line %d: `for: from \"cmd\"` row sources are not implemented in this prototype", ln)
	}
	if m := foldRe.FindStringSubmatch(body); m != nil {
		init, err := ParseExpr(m[1])
		if err != nil {
			return fd, fmt.Errorf("line %d: %v", ln, err)
		}
		fd.E = FoldE{Init: init, By: m[2]}
		return fd, nil
	}
	e, err := ParseExpr(body)
	if err != nil {
		return fd, fmt.Errorf("line %d: %v", ln, err)
	}
	fd.E = e
	return fd, nil
}

func parseBlockHeader(body string, ln int) (*BlockStmt, error) {
	b := &BlockStmt{Line: ln}
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return nil, fmt.Errorf("line %d: empty block header", ln)
	}
	if fields[0] == "each" {
		// | each record [as name] dur
		if len(fields) < 3 {
			return nil, fmt.Errorf("line %d: bad `each` header", ln)
		}
		b.Each = fields[1]
		rest := fields[2:]
		if rest[0] == "as" {
			if len(rest) < 3 {
				return nil, fmt.Errorf("line %d: bad `each ... as` header", ln)
			}
			b.As = rest[1]
			rest = rest[2:]
		}
		m := durRe.FindStringSubmatch(rest[0])
		if m == nil {
			return nil, fmt.Errorf("line %d: bad each duration %q", ln, rest[0])
		}
		b.DurS, _ = strconv.ParseFloat(m[1], 64)
		if len(rest) > 1 {
			return nil, fmt.Errorf("line %d: fast_after is not implemented in this prototype", ln)
		}
		return b, nil
	}
	m := durRe.FindStringSubmatch(fields[0])
	if m == nil {
		return nil, fmt.Errorf("line %d: bad block duration %q", ln, fields[0])
	}
	b.DurS, _ = strconv.ParseFloat(m[1], 64)
	if len(fields) > 1 {
		if !easeNames[fields[1]] {
			return nil, fmt.Errorf("line %d: unknown ease %q", ln, fields[1])
		}
		b.Ease = fields[1]
	}
	if len(fields) > 2 {
		return nil, fmt.Errorf("line %d: trailing tokens in block header", ln)
	}
	return b, nil
}

func parseRow(body string, ln int) (Row, error) {
	row := Row{Line: ln}
	cells := strings.Split(body, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	opCell := cells[len(cells)-1]
	for _, c := range cells[:len(cells)-1] {
		mod, err := parseModCell(c, ln)
		if err != nil {
			return row, err
		}
		row.Mods = append(row.Mods, mod)
	}
	op, err := parseOpCell(opCell, ln)
	if err != nil {
		return row, err
	}
	row.Op = op
	return row, nil
}

func parseWinBound(s string) (v float64, sec bool, ok bool) {
	if s == "" {
		return math.NaN(), false, true
	}
	switch {
	case strings.HasSuffix(s, "%"):
		f, err := strconv.ParseFloat(s[:len(s)-1], 64)
		return f / 100, false, err == nil
	case strings.HasSuffix(s, "s"):
		f, err := strconv.ParseFloat(s[:len(s)-1], 64)
		return f, true, err == nil
	default:
		f, err := strconv.ParseFloat(s, 64)
		return f, false, err == nil
	}
}

func parseModCell(c string, ln int) (RowMod, error) {
	if c == "-" {
		return RowMod{}, fmt.Errorf("line %d: bare `-` is an empty window — write nothing instead", ln)
	}
	if c == "" {
		return RowMod{}, fmt.Errorf("line %d: empty modifier cell", ln)
	}
	fields := strings.Fields(c)
	if len(fields) == 1 {
		if easeNames[c] {
			return RowMod{Kind: "ease", Name: c}, nil
		}
		if m := winRe.FindStringSubmatch(c); m != nil && strings.Contains(c, "-") {
			a, asec, ok1 := parseWinBound(m[1])
			b, bsec, ok2 := parseWinBound(m[2])
			if ok1 && ok2 {
				return RowMod{Kind: "win", A: a, B: b, ASec: asec, BSec: bsec}, nil
			}
		}
		// a single duration cell: `0s` — window from 0 to that time
		if m := durRe.FindStringSubmatch(c); m != nil {
			v, _ := strconv.ParseFloat(m[1], 64)
			return RowMod{Kind: "win", A: 0, B: v, ASec: true, BSec: true}, nil
		}
		return RowMod{}, fmt.Errorf("line %d: unknown modifier %q", ln, c)
	}
	switch fields[0] {
	case "after", "lag", "stagger":
		if len(fields) != 2 {
			return RowMod{}, fmt.Errorf("line %d: bad modifier %q", ln, c)
		}
		m := timeAmtRe.FindStringSubmatch(fields[1])
		if m == nil {
			return RowMod{}, fmt.Errorf("line %d: bad amount in %q", ln, c)
		}
		v, _ := strconv.ParseFloat(m[1], 64)
		return RowMod{Kind: fields[0], D: v, DSec: m[2] == "s"}, nil
	case "by":
		if len(fields) != 2 || fields[1] != "name" && fields[1] != "pos" {
			return RowMod{}, fmt.Errorf("line %d: bad pairing modifier %q", ln, c)
		}
		return RowMod{Kind: "pair", Name: fields[1]}, nil
	}
	return RowMod{}, fmt.Errorf("line %d: unknown modifier %q", ln, c)
}

func parseOpCell(c string, ln int) (RowOp, error) {
	if c == "" {
		return RowOp{}, fmt.Errorf("line %d: empty op cell", ln)
	}
	switch {
	case strings.HasPrefix(c, "write "):
		lhs, err := ParseExpr(strings.TrimSpace(c[6:]))
		if err != nil {
			return RowOp{}, fmt.Errorf("line %d: %v", ln, err)
		}
		return RowOp{Kind: "write", LHS: lhs}, nil
	case strings.HasPrefix(c, "fade_in "):
		rest := strings.TrimSpace(c[8:])
		arg := ""
		if i := strings.Index(rest, " from "); i >= 0 {
			arg = strings.TrimSpace(rest[i+6:])
			rest = strings.TrimSpace(rest[:i])
		}
		lhs, err := ParseExpr(rest)
		if err != nil {
			return RowOp{}, fmt.Errorf("line %d: %v", ln, err)
		}
		return RowOp{Kind: "fade_in", LHS: lhs, Arg: arg}, nil
	case strings.HasPrefix(c, "morph "):
		rest := strings.TrimSpace(c[6:])
		i := strings.Index(rest, "->")
		if i < 0 {
			return RowOp{}, fmt.Errorf("line %d: morph needs `a -> b`", ln)
		}
		lhs, err := ParseExpr(strings.TrimSpace(rest[:i]))
		if err != nil {
			return RowOp{}, fmt.Errorf("line %d: %v", ln, err)
		}
		rhs, err := ParseExpr(strings.TrimSpace(rest[i+2:]))
		if err != nil {
			return RowOp{}, fmt.Errorf("line %d: %v", ln, err)
		}
		return RowOp{Kind: "morph", LHS: lhs, RHS: rhs}, nil
	}
	i := strings.Index(c, "->")
	if i < 0 {
		return RowOp{}, fmt.Errorf("line %d: op cell is neither a verb nor an arrow: %q", ln, c)
	}
	lhs, err := ParseExpr(strings.TrimSpace(c[:i]))
	if err != nil {
		return RowOp{}, fmt.Errorf("line %d: %v", ln, err)
	}
	rhs, err := ParseExpr(strings.TrimSpace(c[i+2:]))
	if err != nil {
		return RowOp{}, fmt.Errorf("line %d: %v", ln, err)
	}
	return RowOp{Kind: "arrow", LHS: lhs, RHS: rhs}, nil
}
