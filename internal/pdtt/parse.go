package pdtt

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

// FamilyStmt is a plural-record family declared with the domain-binder syntax:
//
//	NAME[domainExpr as bindVar]:
//	  local: expr
//	  TYPE memberName:
//	    field: expr
//	  ...
type FamilyStmt struct {
	Name       string
	DomainE    Expr   // e.g. val.indices or 0..n
	BindVar    string // e.g. "i"
	Locals     []FamilyLocalBinding
	Members    []RecordStmt
	Line       int
	baseIndent int // detected from first indented line (parser-internal)
}

type FamilyLocalBinding struct {
	Name string
	E    Expr
	Line int
}

type CaptureStmt struct {
	Name string
	E    Expr
	Live bool // true for colon-binding (`name: expr`) — re-evaluated each frame
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
	ForE       Expr   // row source: list / range(n) / record name
	ForVar     string // bound variable name for domain-binder `NAME[domain as i]:`
	Fields     []FieldDef
	Line       int
}

type RowMod struct {
	Kind string  // "win" "after" "lag" "stagger" "ease" "pair" "transition"
	A, B float64 // window bounds; NaN = absent side
	ASec bool    // bound/offset spelled in seconds
	BSec bool
	D    float64 // after/lag/stagger amount
	DSec bool
	Name string // ease name, pairing mode, or transition name
}

type RowOp struct {
	Kind       string // "arrow" | "enter"
	LHS        Expr
	RHS        Expr
	Transition string // optional transition prefix in op cell

	// enter: `obj{field: expr, …} -> obj` — a self-transition. Snap the named
	// object to a phantom copy with these fields overridden, then tween every
	// overridden field back to the object's declared value over the window.
	EnterName string
	Overrides []EnterField
}

type EnterField struct {
	Name string
	Val  Expr
}

type Row struct {
	Mods []RowMod
	Op   RowOp
	Line int
}

type BlockStmt struct {
	DurS    float64
	Each    string // record name for `each` headers
	As      string
	DefMods []RowMod // header default modifiers, applied to every row in the block
	Rows    []Row
	Line    int
	Inline  bool // true when the header line also carried the first row edit
}

var (
	recordRe    = regexp.MustCompile(`^(\w+) (\w+):$`)
	captureRe   = regexp.MustCompile(`^(\w+)\s*=\s*(.+)$`)
	colonBindRe = regexp.MustCompile(`^(\w+)\s*:\s*(.+)$`) // name: expr  (top-level colon binding)
	durRe       = regexp.MustCompile(`^(\d*\.?\d+)(s|%)?$`)
	winRe       = regexp.MustCompile(`^(\d*\.?\d+(?:%|s)?)?-(\d*\.?\d+(?:%|s)?)?$`)
	foldRe      = regexp.MustCompile(`^scan\((.+) by (\w+)\)$`)
	timeAmtRe   = regexp.MustCompile(`^(\d*\.?\d+)(s?)$`)
)

var easeNames = map[string]bool{
	"linear": true, "smooth": true, "ease_in": true, "ease_out": true, "ease_in_out": true,
}

var transitionNames = map[string]bool{
	"morph":   true,
	"fade_in": true,
	"draw":    true,
	"write":   true,
}

func parseDurToken(tok string) (float64, bool, error) {
	m := durRe.FindStringSubmatch(tok)
	if m == nil {
		return 0, false, fmt.Errorf("bad duration %q", tok)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false, err
	}
	switch m[2] {
	case "s":
		return v, true, nil
	case "%":
		return v / 100, false, nil
	default:
		return v, false, nil
	}
}

// parseCtorRecordHeader parses constructor-style record headers such as
// text("a) b") label: using balanced parentheses inside the argument list.
func parseCtorRecordHeader(trimmed string) (typ, argText, name string, ok bool) {
	i := strings.IndexByte(trimmed, '(')
	if i <= 0 {
		return "", "", "", false
	}
	typ = trimmed[:i]
	if typ != "text" && typ != "typst" {
		return "", "", "", false
	}
	close, found := balancedClosingParen(trimmed, i)
	if !found {
		return "", "", "", false
	}
	argText = strings.TrimSpace(trimmed[i+1 : close])
	rest := strings.TrimSpace(trimmed[close+1:])
	if !strings.HasSuffix(rest, ":") {
		return "", "", "", false
	}
	name = strings.TrimSpace(strings.TrimSuffix(rest, ":"))
	if name == "" || strings.ContainsAny(name, " \t(") {
		return "", "", "", false
	}
	return typ, argText, name, true
}

