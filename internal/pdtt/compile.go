package pdtt

// Score compilation: walk top-level statements with a time cursor, expand
// blocks/rows (windows, each, broadcast) into Anims, captures and `set`
// redefinitions into Events, then run the liveness/one-writer pass —
// checklist.md §7 steps 4–6.

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type easeFn func(float64) float64

// smoothstep is the classic 3u²−2u³ S-curve; `smooth` and `ease_in_out` are the
// same curve under both spellings.
func smoothstep(u float64) float64 { return u * u * (3 - 2*u) }

var easings = map[string]easeFn{
	"linear":      func(u float64) float64 { return u },
	"smooth":      smoothstep,
	"ease_in":     func(u float64) float64 { return u * u },
	"ease_out":    func(u float64) float64 { return 1 - (1-u)*(1-u) },
	"ease_in_out": smoothstep,
	// Stronger ease-out: morph settles early, final value holds longer.
	"ease_out_cubic": func(u float64) float64 {
		x := 1 - u
		return 1 - x*x*x
	},
}

// textMorphReadU: once eased progress passes this, commit the destination
// string and draw crisp glyphs instead of interpolated outline contours.
const textMorphReadU = 0.88

// Anim is one expanded row element: a window plus an apply function.
type Anim struct {
	T0, T1  float64
	Ease    easeFn
	Targets []Ref
	Binds   map[string]Value
	Inputs  map[string]bool
	Start   func(a *Anim, rt *Runtime) error
	Update  func(a *Anim, rt *Runtime, u float64)
	Line    int

	started, done bool
	state         animState // typed per-verb state: plan captured at compile, values resolved in start
}

// animState is a verb's typed per-animation state — the plan captured when the
// row is expanded plus the run-time values resolved in start(). Each verb owns a
// struct (morphAnim, tweenAnim, warpAnim, recordArrowAnim) instead of scribbling
// parallel slices onto the shared Anim.
type animState interface {
	start(a *Anim, rt *Runtime) error
	step(a *Anim, rt *Runtime, u float64)
}

// drive wires a typed state into the anim's Start/Update hooks. Method values
// carry the receiver, so no per-verb adapter closures are needed.
func (a *Anim) drive(st animState) {
	a.state = st
	a.Start = st.start
	a.Update = st.step
}

type textMorphAnim struct {
	E        *Entity
	srcCtrs  [][]Vec
	dstCtrs  [][]Vec
	srcStyle shapeMorphStyle
	dstStyle shapeMorphStyle
	readable bool // destination text committed for crisp rendering
}

type Event struct {
	T    float64
	Run  func(rt *Runtime) error
	Line int
	done bool
}

type PostAssign struct {
	Ref     Ref
	RHS     Expr
	Binds   map[string]Value
	ElemIdx int
	ElemN   int
}

const (
	frameW0 = 14.2
	frameH0 = 8.0
)

type Runtime struct {
	SceneName string
	Globals   map[string]*GVar
	Groups    map[string]*Group
	Families  map[string]*RecordFamily
	Entities  []*Entity // render order = declaration order
	Frame     *Entity
	Anims     []*Anim
	Events    []*Event
	Total     float64
	T, Dt     float64

	liveFields  []fieldSlot
	liveGlobals []*GVar // live global variables (colon-binding)
	liveDeps    map[string]bool
	post        map[string]*PostAssign
	warned      map[string]bool

	writerAnims  map[string][]*Anim
	globalWriter map[string]bool

	// Per evalLiveFields pass: cache family-local bindings by explicit instance ID.
	localBindCache map[int]map[string]Value
	nextLocalID    int
	evalBinds      map[string]Value
}

type fieldSlot struct {
	E *Entity
	F *Field
}

func (rt *Runtime) warnOnce(msg string) {
	if rt.warned == nil {
		rt.warned = map[string]bool{}
	}
	if !rt.warned[msg] {
		rt.warned[msg] = true
		fmt.Println("warning:", msg)
	}
}

func (rt *Runtime) clearLocalBindCache() {
	rt.localBindCache = nil
}

func (rt *Runtime) cachedLocalBind(localID int, name string) (Value, bool) {
	if localID == 0 || rt.localBindCache == nil {
		return nil, false
	}
	m, ok := rt.localBindCache[localID]
	if !ok {
		return nil, false
	}
	v, ok := m[name]
	return v, ok
}

func (rt *Runtime) cacheLocalBind(localID int, name string, val Value) {
	if localID == 0 {
		return
	}
	if rt.localBindCache == nil {
		rt.localBindCache = make(map[int]map[string]Value)
	}
	if rt.localBindCache[localID] == nil {
		rt.localBindCache[localID] = make(map[string]Value)
	}
	rt.localBindCache[localID][name] = val
}

func (rt *Runtime) newLocalID() int {
	rt.nextLocalID++
	return rt.nextLocalID
}

func (rt *Runtime) fieldEvalScope(it Value) *Scope {
	if rt.evalBinds == nil {
		rt.evalBinds = map[string]Value{"it": it}
	} else {
		rt.evalBinds["it"] = it
		delete(rt.evalBinds, "self")
	}
	return &Scope{rt: rt, binds: rt.evalBinds}
}

func NewRuntime() *Runtime {
	rt := &Runtime{
		Globals:  map[string]*GVar{},
		Groups:   map[string]*Group{},
		Families: map[string]*RecordFamily{},
		post:     map[string]*PostAssign{},
	}
	f := &Entity{Type: "frame", Name: "frame", Fields: map[string]*Field{}, Active: true, rt: rt}
	f.field("at").Val = Vec{}
	f.field("w").Val = frameW0
	f.field("h").Val = frameH0
	f.field("angle").Val = 0.0
	rt.Frame = f
	rt.Groups["frame"] = &Group{Name: "frame", Items: []*Entity{f}}
	return rt
}

func (rt *Runtime) clearPost(key string) {
	delete(rt.post, key)
}

func (rt *Runtime) setPost(ref Ref, rhs Expr, binds map[string]Value, elemIdx, elemN int) {
	cp := map[string]Value{}
	for k, v := range binds {
		cp[k] = v
	}
	rt.post[ref.Key()] = &PostAssign{
		Ref:     ref,
		RHS:     rhs,
		Binds:   cp,
		ElemIdx: elemIdx,
		ElemN:   elemN,
	}
}

func (rt *Runtime) globalHasWriter(name string) bool {
	if rt.globalWriter != nil {
		return rt.globalWriter[name]
	}
	elemPrefix := name + "["
	for _, a := range rt.Anims {
		for _, ref := range a.Targets {
			if ref == nil {
				continue
			}
			key := ref.Key()
			if key == name || strings.HasPrefix(key, elemPrefix) {
				return true
			}
		}
	}
	return false
}

func (rt *Runtime) fieldHasWriter(key string) bool {
	if rt.writerAnims != nil {
		return len(rt.writerAnims[key]) > 0
	}
	for _, a := range rt.Anims {
		for _, ref := range a.Targets {
			if ref != nil && ref.Key() == key {
				return true
			}
		}
	}
	return false
}

func (rt *Runtime) fieldHasActiveWriter(key string, t float64) bool {
	anims := rt.Anims
	if rt.writerAnims != nil {
		anims = rt.writerAnims[key]
	}
	for _, a := range anims {
		if t+1e-9 < a.T0 || t > a.T1+1e-9 {
			continue
		}
		for _, ref := range a.Targets {
			if ref != nil && ref.Key() == key {
				return true
			}
		}
	}
	return false
}

func (rt *Runtime) indexWriters() {
	rt.writerAnims = map[string][]*Anim{}
	rt.globalWriter = map[string]bool{}
	for _, a := range rt.Anims {
		for _, ref := range a.Targets {
			if ref == nil {
				continue
			}
			key := ref.Key()
			rt.writerAnims[key] = append(rt.writerAnims[key], a)
			if strings.Contains(key, ".") {
				continue
			}
			name, _, _ := strings.Cut(key, "[")
			rt.globalWriter[name] = true
		}
	}
}

func (rt *Runtime) animNeedsLiveRefresh(a *Anim) bool {
	if len(rt.liveDeps) == 0 {
		return true
	}
	for _, ref := range a.Targets {
		if ref != nil && rt.liveDeps[ref.Key()] {
			return true
		}
	}
	return false
}

