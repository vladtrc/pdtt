package md

import (
	"strings"
	"testing"
)

func TestFullDocsStayHarnessAgnostic(t *testing.T) {
	for _, forbidden := range []string{
		"640x640",
		"Web Playground",
		"Title/narration",
		"1.2s..2.8s",
		"85-90%",
	} {
		if strings.Contains(Full, forbidden) {
			t.Fatalf("Full docs should not include harness/style text %q", forbidden)
		}
	}
}
