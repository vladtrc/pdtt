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

var easings = map[string]easeFn{
	"linear":  func(u float64) float64 { return u },
	"smooth":  func(u float64) float64 { return u * u * (3 - 2*u) },
	"ease_in": func(u float64) float64 { return u * u },
	"ease_out": func(u float64) float64 {
		return 1 - (1-u)*(1-u)
	},
	"ease_in_out": func(u float64) float64 { return u * u * (3 - 2*u) },
}

// Ref is a tweenable storage location.
type Ref interface {
	Get() Value
	Set(v Value)
	Key() string // liveness key: "name" or "entity.field"
}

type FieldRef struct {
	E *Entity
	F *Field
}

func (r FieldRef) Get() Value { return r.F.Val }
func (r FieldRef) Set(v Value) {
	r.F.Val = coerceField(r.F.Name, v)
	if r.E.Type == "frame" {
		coupleFrame(r.E, r.F.Name)
	}
}
func (r FieldRef) Key() string { return r.E.Name + "." + r.F.Name }

// frame keeps its aspect: writing w drives h and vice versa (spec §6: the
// camera is an ordinary record; the coupling is the record's own physics).
func coupleFrame(e *Entity, written string) {
	const aspect = frameW0 / frameH0
	switch written {
	case "w":
		e.field("h").Val = e.fnum("w") / aspect
	case "h":
		e.field("w").Val = e.fnum("h") * aspect
	}
}

func coerceField(name string, v Value) Value {
	if name == "at" {
		if _, ok := v.(AnchorPt); ok {
			return v
		}
	}
	switch name {
	case "at", "from", "to":
		if vec, err := asVec(v); err == nil {
			return vec
		}
	}
	return v
}

type GlobalRef struct{ V *GVar }

func (r GlobalRef) Get() Value  { return r.V.Val }
func (r GlobalRef) Set(v Value) { r.V.Val = v }
func (r GlobalRef) Key() string { return r.V.Name }

type PartColorRef struct{ P *PartState }

func (r PartColorRef) Get() Value {
	if r.P.Color != nil {
		return r.P.Color
	}
	return entityColor(r.P.E)
}
func (r PartColorRef) Set(v Value) { r.P.Color = v }
func (r PartColorRef) Key() string { return r.P.E.Name + ".parts." + r.P.Name + ".color" }

type PartOpacityRef struct{ P *PartState }

func (r PartOpacityRef) Get() Value { return r.P.Opacity }
func (r PartOpacityRef) Set(v Value) {
	f, err := asFloat(v)
	if err == nil {
		r.P.Opacity = f
	}
}
func (r PartOpacityRef) Key() string { return r.P.E.Name + ".parts." + r.P.Name + ".opacity" }

type OffsetRef struct{ E *Entity }

func (r OffsetRef) Get() Value  { return r.E.Offset }
func (r OffsetRef) Set(v Value) { vec, _ := asVec(v); r.E.Offset = vec }
func (r OffsetRef) Key() string { return r.E.Name + ".__offset" }

type RevealRef struct{ E *Entity }

func (r RevealRef) Get() Value  { return r.E.Reveal }
func (r RevealRef) Set(v Value) { f, _ := asFloat(v); r.E.Reveal = f }
func (r RevealRef) Key() string { return r.E.Name + ".__reveal" }

type WarpBlendRef struct{ E *Entity }

func (r WarpBlendRef) Get() Value  { return r.E.WarpBlend }
func (r WarpBlendRef) Set(v Value) { f, _ := asFloat(v); r.E.WarpBlend = f }
func (r WarpBlendRef) Key() string { return r.E.Name + ".__warp" }