func (rt *Runtime) animNeedsLiveInput(a *Anim) bool {
	if len(a.Inputs) == 0 || len(rt.liveDeps) == 0 {
		return false
	}
	for dep := range a.Inputs {
		if rt.liveDeps[dep] {
			return true
		}
		if strings.Contains(dep, ".") {
			continue
		}
		prefix := dep + "."
		for liveDep := range rt.liveDeps {
			if strings.HasPrefix(liveDep, prefix) {
				return true
			}
		}
	}
	return false
}

func exprInputDeps(expr Expr) map[string]bool {
	deps := map[string]bool{}
	exprDeps(expr, deps)
	return deps
}

func evalTweenGoal(rt *Runtime, rhs Expr, binds map[string]Value, elemIdx, elemN int, cur Value) (Value, error) {
	s := &Scope{rt: rt, binds: binds}
	goal, err := s.Eval(rhs)
	if err != nil {
		return nil, err
	}
	if list, ok := goal.([]Value); ok && elemIdx >= 0 && len(list) == elemN {
		goal = list[elemIdx]
	}
	if _, isVec := cur.(Vec); isVec {
		if g, err := asVec(goal); err == nil {
			goal = g
		}
	}
	return goal, nil
}

func Compile(stmts []Stmt) (*Runtime, error) {
	rt := NewRuntime()
	cursor := 0.0
	scope := &Scope{rt: rt}
	sceneDefs := map[string]SceneDefStmt{}
	for _, st := range stmts {
		if v, ok := st.(SceneDefStmt); ok {
			if old, exists := sceneDefs[v.Name]; exists {
				return nil, fmt.Errorf("line %d: scene %s already defined at line %d", v.Line, v.Name, old.Line)
			}
			sceneDefs[v.Name] = v
		}
	}

	var runStack []string
	var compileList func([]Stmt) error
	compileList = func(list []Stmt) error {
		for _, st := range list {
			switch v := st.(type) {
			case SceneStmt:
				rt.SceneName = v.Name
			case SceneDefStmt:
				continue
			case RunStmt:
				def, ok := sceneDefs[v.Name]
				if !ok {
					return fmt.Errorf("line %d: unknown scene %s", v.Line, v.Name)
				}
				for _, name := range runStack {
					if name == v.Name {
						return fmt.Errorf("line %d: recursive run %s", v.Line, v.Name)
					}
				}
				runStack = append(runStack, v.Name)
				if err := compileList(def.Body); err != nil {
					return err
				}
				runStack = runStack[:len(runStack)-1]
			case ExternStmt:
				if _, ok := builtins[v.Name]; !ok {
					return fmt.Errorf("extern fn %s has no Go stub in this prototype", v.Name)
				}
			case CaptureStmt:
				if err := rt.addCapture(v, cursor); err != nil {
					return err
				}
			case FamilyStmt:
				if err := rt.addFamily(v, scope); err != nil {
					return err
				}
			case RecordStmt:
				if _, exists := rt.Groups[v.Name]; exists {
					hasSet := false
					hasMerge := false
					for _, fd := range v.Fields {
						if fd.Set {
							hasSet = true
							continue
						}
						if fd.Name != "for" {
							hasMerge = true
						}
					}
					if hasMerge {
						if err := rt.mergeRecord(v); err != nil {
							return err
						}
					}
					if hasSet {
						rt.addSetEvents(v, cursor)
					}
				} else if err := rt.addRecord(v, scope); err != nil {
					return err
				}
			case BlockStmt:
				dur, err := rt.expandBlock(v, cursor)
				if err != nil {
					return err
				}
				cursor += dur
			}
		}
		return nil
	}
	if err := compileList(stmts); err != nil {
		return nil, err
	}
	rt.Total = cursor

	sort.SliceStable(rt.Events, func(i, j int) bool { return rt.Events[i].T < rt.Events[j].T })
	// captures at the top of the score (t=0) resolve before initial field
	// evaluation — `theta = 0` must exist when `at: [3*cos(theta), ...]` runs
	for _, ev := range rt.Events {
		if ev.T <= 1e-9 {
			ev.done = true
			if err := ev.Run(rt); err != nil {
				return nil, err
			}
		}
	}
	if err := rt.initFields(); err != nil {
		return nil, err
	}
	if err := rt.livenessPass(); err != nil {
		return nil, err
	}
	sort.SliceStable(rt.Anims, func(i, j int) bool { return rt.Anims[i].T0 < rt.Anims[j].T0 })
	return rt, nil
}

// ---------- records ----------

func (rt *Runtime) addRecord(v RecordStmt, scope *Scope) error {
	if err := checkRecordType(v); err != nil {
		return err
	}
	var fields []FieldDef
	var forE Expr
	for _, fd := range v.Fields {
		if fd.Name == "for" && !fd.Set && !fd.Rate {
			forE = fd.E
			continue
		}
		fields = append(fields, fd)
	}

	var its []Value
	if forE != nil {
		src, err := scope.Eval(forE)
		if err != nil {
			return fmt.Errorf("line %d: for: %v", v.Line, err)
		}
		switch x := src.(type) {
		case []Value:
			its = x
		case *Group:
			for _, e := range x.Items {
				its = append(its, e)
			}
		case *Entity:
			its = []Value{x}
		default:
			return fmt.Errorf("line %d: bad row source %T", v.Line, src)
		}
	} else {
		its = []Value{nil}
	}

	grp := &Group{Name: v.Name}
	for i, it := range its {
		e := &Entity{
			Type: v.Type, Name: v.Name, Idx: i, N: len(its),
			Fields: map[string]*Field{}, rt: rt,
		}
		if len(its) > 1 || forE != nil {
			e.It = ItVal{Val: it, I: i, N: len(its)}
		}
		for _, fd := range fields {
			f := e.field(fd.Name)
			f.Def = fd.E
			f.Rate = fd.Rate
		}
		grp.Items = append(grp.Items, e)
	}
	rt.Groups[v.Name] = grp
	rt.Entities = append(rt.Entities, grp.Items...)

	// data records: evaluate columns now (folds run top-to-bottom over rows),
	// then freeze — they are tables, not signals (proposal §1).
	if v.Type == "data" {
		return rt.evalDataColumns(grp, fields, v.Line)
	}
	return nil
}

func checkRecordType(v RecordStmt) error {
	switch v.Type {
	case "arrow", "line":
		return fmt.Errorf("line %d: %q is not a record type; use `path` with `points` and `stroke.end: arrow` when needed", v.Line, v.Type)
	case "rect", "square", "arc", "circle", "ellipse", "polygon":
		return fmt.Errorf("line %d: %q is not a record type; use `path` with explicit points, `closed`, `fill.*`, and `stroke.*`", v.Line, v.Type)
	}
	return nil
}

func (rt *Runtime) mergeRecord(v RecordStmt) error {
	grp := rt.Groups[v.Name]
	if grp == nil {
		return fmt.Errorf("line %d: unknown record %q", v.Line, v.Name)
	}
	for _, fd := range v.Fields {
		if fd.Set || fd.Name == "for" {
			continue
		}
		for _, e := range grp.Items {
			if _, exists := e.Fields[fd.Name]; exists {
				continue
			}
			f := e.field(fd.Name)
			f.Def = fd.E
			f.Rate = fd.Rate
		}
	}
	return nil
}

func (rt *Runtime) evalDataColumns(grp *Group, fields []FieldDef, line int) error {
	acc := map[string]Value{}
	for i, e := range grp.Items {
		cols := map[string]Value{}
		itv, _ := e.It.(ItVal)
		itv.Cols = cols
		for _, fd := range fields {
			s := &Scope{rt: rt, binds: map[string]Value{"it": itv}}
			var val Value
			var err error
			if fold, ok := fd.E.(FoldE); ok {
				if i == 0 {
					val, err = s.Eval(fold.Init)
				} else {
					prevAcc, _ := asFloat(acc[fd.Name])
					prevBy, _ := asFloat(acc["__by_"+fd.Name])
					val = prevAcc + prevBy
				}
				if err != nil {
					return fmt.Errorf("line %d: %v", line, err)
				}
				acc[fd.Name] = val
				// `by` reads this row's column (declared above the fold)
				if byVal, ok := cols[fold.By]; ok {
					acc["__by_"+fd.Name] = byVal
				} else {
					return fmt.Errorf("line %d: scan: column %q not defined above the fold", line, fold.By)
				}
			} else {
				val, err = s.Eval(fd.E)
				if err != nil {
					return fmt.Errorf("line %d: column %s: %v", line, fd.Name, err)
				}
			}
			f := e.field(fd.Name)
			f.Val = val
			f.Def = nil
			f.Frozen = true
			cols[fd.Name] = val
		}
		e.It = itv
	}
	return nil
}

