package pdtt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func blockOf(t *testing.T, src string) *BlockStmt {
	t.Helper()
	stmts, err := ParseFile(src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, s := range stmts {
		if b, ok := s.(BlockStmt); ok {
			return &b
		}
	}
	t.Fatalf("no block in %q", src)
	return nil
}

func TestInlineHeaderRow(t *testing.T) {
	b := blockOf(t, "| 10s | ease:linear | theta -> math.tau\n")
	if b.DurS != 10 {
		t.Errorf("DurS = %v, want 10", b.DurS)
	}
	if len(b.DefMods) != 1 || b.DefMods[0].Kind != "ease" || b.DefMods[0].Name != "linear" {
		t.Errorf("DefMods = %+v, want one linear ease", b.DefMods)
	}
	if len(b.Rows) != 1 {
		t.Fatalf("Rows = %d, want 1 inline row", len(b.Rows))
	}
	if b.Rows[0].Op.LHS == nil || b.Rows[0].Op.RHS == nil {
		t.Errorf("inline row op not parsed: %+v", b.Rows[0].Op)
	}
	if len(b.Rows[0].Mods) != 0 {
		t.Errorf("inline row should carry no per-row mods, got %+v", b.Rows[0].Mods)
	}
}

func TestInlineHeaderEquivalentToTwoLines(t *testing.T) {
	inline := blockOf(t, "| 10s | ease:linear | theta -> math.tau\n")
	split := blockOf(t, "| 10s | ease:linear\n| theta -> math.tau\n")
	if inline.DurS != split.DurS {
		t.Errorf("DurS differs: %v vs %v", inline.DurS, split.DurS)
	}
	if len(inline.DefMods) != len(split.DefMods) {
		t.Errorf("DefMods differ: %d vs %d", len(inline.DefMods), len(split.DefMods))
	}
	if len(inline.Rows) != len(split.Rows) {
		t.Errorf("Rows differ: %d vs %d", len(inline.Rows), len(split.Rows))
	}
}

func TestInlineHeaderRowThenMoreRows(t *testing.T) {
	b := blockOf(t, "| 10s | ease:linear | theta -> math.tau\n| p.opacity -> 1\n")
	if len(b.Rows) != 2 {
		t.Fatalf("Rows = %d, want 2", len(b.Rows))
	}
}

func TestConsecutiveInlineHeadersBecomeSequentialBlocks(t *testing.T) {
	stmts, err := ParseFile("| 1s | in:draw | s\n| 1s | transition:morph | s -> d\n")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(stmts) != 2 {
		t.Fatalf("stmts = %d, want 2", len(stmts))
	}
	for i, st := range stmts {
		b, ok := st.(BlockStmt)
		if !ok {
			t.Fatalf("stmt %d = %T, want BlockStmt", i, st)
		}
		if b.DurS != 1 || len(b.Rows) != 1 {
			t.Fatalf("block %d = dur %v rows %d, want dur 1 rows 1", i, b.DurS, len(b.Rows))
		}
	}
}

func TestConsecutiveSameDurationInlineHeadersBecomeSequentialBlocks(t *testing.T) {
	src := "| 1s | in:draw | s\n| 1s | in:draw | s\n| 1s | in:draw | s\n| 1s | in:draw | s\n"
	stmts, err := ParseFile(src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(stmts) != 4 {
		t.Fatalf("stmts = %d, want 4 sequential blocks", len(stmts))
	}
	for i, st := range stmts {
		b, ok := st.(BlockStmt)
		if !ok {
			t.Fatalf("stmt %d = %T, want BlockStmt", i, st)
		}
		if b.DurS != 1 || len(b.Rows) != 1 {
			t.Fatalf("block %d = dur %v rows %d, want dur 1 rows 1", i, b.DurS, len(b.Rows))
		}
	}
}

func TestCommaSeparatedVerbSubjects(t *testing.T) {
	rt := compileScene(t, `scene comma_subjects

text title:
  text: "Title"

text capL:
  text: "Left"

text capR:
  text: "Right"

| 1s | ease:linear
| in:fade | title, capL, capR
`)
	if len(rt.Anims) != 3 {
		t.Fatalf("anims = %d, want 3 fade entrances", len(rt.Anims))
	}
}

func TestDurationPrefixedRowStaysInSplitBlock(t *testing.T) {
	stmts, err := ParseFile("| 4s | ease:linear\n| x -> 1\n| 1s | y -> 1\n")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("stmts = %d, want 1", len(stmts))
	}
	b, ok := stmts[0].(BlockStmt)
	if !ok {
		t.Fatalf("stmt = %T, want BlockStmt", stmts[0])
	}
	if b.DurS != 4 || len(b.Rows) != 2 {
		t.Fatalf("block = dur %v rows %d, want dur 4 rows 2", b.DurS, len(b.Rows))
	}
	if len(b.Rows[1].Mods) != 1 || b.Rows[1].Mods[0].Kind != "win" || b.Rows[1].Mods[0].B != 1 || !b.Rows[1].Mods[0].BSec {
		t.Fatalf("second row mods = %+v, want 1s window", b.Rows[1].Mods)
	}
}

func TestDurationPrefixedRowStaysAfterInlineHeader(t *testing.T) {
	stmts, err := ParseFile("| 10s | ease:linear | theta -> math.tau\n| 1s | x -> 1\n")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("stmts = %d, want 1", len(stmts))
	}
	b, ok := stmts[0].(BlockStmt)
	if !ok {
		t.Fatalf("stmt = %T, want BlockStmt", stmts[0])
	}
	if b.DurS != 10 || len(b.Rows) != 2 {
		t.Fatalf("block = dur %v rows %d, want dur 10 rows 2", b.DurS, len(b.Rows))
	}
	if len(b.Rows[1].Mods) != 1 || b.Rows[1].Mods[0].Kind != "win" || b.Rows[1].Mods[0].B != 1 || !b.Rows[1].Mods[0].BSec {
		t.Fatalf("second row mods = %+v, want 1s window", b.Rows[1].Mods)
	}
}