// parseFamilyHeaderShape parses NAME[domainExpr as bindVar]: using
// bracket-balanced scanning so domain expressions may contain nested
// brackets, parentheses, or string literals safely.
func parseFamilyHeaderShape(trimmed string) (name, domainText, bindVar string, ok bool) {
	if !strings.HasSuffix(trimmed, ":") {
		return "", "", "", false
	}
	withoutColon := strings.TrimSpace(strings.TrimSuffix(trimmed, ":"))
	open := strings.IndexByte(withoutColon, '[')
	if open <= 0 {
		return "", "", "", false
	}
	name = strings.TrimSpace(withoutColon[:open])
	if name == "" || strings.ContainsAny(name, " \t") {
		return "", "", "", false
	}
	close, found := balancedClosingDelimiter(withoutColon, open, '[', ']')
	if !found || close != len(withoutColon)-1 {
		return "", "", "", false
	}
	domainText, bindVar, ok = splitDomainBindAtDepth0(withoutColon[open+1 : close])
	if !ok {
		return "", "", "", false
	}
	return name, domainText, bindVar, true
}

func splitDomainBindAtDepth0(inner string) (domain, bind string, ok bool) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return "", "", false
	}
	depth := 0
	inStr := false
	esc := false
	lastAS := -1
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		default:
			if depth == 0 && i+4 <= len(inner) && inner[i:i+4] == " as " {
				lastAS = i
				i += 3
			}
		}
	}
	if lastAS < 0 {
		return "", "", false
	}
	domain = strings.TrimSpace(inner[:lastAS])
	bind = strings.TrimSpace(inner[lastAS+4:])
	if domain == "" || bind == "" || strings.ContainsAny(bind, " \t") {
		return "", "", false
	}
	return domain, bind, true
}