// fold lookup inside the row's own columns
func (rt *Runtime) addCapture(v CaptureStmt, t float64) error {
	g := &GVar{Name: v.Name}
	if _, exists := rt.Globals[v.Name]; exists {
		return fmt.Errorf("line %d: capture %s redefined", v.Line, v.Name)
	}
	rt.Globals[v.Name] = g
	if v.Live {
		// Live colon-binding: the global is re-evaluated each frame.
		// Evaluate immediately so downstream declarations (like family domains) can use this value.
		g.Live = true
		g.Def = v.E
		s := &Scope{rt: rt}
		val, err := s.Eval(v.E)
		if err != nil {
			return fmt.Errorf("line %d: live global %s: %v", v.Line, v.Name, err)
		}
		g.Val = val
		rt.liveGlobals = append(rt.liveGlobals, g)
		return nil
	}
	expr := v.E
	if val, err := (&Scope{rt: rt}).Eval(expr); err == nil {
		g.Val = snapshotValue(val)
	}
	rt.Events = append(rt.Events, &Event{T: t, Line: v.Line, Run: func(rt *Runtime) error {
		s := &Scope{rt: rt}
		val, err := s.Eval(expr)
		if err != nil {
			return fmt.Errorf("line %d: capture %s: %v", v.Line, v.Name, err)
		}
		val = snapshotValue(val)
		g.Val = val
		return nil
	}})
	return nil
}

// addFamily expands a domain-binder family declaration into individual member
// entities and registers the family structure.
func (rt *Runtime) addFamily(v FamilyStmt, scope *Scope) error {
	// Evaluate the domain expression to get a list of indices
	domainVal, err := scope.Eval(v.DomainE)
	if err != nil {
		return fmt.Errorf("line %d: family %s domain: %v", v.Line, v.Name, err)
	}
	var indices []Value
	switch x := domainVal.(type) {
	case []Value:
		indices = x
	default:
		return fmt.Errorf("line %d: family domain must evaluate to a list, got %T", v.Line, domainVal)
	}

	// Build member name list
	var memberNames []string
	names := map[string]string{v.BindVar: "family bind variable"}
	for _, local := range v.Locals {
		if prev, exists := names[local.Name]; exists {
			return fmt.Errorf("line %d: family local %q conflicts with %s", local.Line, local.Name, prev)
		}
		names[local.Name] = "family local"
	}
	for _, mem := range v.Members {
		if prev, exists := names[mem.Name]; exists {
			return fmt.Errorf("line %d: family member %q conflicts with %s", mem.Line, mem.Name, prev)
		}
		names[mem.Name] = "family member"
		memberNames = append(memberNames, mem.Name)
	}

	fam := &RecordFamily{
		Name:     v.Name,
		N:        len(indices),
		KeyToPos: map[int]int{},
		Members:  memberNames,
	}
	rt.Families[v.Name] = fam

	// Create a top-level Group for the family whose Items are synthetic proxy
	// entities (one per instance). These are used for broadcast `roots[*]`.
	famGroup := &Group{Name: v.Name}

	for idxPos, idxVal := range indices {
		idxFloat, err := asFloat(idxVal)
		if err != nil {
			return fmt.Errorf("line %d: family domain element %d: %v", v.Line, idxPos, err)
		}
		if math.Trunc(idxFloat) != idxFloat {
			return fmt.Errorf("line %d: family domain element %d must be an integer, got %v", v.Line, idxPos, idxFloat)
		}
		bindVal := idxFloat
		actualIdx := int(bindVal)
		if _, exists := fam.KeyToPos[actualIdx]; exists {
			return fmt.Errorf("line %d: family domain contains duplicate key %d", v.Line, actualIdx)
		}
		fam.Keys = append(fam.Keys, actualIdx)
		fam.KeyToPos[actualIdx] = idxPos

		// The ItVal for this instance carries the bind variable in Cols.
		// We'll add sibling member references to Cols after all members are created.
		cols := map[string]Value{
			v.BindVar: bindVal,
		}
		for _, local := range v.Locals {
			cols[local.Name] = local
		}
		itVal := ItVal{
			Val:     idxVal,
			I:       idxPos,
			N:       len(indices),
			Cols:    cols,
			LocalID: rt.newLocalID(),
		}

		// Create a proxy entity for this instance in the family group
		proxy := &Entity{
			IsFamilyProxy: true,
			Name:          v.Name,
			Idx:           idxPos,
			N:             len(indices),
			Fields:        map[string]*Field{},
			rt:            rt,
		}
		proxy.It = itVal
		famGroup.Items = append(famGroup.Items, proxy)

		// Create all member entities for this instance first
		for _, mem := range v.Members {
			entityName := familyMemberName(v.Name, actualIdx, mem.Name)
			memberRS := RecordStmt{
				Type:   mem.Type,
				Name:   entityName,
				Fields: mem.Fields,
				Line:   mem.Line,
			}
			if err := rt.addRecordWithIt(memberRS, itVal); err != nil {
				return err
			}
		}

		// Now populate sibling references in Cols so member field expressions
		// can reference each other by short name (e.g. `to: mark.at`).
		memMap := map[string]*Entity{}
		for _, mem := range v.Members {
			entityName := familyMemberName(v.Name, actualIdx, mem.Name)
			if grp, ok := rt.Groups[entityName]; ok && len(grp.Items) == 1 {
				cols[mem.Name] = grp.Items[0]
				memMap[mem.Name] = grp.Items[0]
			}
		}
		if len(memMap) > 0 {
			if fam.MemberAt == nil {
				fam.MemberAt = map[int]map[string]*Entity{}
			}
			fam.MemberAt[actualIdx] = memMap
		}
		// Also update the proxy's It to have the sibling refs
		proxy.It = ItVal{Val: idxVal, I: idxPos, N: len(indices), Cols: cols, LocalID: itVal.LocalID}
		// Update all member entities' It as well
		for _, mem := range v.Members {
			entityName := familyMemberName(v.Name, actualIdx, mem.Name)
			if grp, ok := rt.Groups[entityName]; ok {
				for _, e := range grp.Items {
					e.It = ItVal{Val: idxVal, I: idxPos, N: len(indices), Cols: cols, LocalID: itVal.LocalID}
				}
			}
		}
	}

	rt.Groups[v.Name] = famGroup
	return nil
}

// addRecordWithIt creates a single entity with the given ItVal pre-set.
// This is used by addFamily to create member entities whose field expressions
// reference the family's bind variable (stored in itVal.Cols).
func (rt *Runtime) addRecordWithIt(v RecordStmt, itVal ItVal) error {
	if _, exists := rt.Groups[v.Name]; exists {
		return nil // already created (shouldn't happen in normal flow)
	}
	e := &Entity{
		Type:   v.Type,
		Name:   v.Name,
		Idx:    0,
		N:      1,
		Fields: map[string]*Field{},
		rt:     rt,
	}
	e.It = itVal

	for _, fd := range v.Fields {
		f := e.field(fd.Name)
		f.Def = fd.E
		f.Rate = fd.Rate
	}

	grp := &Group{Name: v.Name, Items: []*Entity{e}}
	rt.Groups[v.Name] = grp
	rt.Entities = append(rt.Entities, e)
	return nil
}

