package pdtt

import (
	"fmt"
	"regexp"
	"strings"
)

// LogicalLine is one logical source line after comment stripping and
// delimiter-aware continuation joining.
type LogicalLine struct {
	Text   string
	Line   int // 1-based line where this logical line started
	Indent int // leading spaces/tabs on the starting physical line
}

// Scanner produces logical lines from raw PDTT source.
type Scanner struct {
	lines []LogicalLine
}

// ScanSource tokenizes source into logical lines. Lines are joined while a
// delimiter ((), [], {}) or a string literal stays open, so a string may span
// several physical lines — the line breaks are kept verbatim inside the literal.
// Returns an error when a delimiter or string is still open at EOF.
func ScanSource(src string) (*Scanner, error) {
	rawLines := strings.Split(src, "\n")
	out := make([]LogicalLine, 0, len(rawLines))
	var b strings.Builder
	startLine := 0
	startIndent := 0
	depth := 0
	inStr := false

	for n := 0; n < len(rawLines); n++ {
		raw := rawLines[n]
		ln := n + 1
		if !inStr && depth == 0 {
			if ll, next, ok := scanBlockScalar(rawLines, n); ok {
				out = append(out, ll)
				n = next - 1
				continue
			}
		}
		startedInStr := inStr
		text, delta, endInStr := scanLine(raw, inStr)
		depth += delta
		inStr = endInStr

		switch {
		case startedInStr:
			// Continuation of a string literal opened on an earlier line: keep
			// the newline and the text verbatim so the literal's content is exact.
			b.WriteByte('\n')
			b.WriteString(text)
		case depth-delta == 0:
			// Start of a fresh logical line (the depth before this line was 0).
			startLine = ln
			startIndent = leadingIndent(raw)
			b.Reset()
			b.WriteString(strings.TrimRight(text, " \t"))
		default:
			// Continuation inside open ()/[]/{} delimiters: trim and space-join.
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				b.WriteByte(' ')
				b.WriteString(trimmed)
			}
		}

		if depth <= 0 && !inStr {
			out = append(out, LogicalLine{
				Text:   b.String(),
				Line:   startLine,
				Indent: startIndent,
			})
			depth = 0
		}
	}
	if depth > 0 || inStr {
		return nil, fmt.Errorf("line %d: unclosed delimiter at end of file", startLine)
	}
	return &Scanner{lines: out}, nil
}

// scanLine processes one physical line that begins inside a string when inStr is
// true. It strips a trailing `# comment` (only outside strings), keeps string
// contents verbatim, and reports the bracket-depth change and whether the line
// ends still inside a string.
func scanLine(s string, inStr bool) (text string, delta int, endInStr bool) {
	var b strings.Builder
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if esc {
				esc = false
				continue
			}
			switch c {
			case '\\':
				esc = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '#':
			return b.String(), delta, false // comment runs to end of line
		case '"':
			inStr = true
		case '(', '[', '{':
			delta++
		case ')', ']', '}':
			delta--
		}
		b.WriteByte(c)
	}
	return b.String(), delta, inStr
}

// Lines returns the scanned logical lines.
func (s *Scanner) Lines() []LogicalLine {
	if s == nil {
		return nil
	}
	return s.lines
}

// blockScalarRe matches a field whose value is the multiline block marker
// `|` (YAML-style), e.g. `text: |` or `set text: |`. The indented lines beneath
// it form a verbatim, dedented string.
var blockScalarRe = regexp.MustCompile(`^([ \t]*)((?:set|rate)[ \t]+)?([A-Za-z_][\w.]*)[ \t]*:[ \t]*\|[ \t]*$`)

func scanBlockScalar(lines []string, i int) (LogicalLine, int, bool) {
	m := blockScalarRe.FindStringSubmatch(lines[i])
	if m == nil {
		return LogicalLine{}, i, false
	}
	keyIndent := len(m[1])
	body, next := blockScalarBody(lines, i+1, keyIndent)
	content := dedentBlockScalarBody(body)

	var b strings.Builder
	b.WriteString(m[1])
	b.WriteString(m[2])
	b.WriteString(m[3])
	b.WriteString(`: "`)
	for k, line := range content {
		if k > 0 {
			b.WriteString(`\n`)
		}
		b.WriteString(escapeStringLiteral(line))
	}
	b.WriteByte('"')

	return LogicalLine{Text: b.String(), Line: i + 1, Indent: keyIndent}, next, true
}

func blockScalarBody(lines []string, start int, keyIndent int) ([]string, int) {
	j := start
	var body []string
	for j < len(lines) {
		if strings.TrimSpace(lines[j]) == "" {
			body = append(body, "")
			j++
			continue
		}
		if leadingIndent(lines[j]) <= keyIndent {
			break
		}
		body = append(body, lines[j])
		j++
	}
	return body, j
}

func dedentBlockScalarBody(body []string) []string {
	minIndent := -1
	for _, b := range body {
		if strings.TrimSpace(b) == "" {
			continue
		}
		if ind := leadingIndent(b); minIndent < 0 || ind < minIndent {
			minIndent = ind
		}
	}
	if minIndent < 0 {
		minIndent = 0
	}
	content := make([]string, 0, len(body))
	for _, b := range body {
		if strings.TrimSpace(b) == "" {
			content = append(content, "")
		} else {
			content = append(content, b[minIndent:])
		}
	}
	for len(content) > 0 && content[0] == "" {
		content = content[1:]
	}
	for len(content) > 0 && content[len(content)-1] == "" {
		content = content[:len(content)-1]
	}
	return content
}

func escapeStringLiteral(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func leadingIndent(raw string) int {
	n := 0
	for _, c := range raw {
		if c == ' ' || c == '\t' {
			n++
		} else {
			break
		}
	}
	return n
}