func balancedClosingDelimiter(s string, open int, openCh, closeCh byte) (close int, ok bool) {
	if open < 0 || open >= len(s) || s[open] != openCh {
		return 0, false
	}
	depth := 0
	inStr := false
	esc := false
	for i := open; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func balancedClosingParen(s string, open int) (close int, ok bool) {
	return balancedClosingDelimiter(s, open, '(', ')')
}

func parseCtorRecord(trimmed string, ln int) (*RecordStmt, error) {
	typ, argText, name, ok := parseCtorRecordHeader(trimmed)
	if !ok {
		return nil, nil
	}
	e, err := ParseExpr(argText)
	if err != nil {
		return nil, fmt.Errorf("line %d: %v", ln, err)
	}
	return &RecordStmt{
		Type: typ,
		Name: name,
		Fields: []FieldDef{{
			Name: "text",
			E:    e,
			Line: ln,
		}},
		Line: ln,
	}, nil
}

// looksLikeFlatFieldLine reports whether a column-0 line resembles a record
// field (`name: expr`, optionally prefixed with rate/set) rather than another
// top-level form such as a record header or family binder.
func looksLikeFlatFieldLine(trimmed string) bool {
	s := trimmed
	if strings.HasPrefix(s, "rate ") {
		s = strings.TrimSpace(s[5:])
	} else if strings.HasPrefix(s, "set ") {
		s = strings.TrimSpace(s[4:])
	}
	if recordRe.MatchString(s) {
		return false
	}
	if _, _, _, ok := parseFamilyHeaderShape(s); ok {
		return false
	}
	if _, _, _, ok := parseCtorRecordHeader(s); ok {
		return false
	}
	return colonBindRe.MatchString(s)
}

func parseFamilyLocalBinding(trimmed string, ln int) (*FamilyLocalBinding, error) {
	m := colonBindRe.FindStringSubmatch(trimmed)
	if m == nil {
		return nil, nil
	}
	e, err := ParseExpr(m[2])
	if err != nil {
		return nil, fmt.Errorf("line %d: %v", ln, err)
	}
	return &FamilyLocalBinding{Name: m[1], E: e, Line: ln}, nil
}

func familyMemberHeaderError(ln int, trimmed string) error {
	return fmt.Errorf(
		"line %d: expected member record header or local binding inside family block (use `name: expr` for locals or `type name:` for member records), got %q",
		ln, trimmed,
	)
}

func ParseFile(src string) ([]Stmt, error) {
	sc, err := ScanSource(src)
	if err != nil {
		return nil, err
	}
	lines := sc.Lines()
	var stmts []Stmt
	var curRecord *RecordStmt
	var curBlock *BlockStmt
	var curFamily *FamilyStmt
	var curFamilyMember *RecordStmt // current member record inside a family
	flushFamilyMember := func() {
		if curFamilyMember != nil {
			curFamily.Members = append(curFamily.Members, *curFamilyMember)
			curFamilyMember = nil
		}
	}
	flushFamily := func() {
		if curFamily != nil {
			flushFamilyMember()
			stmts = append(stmts, *curFamily)
			curFamily = nil
		}
	}
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

	for _, ll := range lines {
		ln := ll.Line
		line := ll.Text
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushRecord()
			// Don't flush a family on blank lines — blank lines appear between members.
			// The family is flushed when a non-indented, non-blank line is seen.
			if curFamily == nil {
				flushBlock()
			}
			continue
		}
		depth := ll.Indent
		indented := depth > 0

		// Inside a family: "shallow" indent = member header, "deeper" indent = member field
		// We detect the family's base indent level from the first indented line.
		if curFamily != nil {
			if !indented {
				// End of family block
				flushFamily()
				// fall through to top-level handling below
			} else if curFamily.baseIndent == 0 {
				// First indented line in this family: set the base indent level
				curFamily.baseIndent = depth
				// This line is either a family-local binding or a member header.
				flushFamilyMember()
				if local, err := parseFamilyLocalBinding(trimmed, ln); err != nil {
					return nil, err
				} else if local != nil {
					curFamily.Locals = append(curFamily.Locals, *local)
				} else if rec, err := parseCtorRecord(trimmed, ln); err != nil {
					return nil, err
				} else if rec != nil {
					curFamilyMember = rec
				} else if m := recordRe.FindStringSubmatch(trimmed); m != nil {
					curFamilyMember = &RecordStmt{Type: m[1], Name: m[2], Line: ln}
				} else {
					return nil, familyMemberHeaderError(ln, trimmed)
				}
				continue
			} else if depth == curFamily.baseIndent {
				// Same level as member headers: a new local binding or member header.
				flushFamilyMember()
				if local, err := parseFamilyLocalBinding(trimmed, ln); err != nil {
					return nil, err
				} else if local != nil {
					curFamily.Locals = append(curFamily.Locals, *local)
				} else if rec, err := parseCtorRecord(trimmed, ln); err != nil {
					return nil, err
				} else if rec != nil {
					curFamilyMember = rec
				} else if m := recordRe.FindStringSubmatch(trimmed); m != nil {
					curFamilyMember = &RecordStmt{Type: m[1], Name: m[2], Line: ln}
				} else {
					return nil, familyMemberHeaderError(ln, trimmed)
				}
				continue
			} else {
				// depth > baseIndent: member field
				if curFamilyMember == nil {
					return nil, fmt.Errorf("line %d: indented field line without a member header in family", ln)
				}
				fd, err := parseFieldLine(trimmed, ln)
				if err != nil {
					return nil, err
				}
				curFamilyMember.Fields = append(curFamilyMember.Fields, fd)
				continue
			}
		}

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
			if curBlock != nil && curBlock.Inline && isInlineBlockHeader(body, curBlock) {
				flushBlock()
			}
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

		if curRecord != nil && looksLikeFlatFieldLine(trimmed) {
			return nil, fmt.Errorf(
				"line %d: record field %q must be indented under the record header (use an indented field line, not column-0 `name: expr`)",
				ln, trimmed,
			)
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
			// family binder: NAME[domain as i]:
			if name, domainText, bindVar, ok := parseFamilyHeaderShape(trimmed); ok {
				domainE, err := ParseExpr(domainText)
				if err != nil {
					return nil, fmt.Errorf("line %d: family domain: %v", ln, err)
				}
				curFamily = &FamilyStmt{
					Name:    name,
					DomainE: domainE,
					BindVar: bindVar,
					Line:    ln,
				}
				continue
			}
			if rec, err := parseCtorRecord(trimmed, ln); err != nil {
				return nil, err
			} else if rec != nil {
				curRecord = rec
				continue
			}
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
			// top-level colon binding: `name: expr` (live global, equivalent to `name = expr` but live)
			// Exception: if the RHS is a `snapshot expr`, treat as frozen (one-time capture).
			if m := colonBindRe.FindStringSubmatch(trimmed); m != nil {
				e, err := ParseExpr(m[2])
				if err != nil {
					return nil, fmt.Errorf("line %d: %v", ln, err)
				}
				_, isSnapshot := e.(SnapshotE)
				stmts = append(stmts, CaptureStmt{Name: m[1], E: e, Live: !isSnapshot, Line: ln})
				continue
			}
			return nil, fmt.Errorf("line %d: unrecognized top-level form: %q", ln, trimmed)
		}
	}
	flushRecord()
	flushFamily()
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

// parseBlockHeader parses a block header line. The first `|`-cell carries the
// clock (`4s` or `each record …`); every following `|`-cell is a default
// modifier applied to all rows in the block (e.g. `| 4s | linear`). Per-row
// modifiers override the defaults. The legacy `| 4s linear` whitespace form is
// still accepted but the piped form is canonical.
func parseBlockHeader(body string, ln int) (*BlockStmt, error) {
	b := &BlockStmt{Line: ln}
	cells := splitCells(body)
	fields := strings.Fields(cells[0])
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
		d, _, err := parseDurToken(rest[0])
		if err != nil {
			return nil, fmt.Errorf("line %d: bad each duration %q", ln, rest[0])
		}
		b.DurS = d
		if len(rest) > 1 {
			return nil, fmt.Errorf("line %d: fast_after is not implemented in this prototype", ln)
		}
	} else {
		d, _, err := parseDurToken(fields[0])
		if err != nil {
			return nil, fmt.Errorf("line %d: bad block duration %q", ln, fields[0])
		}
		b.DurS = d
		// legacy whitespace form `| 4s linear`: a trailing ease becomes a default.
		for _, extra := range fields[1:] {
			if !easeNames[extra] {
				return nil, fmt.Errorf("line %d: trailing token %q in block header — put modifiers in their own `|` cell (`| %s | %s`)", ln, extra, fields[0], extra)
			}
			b.DefMods = append(b.DefMods, RowMod{Kind: "ease", Name: extra})
		}
	}
	// Remaining cells: default modifiers, plus an optional inline first row
	// (a cell carrying the tween `->`). Same classification as any `|` line.
	defMods, op, err := parseCells(cells[1:], ln)
	if err != nil {
		return nil, err
	}
	b.DefMods = append(b.DefMods, defMods...)
	if op != nil {
		b.Rows = append(b.Rows, Row{Line: ln, Op: *op})
		b.Inline = true
	}
	return b, nil
}

