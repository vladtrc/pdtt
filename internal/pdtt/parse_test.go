package pdtt

import "testing"

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
	b := blockOf(t, "| 10s | linear | theta -> math.tau\n")
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
	inline := blockOf(t, "| 10s | linear | theta -> math.tau\n")
	split := blockOf(t, "| 10s | linear\n| theta -> math.tau\n")
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
	b := blockOf(t, "| 10s | linear | theta -> math.tau\n| p.opacity -> 1\n")
	if len(b.Rows) != 2 {
		t.Fatalf("Rows = %d, want 2", len(b.Rows))
	}
}

func TestConsecutiveInlineHeadersBecomeSequentialBlocks(t *testing.T) {
	stmts, err := ParseFile("| 1s | s{draw: 0} -> s\n| 1s | morph | s -> d\n")
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

func TestDurationPrefixedRowStaysInSplitBlock(t *testing.T) {
	stmts, err := ParseFile("| 4s | linear\n| x -> 1\n| 1s | y -> 1\n")
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
	stmts, err := ParseFile("| 10s | linear | theta -> math.tau\n| 1s | x -> 1\n")
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

func TestConstructorStyleTextRecords(t *testing.T) {
	rt := compileScene(t, `scene ctor_text

text("plain") label:
  at: [0, 0]

typst("x^2 + y^2") formula:
  at: [1, 0]
`)
	label := oneEntity(t, rt, "label")
	if label.Type != "text" || label.fstr("text") != "plain" {
		t.Fatalf("label = type %q text %q, want text/plain", label.Type, label.fstr("text"))
	}
	formula := oneEntity(t, rt, "formula")
	if formula.Type != "typst" || formula.fstr("text") != "x^2 + y^2" {
		t.Fatalf("formula = type %q text %q, want typst/x^2 + y^2", formula.Type, formula.fstr("text"))
	}
}

func TestEnterActivatesSelfTransition(t *testing.T) {
	rt := compileScene(t, `scene enter

square s:
  side: 2

| 1s | s{draw: 0} -> s
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

square s:
  side: 2

dot d:
  radius: 1

| 1s | s{draw: 0} -> s

| 1s | morph | s -> d
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
