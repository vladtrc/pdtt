package pdtt

import "testing"

func TestRangeDomainBinderCreatesFamily(t *testing.T) {
	rt := compileScene(t, `scene range_domain

n: 5

roots[2..n as i]:
  dot mark:
    at: [i, 0]
    color: color.red
`)

	fam := rt.Families["roots"]
	if fam == nil {
		t.Fatal("roots family was not registered")
	}
	if fam.N != 3 {
		t.Fatalf("family size = %d, want 3", fam.N)
	}
	for _, key := range []int{2, 3, 4} {
		e := oneEntity(t, rt, familyMemberName("roots", key, "mark"))
		if got := e.fvec("at")[0]; got != float64(key) {
			t.Fatalf("mark %d x = %v, want %d", key, got, key)
		}
	}
	v, err := (&Scope{rt: rt}).Eval(mustExpr(t, "roots[3].mark.at"))
	if err != nil {
		t.Fatalf("roots[3].mark.at: %v", err)
	}
	p, err := asVec(v)
	if err != nil {
		t.Fatalf("roots[3].mark.at vec: %v", err)
	}
	if p[0] != 3 {
		t.Fatalf("roots[3].mark.at.x = %v, want 3", p[0])
	}
}

func TestFrozenCaptureDomainBinderCreatesFamily(t *testing.T) {
	rt := compileScene(t, `scene frozen_capture_domain

n = 5

roots[0..n as i]:
  dot mark:
    at: [i, 0]
`)

	fam := rt.Families["roots"]
	if fam == nil {
		t.Fatal("roots family was not registered")
	}
	if fam.N != 5 {
		t.Fatalf("family size = %d, want 5", fam.N)
	}
}

func TestDomainBinderListTweenUpdatesLiveMembers(t *testing.T) {
	rt := compileScene(t, `scene live_family

val: [0, 1]

roots[val.indices as i]:
  dot mark:
    at: [val[i], 0]

  path hit:
    points: [[val[i], -1], mark.at]
    stroke.end: arrow

| 1s | ease:linear
| val[* as i] -> [2, 3][i]
`)

	if len(rt.liveFields) != 4 {
		t.Fatalf("live fields = %d, want 4", len(rt.liveFields))
	}
	if err := rt.Step(0.5); err != nil {
		t.Fatalf("Step(0.5): %v", err)
	}
	m0 := oneEntity(t, rt, familyMemberName("roots", 0, "mark"))
	m1 := oneEntity(t, rt, familyMemberName("roots", 1, "mark"))
	h0 := oneEntity(t, rt, familyMemberName("roots", 0, "hit"))
	h1 := oneEntity(t, rt, familyMemberName("roots", 1, "hit"))
	if got := m0.fvec("at")[0]; got != 1 {
		t.Fatalf("mark 0 x at half tween = %v, want 1", got)
	}
	if got := m1.fvec("at")[0]; got != 2 {
		t.Fatalf("mark 1 x at half tween = %v, want 2", got)
	}
	h0Points, err := asPoints(h0.Fields["points"].Val)
	if err != nil {
		t.Fatalf("hit 0 points: %v", err)
	}
	h1Points, err := asPoints(h1.Fields["points"].Val)
	if err != nil {
		t.Fatalf("hit 1 points: %v", err)
	}
	if got := h0Points[0][0]; got != 1 {
		t.Fatalf("hit 0 points[0].x at half tween = %v, want 1", got)
	}
	if got := h1Points[0][0]; got != 2 {
		t.Fatalf("hit 1 points[0].x at half tween = %v, want 2", got)
	}
	if got := h0Points[1][0]; got != 1 {
		t.Fatalf("hit 0 points[1].x at half tween = %v, want 1", got)
	}
	if got := h1Points[1][0]; got != 2 {
		t.Fatalf("hit 1 points[1].x at half tween = %v, want 2", got)
	}
	if err := rt.Step(1.25); err != nil {
		t.Fatalf("Step(1.25): %v", err)
	}
	if got := m0.fvec("at")[0]; got != 2 {
		t.Fatalf("mark 0 x after tween = %v, want 2", got)
	}
	if got := m1.fvec("at")[0]; got != 3 {
		t.Fatalf("mark 1 x after tween = %v, want 3", got)
	}
}

