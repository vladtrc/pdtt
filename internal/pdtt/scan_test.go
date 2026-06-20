package pdtt

import (
	"strings"
	"testing"
)

func TestScanSourceJoinsMultilineExpression(t *testing.T) {
	sc, err := ScanSource("dot p:\n  at: [\n    1,\n    2\n  ]\n")
	if err != nil {
		t.Fatalf("ScanSource: %v", err)
	}
	var field *LogicalLine
	for _, ll := range sc.Lines() {
		if strings.Contains(ll.Text, "at:") {
			field = &ll
			break
		}
	}
	if field == nil {
		t.Fatalf("lines = %#v, want joined at: field", sc.Lines())
	}
	if !strings.Contains(field.Text, "[ 1, 2 ]") {
		t.Fatalf("joined field = %q", field.Text)
	}
	if field.Line != 2 {
		t.Fatalf("field start line = %d, want 2", field.Line)
	}
}

// A string literal may span several physical lines; the breaks are preserved as
// real newlines inside the literal, and a `#` inside the open string is content,
// not a comment.
func TestScanSourceMultilineString(t *testing.T) {
	sc, err := ScanSource("text t:\n  text: \"line one # not a comment\nline two\"\n  scale: 1\n")
	if err != nil {
		t.Fatalf("ScanSource: %v", err)
	}
	var field *LogicalLine
	for _, ll := range sc.Lines() {
		if strings.Contains(ll.Text, "text:") {
			field = &ll
			break
		}
	}
	if field == nil {
		t.Fatalf("lines = %#v, want joined text: field", sc.Lines())
	}
	want := "  text: \"line one # not a comment\nline two\""
	if field.Text != want {
		t.Fatalf("joined field = %q, want %q", field.Text, want)
	}
}

// A `scale: 1` field must still be scanned as its own line after the multiline
// string closes (the string-open state is cleared correctly).
func TestScanSourceMultilineStringResumesFields(t *testing.T) {
	sc, err := ScanSource("text t:\n  text: \"a\nb\"\n  scale: 2\n")
	if err != nil {
		t.Fatalf("ScanSource: %v", err)
	}
	found := false
	for _, ll := range sc.Lines() {
		if strings.TrimSpace(ll.Text) == "scale: 2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("lines = %#v, want a standalone 'scale: 2' line", sc.Lines())
	}
}

// An unterminated string at EOF is reported, like an unclosed bracket.
func TestScanSourceUnterminatedStringAtEOF(t *testing.T) {
	_, err := ScanSource("text t:\n  text: \"never ends\n")
	if err == nil {
		t.Fatal("expected error for unterminated string")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unclosed") {
		t.Fatalf("error = %q, want unclosed", err)
	}
}

func TestScanSourceUnclosedDelimiterAtEOF(t *testing.T) {
	_, err := ScanSource("dot p:\n  at: [\n    1,\n    2\n")
	if err == nil {
		t.Fatal("expected error for unclosed delimiter")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "unclosed") || !strings.Contains(msg, "end of file") {
		t.Fatalf("error = %q, want unclosed delimiter at EOF", err)
	}
	if !strings.Contains(err.Error(), "line 2:") {
		t.Fatalf("error = %q, want start line 2", err)
	}
}
