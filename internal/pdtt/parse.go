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
	SceneStmt    struct{ Name string }
	SceneDefStmt struct {
		Name string
		Body []Stmt
		Line int
	}
	RunStmt struct {
		Name string
		Line int
	}
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
	Kind       string // "arrow" | "enter" | "exit" | "highlight"
	LHS        Expr   // arrow left side, or the subject of a verb cell (enter/exit/highlight)
	Subjects   []Expr // optional comma-separated verb subjects; defaults to LHS when empty
	RHS        Expr   // arrow right side
	Transition string // arrow transition (set by a `transition:NAME` modifier cell)

	// highlight (`highlight:wiggle | t.sub("x")`): the transient channel keyword.
	// It is driven through a 0→peak→0 envelope over the window and left at rest —
	// distinct from the persistent `->` arrow that sets-and-holds.
	Highlight string

	// enter/exit (`in:fade | title`, `ou:fade | title`): the named entrance/exit
	// preset. enter snaps the subject to the preset's hidden field values then
	// tweens back to the declared values; exit tweens declared→hidden and leaves.
	Preset string
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

// easeAlias maps the `ease:NAME` value spelled on a timeline to the canonical
// easing key in compile.go. The short spellings (`in`, `out`, …) are the surface
// vocabulary; `linear`/`smooth` pass through unchanged.
var easeAlias = map[string]string{
	"linear":    "linear",
	"smooth":    "smooth",
	"in":        "ease_in",
	"out":       "ease_out",
	"in_out":    "ease_in_out",
	"out_cubic": "ease_out_cubic",
}

var transitionNames = map[string]bool{
	"morph":   true,
	"fade_in": true,
	"draw":    true,
	"write":   true,
}

// highlightChannel maps a transient text-modifier keyword to the part channel it
// drives. `highlight:wiggle | t.sub("x")` animates that channel through a
// there-and-back envelope, unlike the persistent `->` arrow that sets-and-holds.
var highlightChannel = map[string]string{
	"flash":     "color",
	"strike":    "strike",
	"underline": "underline",
	"enlarge":   "scale",
	"wiggle":    "wiggle",
}

