package pdtt

import (
	"strings"
	"testing"
)

func TestTypstSourceEscapesLiteralText(t *testing.T) {
	src := typstSource(`price is #5 and "quoted"`, false)
	if !strings.Contains(src, `#text("price is #5 and \"quoted\"")`) {
		t.Fatalf("typst source = %q, want literal text call with escaped contents", src)
	}
}

func TestTypstSourceKeepsMathMarkup(t *testing.T) {
	src := typstSource("x^2 + y^2", true)
	if !strings.Contains(src, "$x^2 + y^2$") {
		t.Fatalf("typst source = %q, want math markup", src)
	}
	if strings.Contains(src, "#text(") {
		t.Fatalf("typst source = %q, math should not be wrapped as literal text", src)
	}
}