// Anim is one expanded row element: a window plus an apply function.
type Anim struct {
	T0, T1  float64
	Ease    easeFn
	Targets []Ref
	Binds   map[string]Value
	Start   func(a *Anim, rt *Runtime) error
	Update  func(a *Anim, rt *Runtime, u float64)
	Line    int

	started, done bool
	start         []Value // captured start values per target
	goal          []Value
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
	Entities  []*Entity // render order = declaration order
	Frame     *Entity
	Anims     []*Anim
	Events    []*Event
	Total     float64
	T, Dt     float64

	liveFields []fieldSlot
	post       map[string]*PostAssign
	warned     map[string]bool
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

func NewRuntime() *Runtime {
	rt := &Runtime{
		Globals: map[string]*GVar{},
		Groups:  map[string]*Group{},
		post:    map[string]*PostAssign{},
	}
	f := &Entity{Type: "frame", Name: "frame", Fields: map[string]*Field{}, Reveal: 1, rt: rt}
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

func evalTweenGoal(rt *Runtime, rhs Expr, binds map[string]Value, elemIdx, elemN int, cur Value) (Value, error) {
	s := &Scope{rt: rt, binds: binds}
	goal, err := s.Eval(rhs)
	if err != nil {
		return nil, err
	}
	if list, ok := goal.([]Value); ok && elemIdx >= 0 && len(list) == elemN {
		goal = list[elemIdx]
	}
	if cur != nil {
		if _, isVec := cur.(Vec); isVec {
			if g, err := asVec(goal); err == nil {
				goal = g
			}
		}
	}
	return goal, nil
}

func Compile(stmts []Stmt) (*Runtime, error) {
	rt := NewRuntime()
	cursor := 0.0
	scope := &Scope{rt: rt}

	for _, st := range stmts {
		switch v := st.(type) {
		case SceneStmt:
			rt.SceneName = v.Name
		case ExternStmt:
			if _, ok := builtins[v.Name]; !ok {
				return nil, fmt.Errorf("extern fn %s has no Go stub in this prototype", v.Name)
			}
		case CaptureStmt:
			if err := rt.addCapture(v, cursor); err != nil {
				return nil, err
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
						return nil, err
					}
				}
				if hasSet {
					if err := rt.addSetEvents(v, cursor); err != nil {
						return nil, err
					}
				}
			} else if err := rt.addRecord(v, scope); err != nil {
				return nil, err
			}
		case BlockStmt:
			dur, err := rt.expandBlock(v, cursor)
			if err != nil {
				return nil, err
			}
			cursor += dur
		}
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
			Fields: map[string]*Field{}, Reveal: 1, rt: rt,
		}
		if len(its) > 1 || forE != nil {
			e.It = ItVal{Val: it, I: i, N: len(its)}
		}
		for _, fd := range fields {
			if fd.Name == "parts" {
				if list, ok := fd.E.(ListE); ok {
					for _, item := range list.Items {
						if id, ok := item.(Ident); ok {
							e.Parts = append(e.Parts, &PartState{Name: string(id), E: e, Opacity: 1})
						}
					}
				}
				continue
			}
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
			if fd.Name == "parts" {
				if list, ok := fd.E.(ListE); ok {
					seen := map[string]bool{}
					for _, p := range e.Parts {
						seen[p.Name] = true
					}
					for _, item := range list.Items {
						id, ok := item.(Ident)
						if !ok {
							continue
						}
						name := string(id)
						if seen[name] {
							continue
						}
						e.Parts = append(e.Parts, &PartState{Name: name, E: e, Opacity: 1})
					}
				}
				continue
			}
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
					_ = err
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
	expr := v.E
	rt.Events = append(rt.Events, &Event{T: t, Line: v.Line, Run: func(rt *Runtime) error {
		s := &Scope{rt: rt}
		val, err := s.Eval(expr)
		if err != nil {
			return fmt.Errorf("line %d: capture %s: %v", v.Line, v.Name, err)
		}
		if e, ok := val.(*Entity); ok { // snapshot a record
			snap := Snapshot{Of: e.Name, Fields: map[string]Value{}}
			for name, f := range e.Fields {
				snap.Fields[name] = f.Val
			}
			val = snap
		}
		g.Val = val
		return nil
	}})
	return nil
}

func (rt *Runtime) addSetEvents(v RecordStmt, t float64) error {
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
	return nil
}

// ---------- block expansion ----------

type winState struct {
	from, to float64 // fractions of the block
	ease     string
	stagger  float64 // per-element shift, fraction of block
	pairing  string
}

func applyMods(mods []RowMod, blockDur float64, prevEnd float64, defEase string) (winState, error) {
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
	return w, nil
}

func (rt *Runtime) expandBlock(b BlockStmt, start float64) (float64, error) {
	defEase := b.Ease
	if defEase == "" {
		defEase = "smooth"
	}

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
			if err := rt.expandRows(b.Rows, iterStart, b.DurS, defEase, binds); err != nil {
				return 0, err
			}
		}
		return float64(len(grp.Items)) * b.DurS, nil
	}

	if err := rt.expandRows(b.Rows, start, b.DurS, defEase, nil); err != nil {
		return 0, err
	}
	return b.DurS, nil
}

func (rt *Runtime) expandRows(rows []Row, start, dur float64, defEase string, binds map[string]Value) error {
	prevEnd := 0.0
	for _, row := range rows {
		w, err := applyMods(row.Mods, dur, prevEnd, defEase)
		if err != nil {
			return err
		}
		prevEnd = w.to
		if err := rt.expandRow(row, w, start, dur, binds); err != nil {
			return err
		}
	}
	return nil
}