// modKeyVal splits a `key:value` modifier cell. ok is false when there is no
// colon (windows, durations, `stagger 0.03s`, `by name` carry none) or when
// either side is empty. This is the one gate that recognizes the unified
// `ease:`/`transition:`/`in:`/`ou:`/`highlight:` modifier family.
func modKeyVal(c string) (key, val string, ok bool) {
	i := strings.IndexByte(c, ':')
	if i <= 0 || i == len(c)-1 {
		return "", "", false
	}
	key, val = c[:i], c[i+1:]
	if strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	return key, val, true
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

// isCtorRecordHeader reports whether trimmed is a (no-longer-supported)
// constructor-style record header such as `text("a) b") label:`, using
// balanced parentheses so the argument list may contain a closing paren.
func isCtorRecordHeader(trimmed string) bool {
	i := strings.IndexByte(trimmed, '(')
	if i <= 0 {
		return false
	}
	typ := trimmed[:i]
	if typ != "text" && typ != "typst" {
		return false
	}
	close, found := balancedClosingParen(trimmed, i)
	if !found {
		return false
	}
	rest := strings.TrimSpace(trimmed[close+1:])
	if !strings.HasSuffix(rest, ":") {
		return false
	}
	name := strings.TrimSpace(strings.TrimSuffix(rest, ":"))
	return name != "" && !strings.ContainsAny(name, " \t(")
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

// ctorRecordError rejects the deprecated `text(...)`/`typst(...)` constructor
// header syntax with a helpful message; it returns nil for any other line.
func ctorRecordError(trimmed string, ln int) error {
	if isCtorRecordHeader(trimmed) {
		return fmt.Errorf(
			"line %d: text/typst constructor syntax is not supported; declare `text name:` or `typst name:` with a `text:` field",
			ln,
		)
	}
	return nil
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
	if isCtorRecordHeader(s) {
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
	return parseLogicalLines(sc.Lines())
}

func hasSceneDefs(lines []LogicalLine) bool {
	for _, ll := range lines {
		if _, ok := sceneDefHeader(ll); ok {
			return true
		}
	}
	return false
}

func sceneDefHeader(ll LogicalLine) (string, bool) {
	if ll.Indent != 0 {
		return "", false
	}
	trimmed := strings.TrimSpace(ll.Text)
	if !strings.HasPrefix(trimmed, "scene ") || !strings.HasSuffix(trimmed, ":") {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "scene "), ":"))
	return name, name != ""
}

func parseSceneDefLines(lines []LogicalLine) ([]Stmt, error) {
	var out []Stmt
	var main []LogicalLine
	flushMain := func() error {
		if len(main) == 0 {
			return nil
		}
		stmts, err := parseLogicalLines(main)
		if err != nil {
			return err
		}
		out = append(out, stmts...)
		main = nil
		return nil
	}

	for i := 0; i < len(lines); {
		name, ok := sceneDefHeader(lines[i])
		if !ok {
			main = append(main, lines[i])
			i++
			continue
		}
		if err := flushMain(); err != nil {
			return nil, err
		}
		start := lines[i]
		i++
		var bodyLines []LogicalLine
		for i < len(lines) {
			if _, next := sceneDefHeader(lines[i]); next {
				break
			}
			bodyLines = append(bodyLines, lines[i])
			i++
		}
		body, err := parseLogicalLines(bodyLines)
		if err != nil {
			return nil, err
		}
		out = append(out, SceneDefStmt{Name: name, Body: body, Line: start.Line})
	}
	if err := flushMain(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseLogicalLines(lines []LogicalLine) ([]Stmt, error) {
	if hasSceneDefs(lines) {
		return parseSceneDefLines(lines)
	}
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

	for i := 0; i < len(lines); i++ {
		ll := lines[i]
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
				} else if err := ctorRecordError(trimmed, ln); err != nil {
					return nil, err
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
				} else if err := ctorRecordError(trimmed, ln); err != nil {
					return nil, err
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
				fd, consumed, err := parseRecordFieldLine(lines, i, depth)
				if err != nil {
					return nil, err
				}
				curFamilyMember.Fields = append(curFamilyMember.Fields, fd)
				i += consumed
				continue
			}
		}

		if indented {
			if curRecord == nil {
				return nil, fmt.Errorf("line %d: indented line outside a record", ln)
			}
			fd, consumed, err := parseRecordFieldLine(lines, i, depth)
			if err != nil {
				return nil, err
			}
			curRecord.Fields = append(curRecord.Fields, fd)
			i += consumed
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
		case strings.HasPrefix(trimmed, "run "):
			name := strings.TrimSpace(trimmed[4:])
			if name == "" {
				return nil, fmt.Errorf("line %d: run needs a scene name", ln)
			}
			stmts = append(stmts, RunStmt{Name: name, Line: ln})
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
			if err := ctorRecordError(trimmed, ln); err != nil {
				return nil, err
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

// parseRecordFieldLine parses one record field, consuming extra lines for
// multiline geometry constructors under `points:`.
func parseRecordFieldLine(lines []LogicalLine, i int, parentDepth int) (FieldDef, int, error) {
	ll := lines[i]
	trimmed := strings.TrimSpace(ll.Text)
	colon := strings.IndexByte(trimmed, ':')
	if colon < 0 {
		return FieldDef{}, 0, fmt.Errorf("line %d: field line missing `:`", ll.Line)
	}
	fieldName := strings.TrimSpace(trimmed[:colon])
	body := strings.TrimSpace(trimmed[colon+1:])
	if body == "" && i+1 < len(lines) && lines[i+1].Indent > parentDepth {
		fd, consumed, err := parseMultilineGeomField(lines, i, parentDepth, fieldName)
		if err != nil {
			return FieldDef{}, 0, err
		}
		return fd, consumed, nil
	}
	fd, err := parseFieldLine(trimmed, ll.Line)
	return fd, 0, err
}

func parseMultilineGeomField(lines []LogicalLine, i int, parentDepth int, fieldName string) (FieldDef, int, error) {
	start := lines[i]
	if i+1 >= len(lines) {
		return FieldDef{}, 0, fmt.Errorf("line %d: %s: expected geometry constructor", start.Line, fieldName)
	}
	header := lines[i+1]
	if header.Indent <= parentDepth {
		return FieldDef{}, 0, fmt.Errorf("line %d: %s: expected indented geometry constructor", start.Line, fieldName)
	}
	geomDepth := header.Indent
	geomTrimmed := strings.TrimSpace(header.Text)
	geomColon := strings.IndexByte(geomTrimmed, ':')
	if geomColon < 0 {
		return FieldDef{}, 0, fmt.Errorf("line %d: geometry constructor header missing `:`", header.Line)
	}
	geomName := strings.TrimSpace(geomTrimmed[:geomColon])
	if geomName == "" {
		return FieldDef{}, 0, fmt.Errorf("line %d: empty geometry constructor name", header.Line)
	}
	geomInline := strings.TrimSpace(geomTrimmed[geomColon+1:])
	if geomInline != "" {
		return FieldDef{}, 0, fmt.Errorf("line %d: geometry constructor %q: use indented fields, not inline body", header.Line, geomName)
	}
	var geomFields []FieldDef
	j := i + 2
	for j < len(lines) && lines[j].Indent > geomDepth {
		fd, err := parseFieldLine(strings.TrimSpace(lines[j].Text), lines[j].Line)
		if err != nil {
			return FieldDef{}, 0, err
		}
		geomFields = append(geomFields, fd)
		j++
	}
	if len(geomFields) == 0 {
		return FieldDef{}, 0, fmt.Errorf("line %d: geometry constructor %q has no fields", header.Line, geomName)
	}
	return FieldDef{
		Name: fieldName,
		E:    GeomE{Name: geomName, Fields: geomFields},
		Line: start.Line,
	}, j - i - 1, nil
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
		if len(fields) > 1 {
			return nil, fmt.Errorf("line %d: trailing token %q in block header — put modifiers in their own `|` cell (`| %s | ease:smooth`)", ln, fields[1], fields[0])
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

// isInlineBlockHeader reports whether a `|`-line following an inline block header
// starts a new sequential block rather than adding a windowed row to the current
// one. A leading duration only reads as a sub-window when it is strictly shorter
// than the current block; when it is `>=` the block's length it cannot fit as a
// window (e.g. consecutive `| 1s | … -> …` lines) so it opens a new block. A
// transition cell, or a transition default on the block, also forces a new block.
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
		d, _, err := parseDurToken(fields[0])
		if err != nil {
			return false
		}
		// A leading absolute duration that can't fit as a sub-window of the
		// current block opens a new sequential block. Percent windows are
		// relative, so they always read as a sub-window and never split.
		if !strings.HasSuffix(fields[0], "%") && d >= curBlock.DurS {
			return true
		}
	}
	if !hasEditCell(cells[1:]) {
		return false
	}
	for _, c := range cells[1 : len(cells)-1] {
		if k, _, ok := modKeyVal(c); ok && k == "transition" {
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
	// Subject-verb row: an `in:`/`ou:`/`highlight:` cell takes the last cell as
	// its subject and carries no `->`. e.g. `in:fade | title`, `highlight:wiggle |
	// t.sub("x")`. Everything else is an ordinary modifier (ease, window, …).
	if vi := subjectVerbCellIndex(cells); vi >= 0 && !hasEditCell(cells) {
		return parseSubjectVerbCells(cells, vi, ln)
	}
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

// subjectVerbCellIndex returns the index of the first `in:`/`ou:`/`highlight:`
// cell, or -1 if none. These verbs name an action on a subject given in the last
// cell rather than a `->` edit.
func subjectVerbCellIndex(cells []string) int {
	for i, c := range cells {
		if k, _, ok := modKeyVal(c); ok {
			switch k {
			case "in", "ou", "highlight":
				return i
			}
		}
	}
	return -1
}

// parseSubjectVerbCells builds an enter/exit/highlight RowOp: the verb cell at
// viIdx names the preset or channel, the last cell is the subject span/object,
// and any remaining cells (ease, window, …) are ordinary modifiers.
func parseSubjectVerbCells(cells []string, viIdx, ln int) ([]RowMod, *RowOp, error) {
	last := len(cells) - 1
	if viIdx >= last {
		return nil, nil, fmt.Errorf("line %d: %q needs a subject cell, e.g. `%s | title`", ln, cells[viIdx], cells[viIdx])
	}
	verbKey, verbVal, _ := modKeyVal(cells[viIdx])
	var mods []RowMod
	for i, c := range cells {
		if i == viIdx || i == last {
			continue
		}
		if c == "" {
			return nil, nil, fmt.Errorf("line %d: empty cell", ln)
		}
		if j := subjectVerbCellIndex([]string{c}); j == 0 {
			return nil, nil, fmt.Errorf("line %d: only one verb per row (%q and %q)", ln, cells[viIdx], c)
		}
		mod, err := parseModCell(c, ln)
		if err != nil {
			return nil, nil, err
		}
		mods = append(mods, mod)
	}
	subjects, err := parseSubjectList(cells[last])
	if err != nil {
		return nil, nil, fmt.Errorf("line %d: %v", ln, err)
	}
	subject := subjects[0]
	switch verbKey {
	case "highlight":
		if _, ok := highlightChannel[verbVal]; !ok {
			return nil, nil, fmt.Errorf("line %d: unknown highlight channel %q", ln, verbVal)
		}
		return mods, &RowOp{Kind: "highlight", LHS: subject, Subjects: subjects, Highlight: verbVal}, nil
	default: // "in" / "ou"
		if _, ok := entrancePresets[verbVal]; !ok {
			return nil, nil, fmt.Errorf("line %d: unknown %s: preset %q", ln, verbKey, verbVal)
		}
		kind := "enter"
		if verbKey == "ou" {
			kind = "exit"
		}
		return mods, &RowOp{Kind: kind, LHS: subject, Subjects: subjects, Preset: verbVal}, nil
	}
}

func parseSubjectList(cell string) ([]Expr, error) {
	parts, err := splitTopLevelCommas(cell)
	if err != nil {
		return nil, err
	}
	out := make([]Expr, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("empty subject in comma list")
		}
		e, err := ParseExpr(part)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func splitTopLevelCommas(s string) ([]string, error) {
	var parts []string
	start := 0
	depth := 0
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
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
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced subject list")
			}
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if inStr || depth != 0 {
		return nil, fmt.Errorf("unbalanced subject list")
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts, nil
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
	if key, val, ok := modKeyVal(c); ok {
		switch key {
		case "ease":
			canon, ok := easeAlias[val]
			if !ok {
				return RowMod{}, fmt.Errorf("line %d: unknown ease %q", ln, val)
			}
			return RowMod{Kind: "ease", Name: canon}, nil
		case "transition":
			if !transitionNames[val] {
				return RowMod{}, fmt.Errorf("line %d: unknown transition %q", ln, val)
			}
			return RowMod{Kind: "transition", Name: val}, nil
		case "in", "ou", "highlight":
			return RowMod{}, fmt.Errorf("line %d: %q needs a subject cell (`%s | subject`)", ln, c, c)
		default:
			return RowMod{}, fmt.Errorf("line %d: unknown modifier %q", ln, c)
		}
	}
	fields := strings.Fields(c)
	if len(fields) == 1 {
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
	i := strings.Index(c, "->")
	if i < 0 {
		return RowOp{}, fmt.Errorf("line %d: op cell must contain `->`: %q", ln, c)
	}
	lhsText := strings.TrimSpace(c[:i])
	rhsText := strings.TrimSpace(c[i+2:])
	if rhsText == "gone" {
		return RowOp{}, fmt.Errorf("line %d: `gone` is not supported; use `ou:fade | obj` to dismiss an object", ln)
	}
	if strings.ContainsAny(lhsText, "{}") {
		return RowOp{}, fmt.Errorf("line %d: object-update entry syntax is gone; use `in:fade | obj` (presets: draw, fade, pop, draw_fade)", ln)
	}
	lhs, err := ParseExpr(lhsText)
	if err != nil {
		return RowOp{}, fmt.Errorf("line %d: %v", ln, err)
	}
	rhs, err := ParseExpr(rhsText)
	if err != nil {
		return RowOp{}, fmt.Errorf("line %d: %v", ln, err)
	}
	return RowOp{Kind: "arrow", LHS: lhs, RHS: rhs}, nil
}