func (rt *Runtime) addSetEvents(v RecordStmt, t float64) {
	grp := rt.Groups[v.Name]
	for _, fd := range v.Fields {
		if !fd.Set {
			continue
		}
		fd := fd
		for _, e := range grp.Items {
			e := e
			if fd.Name == "warp" {
				rt.Events = append(rt.Events, &Event{T: t, Line: fd.Line, Run: func(rt *Runtime) error {
					e.WarpNew = fd.E
					return nil
				}})
				continue
			}
			rt.Events = append(rt.Events, &Event{T: t, Line: fd.Line, Run: func(rt *Runtime) error {
				s := &Scope{rt: rt, binds: map[string]Value{"it": e.It}}
				val, err := s.Eval(fd.E)
				if err != nil {
					return fmt.Errorf("line %d: set %s.%s: %v", fd.Line, e.Name, fd.Name, err)
				}
				f := e.field(fd.Name)
				f.Val = coerceField(fd.Name, val)
				f.Frozen = true // static again, tweenable again
				rt.clearPost(e.Name + "." + fd.Name)
				return nil
			}})
		}
	}
}

// ---------- block expansion ----------

type winState struct {
	from, to float64 // fractions of the block
	ease     string
	stagger  float64 // per-element shift, fraction of block
	pairing  string
}

func applyMods(mods []RowMod, blockDur float64, prevEnd float64, defEase string) winState {
	w := winState{from: 0, to: 1, ease: defEase}
	toFrac := func(v float64, sec bool) float64 {
		if sec {
			return v / blockDur
		}
		return v
	}
	for _, m := range mods {
		switch m.Kind {
		case "win":
			if !math.IsNaN(m.A) {
				w.from = toFrac(m.A, m.ASec)
			} else {
				w.from = 0
			}
			if !math.IsNaN(m.B) {
				w.to = toFrac(m.B, m.BSec)
			} else {
				w.to = 1
			}
		case "after":
			w.from = prevEnd + toFrac(m.D, m.DSec)
		case "lag":
			w.from += toFrac(m.D, m.DSec)
		case "stagger":
			w.stagger = toFrac(m.D, m.DSec)
		case "ease":
			w.ease = m.Name
		case "pair":
			w.pairing = m.Name
		}
	}
	if w.to <= w.from {
		w.to = w.from
	}
	return w
}

func (rt *Runtime) expandBlock(b BlockStmt, start float64) (float64, error) {
	defEase := "smooth"

	if b.Each != "" {
		grp, ok := rt.Groups[b.Each]
		if !ok {
			return 0, fmt.Errorf("line %d: each: unknown record %q", b.Line, b.Each)
		}
		for k, e := range grp.Items {
			binds := map[string]Value{}
			itv, _ := e.It.(ItVal)
			if itv.Cols == nil {
				itv = ItVal{Val: e, I: k, N: len(grp.Items)}
			}
			name := b.As
			if name == "" {
				name = "it"
			}
			binds[name] = itv
			iterStart := start + float64(k)*b.DurS
			if err := rt.expandRows(b.Rows, iterStart, b.DurS, defEase, b.DefMods, binds); err != nil {
				return 0, err
			}
		}
		return float64(len(grp.Items)) * b.DurS, nil
	}

	if err := rt.expandRows(b.Rows, start, b.DurS, defEase, b.DefMods, nil); err != nil {
		return 0, err
	}
	return b.DurS, nil
}

func (rt *Runtime) expandRows(rows []Row, start, dur float64, defEase string, defMods []RowMod, binds map[string]Value) error {
	prevEnd := 0.0
	for _, row := range rows {
		// header defaults come first so per-row modifiers override them; the
		// merged row also feeds rowTransition (a default `morph`/`draw` applies).
		if len(defMods) > 0 {
			merged := make([]RowMod, 0, len(defMods)+len(row.Mods))
			merged = append(merged, defMods...)
			merged = append(merged, row.Mods...)
			row.Mods = merged
		}
		w := applyMods(row.Mods, dur, prevEnd, defEase)
		prevEnd = w.to
		if err := rt.expandRow(row, w, start, dur, binds); err != nil {
			return err
		}
	}
	return nil
}

func findStar(e Expr) bool {
	switch v := e.(type) {
	case IndexE:
		if v.I == nil {
			return true
		}
		return findStar(v.X)
	case AttrE:
		return findStar(v.X)
	case CallE:
		if findStar(v.Fn) {
			return true
		}
		for _, a := range v.Args {
			if findStar(a) {
				return true
			}
		}
	}
	return false
}

// resolveStarBase returns (group entities | parts) under the [*] and the
// BindName if the star was `[* as name]`.
func (rt *Runtime) resolveStarBase(e Expr, binds map[string]Value) (ents []*Entity, parts []*PartState, bindName string, err error) {
	idx, ok := e.(IndexE)
	if !ok || idx.I != nil {
		return nil, nil, "", fmt.Errorf("unsupported broadcast shape")
	}
	bindName = idx.BindName
	s := &Scope{rt: rt, binds: binds}
	base, err := s.Eval(idx.X)
	if err != nil {
		return nil, nil, "", err
	}
	switch v := base.(type) {
	case *Group:
		return v.Items, nil, bindName, nil
	case *Entity:
		return []*Entity{v}, nil, bindName, nil
	case []Value:
		// Broadcasting over a plain list literal (e.g. `val[* as i]`) is not
		// supported; broadcast targets must be groups, entities, or parts.
		return nil, nil, bindName, fmt.Errorf("cannot broadcast over a plain list")
	}
	return nil, nil, "", fmt.Errorf("cannot broadcast over %T", base)
}

// splitStarPath splits LHS path `base[*].rest...` into the starred base expr
// and the trailing attribute names.
func splitStarPath(e Expr) (starBase Expr, trail []string, bindName string, ok bool) {
	switch v := e.(type) {
	case IndexE:
		if v.I == nil {
			return v, nil, v.BindName, true
		}
		return nil, nil, "", false
	case AttrE:
		base, trail, bn, ok := splitStarPath(v.X)
		if !ok {
			return nil, nil, "", false
		}
		return base, append(trail, v.Name), bn, true
	}
	return nil, nil, "", false
}

type exprVariant struct {
	expr  Expr
	binds map[string]Value
}

type starChoice struct {
	index Value
	bind  Value
	it    ItVal
}

func copyBinds(binds map[string]Value) map[string]Value {
	cp := map[string]Value{}
	for k, v := range binds {
		cp[k] = v
	}
	return cp
}

func (rt *Runtime) starChoices(base Value) ([]starChoice, error) {
	switch v := base.(type) {
	case []Value:
		out := make([]starChoice, len(v))
		for i := range v {
			idx := float64(i)
			out[i] = starChoice{
				index: idx,
				bind:  idx,
				it:    ItVal{Val: v[i], I: i, N: len(v)},
			}
		}
		return out, nil
	case *Group:
		if fam, ok := rt.Families[v.Name]; ok {
			out := make([]starChoice, len(fam.Keys))
			for i, key := range fam.Keys {
				idx := float64(key)
				it := ItVal{Val: v.Items[i], I: i, N: len(v.Items)}
				if existing, ok := v.Items[i].It.(ItVal); ok {
					it = existing
				}
				out[i] = starChoice{index: idx, bind: idx, it: it}
			}
			return out, nil
		}
		out := make([]starChoice, len(v.Items))
		for i := range v.Items {
			idx := float64(i)
			it := ItVal{Val: firstNonNil(v.Items[i].It, v.Items[i]), I: i, N: len(v.Items)}
			if existing, ok := v.Items[i].It.(ItVal); ok {
				it = existing
			}
			out[i] = starChoice{index: idx, bind: idx, it: it}
		}
		return out, nil
	case *Entity:
		it := ItVal{Val: firstNonNil(v.It, v), I: 0, N: 1}
		if existing, ok := v.It.(ItVal); ok {
			it = existing
		}
		return []starChoice{{index: 0.0, bind: 0.0, it: it}}, nil
	}
	return nil, fmt.Errorf("cannot broadcast over %T", base)
}

// seqVariant is one element of the cartesian product produced by expandSeq:
// a concrete list of expanded sub-expressions plus the binds threaded through
// them (broadcast `[*]` over `a[*] + b[*]` advances i/j/k/l left-to-right).
type seqVariant struct {
	exprs []Expr
	binds map[string]Value
}