func TestFamilyLocalBindingsAreSharedAndLive(t *testing.T) {
	rt := compileScene(t, `scene family_local_bindings

scale: 1

roots[0..2 as i]:
  a: i + 1
  x: scale * a

  dot p:
    at: [x, 0]

  path ray:
    points: [[0, 0], [x, 0]]
    stroke.end: arrow

| 1s | ease:linear
| scale -> 3
`)

	if err := rt.Step(0.5); err != nil {
		t.Fatalf("Step(0.5): %v", err)
	}
	for _, key := range []int{0, 1} {
		want := float64(key+1) * 2
		p := oneEntity(t, rt, familyMemberName("roots", key, "p"))
		ray := oneEntity(t, rt, familyMemberName("roots", key, "ray"))
		if got := p.fvec("at")[0]; got != want {
			t.Fatalf("roots[%d].p.at.x = %v, want %v", key, got, want)
		}
		points, err := asPoints(ray.Fields["points"].Val)
		if err != nil {
			t.Fatalf("roots[%d].ray.points: %v", key, err)
		}
		if got := points[1][0]; got != want {
			t.Fatalf("roots[%d].ray.points[1].x = %v, want %v", key, got, want)
		}
	}
}

func TestFamilyMemberCanDependOnSiblingInstanceMember(t *testing.T) {
	rt := compileScene(t, `scene family_neighbor_dependency

n = 3
phase: 0

ring[0..n as i]:
  a: math.tau * i / n + phase
  prev_i: (i - 1 + n) % n

  dot p:
    at: [cos(a), sin(a)]

  path chord:
    points: [ring[prev_i].p.at, p.at]
    stroke.end: arrow

| 1s | ease:linear
| phase -> math.tau
`)

	if err := rt.Step(0.5); err != nil {
		t.Fatalf("Step(0.5): %v", err)
	}

	p0 := oneEntity(t, rt, familyMemberName("ring", 0, "p"))
	p2 := oneEntity(t, rt, familyMemberName("ring", 2, "p"))
	chord0 := oneEntity(t, rt, familyMemberName("ring", 0, "chord"))

	points, err := asPoints(chord0.Fields["points"].Val)
	if err != nil {
		t.Fatalf("ring[0].chord.points: %v", err)
	}
	assertVecNear(t, "ring[0].chord.points[0]", points[0], p2.fvec("at"))
	assertVecNear(t, "ring[0].chord.points[1]", points[1], p0.fvec("at"))
}

func TestSnapshotColonBindingFreezesRecord(t *testing.T) {
	rt := compileScene(t, `scene snapshot_freeze

home: snapshot frame

| 1s | ease:linear
| frame.w -> 7.1
`)

	if err := rt.Step(1); err != nil {
		t.Fatalf("Step(1): %v", err)
	}
	home, ok := rt.Globals["home"].Val.(Snapshot)
	if !ok {
		t.Fatalf("home = %T, want Snapshot", rt.Globals["home"].Val)
	}
	got, err := asFloat(home.Fields["w"])
	if err != nil {
		t.Fatalf("home.w: %v", err)
	}
	if got != frameW0 {
		t.Fatalf("home.w = %v, want frozen %v", got, frameW0)
	}
}

func TestPlaneCoordinatesAlignWithAxesPoints(t *testing.T) {
	rt := compileScene(t, `scene aligned_coordinates

plane grid:
  at: [-1.25, -1.15]
  x_range: [-4, 4, 1]
  y_range: [-3, 3, 1]

axes ax:
  at: grid.at
  x_range: [-4, 4, 1]
  y_range: [-3, 3, 1]
`)

	grid := oneEntity(t, rt, "grid")
	ax := oneEntity(t, rt, "ax")
	for _, p := range []Vec{{-3, 0}, {1, 0}, {0, 2}} {
		gridPoint := axesLocalPoint(grid, p[0], p[1]).Add(grid.fvec("at"))
		axPoint := axesPoint(ax, p[0], p[1])
		if gridPoint != axPoint {
			t.Fatalf("grid point %v = %v, axes point = %v", p, gridPoint, axPoint)
		}
	}
}

func TestAnonymousRHSBroadcastExpandsThenConflictsOnDuplicateTargets(t *testing.T) {
	stmts, err := ParseFile(`scene bad_rhs_star

val: [0, 1]
other: [2, 3]

| 1s
| val[*] -> other[*]
`)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if _, err := Compile(stmts); err == nil {
		t.Fatal("Compile succeeded, want duplicate target error")
	}
}

