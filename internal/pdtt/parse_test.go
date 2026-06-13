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

func TestInlineEditMustBeLastCell(t *testing.T) {
	_, err := ParseFile("| 10s | theta -> math.tau | linear\n")
	if err == nil {
		t.Fatal("expected error: inline edit not last cell")
	}
}
