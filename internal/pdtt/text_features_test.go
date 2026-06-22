package pdtt

import (
	"math"
	"testing"
)

func colorClose(a, b Color) bool {
	const eps = 1e-9
	return math.Abs(a.R-b.R) < eps && math.Abs(a.G-b.G) < eps && math.Abs(a.B-b.B) < eps && math.Abs(a.A-b.A) < eps
}

// String literals support the standard `\n` escape, so a single `text:` field
// can hold a multiline string instead of relying on the math-only `\ ` shorthand.
func TestStringLiteralNewlineEscape(t *testing.T) {
	e, err := ParseExpr(`"a\nb\tc\rd"`)
	if err != nil {
		t.Fatalf("ParseExpr: %v", err)
	}
	s, ok := e.(Str)
	if !ok {
		t.Fatalf("expr is %T, want Str", e)
	}
	if string(s) != "a\nb\tc\rd" {
		t.Fatalf("escaped string = %q, want %q", string(s), "a\nb\tc\rd")
	}
}

// A newline in the text splits the layout into multiple stacked lines.
func TestTextLayoutSplitsOnNewline(t *testing.T) {
	rt := compileScene(t, `scene multiline

text m:
  text: "one\ntwo\nthree"
  scale: 1
`)
	lay := textLayoutOf(oneEntity(t, rt, "m"))
	if lay == nil {
		t.Fatal("textLayoutOf returned nil")
	}
	if len(lay.Lines) != 3 {
		t.Fatalf("line count = %d, want 3", len(lay.Lines))
	}
}

// `<text>.sub("phrase").<attr>` resolves to a tweenable ref for every emphasis
// channel (colour, strike, underline, scale, wiggle), and the tween lands.
func TestPartEmphasisTweens(t *testing.T) {
	rt := compileScene(t, `scene emphasis

text m:
  text: "hi there"
  color: color.white

| 1s | m.sub("hi").strike -> 1

| 1s | m.sub("hi").underline -> 1

| 1s | m.sub("hi").scale -> 1.5

| 1s | m.sub("hi").color -> color.red
`)
	if err := rt.Step(4.0); err != nil {
		t.Fatalf("Step: %v", err)
	}
	m := oneEntity(t, rt, "m")
	p := m.partBySub("hi")
	if p == nil {
		t.Fatal("sub(\"hi\") part not found")
	}
	if p.Strike < 0.99 {
		t.Fatalf("strike = %v, want ~1", p.Strike)
	}
	if p.Underline < 0.99 {
		t.Fatalf("underline = %v, want ~1", p.Underline)
	}
	if p.scaleOr1() < 1.49 || p.scaleOr1() > 1.51 {
		t.Fatalf("scale = %v, want ~1.5", p.scaleOr1())
	}
	col, err := asColor(p.Color)
	if err != nil {
		t.Fatalf("part color: %v", err)
	}
	if col != namedColors["red"] {
		t.Fatalf("part color = %+v, want red", col)
	}
}

// A transient `| <channel> | text.sub(...)` modifier drives the channel through a
// 0→peak→0 envelope: lit at the window's midpoint, back to rest at its end. This
// "appears then vanishes" form is distinct from the persistent `->` arrow.
func TestTransientModifierEnvelope(t *testing.T) {
	rt := compileScene(t, `scene highlight

text m:
  text: "hi there"
  color: color.white

| 1s | ease:linear | highlight:strike | m.sub("hi")
`)
	m := oneEntity(t, rt, "m")
	if err := rt.Step(0.5); err != nil {
		t.Fatalf("Step(0.5): %v", err)
	}
	p := m.partBySub("hi")
	if p == nil {
		t.Fatal(`sub("hi") part not created`)
	}
	if p.Strike < 0.99 {
		t.Fatalf("strike at midpoint = %v, want ~1 (peak)", p.Strike)
	}
	if err := rt.Step(1.0); err != nil {
		t.Fatalf("Step(1): %v", err)
	}
	if p.Strike > 0.01 {
		t.Fatalf("strike at end = %v, want ~0 (rest)", p.Strike)
	}
}