func TestAnonymousRHSBroadcastCanExpandWhenTargetsStayDistinct(t *testing.T) {
	rt := compileScene(t, `scene rhs_star_ok

val: [0, 1]
other: [2]

| 1s | ease:linear
| val[*] -> other[*]
`)

	if len(rt.Anims) != 2 {
		t.Fatalf("anims = %d, want 2", len(rt.Anims))
	}
	if err := rt.Step(1); err != nil {
		t.Fatalf("Step(1): %v", err)
	}
	val := rt.Globals["val"].Val.([]Value)
	for i, got := range val {
		f, err := asFloat(got)
		if err != nil {
			t.Fatalf("val[%d]: %v", i, err)
		}
		if f != 2 {
			t.Fatalf("val[%d] = %v, want 2", i, f)
		}
	}
}

func TestPlainBroadcastBindsCanonicalIndexName(t *testing.T) {
	rt := compileScene(t, `scene canonical_index

a: [0, 1]
b: [2, 3]

| 1s | ease:linear
| a[*] -> b[i]
`)

	if err := rt.Step(1); err != nil {
		t.Fatalf("Step(1): %v", err)
	}
	got := rt.Globals["a"].Val.([]Value)
	for i, want := range []float64{2, 3} {
		f, err := asFloat(got[i])
		if err != nil {
			t.Fatalf("a[%d]: %v", i, err)
		}
		if f != want {
			t.Fatalf("a[%d] = %v, want %v", i, f, want)
		}
	}
}

func TestPlainBroadcastBindsCanonicalItValue(t *testing.T) {
	rt := compileScene(t, `scene canonical_it

a: [1, 2]

| 1s | ease:linear
| a[*] -> it + 10
`)

	if err := rt.Step(1); err != nil {
		t.Fatalf("Step(1): %v", err)
	}
	got := rt.Globals["a"].Val.([]Value)
	for i, want := range []float64{11, 12} {
		f, err := asFloat(got[i])
		if err != nil {
			t.Fatalf("a[%d]: %v", i, err)
		}
		if f != want {
			t.Fatalf("a[%d] = %v, want %v", i, f, want)
		}
	}
}

func TestNumericItAttributeParsesAndEvaluates(t *testing.T) {
	rt := NewRuntime()
	expr := mustExpr(t, "it.0.i + it.1.i")
	scope := &Scope{
		rt: rt,
		binds: map[string]Value{
			"it": []Value{
				ItVal{I: 2, N: 4},
				ItVal{I: 3, N: 5},
			},
		},
	}
	got, err := scope.Eval(expr)
	if err != nil {
		t.Fatalf("Eval(it.0.i + it.1.i): %v", err)
	}
	f, err := asFloat(got)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if f != 5 {
		t.Fatalf("result = %v, want 5", f)
	}
}

func TestFamilyDomainRejectsNonIntegerAndDuplicateKeys(t *testing.T) {
	for _, src := range []string{
		`scene non_integer
roots[[0.2, 0.8] as i]:
  dot mark:
    at: [i, 0]
`,
		`scene duplicate
roots[[1, 1] as i]:
  dot mark:
    at: [i, 0]
`,
	} {
		stmts, err := ParseFile(src)
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		if _, err := Compile(stmts); err == nil {
			t.Fatalf("Compile(%q) succeeded, want invalid domain error", src)
		}
	}
}

func TestPlotFnEvalWithFamilyIndex(t *testing.T) {
	rt := compileScene(t, `scene plot_family

energies: [1, 4]

axes ax:
  x_range: [-2, 2, 1]
  y_range: [-2, 2, 1]

orbits[energies.indices as i]:
  plot hi:
    axes: ax
    fn: energies[i] + x
`)

	e := oneEntity(t, rt, familyMemberName("orbits", 1, "hi"))
	it, ok := e.It.(ItVal)
	if !ok {
		t.Fatalf("It = %T, want ItVal", e.It)
	}
	v, err := evalWith(rt, e.Fields["fn"].Def, map[string]Value{"x": 0.0, "it": it})
	if err != nil {
		t.Fatalf("evalWith: %v", err)
	}
	f, err := asFloat(v)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if f != 4 {
		t.Fatalf("energies[1] + 0 = %v, want 4", f)
	}
}

func mustExpr(t *testing.T, src string) Expr {
	t.Helper()
	e, err := ParseExpr(src)
	if err != nil {
		t.Fatalf("ParseExpr(%q): %v", src, err)
	}
	return e
}