func TestInlineEditMustBeLastCell(t *testing.T) {
	_, err := ParseFile("| 10s | theta -> math.tau | linear\n")
	if err == nil {
		t.Fatal("expected error: inline edit not last cell")
	}
}

func TestParsePathRunsSiblingScene(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "run.pdtt")
	if err := os.WriteFile(mainPath, []byte(`scene main
run part
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part.pdtt"), []byte(`scene part:
dot d:
  at: [0, 0]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	stmts, err := NewSceneParser().ParsePath(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := Compile(stmts)
	if err != nil {
		t.Fatal(err)
	}
	if rt.Groups["d"] == nil {
		t.Fatal("run part did not compile sibling scene record")
	}
}

func compileScene(t *testing.T, src string) *Runtime {
	t.Helper()
	stmts, err := ParseFile(src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	rt, err := Compile(stmts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return rt
}

func TestColorNamespaceIncludesExtendedPalette(t *testing.T) {
	for _, name := range []string{"magenta", "cyan", "gold", "maroon", "violet", "light_gray"} {
		if _, ok := namespaces["color"][name]; !ok {
			t.Fatalf("color namespace missing %q", name)
		}
	}
}

func TestMixInterpolatesColors(t *testing.T) {
	stmts, err := ParseFile(`
scene color_mix

dot p:
  color: mix(color.cyan, color.magenta, 0.5)
`)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := Compile(stmts)
	if err != nil {
		t.Fatal(err)
	}
	p := oneEntity(t, rt, "p")
	got := entityColor(p)
	if got.R != 0.5 || got.G != 0.5 || got.B != 1 {
		t.Fatalf("mixed color = %+v, want {0.5 0.5 1 1}", got)
	}
}

func oneEntity(t *testing.T, rt *Runtime, name string) *Entity {
	t.Helper()
	grp := rt.Groups[name]
	if grp == nil || len(grp.Items) != 1 {
		t.Fatalf("record %s not found as one entity", name)
	}
	return grp.Items[0]
}

func TestRecordsStartInactive(t *testing.T) {
	rt := compileScene(t, `scene inactive

dot d:
  at: [0, 0]
`)
	if oneEntity(t, rt, "d").Active {
		t.Fatal("declared record is active before an entry or morph")
	}
}

func TestDerivedShapeTypesAreRejected(t *testing.T) {
	for _, typ := range []string{"arrow", "line", "rect", "square", "arc", "circle", "ellipse", "polygon"} {
		t.Run(typ, func(t *testing.T) {
			stmts, err := ParseFile("scene no_sugar\n\n" + typ + " x:\n")
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			_, err = Compile(stmts)
			if err == nil {
				t.Fatalf("Compile accepted derived shape type %q", typ)
			}
			if !strings.Contains(err.Error(), "path") {
				t.Fatalf("error = %q, want path guidance", err)
			}
		})
	}
}

func TestTextTypstConstructorSyntaxRejected(t *testing.T) {
	for _, src := range []string{
		`scene bad_text
text("plain") label:
  at: [0, 0]
`,
		`scene bad_typst
typst("x^2 + y^2") formula:
  at: [0, 0]
`,
		`scene bad_paren
text("a) b") label:
  at: [0, 0]
`,
	} {
		_, err := ParseFile(src)
		if err == nil {
			t.Fatalf("ParseFile(%q) succeeded, want constructor syntax error", src)
		}
		if !strings.Contains(err.Error(), "constructor syntax is not supported") {
			t.Fatalf("ParseFile(%q) error = %q, want constructor rejection", src, err)
		}
	}
}

func TestTextFieldMorphUsesOutline(t *testing.T) {
	rt := compileScene(t, `scene text_field_morph

text label:
  text: "circle{radius: 0.5}"
  at: [0, 0]
  scale: 1.2
  color: color.white

| 1s | ease:smooth
| label.text -> "ellipse{rx: 1, ry: 0.5}"
`)
	if err := rt.Step(0.5); err != nil {
		t.Fatal(err)
	}
	label := oneEntity(t, rt, "label")
	if len(label.MorphContours) == 0 {
		t.Fatal("expected outline morph contours during text field tween")
	}
	if label.fstr("text") != "circle{radius: 0.5}" {
		t.Fatalf("text = %q during morph, want source until readable snap", label.fstr("text"))
	}
	if err := rt.Step(1.0); err != nil {
		t.Fatal(err)
	}
	if label.fstr("text") != "ellipse{rx: 1, ry: 0.5}" {
		t.Fatalf("text = %q after morph, want destination string", label.fstr("text"))
	}
	if len(label.MorphContours) != 0 {
		t.Fatal("expected morph contours cleared after tween")
	}
}

func TestTextMorphEaseOutSettlesBeforeSmooth(t *testing.T) {
	const dest = "ellipse{rx: 1, ry: 0.5}"
	const src = `scene text_ease

text label:
  text: "circle{radius: 0.5}"
  at: [0, 0]
  scale: 1.2
  color: color.white

| 1s | ease:smooth
| ease:out | label.text -> "` + dest + `"
`
	rtOut := compileScene(t, src)
	rtSmooth := compileScene(t, strings.Replace(src, "ease:out", "ease:smooth", 1))

	readable := func(rt *Runtime) bool {
		label := oneEntity(t, rt, "label")
		return label.fstr("text") == dest && len(label.MorphContours) == 0
	}
	// ease_out(0.66) ≈ 0.88; smooth(0.66) ≈ 0.47 — only ease_out should be readable yet.
	if err := rtOut.Step(0.66); err != nil {
		t.Fatal(err)
	}
	if err := rtSmooth.Step(0.66); err != nil {
		t.Fatal(err)
	}
	if !readable(rtOut) {
		t.Fatal("ease_out should commit readable text before smooth at 66% clock")
	}
	if readable(rtSmooth) {
		t.Fatal("smooth should still be morphing outlines at 66% clock")
	}
}

func TestTextMorphEarlyReadableCommit(t *testing.T) {
	const dest = "ellipse{rx: 1, ry: 0.5}"
	rt := compileScene(t, `scene text_readable

text label:
  text: "circle{radius: 0.5}"
  at: [0, 0]
  scale: 1.2
  color: color.white

| 1s | ease:smooth
| ease:out | label.text -> "`+dest+`"
`)
	// ease_out(0.7) ≈ 0.91 ≥ textMorphReadU — crisp destination text before window ends.
	if err := rt.Step(0.7); err != nil {
		t.Fatal(err)
	}
	label := oneEntity(t, rt, "label")
	if label.fstr("text") != dest {
		t.Fatalf("text = %q at 70%% clock, want early readable %q", label.fstr("text"), dest)
	}
	if len(label.MorphContours) != 0 {
		t.Fatal("expected morph contours cleared once readable")
	}
	if err := rt.Step(1.0); err != nil {
		t.Fatal(err)
	}
	if label.fstr("text") != dest {
		t.Fatalf("text = %q after tween, want %q", label.fstr("text"), dest)
	}
}

func TestTypstRecordCanUseTextField(t *testing.T) {
	rt := compileScene(t, `scene typst_field

typst formula:
  text: "x^2 + y^2"
  at: [1, 0]
`)
	formula := oneEntity(t, rt, "formula")
	if formula.Type != "typst" || formula.fstr("text") != "x^2 + y^2" {
		t.Fatalf("formula = type %q text %q, want typst/x^2 + y^2", formula.Type, formula.fstr("text"))
	}
}

func TestUnclosedMultilineFieldExpressionParseError(t *testing.T) {
	_, err := ParseFile("dot p:\n  at: [\n    1,\n    2\n")
	if err == nil {
		t.Fatal("expected parse error for unclosed delimiter")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "unclosed") {
		t.Fatalf("error = %q, want unclosed delimiter mention", err)
	}
	if !strings.Contains(msg, "end of file") {
		t.Fatalf("error = %q, want EOF mention", err)
	}
	if !strings.Contains(err.Error(), "line 2:") {
		t.Fatalf("error = %q, want start line 2", err)
	}
}

func TestPlainTextRecordCanUseTextField(t *testing.T) {
	rt := compileScene(t, `scene text_field

text label:
  text: "plain"
  at: [0, 0]
`)
	label := oneEntity(t, rt, "label")
	if label.Type != "text" || label.fstr("text") != "plain" {
		t.Fatalf("label = type %q text %q, want text/plain", label.Type, label.fstr("text"))
	}
}

func TestPathRecordSupportsDottedMaterialFields(t *testing.T) {
	rt := compileScene(t, `scene path_material

dot a:
  at: [-1, 0]

dot b:
  at: [1, 0]

path edge:
  points: [a.at, b.at]
  stroke.color: color.gold
  stroke.width: 0.04
  stroke.end: arrow
  draw: 1

| 1s | ease:linear | in:draw | edge

| 1s | ease:linear
| edge.stroke.color -> color.red
`)
	edge := oneEntity(t, rt, "edge")
	if edge.Type != "path" {
		t.Fatalf("edge type = %q, want path", edge.Type)
	}
	if _, err := asPoints(edge.Fields["points"].Val); err != nil {
		t.Fatalf("path points not coerced: %v", err)
	}
	if got := edge.fstr("stroke.end"); got != "arrow" {
		t.Fatalf("stroke.end = %q, want arrow", got)
	}
	if err := rt.Step(1.5); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got := fieldColor(edge, "stroke.color", Color{}); got.R <= 0.9 || got.G >= 0.6 {
		t.Fatalf("stroke.color did not tween toward red: %+v", got)
	}
	NewRenderer(320, 180).Frame(rt)
}

func TestMultilineFieldExpression(t *testing.T) {
	rt := compileScene(t, `scene multiline_expr

dot p:
  at: [
    1 + 2,
    3 + 4
  ]
`)
	p := oneEntity(t, rt, "p")
	if got := p.fvec("at"); got != (Vec{3, 7}) {
		t.Fatalf("p.at = %v, want [3 7 0]", got)
	}
}

func TestEnterActivatesSelfTransition(t *testing.T) {
	rt := compileScene(t, `scene enter

path s:
  points: [[-1, -1], [1, -1], [1, 1], [-1, 1]]
  closed: 1
  stroke.color: color.white

| 1s | in:draw | s
`)
	s := oneEntity(t, rt, "s")
	if s.Active {
		t.Fatal("record active before stepping into entry")
	}
	if err := rt.Step(0); err != nil {
		t.Fatalf("Step(0): %v", err)
	}
	if !s.Active {
		t.Fatal("entry did not activate record")
	}
	if got := s.fnum("draw"); got != 0 {
		t.Fatalf("draw at entry start = %v, want 0", got)
	}
}

func TestMorphActivatesTargetAndDeactivatesSource(t *testing.T) {
	rt := compileScene(t, `scene morph_presence

path s:
  points: [[-1, -1], [1, -1], [1, 1], [-1, 1]]
  closed: 1
  stroke.color: color.white

dot d:
  radius: 1

| 1s | in:draw | s

| 1s | transition:morph | s -> d
`)
	s := oneEntity(t, rt, "s")
	d := oneEntity(t, rt, "d")
	if err := rt.Step(0); err != nil {
		t.Fatalf("Step(0): %v", err)
	}
	if !s.Active || d.Active {
		t.Fatalf("after entry start: s.Active=%v d.Active=%v, want true false", s.Active, d.Active)
	}
	if err := rt.Step(2); err != nil {
		t.Fatalf("Step(2): %v", err)
	}
	if s.Active || !d.Active {
		t.Fatalf("after morph: s.Active=%v d.Active=%v, want false true", s.Active, d.Active)
	}
}

func TestGoneTargetRejected(t *testing.T) {
	_, err := ParseFile("| 1s | d -> gone\n")
	if err == nil {
		t.Fatal("expected gone target to be rejected")
	}
}

func TestFlatRecordFieldRequiresIndent(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "text field",
			src:  "text heart:\ntext: \"x\"",
		},
		{
			name: "at field",
			src:  "dot p:\nat: [0,0]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFile(tc.src)
			if err == nil {
				t.Fatal("expected parse error for flat record field")
			}
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "indent") {
				t.Fatalf("error = %q, want mention of indent", err)
			}
		})
	}
}

