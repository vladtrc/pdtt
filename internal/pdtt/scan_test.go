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
