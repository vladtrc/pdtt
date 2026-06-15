package pdtt

import (
	"fmt"
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

// ScanSource tokenizes source into logical lines. Returns an error when open
// (), [], or {} delimiters remain at EOF.
func ScanSource(src string) (*Scanner, error) {
	rawLines := strings.Split(src, "\n")
	out := make([]LogicalLine, 0, len(rawLines))
	var b strings.Builder
	startLine := 0
	startIndent := 0
	depth := 0

	for n, raw := range rawLines {
		ln := n + 1
		line := strings.TrimRight(stripLineComment(raw), " \t")
		trimmed := strings.TrimSpace(line)
		if depth == 0 {
			startLine = ln
			startIndent = leadingIndent(raw)
			b.Reset()
			b.WriteString(line)
		} else if trimmed != "" {
			b.WriteByte(' ')
			b.WriteString(trimmed)
		}
		depth += delimiterDepthDelta(line)
		if depth <= 0 {
			out = append(out, LogicalLine{
				Text:   b.String(),
				Line:   startLine,
				Indent: startIndent,
			})
			depth = 0
		}
	}
	if depth > 0 {
		return nil, fmt.Errorf("line %d: unclosed delimiter at end of file", startLine)
	}
	return &Scanner{lines: out}, nil
}

// Lines returns the scanned logical lines.
func (s *Scanner) Lines() []LogicalLine {
	if s == nil {
		return nil
	}
	return s.lines
}

func stripLineComment(s string) string {
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

func delimiterDepthDelta(s string) int {
	inStr := false
	esc := false
	delta := 0
	for i := 0; i < len(s); i++ {
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
		case '(', '[', '{':
			delta++
		case ')', ']', '}':
			delta--
		}
	}
	return delta
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