// A `flash` modifier blends the span to yellow at the envelope peak, then drops
// the override entirely so the span reverts to its inherited colour.
func TestFlashModifierReturnsToInherit(t *testing.T) {
	rt := compileScene(t, `scene flash

text m:
  text: "hi there"
  color: color.white

| 1s | ease:linear | highlight:flash | m.sub("hi")
`)
	m := oneEntity(t, rt, "m")
	if err := rt.Step(0.5); err != nil {
		t.Fatalf("Step(0.5): %v", err)
	}
	p := m.partBySub("hi")
	if p == nil {
		t.Fatal(`sub("hi") part not created`)
	}
	col, err := asColor(p.Color)
	if err != nil {
		t.Fatalf("flash midpoint color: %v", err)
	}
	if col != namedColors["yellow"] {
		t.Fatalf("flash midpoint = %+v, want yellow", col)
	}
	if err := rt.Step(1.0); err != nil {
		t.Fatalf("Step(1): %v", err)
	}
	if p.Color != nil {
		t.Fatalf("flash end color = %v, want nil (inherited)", p.Color)
	}
}

func TestFlashModifierRestoresPartColorOverride(t *testing.T) {
	rt := compileScene(t, `scene flash_restore

text m:
  text: "hi there"
  color: color.white

| 1s | m.sub("hi").color -> color.red

| 1s | ease:linear | highlight:flash | m.sub("hi")
`)
	m := oneEntity(t, rt, "m")
	if err := rt.Step(1.5); err != nil {
		t.Fatalf("Step(1.5): %v", err)
	}
	p := m.partBySub("hi")
	if p == nil {
		t.Fatal(`sub("hi") part not created`)
	}
	col, err := asColor(p.Color)
	if err != nil {
		t.Fatalf("flash midpoint color: %v", err)
	}
	if col != namedColors["yellow"] {
		t.Fatalf("flash midpoint = %+v, want yellow", col)
	}
	if err := rt.Step(2.0); err != nil {
		t.Fatalf("Step(2): %v", err)
	}
	col, err = asColor(p.Color)
	if err != nil {
		t.Fatalf("flash restored color: %v", err)
	}
	if !colorClose(col, namedColors["red"]) {
		t.Fatalf("flash restored color = %+v, want red", col)
	}
}

func TestTransientModifierClearsHeldPostTween(t *testing.T) {
	rt := compileScene(t, `scene transient_clears_post

text m:
  text: "hi there"

| 1s | m.sub("hi").wiggle -> 1

| 1s | ease:linear | highlight:wiggle | m.sub("hi")
`)
	m := oneEntity(t, rt, "m")
	if err := rt.Step(2.0); err != nil {
		t.Fatalf("Step(2): %v", err)
	}
	p := m.partBySub("hi")
	if p == nil {
		t.Fatal(`sub("hi") part not created`)
	}
	if p.Wiggle > 0.01 {
		t.Fatalf("wiggle after transient = %v, want rest", p.Wiggle)
	}
}

func TestSubRejectsEmptySubstring(t *testing.T) {
	stmts, err := ParseFile(`scene empty_sub

text m:
  text: "hi"

| 1s | m.sub("").color -> color.red
`)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	_, err = Compile(stmts)
	if err == nil {
		t.Fatal("Compile succeeded, want empty sub error")
	}
}

func TestPartAttributeDependencyUsesSubKey(t *testing.T) {
	rt := compileScene(t, `scene part_dep

text src:
  text: "hi"
  color: color.white

text label:
  text: "label"
  color: src.sub("hi").color

| 1s | src.sub("hi").color -> color.red
`)
	if err := rt.Step(1.0); err != nil {
		t.Fatalf("Step: %v", err)
	}
	label := oneEntity(t, rt, "label")
	col, err := asColor(label.field("color").Val)
	if err != nil {
		t.Fatalf("label color: %v", err)
	}
	if !colorClose(col, namedColors["red"]) {
		t.Fatalf("label color = %+v, want red", col)
	}
}

