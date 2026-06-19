package pdtt

import (
	"fmt"
	"sort"
	"strings"
)

// ---------- init & liveness ----------

func (rt *Runtime) initFields() error {
	init := fieldInitializer{
		rt:    rt,
		state: map[fieldInitKey]int{},
	}
	for _, e := range rt.Entities {
		for _, name := range e.Order {
			f := e.Fields[name]
			if f.Def != nil && !f.Frozen {
				if err := init.visit(e, f); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type fieldInitKey struct {
	e *Entity
	f *Field
}

type fieldInitializer struct {
	rt    *Runtime
	state map[fieldInitKey]int // 0 unvisited, 1 visiting, 2 done
}

func (in *fieldInitializer) visit(e *Entity, f *Field) error {
	k := fieldInitKey{e: e, f: f}
	switch in.state[k] {
	case 1:
		return fmt.Errorf("dependency cycle through %s.%s", e.Name, f.Name)
	case 2:
		return nil
	}
	in.state[k] = 1
	if f.Def != nil && !f.Frozen {
		if err := in.visitDeps(e, f); err != nil {
			return err
		}
		if f.Name != "fn" { // plot fn stays an expression (binder x)
			if err := in.eval(e, f); err != nil {
				return err
			}
		}
	}
	in.state[k] = 2
	return nil
}

func (in *fieldInitializer) visitDeps(e *Entity, f *Field) error {
	for dep := range entityExprDeps(e, f.Def) {
		if err := in.visitDep(dep); err != nil {
			return err
		}
	}
	return nil
}

func (in *fieldInitializer) visitDep(dep string) error {
	name, fname, _ := strings.Cut(dep, ".")
	grp, ok := in.rt.Groups[name]
	if !ok {
		return nil
	}
	for _, e := range grp.Items {
		if fname != "" {
			if f, ok := e.Fields[fname]; ok && f.Def != nil {
				if err := in.visit(e, f); err != nil {
					return err
				}
			}
			continue
		}
		for _, name := range e.Order {
			f := e.Fields[name]
			if f.Def != nil && !f.Rate {
				if err := in.visit(e, f); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (in *fieldInitializer) eval(e *Entity, f *Field) error {
	if f.Rate {
		f.Val = coerceField(f.Name, defaultFieldVal(f.Name))
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
	s := &Scope{rt: in.rt, binds: map[string]Value{"it": e.It}}
	val, err := s.Eval(f.Def)
	if err != nil {
		return fmt.Errorf("%s.%s: %v", e.Name, f.Name, err)
	}
	f.Val = coerceField(f.Name, val)
	return nil
}

// livenessPass classifies fields static/live/rate, enforces one writer, and
// records the per-frame evaluation order for live fields.
func (rt *Runtime) livenessPass() error {
	rt.indexWriters()

	exprFields, depsOf := rt.collectExprFieldDeps()
	rt.markLiveFields(exprFields, depsOf)
	if err := rt.rejectRateFieldTweens(); err != nil {
		return err
	}
	rt.orderLiveFields(exprFields, depsOf)
	rt.indexLiveDeps(depsOf)
	return nil
}

func (rt *Runtime) collectExprFieldDeps() ([]fieldSlot, map[*Field]map[string]bool) {
	var exprFields []fieldSlot
	depsOf := map[*Field]map[string]bool{}
	for _, e := range rt.Entities {
		for _, name := range e.Order {
			f := e.Fields[name]
			if isLiveCandidate(name, f) {
				exprFields = append(exprFields, fieldSlot{E: e, F: f})
				depsOf[f] = entityExprDeps(e, f.Def)
			}
		}
	}
	return exprFields, depsOf
}

func isLiveCandidate(name string, f *Field) bool {
	return f.Def != nil && !f.Frozen && !f.Rate && name != "fn" && name != "warp"
}

func (rt *Runtime) markLiveFields(exprFields []fieldSlot, depsOf map[*Field]map[string]bool) {
	liveKeys := map[string]bool{}
	changed := true
	for changed {
		changed = false
		for _, sl := range exprFields {
			if sl.F.Live {
				continue
			}
			if !rt.anyLiveDependency(depsOf[sl.F], liveKeys) {
				continue
			}
			sl.F.Live = true
			liveKeys[fieldKey(sl.E, sl.F.Name)] = true
			changed = true
		}
	}
}

func (rt *Runtime) anyLiveDependency(deps map[string]bool, liveKeys map[string]bool) bool {
	for dep := range deps {
		if rt.dependencyIsLive(dep, liveKeys) {
			return true
		}
	}
	return false
}

func (rt *Runtime) dependencyIsLive(dep string, liveKeys map[string]bool) bool {
	name, fname, _ := strings.Cut(dep, ".")
	grp, isEntity := rt.Groups[name]
	if !isEntity {
		g, isGlobal := rt.Globals[name]
		return isGlobal && (g.Live || rt.globalHasWriter(name))
	}

	for _, e := range grp.Items {
		if fname != "" {
			if rt.fieldIsLiveRoot(e, fname, liveKeys) {
				return true
			}
			continue
		}
		// Whole-record deps such as `center(box)` only read geometry. A tween
		// on box.opacity must not make label.at live.
		for _, name := range e.Order {
			if isGeomField(name) && rt.fieldIsLiveRoot(e, name, liveKeys) {
				return true
			}
		}
	}
	return false
}

func (rt *Runtime) fieldIsLiveRoot(e *Entity, fname string, liveKeys map[string]bool) bool {
	key := fieldKey(e, fname)
	return rt.isRootField(e, fname) || liveKeys[key]
}

func fieldKey(e *Entity, fname string) string {
	return e.Name + "." + fname
}

// one writer: arrows on rate fields are compile errors; live fields are
// allowed — the tween blends lerp(live_expr(t), goal, u) each frame.
func (rt *Runtime) rejectRateFieldTweens() error {
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
	return nil
}

func (rt *Runtime) orderLiveFields(exprFields []fieldSlot, depsOf map[*Field]map[string]bool) {
	rt.liveFields = rt.liveFields[:0]
	for _, sl := range exprFields {
		if sl.F.Live {
			sl.F.depth = rt.liveFieldDepth(sl.F, depsOf, map[*Field]bool{})
			rt.liveFields = append(rt.liveFields, sl)
		}
	}
	sort.SliceStable(rt.liveFields, func(i, j int) bool {
		return rt.liveFields[i].F.depth < rt.liveFields[j].F.depth
	})
}

func (rt *Runtime) liveFieldDepth(f *Field, depsOf map[*Field]map[string]bool, seen map[*Field]bool) int {
	if seen[f] {
		return 0
	}
	seen[f] = true
	depth := 0
	for dep := range depsOf[f] {
		for _, df := range rt.liveDepFields(dep) {
			if dd := rt.liveFieldDepth(df, depsOf, seen) + 1; dd > depth {
				depth = dd
			}
		}
	}
	return depth
}

func (rt *Runtime) liveDepFields(dep string) []*Field {
	name, fname, _ := strings.Cut(dep, ".")
	grp, ok := rt.Groups[name]
	if !ok {
		return nil
	}

	var fields []*Field
	for _, e := range grp.Items {
		if fname != "" {
			if f, ok := e.Fields[fname]; ok && f.Live {
				fields = append(fields, f)
			}
			continue
		}
		for _, name := range e.Order {
			if f, ok := e.Fields[name]; ok && f.Live {
				fields = append(fields, f)
			}
		}
	}
	return fields
}

func (rt *Runtime) indexLiveDeps(depsOf map[*Field]map[string]bool) {
	rt.liveDeps = map[string]bool{}
	for _, sl := range rt.liveFields {
		rt.liveDeps[fieldKey(sl.E, sl.F.Name)] = true
		for dep := range depsOf[sl.F] {
			rt.liveDeps[dep] = true
		}
	}
}

func entityExprDeps(e *Entity, expr Expr) map[string]bool {
	deps := map[string]bool{}
	exprDeps(expr, deps)
	addResolvedAttrDeps(e, expr, deps)

	it, ok := e.It.(ItVal)
	if !ok {
		return deps
	}
	seenLocal := map[string]bool{}
	for {
		changed := false
		for dep := range deps {
			name, field, hasField := strings.Cut(dep, ".")
			local, ok := it.Cols[name]
			if !ok {
				continue
			}
			delete(deps, dep)
			changed = true
			switch v := local.(type) {
			case *Entity:
				if hasField {
					deps[v.Name+"."+field] = true
				} else {
					deps[v.Name] = true
				}
			case FamilyLocalBinding:
				if seenLocal[v.Name] {
					continue
				}
				seenLocal[v.Name] = true
				localDeps := map[string]bool{}
				exprDeps(v.E, localDeps)
				for d := range localDeps {
					deps[d] = true
				}
			default:
				// Domain bind values and literal locals are constants for this
				// family instance, so they do not contribute liveness deps.
			}
		}
		if !changed {
			break
		}
	}
	return deps
}

func addResolvedAttrDeps(e *Entity, expr Expr, deps map[string]bool) {
	w := attrDepWalker{
		scope: &Scope{rt: e.rt, binds: map[string]Value{"it": e.It}},
		deps:  deps,
	}
	w.walk(expr)
}

type attrDepWalker struct {
	scope *Scope
	deps  map[string]bool
}

func (w attrDepWalker) walk(expr Expr) {
	switch v := expr.(type) {
	case AttrE:
		w.addAttrDeps(v)
		w.walk(v.X)
	case IndexE:
		w.walk(v.X)
		if v.I != nil {
			w.walk(v.I)
		}
	case CallE:
		w.walk(v.Fn)
		for _, a := range v.Args {
			w.walk(a)
		}
	case BinE:
		w.walk(v.L)
		w.walk(v.R)
	case RangeE:
		w.walk(v.Start)
		w.walk(v.End)
	case UnE:
		w.walk(v.X)
	case ListE:
		for _, it := range v.Items {
			w.walk(it)
		}
	case CondE:
		w.walk(v.Cond)
		w.walk(v.Then)
		w.walk(v.Else)
	case AlphaE:
		w.walk(v.X)
	case SnapshotE:
		// snapshot is frozen, matching exprDeps.
	}
}

func (w attrDepWalker) addAttrDeps(expr AttrE) {
	if base, err := w.scope.Eval(expr.X); err == nil {
		switch b := base.(type) {
		case *Entity:
			w.deps[entityAttrDepKey(b, expr.Name)] = true
		case *PartState:
			w.deps[b.E.Name+".parts."+b.Name+"."+expr.Name] = true
		}
	}
	if val, err := w.scope.Eval(expr); err == nil {
		if ent, ok := val.(*Entity); ok {
			w.deps[ent.Name] = true
		}
	}
}

func entityAttrDepKey(e *Entity, attr string) string {
	if _, ok := e.Fields[attr]; ok {
		return e.Name + "." + attr
	}
	if defaultFieldVal(attr) != nil {
		return e.Name + "." + attr
	}
	return e.Name
}

func isGeomField(name string) bool {
	switch name {
	case "opacity", "color", "fill", "stroke", "draw":
		return false
	}
	return true
}

func (rt *Runtime) isRootField(e *Entity, fname string) bool {
	if rt.fieldHasWriter(fieldKey(e, fname)) {
		return true
	}
	if f, ok := e.Fields[fname]; ok && f.Rate {
		return true
	}
	return false
}