// expandSeq expands each sub-expression in order, threading binds, and returns
// the cartesian product of their `[*]` variants. This is the one place that
// owns "expand a tuple of child expressions"; every composite node (BinE,
// CallE, ListE, …) reconstructs itself from these via mapSeq.
func (rt *Runtime) expandSeq(es []Expr, binds map[string]Value) ([]seqVariant, error) {
	out := []seqVariant{{binds: copyBinds(binds)}}
	for _, e := range es {
		var next []seqVariant
		for _, acc := range out {
			vs, err := rt.expandExprStars(e, acc.binds)
			if err != nil {
				return nil, err
			}
			for _, v := range vs {
				exprs := append(append([]Expr(nil), acc.exprs...), v.expr)
				next = append(next, seqVariant{exprs: exprs, binds: v.binds})
			}
		}
		out = next
	}
	return out, nil
}

// mapSeq rebuilds a node from each expanded child tuple.
func mapSeq(seqs []seqVariant, build func([]Expr) Expr) []exprVariant {
	out := make([]exprVariant, len(seqs))
	for i, s := range seqs {
		out[i] = exprVariant{expr: build(s.exprs), binds: s.binds}
	}
	return out
}

func (rt *Runtime) expandExprStars(e Expr, binds map[string]Value) ([]exprVariant, error) {
	// seq expands the given child expressions and rebuilds the node with build.
	seq := func(es []Expr, build func([]Expr) Expr) ([]exprVariant, error) {
		vs, err := rt.expandSeq(es, binds)
		if err != nil {
			return nil, err
		}
		return mapSeq(vs, build), nil
	}
	switch v := e.(type) {
	case nil, Num, Str, Ident, litVal:
		return []exprVariant{{expr: e, binds: copyBinds(binds)}}, nil
	case AttrE:
		return seq([]Expr{v.X}, func(c []Expr) Expr { return AttrE{X: c[0], Name: v.Name} })
	case UnE:
		return seq([]Expr{v.X}, func(c []Expr) Expr { return UnE{Op: v.Op, X: c[0]} })
	case AlphaE:
		return seq([]Expr{v.X}, func(c []Expr) Expr { return AlphaE{X: c[0], Pct: v.Pct} })
	case SnapshotE:
		return seq([]Expr{v.X}, func(c []Expr) Expr { return SnapshotE{X: c[0]} })
	case BinE:
		return seq([]Expr{v.L, v.R}, func(c []Expr) Expr { return BinE{Op: v.Op, L: c[0], R: c[1]} })
	case RangeE:
		return seq([]Expr{v.Start, v.End}, func(c []Expr) Expr { return RangeE{Start: c[0], End: c[1]} })
	case CondE:
		return seq([]Expr{v.Cond, v.Then, v.Else}, func(c []Expr) Expr {
			return CondE{Cond: c[0], Then: c[1], Else: c[2]}
		})
	case ListE:
		return seq(v.Items, func(c []Expr) Expr { return ListE{Items: c} })
	case CallE:
		return seq(append([]Expr{v.Fn}, v.Args...), func(c []Expr) Expr {
			return CallE{Fn: c[0], Args: c[1:]}
		})
	case GeomE:
		es := make([]Expr, len(v.Fields))
		for i, fd := range v.Fields {
			es[i] = fd.E
		}
		return seq(es, func(c []Expr) Expr {
			fields := make([]FieldDef, len(c))
			for i := range c {
				fields[i] = FieldDef{Name: v.Fields[i].Name, E: c[i], Line: v.Fields[i].Line}
			}
			return GeomE{Name: v.Name, Fields: fields}
		})
	case IndexE:
		if v.I != nil {
			return seq([]Expr{v.X, v.I}, func(c []Expr) Expr { return IndexE{X: c[0], I: c[1]} })
		}
		// `[*]` star index: expand the base, then fan out over its elements,
		// binding i/j/k/l and `it` for each choice.
		xs, err := rt.expandExprStars(v.X, binds)
		if err != nil {
			return nil, err
		}
		var out []exprVariant
		for _, x := range xs {
			base, err := (&Scope{rt: rt, binds: x.binds}).Eval(x.expr)
			if err != nil {
				return nil, err
			}
			choices, err := rt.starChoices(base)
			if err != nil {
				return nil, err
			}
			for _, choice := range choices {
				nb := copyBinds(x.binds)
				count := 0
				if n, ok := nb["__star_count"]; ok {
					if f, err := asFloat(n); err == nil {
						count = int(f)
					}
				}
				nb["__star_count"] = float64(count + 1)
				if count < len("ijkl") {
					nb[string("ijkl"[count])] = choice.bind
				}
				switch current := nb["it"].(type) {
				case nil:
					nb["it"] = choice.it
				case []Value:
					nb["it"] = append(append([]Value(nil), current...), choice.it)
				default:
					nb["it"] = []Value{current, choice.it}
				}
				if v.BindName != "" {
					nb[v.BindName] = choice.bind
				}
				out = append(out, exprVariant{
					expr:  IndexE{X: x.expr, I: litVal{V: choice.index}},
					binds: nb,
				})
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("cannot expand broadcast in %T", e)
}

func (rt *Runtime) expandRowVariants(lhs, rhs Expr, binds map[string]Value) ([]struct{ lhs, rhs exprVariant }, error) {
	lhsVars, err := rt.expandExprStars(lhs, binds)
	if err != nil {
		return nil, err
	}
	var out []struct{ lhs, rhs exprVariant }
	for _, lv := range lhsVars {
		rhsVars, err := rt.expandExprStars(rhs, lv.binds)
		if err != nil {
			return nil, err
		}
		for _, rv := range rhsVars {
			out = append(out, struct{ lhs, rhs exprVariant }{lhs: lv, rhs: rv})
		}
	}
	return out, nil
}

func checkRowTargetConflicts(line int, seen map[string]bool, targets []Ref) error {
	for _, ref := range targets {
		if ref == nil {
			continue
		}
		key := ref.Key()
		if seen[key] {
			return fmt.Errorf("line %d: broadcast expansion writes %s more than once in the same row", line, key)
		}
		seen[key] = true
	}
	return nil
}

func (rt *Runtime) checkBoundExpr(e Expr, binds map[string]Value) error {
	var walk func(Expr) error
	walk = func(e Expr) error {
		switch v := e.(type) {
		case nil, Num, Str, litVal:
			return nil
		case Ident:
			name := string(v)
			if _, ok := binds[name]; ok {
				return nil
			}
			if _, ok := rt.Globals[name]; ok {
				return nil
			}
			if _, ok := rt.Groups[name]; ok {
				return nil
			}
			if _, ok := namedColors[name]; ok {
				return nil
			}
			if _, ok := namedVecs[name]; ok {
				return nil
			}
			if _, ok := namedNums[name]; ok {
				return nil
			}
			if _, ok := namedStrings[name]; ok {
				return nil
			}
			if _, ok := namespaces[name]; ok {
				return nil
			}
			if _, ok := builtins[name]; ok {
				return nil
			}
			return fmt.Errorf("unbound name %q", name)
		case AttrE:
			return walk(v.X)
		case IndexE:
			if err := walk(v.X); err != nil {
				return err
			}
			if v.I != nil {
				return walk(v.I)
			}
		case CallE:
			if err := walk(v.Fn); err != nil {
				return err
			}
			for _, a := range v.Args {
				if err := walk(a); err != nil {
					return err
				}
			}
		case BinE:
			if err := walk(v.L); err != nil {
				return err
			}
			return walk(v.R)
		case RangeE:
			if err := walk(v.Start); err != nil {
				return err
			}
			return walk(v.End)
		case UnE:
			return walk(v.X)
		case ListE:
			for _, it := range v.Items {
				if err := walk(it); err != nil {
					return err
				}
			}
		case CondE:
			if err := walk(v.Cond); err != nil {
				return err
			}
			if err := walk(v.Then); err != nil {
				return err
			}
			return walk(v.Else)
		case AlphaE:
			return walk(v.X)
		case SnapshotE:
			return walk(v.X)
		}
		return nil
	}
	return walk(e)
}

func (rt *Runtime) expandRow(row Row, w winState, start, dur float64, binds map[string]Value) error {
	mkAnim := func(from, to float64, elemBinds map[string]Value) *Anim {
		t0 := start + from*dur
		t1 := start + to*dur
		if t1 < t0 {
			t1 = t0
		}
		ease := easings[w.ease]
		if ease == nil {
			ease = easings["smooth"]
		}
		merged := map[string]Value{}
		for k, v := range binds {
			merged[k] = v
		}
		for k, v := range elemBinds {
			merged[k] = v
		}
		return &Anim{T0: t0, T1: t1, Ease: ease, Binds: merged, Line: row.Line}
	}

	switch row.Op.Kind {
	case "enter":
		return rt.expandEntrance(row, w, mkAnim, false)
	case "exit":
		return rt.expandEntrance(row, w, mkAnim, true)
	case "highlight":
		return rt.expandHighlight(row, w, mkAnim)
	case "arrow":
		// handled below
	default:
		return fmt.Errorf("line %d: unsupported row operation %q", row.Line, row.Op.Kind)
	}
	transition := rowTransition(row)
	if transition == "morph" {
		return rt.expandMorph(row, w, mkAnim)
	}
	return rt.expandArrow(row, w, mkAnim)
}

func rowTransition(row Row) string {
	tr := row.Op.Transition
	for _, mod := range row.Mods {
		if mod.Kind == "transition" {
			tr = mod.Name
		}
	}
	return tr
}

func (rt *Runtime) verbSubjects(lhs Expr, binds map[string]Value) ([]*Entity, []*PartState, error) {
	if findStar(lhs) {
		base, trail, _, ok := splitStarPath(lhs)
		if !ok || len(trail) > 0 {
			return nil, nil, fmt.Errorf("unsupported broadcast verb subject")
		}
		ents, parts, _, err := rt.resolveStarBase(base, binds)
		return ents, parts, err
	}
	s := &Scope{rt: rt, binds: binds}
	v, err := s.Eval(lhs)
	if err != nil {
		return nil, nil, err
	}
	switch x := v.(type) {
	case *Entity:
		return []*Entity{x}, nil, nil
	case *Group:
		return x.Items, nil, nil
	case *PartState:
		return nil, []*PartState{x}, nil
	}
	return nil, nil, fmt.Errorf("verb subject is %T", v)
}

func (rt *Runtime) expandArrow(row Row, w winState, mk func(from, to float64, eb map[string]Value) *Anim) error {
	lhs, rhs := row.Op.LHS, row.Op.RHS
	binds := mk(0, 0, nil).Binds

	variants, err := rt.expandRowVariants(lhs, rhs, binds)
	if err != nil {
		return fmt.Errorf("line %d: %v", row.Line, err)
	}
	seenTargets := map[string]bool{}
	for k, variant := range variants {
		if err := rt.checkBoundExpr(variant.rhs.expr, variant.rhs.binds); err != nil {
			return fmt.Errorf("line %d: %v", row.Line, err)
		}
		ref, special, err := rt.resolveRef(variant.lhs.expr, variant.rhs.binds)
		if err != nil {
			return fmt.Errorf("line %d: %v", row.Line, err)
		}
		from := w.from + float64(k)*w.stagger
		a := mk(from, w.to, variant.rhs.binds)
		switch special {
		case "record":
			rt.fillRecordArrow(a, variant.lhs.expr, variant.rhs.expr, variant.rhs.binds)
		case "warp":
			rt.fillWarpArrow(a, ref)
		default:
			fillTween(a, ref, variant.rhs.expr, -1, 0)
		}
		if err := checkRowTargetConflicts(row.Line, seenTargets, a.Targets); err != nil {
			return err
		}
		rt.Anims = append(rt.Anims, a)
	}
	return nil
}

// transientEnvelope maps eased progress u in [0,1] to a there-and-back amplitude
// (0 at the ends, 1 in the middle) so a modifier row lights its channel up and
// settles it back to rest within the window.
func transientEnvelope(u float64) float64 {
	return math.Sin(math.Pi * clamp01(u))
}

// enlargePeak is the scale a transient `enlarge` modifier swells a span to at the
// envelope's midpoint before settling back to 1.
const enlargePeak = 1.5

// expandHighlight compiles a transient text modifier (`| wiggle | t.sub("x")`).
// The named channel is driven through transientEnvelope and left at rest — unlike
// the persistent `->` arrow that sets-and-holds.
func (rt *Runtime) expandHighlight(row Row, w winState, mk func(from, to float64, eb map[string]Value) *Anim) error {
	binds := mk(0, 0, nil).Binds
	channel := highlightChannel[row.Op.Highlight]
	for _, subject := range rowSubjects(row) {
		variants, err := rt.expandExprStars(subject, binds)
		if err != nil {
			return fmt.Errorf("line %d: %v", row.Line, err)
		}
		for k, variant := range variants {
			v, err := (&Scope{rt: rt, binds: variant.binds}).Eval(variant.expr)
			if err != nil {
				return fmt.Errorf("line %d: %v", row.Line, err)
			}
			p, ok := v.(*PartState)
			if !ok {
				return fmt.Errorf("line %d: transient modifier %q applies to a text span; select one with `text.sub(\"...\")`", row.Line, row.Op.Highlight)
			}
			from := w.from + float64(k)*w.stagger
			a := mk(from, w.to, variant.binds)
			fillHighlight(a, p, channel)
			rt.Anims = append(rt.Anims, a)
		}
	}
	return nil
}

// fillHighlight drives one part channel through the transient envelope. Every
// channel returns to its rest value at the window's end: 0 for the rule/shake
// channels, 1 for scale, and the span's inherited colour for a flash.
func fillHighlight(a *Anim, p *PartState, channel string) {
	clearPostOnStart := func(ref Ref) {
		a.Start = func(a *Anim, rt *Runtime) error {
			rt.clearPost(ref.Key())
			return nil
		}
	}
	switch channel {
	case "color":
		ref := PartColorRef{P: p}
		a.Targets = []Ref{ref}
		target := namedColors["yellow"]
		var (
			base  Color
			rest  Value
			hadPC bool
		)
		a.Start = func(a *Anim, rt *Runtime) error {
			rt.clearPost(ref.Key())
			rest = p.Color
			hadPC = p.Color != nil
			base, _ = asColor(ref.Get())
			return nil
		}
		a.Update = func(a *Anim, rt *Runtime, u float64) {
			env := transientEnvelope(u)
			if env < 1e-4 {
				if hadPC {
					p.Color = rest
				} else {
					p.Color = nil
				}
				return
			}
			p.Color = lerpValue(base, target, env)
		}
	case "scale":
		ref := partFloatRef{P: p, Attr: "scale"}
		a.Targets = []Ref{ref}
		clearPostOnStart(ref)
		a.Update = func(a *Anim, rt *Runtime, u float64) {
			ref.Set(1 + (enlargePeak-1)*transientEnvelope(u))
		}
	case "strike", "underline":
		// Sweep the rule across the span: the head (right edge) wipes on
		// left→right over the first half of the window, then the tail (left
		// edge) chases it over the second half, so the line enters on one side
		// and leaves on the other rather than retreating the way it came. Both
		// edges meet at the far side and the rule clears itself by the end.
		ref := partFloatRef{P: p, Attr: channel}
		a.Targets = []Ref{ref}
		setTail := func(v float64) { p.UnderlineTail = v }
		if channel == "strike" {
			setTail = func(v float64) { p.StrikeTail = v }
		}
		a.Update = func(a *Anim, rt *Runtime, u float64) {
			head, tail := clamp01(2*u), clamp01(2*u-1)
			if u >= 0.999 {
				head, tail = 0, 0 // settle to a clean, empty rest
			}
			ref.Set(head)
			setTail(tail)
		}
		clearPostOnStart(ref)
	default: // wiggle — a 0→1→0 amplitude
		ref := partFloatRef{P: p, Attr: channel}
		a.Targets = []Ref{ref}
		clearPostOnStart(ref)
		a.Update = func(a *Anim, rt *Runtime, u float64) {
			ref.Set(transientEnvelope(u))
		}
	}
}

// litVal is an Expr that evaluates to a captured Value verbatim. It lets the
// object update tween synthesise a goal expression for a field that has no
// declaration (its goal is just the type default, e.g. draw/opacity = 1).
type litVal struct{ V Value }

// entrancePresets maps an `in:`/`ou:` preset name to the fields it animates and
// the hidden value each takes before (entrance) or after (exit) the window.
type presetField struct {
	name   string
	hidden float64
}

var entrancePresets = map[string][]presetField{
	"draw":      {{"draw", 0}},
	"fade":      {{"opacity", 0}},
	"pop":       {{"opacity", 0}, {"scale", 0.2}},
	"draw_fade": {{"draw", 0}, {"opacity", 0}},
}

func entitiesOf(v Value) []*Entity {
	switch x := v.(type) {
	case *Entity:
		return []*Entity{x}
	case *Group:
		return x.Items
	}
	return nil
}

// expandEntrance compiles `in:PRESET | subject` (entrance) and `ou:PRESET |
// subject` (exit). For each preset field it tweens between the subject's declared
// value and the preset's hidden value: entrance snaps to hidden then tweens up
// (marking the subject present), exit tweens declared→hidden then leaves. The
// subject may broadcast (`ring[* as i].p`); each expansion is one self-transition,
// never a morph.
func (rt *Runtime) expandEntrance(row Row, w winState, mk func(from, to float64, eb map[string]Value) *Anim, isExit bool) error {
	preset := entrancePresets[row.Op.Preset]
	binds := mk(0, 0, nil).Binds
	for _, subject := range rowSubjects(row) {
		variants, err := rt.expandExprStars(subject, binds)
		if err != nil {
			return fmt.Errorf("line %d: %v", row.Line, err)
		}
		for k, variant := range variants {
			v, err := (&Scope{rt: rt, binds: variant.binds}).Eval(variant.expr)
			if err != nil {
				return fmt.Errorf("line %d: %v", row.Line, err)
			}
			ents := entitiesOf(v)
			if ents == nil {
				return fmt.Errorf("line %d: %s: subject is %T, want an object", row.Line, row.Op.Preset, v)
			}
			from := w.from + float64(k)*w.stagger
			for _, e := range ents {
				for _, pf := range preset {
					f := e.field(pf.name)
					a := mk(from, w.to, variant.binds)
					if isExit {
						fillExit(a, e, f, pf.hidden)
					} else {
						fillEntrance(a, e, f, pf.hidden)
					}
					rt.Anims = append(rt.Anims, a)
				}
			}
		}
	}
	return nil
}

func rowSubjects(row Row) []Expr {
	if len(row.Op.Subjects) > 0 {
		return row.Op.Subjects
	}
	return []Expr{row.Op.LHS}
}

// fillEntrance snaps the field to its hidden value (frozen, so initFields and the
// live evaluator leave it alone) and tweens back to the declared value, marking
// the entity present when the window starts.
func fillEntrance(a *Anim, e *Entity, f *Field, hidden float64) {
	var goal Expr
	if f.Def != nil {
		goal = f.Def
	} else {
		goal = litVal{V: f.Val}
	}
	f.Val = coerceField(f.Name, hidden)
	f.Def = nil
	f.Live = false
	f.Frozen = true
	fillTween(a, FieldRef{E: e, F: f}, goal, -1, 0)
	prevStart := a.Start
	a.Start = func(a *Anim, rt *Runtime) error {
		e.Active = true
		if prevStart != nil {
			return prevStart(a, rt)
		}
		return nil
	}
}

// fillExit tweens the field from its current declared value down to the hidden
// value, freezing it at the start so live-eval can't fight the tween, and marks
// the entity absent once the window completes.
func fillExit(a *Anim, e *Entity, f *Field, hidden float64) {
	fillTween(a, FieldRef{E: e, F: f}, litVal{V: hidden}, -1, 0)
	prevStart := a.Start
	a.Start = func(a *Anim, rt *Runtime) error {
		f.Def = nil
		f.Live = false
		f.Frozen = true
		if prevStart != nil {
			return prevStart(a, rt)
		}
		return nil
	}
	prevUpdate := a.Update
	a.Update = func(a *Anim, rt *Runtime, u float64) {
		if prevUpdate != nil {
			prevUpdate(a, rt, u)
		}
		if u >= 1 {
			e.Active = false
		}
	}
}

func firstNonNil(a, b Value) Value {
	if a != nil {
		return a
	}
	return b
}

// refForTrail resolves entity field paths like ["scale"], ["at"], or
// material fields spelled as dotted attributes (`shape.stroke.color`).
func refForTrail(e *Entity, trail []string) (Ref, error) {
	if len(trail) == 0 {
		return nil, fmt.Errorf("unsupported field path depth %v", trail)
	}
	name := strings.Join(trail, ".")
	return FieldRef{E: e, F: e.field(name)}, nil
}

// resolveRef resolves a non-broadcast arrow LHS. Returns special = "record"
// for whole-record arrows, "warp" for the warp blend.
func (rt *Runtime) resolveRef(lhs Expr, binds map[string]Value) (Ref, string, error) {
	switch v := lhs.(type) {
	case Ident:
		name := string(v)
		if g, ok := rt.Globals[name]; ok {
			return GlobalRef{V: g}, "", nil
		}
		if grp, ok := rt.Groups[name]; ok && len(grp.Items) == 1 {
			return nil, "record", nil
		}
		return nil, "", fmt.Errorf("unknown arrow target %q", name)
	case AttrE:
		if ent, trail, ok := rt.entityTrailRef(v, binds); ok {
			if len(trail) == 1 && trail[0] == "warp" {
				return WarpBlendRef{E: ent}, "warp", nil
			}
			ref, err := refForTrail(ent, trail)
			return ref, "", err
		}
		s := &Scope{rt: rt, binds: binds}
		base, err := s.Eval(v.X)
		if err != nil {
			return nil, "", err
		}
		switch b := base.(type) {
		case *Entity:
			if v.Name == "warp" {
				return WarpBlendRef{E: b}, "warp", nil
			}
			return FieldRef{E: b, F: b.field(v.Name)}, "", nil
		case *PartState:
			if ref, ok := partRefFor(b, v.Name); ok {
				return ref, "", nil
			}
		case FamilyInstance:
			// e.g. roots[i].mark — not valid as a tween target without a field
			return nil, "", fmt.Errorf("use `roots[i].%s.fieldname` to tween a field", v.Name)
		}
		return nil, "", fmt.Errorf("cannot tween %T.%s", base, v.Name)
	case IndexE:
		// list[k] — arrow into a list element of a global
		if v.I != nil {
			if id, ok := v.X.(Ident); ok {
				if g, ok2 := rt.Globals[string(id)]; ok2 {
					s := &Scope{rt: rt, binds: binds}
					idxVal, err := s.Eval(v.I)
					if err != nil {
						return nil, "", err
					}
					idxF, err := asFloat(idxVal)
					if err != nil {
						return nil, "", err
					}
					return ListElemRef{G: g, Idx: int(idxF)}, "", nil
				}
			}
		}
		s := &Scope{rt: rt, binds: binds}
		val, err := s.Eval(v)
		if err != nil {
			return nil, "", err
		}
		if _, ok := val.(*Entity); ok {
			return nil, "", fmt.Errorf("whole-element arrows need a field")
		}
		return nil, "", fmt.Errorf("bad arrow target")
	}
	return nil, "", fmt.Errorf("bad arrow target %T", lhs)
}

func (rt *Runtime) entityTrailRef(lhs Expr, binds map[string]Value) (*Entity, []string, bool) {
	var trail []string
	cur := lhs
	for {
		attr, ok := cur.(AttrE)
		if !ok {
			break
		}
		trail = append([]string{attr.Name}, trail...)
		cur = attr.X
	}
	if len(trail) == 0 {
		return nil, nil, false
	}
	s := &Scope{rt: rt, binds: binds}
	base, err := s.Eval(cur)
	if err != nil {
		return nil, nil, false
	}
	e, ok := base.(*Entity)
	if !ok {
		return nil, nil, false
	}
	return e, trail, true
}

// tweenAnim is the typed state for a standard field tween (`field -> expr`). It
// also carries the text-morph special case: when the tweened field is a text
// entity's glyph string, the outline is morphed contour-by-contour up to
// textMorphReadU, then the crisp destination string is committed.
type tweenAnim struct {
	// plan
	ref       Ref
	fr        FieldRef // valid when isField
	isField   bool
	textField bool // tweening a text entity's `text` field → morph glyphs, not lerp
	rhs       Expr
	elemIdx   int
	elemN     int

	// run-time (resolved in start)
	from Value
	goal Value
	tm   *textMorphAnim
}

// fillTween: standard tween; elemIdx >= 0 enables list-RHS positional pairing.
func fillTween(a *Anim, ref Ref, rhs Expr, elemIdx, elemN int) {
	a.Targets = []Ref{ref}
	a.Inputs = exprInputDeps(rhs)
	fr, isField := ref.(FieldRef)
	t := &tweenAnim{
		ref:     ref,
		fr:      fr,
		isField: isField,
		rhs:     rhs,
		elemIdx: elemIdx,
		elemN:   elemN,
	}
	t.textField = isField && fr.F != nil && fr.E != nil &&
		fr.F.Name == "text" && isTextType(fr.E.Type)
	a.drive(t)
}

func (t *tweenAnim) start(a *Anim, rt *Runtime) error {
	rt.clearPost(t.ref.Key())
	cur := t.ref.Get()
	goal, err := evalTweenGoal(rt, t.rhs, a.Binds, t.elemIdx, t.elemN, cur)
	if err != nil {
		return fmt.Errorf("line %d: %v", a.Line, err)
	}
	t.from, t.goal = cur, goal
	if t.textField {
		if fromStr, ok := valueAsString(cur); ok {
			if goalStr, ok := valueAsString(goal); ok && fromStr != goalStr {
				if tm, ok := prepareTextMorphAnim(t.fr.E, fromStr, goalStr); ok {
					t.tm = tm
				}
			}
		}
	}
	return nil
}

func (t *tweenAnim) step(a *Anim, rt *Runtime, u float64) {
	if goal, err := evalTweenGoal(rt, t.rhs, a.Binds, t.elemIdx, t.elemN, t.ref.Get()); err == nil {
		t.goal = goal
	}
	if t.tm != nil {
		t.stepTextMorph(a, rt, u)
		return
	}
	from := t.from
	if t.isField && t.fr.F != nil && t.fr.F.Live {
		from = t.ref.Get() // re-read: live-eval already ran this frame
	}
	t.ref.Set(lerpValue(from, t.goal, u))
	if u >= 1 && len(a.Inputs) > 0 {
		rt.setPost(t.ref, t.rhs, a.Binds, t.elemIdx, t.elemN)
	}
}

func (t *tweenAnim) stepTextMorph(a *Anim, rt *Runtime, u float64) {
	if !t.tm.readable {
		applyTextMorphStep(t.tm, u)
	}
	if !t.tm.readable && u >= textMorphReadU {
		t.ref.Set(t.goal)
		clearTextMorph(t.tm.E)
		t.tm.readable = true
	}
	if u >= 1 {
		if !t.tm.readable {
			t.ref.Set(t.goal)
			clearTextMorph(t.tm.E)
		}
		if len(a.Inputs) > 0 {
			rt.setPost(t.ref, t.rhs, a.Binds, t.elemIdx, t.elemN)
		}
	}
}

func valueAsString(v Value) (string, bool) {
	switch x := v.(type) {
	case Str:
		return string(x), true
	case string:
		return x, true
	}
	return "", false
}

func withEntityTextValue(e *Entity, text string, fn func()) {
	f := e.field("text")
	saved := f.Val
	f.Val = Str(text)
	e.layoutCache = nil
	e.layoutKey = ""
	fn()
	f.Val = saved
	e.layoutCache = nil
	e.layoutKey = ""
}

func textMorphLoopsFor(e *Entity, text string) [][]Vec {
	var out [][]Vec
	withEntityTextValue(e, text, func() {
		out = morphLoops(e)
	})
	return out
}

func prepareTextMorphAnim(e *Entity, fromStr, goalStr string) (*textMorphAnim, bool) {
	src := textMorphLoopsFor(e, fromStr)
	dst := textMorphLoopsFor(e, goalStr)
	if len(src) == 0 || len(dst) == 0 {
		return nil, false
	}
	srcPairs, dstPairs := matchLoops(src, dst, morphSamples)
	if len(srcPairs) == 0 || len(srcPairs) != len(dstPairs) {
		return nil, false
	}
	return &textMorphAnim{
		E:        e,
		srcCtrs:  srcPairs,
		dstCtrs:  dstPairs,
		srcStyle: shapeStyleForMorph(e),
		dstStyle: shapeStyleForMorph(e),
	}, true
}

func applyTextMorphStep(tm *textMorphAnim, u float64) {
	e := tm.E
	e.MorphContours = lerpLoops(tm.srcCtrs, tm.dstCtrs, u)
	col := entityColor(e)
	op := e.fnum("opacity")
	if op == 0 {
		op = 1
	}
	e.MorphHasFill = col.A*op > 1e-6
	e.MorphFill = Color{R: col.R, G: col.G, B: col.B, A: col.A * op}
	e.MorphHasStroke = false
	e.MorphStrokeW = 0
}

func clearTextMorph(e *Entity) {
	e.MorphContours = nil
	e.MorphHasFill = false
	e.MorphHasStroke = false
	e.MorphStrokeW = 0
	e.layoutCache = nil
	e.layoutKey = ""
}

// warpAnim ramps an entity's warp blend from its current value to 1.
type warpAnim struct {
	ref  Ref
	from float64
}

func (rt *Runtime) fillWarpArrow(a *Anim, ref Ref) {
	a.Targets = []Ref{ref}
	a.drive(&warpAnim{ref: ref})
}

func (wa *warpAnim) start(a *Anim, rt *Runtime) error {
	rt.clearPost(wa.ref.Key())
	wa.from, _ = asFloat(wa.ref.Get())
	return nil
}

func (wa *warpAnim) step(a *Anim, rt *Runtime, u float64) {
	wa.ref.Set(lerp(wa.from, 1, u))
}

// recordArrowAnim tweens every field of a record toward a snapshot of the RHS,
// paired by name (`frame -> home`).
type recordArrowAnim struct {
	lhs, rhs Expr
	binds    map[string]Value

	refs []Ref
	from []Value
	goal []Value
}

// record arrow: `frame -> home` — field-wise tweens paired by name.
func (rt *Runtime) fillRecordArrow(a *Anim, lhs, rhs Expr, binds map[string]Value) {
	a.Inputs = exprInputDeps(rhs)
	a.drive(&recordArrowAnim{lhs: lhs, rhs: rhs, binds: binds})
	// static target list for liveness: all fields of the record
	if id, ok := lhs.(Ident); ok {
		if grp, ok := rt.Groups[string(id)]; ok && len(grp.Items) == 1 {
			e := grp.Items[0]
			for _, name := range e.Order {
				a.Targets = append(a.Targets, FieldRef{E: e, F: e.Fields[name]})
			}
		}
	}
}

func (ra *recordArrowAnim) start(a *Anim, rt *Runtime) error {
	s := &Scope{rt: rt, binds: ra.binds}
	lv, err := s.Eval(ra.lhs)
	if err != nil {
		return err
	}
	e, ok := lv.(*Entity)
	if !ok {
		return fmt.Errorf("line %d: record arrow target is %T", a.Line, lv)
	}
	rv, err := s.Eval(ra.rhs)
	if err != nil {
		return err
	}
	snap, ok := rv.(Snapshot)
	if !ok {
		return fmt.Errorf("line %d: record arrow RHS must be a snapshot, got %T", a.Line, rv)
	}
	ra.refs, ra.from, ra.goal = nil, nil, nil
	for name, goal := range snap.Fields {
		if goal == nil {
			continue
		}
		f, ok := e.Fields[name]
		if !ok {
			continue
		}
		ra.refs = append(ra.refs, FieldRef{E: e, F: f})
		rt.clearPost(e.Name + "." + name)
		ra.from = append(ra.from, f.Val)
		ra.goal = append(ra.goal, goal)
	}
	a.Targets = ra.refs
	return nil
}

func (ra *recordArrowAnim) step(a *Anim, rt *Runtime, u float64) {
	for i, r := range ra.refs {
		r.Set(lerpValue(ra.from[i], ra.goal[i], u))
	}
}