func TestTransientModifierBroadcastsOverSubstringCalls(t *testing.T) {
	rt := compileScene(t, `scene broadcast_highlight

text labels:
  for: range(2)
  text: "hi"

| 1s | ease:linear | highlight:flash | labels[*].sub("hi")
`)
	if err := rt.Step(0.5); err != nil {
		t.Fatalf("Step: %v", err)
	}
	grp := rt.Groups["labels"]
	if grp == nil || len(grp.Items) != 2 {
		t.Fatalf("labels group = %#v, want 2 items", grp)
	}
	for i, e := range grp.Items {
		p := e.partBySub("hi")
		if p == nil {
			t.Fatalf("labels[%d] sub part missing", i)
		}
		col, err := asColor(p.Color)
		if err != nil {
			t.Fatalf("labels[%d] color: %v", i, err)
		}
		if !colorClose(col, namedColors["yellow"]) {
			t.Fatalf("labels[%d] color = %+v, want yellow", i, col)
		}
	}
}

// A `text: |` block scalar dedents its body, keeps interior blank lines, and
// trims surrounding blank lines — the value is the verbatim multiline text.
func TestBlockScalarMultiline(t *testing.T) {
	rt := compileScene(t, `scene block

text m:
  text: |
    first line
    second line

    after a gap
  scale: 1
`)
	m := oneEntity(t, rt, "m")
	got := m.fstr("text")
	want := "first line\nsecond line\n\nafter a gap"
	if got != want {
		t.Fatalf("block text = %q, want %q", got, want)
	}
	lay := textLayoutOf(m)
	if lay == nil {
		t.Fatal("textLayoutOf returned nil")
	}
	if len(lay.Lines) != 4 {
		t.Fatalf("line count = %d, want 4 (incl. the blank gap)", len(lay.Lines))
	}
}

// `text.sub("phrase")` segments out exactly that substring as its own span and
// leaves the rest as plain text, even when the substring sits on its own line.
func TestSubSelectsSubstringSpan(t *testing.T) {
	rt := compileScene(t, `scene subspan

text m:
  text: |
    enlarge it
    or wiggle it

| 1s | m.sub("or wiggle it").scale -> 1.4
`)
	m := oneEntity(t, rt, "m")
	if err := rt.Step(1.0); err != nil {
		t.Fatalf("Step: %v", err)
	}
	p := m.partBySub("or wiggle it")
	if p == nil {
		t.Fatal(`sub("or wiggle it") not created`)
	}
	if p.scaleOr1() < 1.39 || p.scaleOr1() > 1.41 {
		t.Fatalf("scale = %v, want ~1.4", p.scaleOr1())
	}
	lay := textLayoutOf(m)
	var spanText string
	for _, line := range lay.Lines {
		for _, sg := range line.Segs {
			if sg.Part == p {
				spanText += sg.Text
			}
		}
	}
	if spanText != "or wiggle it" {
		t.Fatalf("span text = %q, want %q", spanText, "or wiggle it")
	}
}

// scaleOr1 treats the zero value as the identity so an un-tweened part renders
// at its natural size.
func TestPartScaleZeroIsIdentity(t *testing.T) {
	p := &PartState{}
	if p.scaleOr1() != 1 {
		t.Fatalf("unset scale = %v, want 1", p.scaleOr1())
	}
}

// The `draw` reveal field is a real, tweenable field on text: a self-transition
// snaps it to 0 and writes it back to 1 over the window (manim's Write).
func TestTextDrawReveal(t *testing.T) {
	rt := compileScene(t, `scene write

text m:
  text: "hello"

| 1s | in:draw | m
`)
	m := oneEntity(t, rt, "m")
	if err := rt.Step(0.0); err != nil {
		t.Fatalf("Step(0): %v", err)
	}
	if !m.Active {
		t.Fatal("text not activated by write entry")
	}
	if got := m.fnum("draw"); got > 0.01 {
		t.Fatalf("draw at t=0 = %v, want ~0", got)
	}
	if err := rt.Step(1.0); err != nil {
		t.Fatalf("Step(1): %v", err)
	}
	if got := m.fnum("draw"); got < 0.99 {
		t.Fatalf("draw at t=1 = %v, want ~1", got)
	}
}