// splitCells splits a `|`-delimited line body into trimmed cells.
func splitCells(body string) []string {
	cells := strings.Split(body, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func isInlineBlockHeader(body string, curBlock *BlockStmt) bool {
	cells := splitCells(body)
	if len(cells) < 2 {
		return false
	}
	fields := strings.Fields(cells[0])
	if len(fields) == 0 {
		return false
	}
	if fields[0] != "each" {
		if _, _, err := parseDurToken(fields[0]); err != nil {
			return false
		}
	}
	if !hasEditCell(cells[1:]) {
		return false
	}
	for _, c := range cells[1 : len(cells)-1] {
		if transitionNames[c] {
			return true
		}
	}
	for _, mod := range curBlock.DefMods {
		if mod.Kind == "transition" {
			return true
		}
	}
	return false
}

func hasEditCell(cells []string) bool {
	for _, c := range cells {
		if strings.Contains(c, "->") {
			return true
		}
	}
	return false
}

// parseCells classifies a list of `|`-cells into leading modifiers and an
// optional trailing edit. A cell containing the tween `->` is the edit and
// must be the last cell on the line; every other cell is a modifier. This is
// the one rule shared by block headers and rows.
func parseCells(cells []string, ln int) ([]RowMod, *RowOp, error) {
	var mods []RowMod
	for i, c := range cells {
		if c == "" {
			return nil, nil, fmt.Errorf("line %d: empty cell", ln)
		}
		if strings.Contains(c, "->") {
			if i != len(cells)-1 {
				return nil, nil, fmt.Errorf("line %d: the `->` edit must be the last cell on the line", ln)
			}
			op, err := parseOpCell(c, ln)
			if err != nil {
				return nil, nil, err
			}
			return mods, &op, nil
		}
		mod, err := parseModCell(c, ln)
		if err != nil {
			return nil, nil, err
		}
		mods = append(mods, mod)
	}
	return mods, nil, nil
}

func parseRow(body string, ln int) (Row, error) {
	mods, op, err := parseCells(splitCells(body), ln)
	if err != nil {
		return Row{}, err
	}
	if op == nil {
		return Row{}, fmt.Errorf("line %d: row has no `->` edit", ln)
	}
	return Row{Mods: mods, Op: *op, Line: ln}, nil
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
		if transitionNames[c] {
			return RowMod{Kind: "transition", Name: c}, nil
		}
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
			v, sec, err := parseDurToken(c)
			if err == nil {
				return RowMod{Kind: "win", A: 0, B: v, ASec: sec, BSec: sec}, nil
			}
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
	transition := ""
	fields := strings.Fields(c)
	if len(fields) > 0 && transitionNames[fields[0]] {
		transition = fields[0]
		c = strings.TrimSpace(c[len(fields[0]):])
	}
	i := strings.Index(c, "->")
	if i < 0 {
		return RowOp{}, fmt.Errorf("line %d: op cell must contain `->`: %q", ln, c)
	}
	lhsText := strings.TrimSpace(c[:i])
	rhsText := strings.TrimSpace(c[i+2:])
	if rhsText == "gone" {
		return RowOp{}, fmt.Errorf("line %d: `gone` is not supported; tween an explicit field such as opacity instead", ln)
	}
	if strings.Contains(lhsText, "{") || strings.Contains(lhsText, "}") {
		name, overrides, err := parseUpdateExpr(lhsText, ln)
		if err != nil {
			return RowOp{}, err
		}
		// For broadcast enter tweens (`roots[* as i].mark{...} -> roots[i].mark`),
		// the names won't match exactly. Allow them through if the LHS contains `[*`.
		if rhsText != name && !strings.Contains(name, "[*") {
			return RowOp{}, fmt.Errorf("line %d: object update tween is a self-transition; both sides must name the same object (`%s{...} -> %s`), got %q", ln, name, name, rhsText)
		}
		if len(overrides) == 0 {
			return RowOp{}, fmt.Errorf("line %d: object update tween needs at least one overridden field, e.g. `%s{opacity: 0} -> %s`", ln, name, name)
		}
		// For broadcast: store the full LHS path (with `[*`) as the enter name.
		// The RHS is stored separately in LHS/RHS of RowOp for broadcast resolution.
		if strings.Contains(name, "[*") {
			// Parse name as LHS expr and rhsText as RHS expr for broadcast enter
			lhsE, err2 := ParseExpr(name)
			if err2 != nil {
				return RowOp{}, fmt.Errorf("line %d: %v", ln, err2)
			}
			rhsE, err2 := ParseExpr(rhsText)
			if err2 != nil {
				return RowOp{}, fmt.Errorf("line %d: %v", ln, err2)
			}
			return RowOp{Kind: "enter_broadcast", LHS: lhsE, RHS: rhsE, Overrides: overrides}, nil
		}
		return RowOp{Kind: "enter", EnterName: name, Overrides: overrides}, nil
	}
	lhs, err := ParseExpr(lhsText)
	if err != nil {
		return RowOp{}, fmt.Errorf("line %d: %v", ln, err)
	}
	rhs, err := ParseExpr(rhsText)
	if err != nil {
		return RowOp{}, fmt.Errorf("line %d: %v", ln, err)
	}
	return RowOp{Kind: "arrow", LHS: lhs, RHS: rhs, Transition: transition}, nil
}

// parseUpdateExpr parses an update expression `name{field: expr, …}`.
func parseUpdateExpr(s string, ln int) (string, []EnterField, error) {
	open := strings.IndexByte(s, '{')
	if open < 0 || !strings.HasSuffix(s, "}") {
		return "", nil, fmt.Errorf("line %d: object update tween left side must be `obj{field: expr, ...}`, got %q", ln, s)
	}
	name := strings.TrimSpace(s[:open])
	if name == "" {
		return "", nil, fmt.Errorf("line %d: object update tween left side has no object name: %q", ln, s)
	}
	var fields []EnterField
	for _, part := range splitTopLevel(s[open+1 : len(s)-1]) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		ci := strings.IndexByte(part, ':')
		if ci < 0 {
			return "", nil, fmt.Errorf("line %d: bad override %q (want `field: expr`)", ln, part)
		}
		key := strings.TrimSpace(part[:ci])
		ex, err := ParseExpr(strings.TrimSpace(part[ci+1:]))
		if err != nil {
			return "", nil, fmt.Errorf("line %d: %v", ln, err)
		}
		fields = append(fields, EnterField{Name: key, Val: ex})
	}
	return name, fields, nil
}

// splitTopLevel splits on commas that are not nested inside (), [], or {}.
func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}