// broadcastElems finds the `[*]` in an op path and returns the element list
// plus a path-rewriting template. Elements carry their own `it`.
type bcElem struct {
	it  ItVal
	idx int
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

// resolveStarBase returns (group entities | parts) under the [*].
func (rt *Runtime) resolveStarBase(e Expr, binds map[string]Value) (ents []*Entity, parts []*PartState, err error) {
	idx, ok := e.(IndexE)
	if !ok || idx.I != nil {
		return nil, nil, fmt.Errorf("unsupported broadcast shape")
	}
	s := &Scope{rt: rt, binds: binds}
	base, err := s.Eval(idx.X)
	if err != nil {
		return nil, nil, err
	}
	switch v := base.(type) {
	case *Group:
		return v.Items, nil, nil
	case *Entity:
		return []*Entity{v}, nil, nil
	case partsRef:
		return nil, v.E.Parts, nil
	}
	return nil, nil, fmt.Errorf("cannot broadcast over %T", base)
}

// splitStarPath splits LHS path `base[*].rest...` into the starred base expr
// and the trailing attribute names.
func splitStarPath(e Expr) (starBase Expr, trail []string, ok bool) {
	switch v := e.(type) {
	case IndexE:
		if v.I == nil {
			return v, nil, true
		}
		return nil, nil, false
	case AttrE:
		base, trail, ok := splitStarPath(v.X)
		if !ok {
			return nil, nil, false
		}
		return base, append(trail, v.Name), true
	}
	return nil, nil, false
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

	if row.Op.Kind != "arrow" {
		return fmt.Errorf("line %d: unsupported row operation %q", row.Line, row.Op.Kind)
	}
	transition := rowTransition(row)
	if transition == "morph" {
		return rt.expandMorph(row, w, mkAnim)
	}
	return rt.expandArrow(row, w, mkAnim, transition)
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

// expandVerbSimple handles single-subject verbs (write/fade_in), broadcasting
// if the subject contains [*].
func (rt *Runtime) expandVerbSimple(row Row, w winState, mk func(from, to float64, eb map[string]Value) *Anim, fill func(e *Entity, a *Anim)) error {
	subjects, _, err := rt.verbSubjects(row.Op.LHS, mk(0, 0, nil).Binds)
	if err != nil {
		return fmt.Errorf("line %d: %v", row.Line, err)
	}
	for k, e := range subjects {
		from := w.from + float64(k)*w.stagger
		a := mk(from, w.to, nil)
		fill(e, a)
		rt.Anims = append(rt.Anims, a)
	}
	return nil
}

func (rt *Runtime) verbSubjects(lhs Expr, binds map[string]Value) ([]*Entity, []*PartState, error) {
	if findStar(lhs) {
		base, trail, ok := splitStarPath(lhs)
		if !ok || len(trail) > 0 {
			return nil, nil, fmt.Errorf("unsupported broadcast verb subject")
		}
		return rt.starList(base, binds)
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

func (rt *Runtime) starList(starBase Expr, binds map[string]Value) ([]*Entity, []*PartState, error) {
	ents, parts, err := rt.resolveStarBase(starBase, binds)
	return ents, parts, err
}

func (rt *Runtime) expandArrow(row Row, w winState, mk func(from, to float64, eb map[string]Value) *Anim, transition string) error {
	lhs, rhs := row.Op.LHS, row.Op.RHS
	binds := mk(0, 0, nil).Binds
	_ = transition

	if !findStar(lhs) {
		ref, special, err := rt.resolveRef(lhs, binds)
		if err != nil {
			return fmt.Errorf("line %d: %v", row.Line, err)
		}
		a := mk(w.from, w.to, nil)
		if special == "record" {
			rt.fillRecordArrow(a, lhs, rhs, binds)
		} else if special == "warp" {
			rt.fillWarpArrow(a, ref)
		} else {
			fillTween(a, ref, rhs, -1, 0)
		}
		rt.Anims = append(rt.Anims, a)
		return nil
	}

	base, trail, ok := splitStarPath(lhs)
	if !ok {
		return fmt.Errorf("line %d: unsupported broadcast path", row.Line)
	}
	ents, parts, err := rt.starList(base, binds)
	if err != nil {
		return fmt.Errorf("line %d: %v", row.Line, err)
	}
	n := len(ents) + len(parts)
	mkElem := func(k int, it ItVal) (*Anim, error) {
		from := w.from + float64(k)*w.stagger
		return mk(from, w.to, map[string]Value{"it": it}), nil
	}
	for k, e := range ents {
		it := ItVal{Val: firstNonNil(e.It, e), I: k, N: n}
		if iv, ok := e.It.(ItVal); ok {
			it = iv
		}
		a, _ := mkElem(k, it)
		ref, err := refForTrail(e, trail)
		if err != nil {
			return fmt.Errorf("line %d: %v", row.Line, err)
		}
		fillTween(a, ref, rhs, k, n)
		rt.Anims = append(rt.Anims, a)
	}
	for k, p := range parts {
		it := ItVal{Val: p, I: k, N: n}
		a, _ := mkElem(k, it)
		var ref Ref
		attr := ""
		if len(trail) > 0 {
			attr = trail[len(trail)-1]
		}
		switch attr {
		case "color":
			ref = PartColorRef{P: p}
		case "opacity":
			ref = PartOpacityRef{P: p}
		default:
			return fmt.Errorf("line %d: parts broadcast supports .color/.opacity, got %q", row.Line, attr)
		}
		fillTween(a, ref, rhs, k, n)
		rt.Anims = append(rt.Anims, a)
	}
	return nil
}

func firstNonNil(a, b Value) Value {
	if a != nil {
		return a
	}
	return b
}

// refForTrail resolves entity field path like ["scale"] or ["at"].
func refForTrail(e *Entity, trail []string) (Ref, error) {
	if len(trail) != 1 {
		return nil, fmt.Errorf("unsupported field path depth %v", trail)
	}
	return FieldRef{E: e, F: e.field(trail[0])}, nil
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
			switch v.Name {
			case "color":
				return PartColorRef{P: b}, "", nil
			case "opacity":
				return PartOpacityRef{P: b}, "", nil
			}
		}
		return nil, "", fmt.Errorf("cannot tween %T.%s", base, v.Name)
	case IndexE:
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

// fillTween: standard tween; elemIdx >= 0 enables list-RHS positional pairing.
func fillTween(a *Anim, ref Ref, rhs Expr, elemIdx, elemN int) {
	a.Targets = []Ref{ref}
	live := func() bool { fr, ok := ref.(FieldRef); return ok && fr.F.Live }
	a.Start = func(a *Anim, rt *Runtime) error {
		rt.clearPost(ref.Key())
		cur := ref.Get()
		goal, err := evalTweenGoal(rt, rhs, a.Binds, elemIdx, elemN, cur)
		if err != nil {
			return fmt.Errorf("line %d: %v", a.Line, err)
		}
		a.start = []Value{cur}
		a.goal = []Value{goal}
		return nil
	}
	a.Update = func(a *Anim, rt *Runtime, u float64) {
		goal, err := evalTweenGoal(rt, rhs, a.Binds, elemIdx, elemN, ref.Get())
		if err == nil {
			a.goal[0] = goal
		}
		from := a.start[0]
		if live() {
			from = ref.Get() // re-read: live-eval already ran this frame
		}
		ref.Set(lerpValue(from, a.goal[0], u))
		if u >= 1 {
			rt.setPost(ref, rhs, a.Binds, elemIdx, elemN)
		}
	}
}

func (rt *Runtime) fillWarpArrow(a *Anim, ref Ref) {
	a.Targets = []Ref{ref}
	a.Start = func(a *Anim, rt *Runtime) error {
		rt.clearPost(ref.Key())
		a.start = []Value{ref.Get()}
		return nil
	}
	a.Update = func(a *Anim, rt *Runtime, u float64) {
		s, _ := asFloat(a.start[0])
		ref.Set(lerp(s, 1, u))
	}
}

// record arrow: `frame -> home` — field-wise tweens paired by name.
func (rt *Runtime) fillRecordArrow(a *Anim, lhs, rhs Expr, binds map[string]Value) {
	var refs []Ref
	a.Start = func(a *Anim, rt *Runtime) error {
		s := &Scope{rt: rt, binds: binds}
		lv, err := s.Eval(lhs)
		if err != nil {
			return err
		}
		e, ok := lv.(*Entity)
		if !ok {
			return fmt.Errorf("line %d: record arrow target is %T", a.Line, lv)
		}
		rv, err := s.Eval(rhs)
		if err != nil {
			return err
		}
		snap, ok := rv.(Snapshot)
		if !ok {
			return fmt.Errorf("line %d: record arrow RHS must be a snapshot, got %T", a.Line, rv)
		}
		a.start = nil
		a.goal = nil
		refs = nil
		for name, goal := range snap.Fields {
			if goal == nil {
				continue
			}
			f, ok := e.Fields[name]
			if !ok {
				continue
			}
			refs = append(refs, FieldRef{E: e, F: f})
			rt.clearPost(e.Name + "." + name)
			a.start = append(a.start, f.Val)
			a.goal = append(a.goal, goal)
		}
		a.Targets = refs
		return nil
	}
	a.Update = func(a *Anim, rt *Runtime, u float64) {
		for i, r := range refs {
			r.Set(lerpValue(a.start[i], a.goal[i], u))
		}
	}
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

func (rt *Runtime) expandMorph(row Row, w winState, mk func(from, to float64, eb map[string]Value) *Anim) error {
	binds := mk(0, 0, nil).Binds
	srcEnts, srcParts, err := rt.verbSubjects(row.Op.LHS, binds)
	if err != nil {
		return fmt.Errorf("line %d: morph: %v", row.Line, err)
	}
	n := len(srcEnts) + len(srcParts)
	rhs := row.Op.RHS

	addPair := func(k int, srcE *Entity, srcP *PartState) {
		it := ItVal{I: k, N: n}
		if srcP != nil {
			it.Val = srcP
		} else {
			it.Val = srcE
			if iv, ok := srcE.It.(ItVal); ok {
				it = ItVal{Val: iv.Val, I: k, N: n, Cols: iv.Cols}
			}
		}
		from := w.from + float64(k)*w.stagger
		a := mk(from, w.to, map[string]Value{"it": it})

		var srcOpRef Ref
		var srcPos func() Vec
		var srcMove *Entity
		var textMorph bool
		var shapeMorph bool
		var srcText, dstText *Field
		var dstFontSize, dstScale float64
		var srcPts, dstPts []Vec
		var srcShapeStyle, dstShapeStyle shapeMorphStyle
		if srcP != nil {
			srcOpRef = PartOpacityRef{P: srcP}
			srcPos = func() Vec { at, _, _ := partBox(srcP); return at }
		} else {
			e := srcE
			srcMove = e
			srcOpRef = FieldRef{E: e, F: e.field("opacity")}
			srcPos = func() Vec { return e.fvec("at").Add(e.Offset) }
		}

		var dst *Entity
		var dstOpRef Ref
		a.Targets = []Ref{srcOpRef}
		a.Start = func(a *Anim, rt *Runtime) error {
			s := &Scope{rt: rt, binds: a.Binds}
			dv, err := s.Eval(rhs)
			if err != nil {
				return fmt.Errorf("line %d: morph target: %v", a.Line, err)
			}
			switch x := dv.(type) {
			case *Entity:
				dst = x
			case *Group:
				if len(x.Items) == 1 {
					dst = x.Items[0]
				}
			}
			if dst == nil {
				return fmt.Errorf("line %d: morph target is %T", a.Line, dv)
			}
			dstOpRef = FieldRef{E: dst, F: dst.field("opacity")}
			textMorph = srcMove != nil && isTextType(srcMove.Type) && isTextType(dst.Type)
			shapeMorph = !textMorph && srcMove != nil && isShapeType(srcMove.Type) && isShapeType(dst.Type)
			if textMorph {
				srcText = srcMove.field("text")
				dstText = dst.field("text")
				dstFontSize = dst.fnum("font_size")
				dstScale = dst.fnum("scale")
				dstOpRef.Set(0.0)
			}
			if shapeMorph {
				srcPts = outlinePoints(srcMove, 64)
				dstPts = outlinePoints(dst, 64)
				srcShapeStyle = shapeStyleForMorph(srcMove)
				dstShapeStyle = shapeStyleForMorph(dst)
				dstOpRef.Set(0.0)
			}
			start := []Value{srcOpRef.Get(), dstOpRef.Get()}
			if !shapeMorph {
				start = append(start, srcPos().Sub(dst.fvec("at")))
			}
			if srcMove != nil && !shapeMorph {
				start = append(start, srcMove.Offset)
			}
			a.start = start
			return nil
		}
		a.Update = func(a *Anim, rt *Runtime, u float64) {
			s0, _ := asFloat(a.start[0])
			if textMorph && srcMove != nil && len(a.start) > 3 {
				off, _ := asVec(a.start[2])
				srcOff, _ := asVec(a.start[3])
				srcOpRef.Set(s0)
				dstOpRef.Set(0.0)
				srcMove.Offset = srcOff.Sub(off.Mul(u))
				if u >= 0.5 && srcText != nil && dstText != nil {
					srcText.Val = dstText.Val
					srcMove.field("font_size").Val = dstFontSize
					srcMove.field("scale").Val = dstScale
					srcMove.layoutCache = nil
				}
				if u >= 1 {
					srcOpRef.Set(0.0)
					dstOpRef.Set(1.0)
					dst.Offset = Vec{}
				}
				return
			}
			if shapeMorph && srcMove != nil && len(srcPts) == len(dstPts) {
				pts := make([]Vec, len(srcPts))
				for i := range pts {
					pts[i] = Vec{
						lerp(srcPts[i][0], dstPts[i][0], u),
						lerp(srcPts[i][1], dstPts[i][1], u),
						lerp(srcPts[i][2], dstPts[i][2], u),
					}
				}
				srcMove.MorphPath = pts
				blend := Color{
					R: lerp(srcShapeStyle.EffectiveColor.R, dstShapeStyle.EffectiveColor.R, u),
					G: lerp(srcShapeStyle.EffectiveColor.G, dstShapeStyle.EffectiveColor.G, u),
					B: lerp(srcShapeStyle.EffectiveColor.B, dstShapeStyle.EffectiveColor.B, u),
					A: 1,
				}
				srcMove.MorphHasStroke = true
				srcMove.MorphStroke = Color{
					R: blend.R,
					G: blend.G,
					B: blend.B,
					A: lerp(srcShapeStyle.StrokeA, dstShapeStyle.StrokeA, u),
				}
				fillA := lerp(srcShapeStyle.FillA, dstShapeStyle.FillA, u)
				srcPremulR := srcShapeStyle.FillColor.R * srcShapeStyle.FillA
				srcPremulG := srcShapeStyle.FillColor.G * srcShapeStyle.FillA
				srcPremulB := srcShapeStyle.FillColor.B * srcShapeStyle.FillA
				dstPremulR := dstShapeStyle.FillColor.R * dstShapeStyle.FillA
				dstPremulG := dstShapeStyle.FillColor.G * dstShapeStyle.FillA
				dstPremulB := dstShapeStyle.FillColor.B * dstShapeStyle.FillA
				fillR := dstShapeStyle.FillColor.R
				fillG := dstShapeStyle.FillColor.G
				fillB := dstShapeStyle.FillColor.B
				if fillA > 1e-6 {
					fillR = lerp(srcPremulR, dstPremulR, u) / fillA
					fillG = lerp(srcPremulG, dstPremulG, u) / fillA
					fillB = lerp(srcPremulB, dstPremulB, u) / fillA
				}
				srcMove.MorphHasFill = true
				srcMove.MorphFill = Color{
					R: fillR,
					G: fillG,
					B: fillB,
					A: fillA,
				}
				srcOpRef.Set(s0)
				dstOpRef.Set(0.0)
				if u >= 1 {
					srcMove.MorphPath = nil
					srcMove.MorphHasStroke = false
					srcMove.MorphHasFill = false
					srcOpRef.Set(0.0)
					dstOpRef.Set(1.0)
				}
				return
			}
			d0, _ := asFloat(a.start[1])
			off, _ := asVec(a.start[2])
			srcOpRef.Set(lerp(s0, 0, u))
			dstOpRef.Set(lerp(d0, 1, u))
			dst.Offset = off.Mul(1 - u)
			if srcMove != nil && len(a.start) > 3 {
				srcOff, _ := asVec(a.start[3])
				srcMove.Offset = srcOff.Sub(off.Mul(u))
			}
		}
		rt.Anims = append(rt.Anims, a)
	}

	for k, e := range srcEnts {
		addPair(k, e, nil)
	}
	for k, p := range srcParts {
		addPair(k+len(srcEnts), nil, p)
	}
	return nil
}

func isTextType(typ string) bool {
	switch typ {
	case "tex", "text", "decimal":
		return true
	}
	return false
}

func isShapeType(typ string) bool {
	switch typ {
	case "rect", "square", "dot":
		return true
	}
	return false
}

type shapeMorphStyle struct {
	EffectiveColor Color
	FillColor      Color
	StrokeA        float64
	FillA          float64
}

func shapeStyleForMorph(e *Entity) shapeMorphStyle {
	var strokeCol Color
	var strokeA float64
	switch e.Type {
	case "square":
		strokeCol = namedColors["white"]
		strokeA = 1
		if f, ok := e.Fields["stroke"]; ok && f.Val != nil {
			if c, err := asColor(f.Val); err == nil {
				strokeCol = c
				strokeA = c.A
			}
		}
	case "rect":
		if f, ok := e.Fields["stroke"]; ok && f.Val != nil {
			if c, err := asColor(f.Val); err == nil {
				strokeCol = c
				strokeA = c.A
			}
		}
	case "dot":
		if f, ok := e.Fields["stroke"]; ok && f.Val != nil {
			if c, err := asColor(f.Val); err == nil {
				strokeCol = c
				strokeA = c.A
			}
		}
	}

	var fillCol Color
	var fillA float64
	switch e.Type {
	case "dot":
		fillCol = entityColor(e)
		fillA = fillCol.A
	case "rect", "square":
		if f, ok := e.Fields["fill"]; ok && f.Val != nil {
			if c, err := asColor(f.Val); err == nil {
				fillCol = c
				fillA = c.A
			}
		}
	}

	// EffectiveColor is the stroke RGB used when blending the morph outline.
	// The fill is interpolated separately (FillColor/FillA), so the stroke
	// must track the stroke color and never fall back to the fill — otherwise
	// a stroked, filled target (e.g. WHITE stroke + PINK fill dot) drags the
	// outline toward the fill mid-morph and then snaps back at u>=1.
	eff := strokeCol
	if strokeA <= 0 {
		// No stroke is drawn; RGB is irrelevant but keep a sane value.
		eff = entityColor(e)
	}
	return shapeMorphStyle{
		EffectiveColor: eff,
		FillColor:      fillCol,
		StrokeA:        strokeA,
		FillA:          fillA,
	}
}

// ---------- init & liveness ----------

func (rt *Runtime) initFields() error {
	// evaluate every expr-defined field once, in dependency order via
	// recursive memoization
	type key struct {
		e *Entity
		f *Field
	}
	state := map[key]int{} // 0 unvisited, 1 visiting, 2 done
	var visit func(e *Entity, f *Field) error
	evalField := func(e *Entity, f *Field) error {
		s := &Scope{rt: rt, binds: map[string]Value{"it": e.It}}
		if f.Rate {
			// rate fields start at their integrator origin
			f.Val = coerceField(f.Name, defaultFieldVal(e.Type, f.Name))
			if f.Val == nil {
				f.Val = 0.0
			}
			return nil
		}
		if f.Name == "warp" {
			e.WarpNew = f.Def
			e.WarpBlend = 1
			f.Val = nil
			return nil
		}
		val, err := s.Eval(f.Def)
		if err != nil {
			return fmt.Errorf("%s.%s: %v", e.Name, f.Name, err)
		}
		f.Val = coerceField(f.Name, val)
		return nil
	}
	visit = func(e *Entity, f *Field) error {
		k := key{e, f}
		switch state[k] {
		case 1:
			return fmt.Errorf("dependency cycle through %s.%s", e.Name, f.Name)
		case 2:
			return nil
		}
		state[k] = 1
		if f.Def != nil && !f.Frozen {
			deps := map[string]bool{}
			exprDeps(f.Def, deps)
			for d := range deps {
				name, fname, _ := strings.Cut(d, ".")
				grp, ok := rt.Groups[name]
				if !ok {
					continue
				}
				for _, de := range grp.Items {
					if fname != "" {
						if df, ok := de.Fields[fname]; ok && df.Def != nil {
							if err := visit(de, df); err != nil {
								return err
							}
						}
					} else {
						for _, dn := range de.Order {
							df := de.Fields[dn]
							if df.Def != nil && !df.Rate {
								if err := visit(de, df); err != nil {
									return err
								}
							}
						}
					}
				}
			}
			if f.Name != "fn" { // plot fn stays an expression (binder x)
				if err := evalField(e, f); err != nil {
					return err
				}
			}
		}
		state[k] = 2
		return nil
	}
	for _, e := range rt.Entities {
		for _, name := range e.Order {
			f := e.Fields[name]
			if f.Def != nil && !f.Frozen {
				if err := visit(e, f); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// livenessPass classifies fields static/live/rate, enforces one writer, and
// records the per-frame evaluation order for live fields.
func (rt *Runtime) livenessPass() error {
	// roots: score-written keys (anim targets) and rate fields
	written := map[string]*Anim{}
	for _, a := range rt.Anims {
		for _, ref := range a.Targets {
			if ref != nil {
				written[ref.Key()] = a
			}
		}
	}
	isRoot := func(k string) bool {
		if _, ok := written[k]; ok {
			return true
		}
		name, fname, _ := strings.Cut(k, ".")
		if grp, ok := rt.Groups[name]; ok {
			for _, e := range grp.Items {
				if fname == "" {
					return true // whole-record dep: conservative
				}
				if f, ok := e.Fields[fname]; ok && f.Rate {
					return true
				}
			}
		}
		return false
	}

	type slot struct {
		e *Entity
		f *Field
	}
	var exprFields []slot
	depsOf := map[*Field]map[string]bool{}
	for _, e := range rt.Entities {
		for _, n := range e.Order {
			f := e.Fields[n]
			if f.Def != nil && !f.Frozen && !f.Rate && n != "fn" && n != "warp" {
				exprFields = append(exprFields, slot{e, f})
				d := map[string]bool{}
				exprDeps(f.Def, d)
				depsOf[f] = d
			}
		}
	}

	liveKeys := map[string]bool{}
	changed := true
	for changed {
		changed = false
		for _, sl := range exprFields {
			if sl.f.Live {
				continue
			}
			for d := range depsOf[sl.f] {
				name, fname, _ := strings.Cut(d, ".")
				if _, isEnt := rt.Groups[name]; !isEnt {
					if _, isGlob := rt.Globals[name]; isGlob && written[name] != nil {
						sl.f.Live = true
					}
					continue
				}
				// any written/rate/live field under this dep?
				grp := rt.Groups[name]
				for _, de := range grp.Items {
					if fname != "" {
						k := de.Name + "." + fname
						if isRootField(de, fname, written) || liveKeys[k] {
							sl.f.Live = true
						}
					} else {
						// whole-record dep (e.g. `center(box)`): only the
						// record's geometry can leak into the reader — a
						// tween on box.opacity must not make label.at live
						for _, dn := range de.Order {
							if !isGeomField(dn) {
								continue
							}
							k := de.Name + "." + dn
							if isRootField(de, dn, written) || liveKeys[k] {
								sl.f.Live = true
							}
						}
					}
				}
			}
			if sl.f.Live {
				liveKeys[sl.e.Name+"."+sl.f.Name] = true
				changed = true
			}
		}
	}
	_ = isRoot

	// one writer: arrows on rate fields are compile errors; live fields are
	// allowed — the tween blends lerp(live_expr(t), goal, u) each frame.
	for _, a := range rt.Anims {
		for _, ref := range a.Targets {
			fr, ok := ref.(FieldRef)
			if !ok {
				continue
			}
			if fr.F.Rate {
				return fmt.Errorf("line %d: cannot tween %s — rate field (the integrator is the writer)", a.Line, ref.Key())
			}
		}
	}

	// depth-sort live fields for per-frame evaluation
	var depth func(e *Entity, f *Field, seen map[*Field]bool) int
	depth = func(e *Entity, f *Field, seen map[*Field]bool) int {
		if seen[f] {
			return 0
		}
		seen[f] = true
		d := 0
		for dep := range depsOf[f] {
			name, fname, _ := strings.Cut(dep, ".")
			grp, ok := rt.Groups[name]
			if !ok {
				continue
			}
			for _, de := range grp.Items {
				names := []string{fname}
				if fname == "" {
					names = de.Order
				}
				for _, dn := range names {
					if df, ok := de.Fields[dn]; ok && df.Live {
						if dd := depth(de, df, seen) + 1; dd > d {
							d = dd
						}
					}
				}
			}
		}
		return d
	}
	for _, sl := range exprFields {
		if sl.f.Live {
			sl.f.depth = depth(sl.e, sl.f, map[*Field]bool{})
			rt.liveFields = append(rt.liveFields, fieldSlot{E: sl.e, F: sl.f})
		}
	}
	sort.SliceStable(rt.liveFields, func(i, j int) bool {
		return rt.liveFields[i].F.depth < rt.liveFields[j].F.depth
	})
	return nil
}

func isGeomField(name string) bool {
	switch name {
	case "opacity", "color", "fill", "stroke", "draw":
		return false
	}
	return true
}

func isRootField(e *Entity, fname string, written map[string]*Anim) bool {
	k := e.Name + "." + fname
	if written[k] != nil {
		return true
	}
	if f, ok := e.Fields[fname]; ok && f.Rate {
		return true
	}
	return false
}

// ---------- frame stepping ----------

func (rt *Runtime) Step(t float64) error {
	rt.Dt = t - rt.T
	rt.T = t

	for _, ev := range rt.Events {
		if !ev.done && ev.T <= t+1e-9 {
			ev.done = true
			if err := ev.Run(rt); err != nil {
				return err
			}
		}
	}

	// live fields first: anims that target live fields read the fresh expr value
	for _, sl := range rt.liveFields {
		if sl.F.Frozen {
			continue
		}
		s := &Scope{rt: rt, binds: map[string]Value{"it": sl.E.It}}
		val, err := s.Eval(sl.F.Def)
		if err != nil {
			return fmt.Errorf("live field %s.%s: %v", sl.E.Name, sl.F.Name, err)
		}
		sl.F.Val = coerceField(sl.F.Name, val)
	}

	for key, post := range rt.post {
		cur := post.Ref.Get()
		goal, err := evalTweenGoal(rt, post.RHS, post.Binds, post.ElemIdx, post.ElemN, cur)
		if err != nil {
			rt.warnOnce(fmt.Sprintf("post-tween `%s`: %v", key, err))
			continue
		}
		post.Ref.Set(goal)
	}

	for _, a := range rt.Anims {
		if a.done || t+1e-9 < a.T0 {
			continue
		}
		if !a.started {
			a.started = true
			if a.Start != nil {
				if err := a.Start(a, rt); err != nil {
					return err
				}
			}
		}
		u := 1.0
		if a.T1 > a.T0 {
			u = (t - a.T0) / (a.T1 - a.T0)
		}
		if u >= 1 {
			u = 1
			a.done = true
		}
		if a.Update != nil {
			a.Update(a, rt, a.Ease(u))
		}
	}

	// rate fields: fixed-dt Euler, prev-frame self
	if rt.Dt > 0 {
		for _, e := range rt.Entities {
			for _, n := range e.Order {
				f := e.Fields[n]
				if !f.Rate || f.Def == nil {
					continue
				}
				s := &Scope{rt: rt, binds: map[string]Value{"it": e.It, "self": f.Val}}
				rate, err := s.Eval(f.Def)
				if err != nil {
					return fmt.Errorf("rate field %s.%s: %v", e.Name, n, err)
				}
				switch rv := rate.(type) {
				case Vec:
					cur, _ := asVec(f.Val)
					f.Val = cur.Add(rv.Mul(rt.Dt))
				default:
					rf, err := asFloat(rate)
					if err != nil {
						return err
					}
					cur, _ := asFloat(f.Val)
					f.Val = cur + rf*rt.Dt
				}
			}
		}
	}
	return nil
}