func TestFamilyLocalBindingAccepted(t *testing.T) {
	stmts, err := ParseFile(`roots[0..3 as i]:
  a: i
  x: a + 1
  dot p:
    at: [x, 0]`)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	var fam *FamilyStmt
	for _, s := range stmts {
		if f, ok := s.(FamilyStmt); ok {
			fam = &f
			break
		}
	}
	if fam == nil {
		t.Fatal("expected FamilyStmt")
	}
	if len(fam.Locals) != 2 {
		t.Fatalf("family locals = %d, want 2", len(fam.Locals))
	}
}

func TestMultilineFieldExpressionInFamilyMember(t *testing.T) {
	rt := compileScene(t, `scene family_multiline

roots[0..2 as i]:
  dot p:
    at: [
      i,
      i + 1
    ]
`)

	for _, key := range []int{0, 1} {
		p := oneEntity(t, rt, familyMemberName("roots", key, "p"))
		if got := p.fvec("at"); got != (Vec{float64(key), float64(key + 1)}) {
			t.Fatalf("roots[%d].p.at = %v, want [%d %d 0]", key, got, key, key+1)
		}
	}
}

func TestFamilyHeaderNestedListDomain(t *testing.T) {
	stmts, err := ParseFile(`roots[[0, 1, 2] as i]:
  dot p:
    at: [i, 0]
`)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	var fam *FamilyStmt
	for _, s := range stmts {
		if f, ok := s.(FamilyStmt); ok {
			fam = &f
			break
		}
	}
	if fam == nil {
		t.Fatal("expected FamilyStmt")
	}
	if fam.Name != "roots" || fam.BindVar != "i" {
		t.Fatalf("family = %+v, want roots[..] as i", fam)
	}
	if _, ok := fam.DomainE.(ListE); !ok {
		t.Fatalf("domain = %T, want ListE", fam.DomainE)
	}

	rt, err := Compile(stmts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := rt.Families["roots"].N; got != 3 {
		t.Fatalf("family size = %d, want 3", got)
	}
}

func TestFamilyHeaderDomainWithParens(t *testing.T) {
	_, err := ParseFile(`roots[(0..3) as i]:
  dot p:
    at: [i, 0]
`)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
}

func TestMalformedFamilyHeaderNoPanic(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "missing close bracket", src: "roots[0..n as i"},
		{name: "missing as keyword", src: "roots[0..n i]:"},
		{name: "bad domain expr", src: "roots[[0.. as i]:"},
		{name: "incomplete header", src: "not_a_family[broken"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFile(tc.src)
			if err == nil {
				t.Fatal("expected parse error")
			}
		})
	}
}

func TestLuxeHeartExample(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "heart", "run.pdtt")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rt := compileScene(t, string(src))
	fam := rt.Families["heart"]
	if fam == nil {
		t.Fatal("heart family was not registered")
	}
	if fam.N != 28 {
		t.Fatalf("heart.N = %d, want 28", fam.N)
	}
}
