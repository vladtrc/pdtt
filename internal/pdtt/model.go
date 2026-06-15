package pdtt

// Values, records (entities), scopes, expression evaluation, builtins.

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ---------- values ----------

type Vec [3]float64

func (a Vec) Add(b Vec) Vec     { return Vec{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func (a Vec) Sub(b Vec) Vec     { return Vec{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func (a Vec) Mul(k float64) Vec { return Vec{a[0] * k, a[1] * k, a[2] * k} }

type Color struct{ R, G, B, A float64 }

// AnchorPt is a point plus the alignment direction it was produced with
// (`corner(UL)`, `title.UL`, `stack(x).top`). Used as a raw point (arrow
// endpoints, math) it is just P; stored into an `at:` field it aligns the
// entity's own bounding box: the Dir-corner of the entity lands on P.
type AnchorPt struct{ P, Dir Vec }

func resolveAnchored(e *Entity, v Value) Value {
	ap, ok := v.(AnchorPt)
	if !ok {
		return v
	}
	w, h := entitySize(e)
	return Vec{ap.P[0] - ap.Dir[0]*w/2, ap.P[1] - ap.Dir[1]*h/2, 0}
}

// Snapshot is the value of `name = record` — frozen field values, paired by
// name on restore (record arrow).
type Snapshot struct {
	Of     string
	Fields map[string]Value
}

func snapshotValue(v Value) Value {
	e, ok := v.(*Entity)
	if !ok {
		return v
	}
	snap := Snapshot{Of: e.Name, Fields: map[string]Value{}}
	for name, f := range e.Fields {
		snap.Fields[name] = f.Val
	}
	return snap
}

type ItVal struct {
	Val  Value
	I, N int
	Cols map[string]Value
}

type Value interface{}

type Field struct {
	Name   string
	Def    Expr
	Rate   bool
	Live   bool
	Frozen bool // redefined via `set` — static again
	Val    Value
	depth  int // live-eval order
}

type PartState struct {
	Name    string
	E       *Entity
	Color   Value // nil = inherit entity color
	Opacity float64
}

type Entity struct {
	Type, Name string
	Idx, N     int
	It         Value // row-source value for plural records
	Fields     map[string]*Field
	Order      []string
	Parts      []*PartState
	rt         *Runtime
	Active     bool // present on screen; declarations start inactive

	// animation state owned by verbs
	Offset         Vec
	Reveal         float64
	MorphContours  [][]Vec
	MorphHasStroke bool
	MorphStroke    Color
	MorphStrokeW   float64 // interpolated stroke width in world units (manim: stroke_width)
	MorphHasFill   bool
	MorphFill      Color

	// warp special-casing (concept 36 leaves warp function-valued; the
	// prototype tweens a blend between identity and the `set` expression)
	WarpNew   Expr
	WarpBlend float64

	layoutCache *TextLayout
	layoutKey   string
}

// Transform is an entity's spatial state for one frame: world position
// (the `at` field plus the verb-owned animation Offset), uniform Scale,
// Angle, and Opacity. Renderers and attr lookups read these through
// e.transform() instead of re-deriving them — one place owns the defaults
// (scale 0 → 1) and the fact that position lives in `at` + Offset.
type Transform struct {
	At      Vec
	Scale   float64
	Angle   float64
	Opacity float64
}

func (e *Entity) transform() Transform {
	scale := e.fnum("scale")
	if scale == 0 {
		scale = 1
	}
	return Transform{
		At:      e.fvec("at").Add(e.Offset),
		Scale:   scale,
		Angle:   e.fnum("angle"),
		Opacity: e.fnum("opacity"),
	}
}

func (e *Entity) field(name string) *Field {
	if f, ok := e.Fields[name]; ok {
		return f
	}
	f := &Field{Name: name, Val: defaultFieldVal(e.Type, name)}
	e.Fields[name] = f
	e.Order = append(e.Order, name)
	return f
}

func defaultFieldVal(typ, name string) Value {
	switch name {
	case "opacity", "draw", "scale":
		return 1.0
	case "at", "from", "to":
		return Vec{}
	case "value", "angle", "side", "w", "h", "radius":
		return 0.0
	}
	return nil
}

func (e *Entity) fnum(name string) float64 {
	f, ok := e.Fields[name]
	if !ok || f.Val == nil {
		if d := defaultFieldVal(e.Type, name); d != nil {
			if x, ok := d.(float64); ok {
				return x
			}
		}
		return 0
	}
	x, _ := asFloat(f.Val)
	return x
}

func (e *Entity) fvec(name string) Vec {
	f, ok := e.Fields[name]
	if !ok || f.Val == nil {
		return Vec{}
	}
	v, _ := asVec(resolveAnchored(e, f.Val))
	return v
}

func (e *Entity) fstr(name string) string {
	if f, ok := e.Fields[name]; ok {
		if s, ok := f.Val.(Str); ok {
			return string(s)
		}
		if s, ok := f.Val.(string); ok {
			return s
		}
	}
	return ""
}

type Group struct {
	Name  string
	Items []*Entity
}

// RecordFamily stores the structure of a domain-binder family declaration.
// The actual entities are stored in rt.Groups under computed names
// (see familyMemberName). The Group named after the family's name holds
// one synthetic Entity per instance (used for broadcast [*] over the family).
type RecordFamily struct {
	Name     string
	N        int   // number of instances
	Keys     []int // domain keys in declaration order
	KeyToPos map[int]int
	Members  []string // member names in declaration order, e.g. ["mark", "hit", "factor", "n"]
}

// familyMemberName returns the entity/group name for member `mem` of instance `idx`
// in family `family`.
func familyMemberName(family string, idx int, mem string) string {
	return fmt.Sprintf("%s__%d__%s", family, idx, mem)
}

// FamilyInstance is the runtime value of `roots[i]` — a handle to one instance
// of a family with named member entities.
type FamilyInstance struct {
	Family *RecordFamily
	Idx    int
	rt     *Runtime
}

type GVar struct {
	Name string
	Val  Value
	Live bool // true for colon-binding (`name: expr`) — re-evaluated each frame
	Def  Expr // non-nil if Live
}

// ---------- conversions ----------

func asFloat(v Value) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case Num:
		return float64(x), nil
	case int:
		return float64(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case ItVal:
		return asFloat(x.Val)
	}
	return 0, fmt.Errorf("not a number: %T", v)
}

func asVec(v Value) (Vec, error) {
	switch x := v.(type) {
	case AnchorPt:
		return x.P, nil
	case Vec:
		return x, nil
	case []Value:
		var out Vec
		if len(x) > 3 {
			return out, fmt.Errorf("list too long for a point")
		}
		for i, it := range x {
			f, err := asFloat(it)
			if err != nil {
				return out, err
			}
			out[i] = f
		}
		return out, nil
	case ItVal:
		return asVec(x.Val)
	}
	if f, err := asFloat(v); err == nil {
		return Vec{f, f, f}, nil
	}
	return Vec{}, fmt.Errorf("not a point: %T", v)
}

func asColor(v Value) (Color, error) {
	switch x := v.(type) {
	case Color:
		return x, nil
	}
	return Color{}, fmt.Errorf("not a color: %T", v)
}

func lerp(a, b, u float64) float64 { return a + (b-a)*u }

func lerpValue(a, b Value, u float64) Value {
	if a == nil {
		return b
	}
	switch av := a.(type) {
	case float64:
		bv, err := asFloat(b)
		if err != nil {
			return b
		}
		return lerp(av, bv, u)
	case Vec:
		bv, err := asVec(b)
		if err != nil {
			return b
		}
		return Vec{lerp(av[0], bv[0], u), lerp(av[1], bv[1], u), lerp(av[2], bv[2], u)}
	case Color:
		bv, err := asColor(b)
		if err != nil {
			return b
		}
		return Color{lerp(av.R, bv.R, u), lerp(av.G, bv.G, u), lerp(av.B, bv.B, u), lerp(av.A, bv.A, u)}
	}
	if u >= 1 {
		return b
	}
	return a
}

// ---------- constants ----------

func hexColor(s string) Color {
	var r, g, b int
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return Color{1, 1, 1, 1}
	}
	return Color{float64(r) / 255, float64(g) / 255, float64(b) / 255, 1}
}

var namedColors = map[string]Color{
	"white":      {1, 1, 1, 1},
	"black":      {0, 0, 0, 1},
	"blue":       hexColor("#58C4DD"),
	"blue_e":     hexColor("#1C758A"),
	"cyan":       hexColor("#00FFFF"),
	"teal":       hexColor("#5CD0B3"),
	"green":      hexColor("#83C167"),
	"lime":       hexColor("#32CD32"),
	"yellow":     hexColor("#FFFF00"),
	"gold":       hexColor("#F0AC5F"),
	"orange":     hexColor("#FF862F"),
	"red":        hexColor("#FC6255"),
	"red_b":      hexColor("#FF8080"),
	"maroon":     hexColor("#C55F73"),
	"pink":       hexColor("#D147BD"),
	"magenta":    hexColor("#FF00FF"),
	"purple":     hexColor("#9A72AC"),
	"violet":     hexColor("#EE82EE"),
	"brown":      hexColor("#8B4513"),
	"grey":       hexColor("#888888"),
	"gray":       hexColor("#888888"),
	"dark_gray":  hexColor("#444444"),
	"light_gray": hexColor("#BBBBBB"),
}

var namedVecs = map[string]Vec{
	"up": {0, 1, 0}, "down": {0, -1, 0}, "left": {-1, 0, 0}, "right": {1, 0, 0},
	"ul": {-1, 1, 0}, "ur": {1, 1, 0}, "dl": {-1, -1, 0}, "dr": {1, -1, 0},
	"center": {0, 0, 0},
}

var namedNums = map[string]float64{
	"pi": math.Pi, "tau": 2 * math.Pi, "e": math.E,
}

type namespace map[string]Value

func colorNamespace() namespace {
	ns := make(namespace, len(namedColors))
	for name, color := range namedColors {
		ns[name] = color
	}
	return ns
}

var namespaces = map[string]namespace{
	"color": colorNamespace(),
	"corner": {
		"ul":     namedVecs["ul"],
		"ur":     namedVecs["ur"],
		"dl":     namedVecs["dl"],
		"dr":     namedVecs["dr"],
		"center": namedVecs["center"],
	},
	"approx": {
		"above": namedVecs["up"],
		"below": namedVecs["down"],
		"left":  namedVecs["left"],
		"right": namedVecs["right"],
	},
	"math": {
		"pi":  namedNums["pi"],
		"tau": namedNums["tau"],
		"e":   namedNums["e"],
	},
}

// ---------- scope & eval ----------

type Scope struct {
	rt         *Runtime
	binds      map[string]Value
	localStack map[string]bool
}

func (s *Scope) with(name string, v Value) *Scope {
	nb := map[string]Value{}
	for k, x := range s.binds {
		nb[k] = x
	}
	nb[name] = v
	return &Scope{rt: s.rt, binds: nb, localStack: s.localStack}
}

type boundMethod struct {
	recv *Entity
	name string
}

type partsRef struct{ E *Entity }

func (s *Scope) lookup(name string) (Value, error) {
	if s.binds != nil {
		if v, ok := s.binds[name]; ok {
			return v, nil
		}
		if it, ok := s.binds["it"].(ItVal); ok {
			// Check the ItVal.Cols for named bind variables (family binder, data columns)
			if it.Cols != nil {
				if c, ok := it.Cols[name]; ok {
					if local, ok := c.(FamilyLocalBinding); ok {
						return s.evalFamilyLocal(local)
					}
					return c, nil
				}
			}
			switch name {
			case "i":
				return float64(it.I), nil
			}
		}
	}
	if g, ok := s.rt.Globals[name]; ok {
		return g.Val, nil
	}
	if grp, ok := s.rt.Groups[name]; ok {
		if len(grp.Items) == 1 {
			return grp.Items[0], nil
		}
		return grp, nil
	}
	if c, ok := namedColors[name]; ok {
		return c, nil
	}
	if v, ok := namedVecs[name]; ok {
		return v, nil
	}
	if n, ok := namedNums[name]; ok {
		return n, nil
	}
	if ns, ok := namespaces[name]; ok {
		return ns, nil
	}
	if _, ok := builtins[name]; ok {
		return Ident(name), nil // builtins are called by name
	}
	s.rt.warnOnce("unresolved name `" + name + "` (treated as nil)")
	return nil, nil
}

func (s *Scope) evalFamilyLocal(local FamilyLocalBinding) (Value, error) {
	if s.localStack == nil {
		s.localStack = map[string]bool{}
	}
	if s.localStack[local.Name] {
		return nil, fmt.Errorf("cycle in family-local binding %q", local.Name)
	}
	s.localStack[local.Name] = true
	defer delete(s.localStack, local.Name)

	return (&Scope{rt: s.rt, binds: s.binds, localStack: s.localStack}).Eval(local.E)
}

func (s *Scope) Eval(e Expr) (Value, error) {
	switch v := e.(type) {
	case nil:
		return nil, nil
	case litVal:
		return v.V, nil
	case Num:
		return float64(v), nil
	case Str:
		return string(v), nil
	case Ident:
		return s.lookup(string(v))
	case ListE:
		var out []Value
		for _, it := range v.Items {
			x, err := s.Eval(it)
			if err != nil {
				return nil, err
			}
			out = append(out, x)
		}
		return out, nil
	case AlphaE:
		x, err := s.Eval(v.X)
		if err != nil {
			return nil, err
		}
		c, err := asColor(x)
		if err != nil {
			return nil, err
		}
		c.A = v.Pct / 100
		return c, nil
	case UnE:
		x, err := s.Eval(v.X)
		if err != nil {
			return nil, err
		}
		if vec, ok := x.(Vec); ok {
			return vec.Mul(-1), nil
		}
		f, err := asFloat(x)
		if err != nil {
			return nil, err
		}
		return -f, nil
	case BinE:
		return s.evalBin(v)
	case RangeE:
		start, err := s.Eval(v.Start)
		if err != nil {
			return nil, err
		}
		end, err := s.Eval(v.End)
		if err != nil {
			return nil, err
		}
		sf, err := asFloat(start)
		if err != nil {
			return nil, err
		}
		ef, err := asFloat(end)
		if err != nil {
			return nil, err
		}
		var out []Value
		for i := int(sf); i < int(ef); i++ {
			out = append(out, float64(i))
		}
		return out, nil
	case CondE:
		c, err := s.Eval(v.Cond)
		if err != nil {
			return nil, err
		}
		b, _ := c.(bool)
		if !b {
			if f, err := asFloat(c); err == nil && f != 0 {
				b = true
			}
		}
		if b {
			return s.Eval(v.Then)
		}
		return s.Eval(v.Else)
	case AttrE:
		x, err := s.Eval(v.X)
		if err != nil {
			return nil, err
		}
		result, attrErr := s.attr(x, v.Name)
		if attrErr != nil {
			// If the base expression is a simple name that also names a namespace,
			// and the attribute lookup failed (e.g. user named their list `color`
			// shadowing the `color` namespace), fall back to the namespace.
			if id, ok := v.X.(Ident); ok {
				if ns, ok2 := namespaces[string(id)]; ok2 {
					if nsv, ok3 := ns[v.Name]; ok3 {
						return nsv, nil
					}
				}
			}
			return nil, attrErr
		}
		return result, nil
	case IndexE:
		x, err := s.Eval(v.X)
		if err != nil {
			return nil, err
		}
		if v.I == nil {
			return nil, fmt.Errorf("`[*]` outside an op-row broadcast position")
		}
		iv, err := s.Eval(v.I)
		if err != nil {
			return nil, err
		}
		fi, err := asFloat(iv)
		if err != nil {
			return nil, err
		}
		return indexValue(x, int(fi))
	case CallE:
		return s.evalCall(v)
	case FoldE:
		return nil, fmt.Errorf("fold expression outside a data column")
	case SnapshotE:
		// Evaluate the inner expression and return it as a frozen value.
		// The live-eval pass treats snapshot as not live (no deps), so this
		// value is only computed once at declaration/event time.
		val, err := s.Eval(v.X)
		if err != nil {
			return nil, err
		}
		return snapshotValue(val), nil
	}
	return nil, fmt.Errorf("cannot evaluate %T", e)
}

func indexValue(x Value, i int) (Value, error) {
	switch v := x.(type) {
	case []Value:
		if i < 0 || i >= len(v) {
			return nil, fmt.Errorf("index %d out of range (%d)", i, len(v))
		}
		return v[i], nil
	case *Group:
		// If this group is a family proxy group, return a FamilyInstance
		if len(v.Items) > 0 && v.Items[0].Type == "__family_proxy__" && v.Items[0].rt != nil {
			if fam, ok := v.Items[0].rt.Families[v.Name]; ok {
				if _, ok := fam.KeyToPos[i]; !ok {
					return nil, fmt.Errorf("family %s has no key %d", v.Name, i)
				}
				return FamilyInstance{Family: fam, Idx: i, rt: v.Items[0].rt}, nil
			}
		}
		if i < 0 || i >= len(v.Items) {
			return nil, fmt.Errorf("index %d out of range for record %s (len %d)", i, v.Name, len(v.Items))
		}
		return v.Items[i], nil
	case partsRef:
		if i < 0 || i >= len(v.E.Parts) {
			return nil, fmt.Errorf("part index %d out of range for %s", i, v.E.Name)
		}
		return v.E.Parts[i], nil
	case Vec:
		if i < 0 || i > 2 {
			return nil, fmt.Errorf("point index out of range")
		}
		return v[i], nil
	}
	return nil, fmt.Errorf("cannot index %T", x)
}

func (s *Scope) evalBin(v BinE) (Value, error) {
	l, err := s.Eval(v.L)
	if err != nil {
		return nil, err
	}
	r, err := s.Eval(v.R)
	if err != nil {
		return nil, err
	}
	// vector arithmetic when either side is (coercible to) a point
	// Check this BEFORE list-vectorized arithmetic so that
	// `Vec + [x, y]` coerces the list to Vec, not element-wise.
	lv, lIsVec := l.(Vec)
	rv, rIsVec := r.(Vec)
	if list, ok := l.([]Value); ok && rIsVec {
		if vv, err := asVec(list); err == nil {
			lv, lIsVec = vv, true
		}
	}
	if list, ok := r.([]Value); ok && lIsVec {
		if vv, err := asVec(list); err == nil {
			rv, rIsVec = vv, true
		}
	}
	if lIsVec || rIsVec {
		switch v.Op {
		case "+":
			if lIsVec && rIsVec {
				return lv.Add(rv), nil
			}
		case "-":
			if lIsVec && rIsVec {
				return lv.Sub(rv), nil
			}
		case "*":
			if lIsVec {
				k, err := asFloat(r)
				if err == nil {
					return lv.Mul(k), nil
				}
			} else {
				k, err := asFloat(l)
				if err == nil {
					return rv.Mul(k), nil
				}
			}
		case "/":
			if lIsVec {
				k, err := asFloat(r)
				if err == nil && k != 0 {
					return lv.Mul(1 / k), nil
				}
			}
		}
		return nil, fmt.Errorf("bad point arithmetic: %v", v.Op)
	}
	lf, lfErr := asFloat(l)
	rf, rfErr := asFloat(r)
	if lfErr == nil && rfErr == nil {
		switch v.Op {
		case "+":
			return lf + rf, nil
		case "-":
			return lf - rf, nil
		case "*":
			return lf * rf, nil
		case "/":
			return lf / rf, nil
		case "%":
			return math.Mod(lf, rf), nil
		case "==":
			return lf == rf, nil
		case "!=":
			return lf != rf, nil
		case "<":
			return lf < rf, nil
		case ">":
			return lf > rf, nil
		case "<=":
			return lf <= rf, nil
		case ">=":
			return lf >= rf, nil
		}
		return nil, fmt.Errorf("unknown operator %q", v.Op)
	}
	// Vectorized arithmetic: scalar/list op list/scalar → element-wise list result
	// Used for `prod(x - val)` patterns (x is float, val is list).
	if lList, ok := l.([]Value); ok {
		var out []Value
		for _, elem := range lList {
			res, err := s.evalBin(BinE{Op: v.Op, L: litVal{V: elem}, R: litVal{V: r}})
			if err != nil {
				return nil, err
			}
			out = append(out, res)
		}
		return out, nil
	}
	if rList, ok := r.([]Value); ok {
		var out []Value
		for _, elem := range rList {
			res, err := s.evalBin(BinE{Op: v.Op, L: litVal{V: l}, R: litVal{V: elem}})
			if err != nil {
				return nil, err
			}
			out = append(out, res)
		}
		return out, nil
	}
	if lfErr != nil {
		return nil, lfErr
	}
	return nil, rfErr
}

func (s *Scope) attr(x Value, name string) (Value, error) {
	switch v := x.(type) {
	case *Entity:
		return s.entityAttr(v, name)
	case namespace:
		if val, ok := v[name]; ok {
			return val, nil
		}
		return nil, fmt.Errorf("namespace has no entry %q", name)
	case Vec:
		switch name {
		case "x":
			return v[0], nil
		case "y":
			return v[1], nil
		case "z":
			return v[2], nil
		}
		return nil, fmt.Errorf("point has no attribute %q", name)
	case ItVal:
		switch name {
		case "i":
			return float64(v.I), nil
		case "n":
			return float64(v.N), nil
		}
		if v.Cols != nil {
			if c, ok := v.Cols[name]; ok {
				return c, nil
			}
		}
		return s.attr(v.Val, name)
	case *PartState:
		return s.partAttr(v, name)
	case Snapshot:
		if f, ok := v.Fields[name]; ok {
			return f, nil
		}
		return nil, fmt.Errorf("snapshot of %s has no field %q", v.Of, name)
	case stackRegion:
		return s.stackAttr(name)
	case []Value:
		if i, err := strconv.Atoi(name); err == nil {
			if i < 0 || i >= len(v) {
				return nil, fmt.Errorf("tuple index %d out of range", i)
			}
			return v[i], nil
		}
		switch name {
		case "indices":
			// Return the index domain 0..len(v) as a []Value of float64
			out := make([]Value, len(v))
			for i := range v {
				out[i] = float64(i)
			}
			return out, nil
		case "len":
			return float64(len(v)), nil
		}
		return nil, fmt.Errorf("list has no attribute %q (did you mean .indices or .len?)", name)
	case nil:
		// nil.name: fall through to namespace check in AttrE eval (handled in Eval)
		return nil, nil
	case FamilyInstance:
		// family[i].memberName — look up the member entity
		memberName := familyMemberName(v.Family.Name, v.Idx, name)
		if grp, ok := v.rt.Groups[memberName]; ok {
			if len(grp.Items) == 1 {
				return grp.Items[0], nil
			}
			return grp, nil
		}
		return nil, fmt.Errorf("family %s instance %d has no member %q", v.Family.Name, v.Idx, name)
	}
	return nil, fmt.Errorf("%T has no attributes", x)
}

func (s *Scope) entityAttr(e *Entity, name string) (Value, error) {
	switch name {
	case "parts":
		return partsRef{E: e}, nil
	case "point":
		if e.Type == "axes" {
			return boundMethod{recv: e, name: "point"}, nil
		}
	}
	at := e.transform().At
	w, h := entitySize(e)
	switch name {
	case "at":
		return at, nil
	case "top":
		return AnchorPt{P: Vec{at[0], at[1] + h/2, 0}, Dir: Vec{0, 1, 0}}, nil
	case "bottom":
		return AnchorPt{P: Vec{at[0], at[1] - h/2, 0}, Dir: Vec{0, -1, 0}}, nil
	case "left":
		return AnchorPt{P: Vec{at[0] - w/2, at[1], 0}, Dir: Vec{-1, 0, 0}}, nil
	case "right":
		return AnchorPt{P: Vec{at[0] + w/2, at[1], 0}, Dir: Vec{1, 0, 0}}, nil
	case "ul":
		return AnchorPt{P: Vec{at[0] - w/2, at[1] + h/2, 0}, Dir: Vec{-1, 1, 0}}, nil
	case "ur":
		return AnchorPt{P: Vec{at[0] + w/2, at[1] + h/2, 0}, Dir: Vec{1, 1, 0}}, nil
	case "dl":
		return AnchorPt{P: Vec{at[0] - w/2, at[1] - h/2, 0}, Dir: Vec{-1, -1, 0}}, nil
	case "dr":
		return AnchorPt{P: Vec{at[0] + w/2, at[1] - h/2, 0}, Dir: Vec{1, -1, 0}}, nil
	}
	if f, ok := e.Fields[name]; ok && f.Val != nil {
		return f.Val, nil
	}
	switch name {
	case "w":
		return w, nil
	case "h":
		return h, nil
	}
	if d := defaultFieldVal(e.Type, name); d != nil {
		return d, nil
	}
	return nil, fmt.Errorf("record %s has no field %q", e.Name, name)
}

func (s *Scope) partAttr(p *PartState, name string) (Value, error) {
	switch name {
	case "color":
		if p.Color != nil {
			return p.Color, nil
		}
		return entityColor(p.E), nil
	case "opacity":
		return p.Opacity, nil
	}
	// geometric attrs come from the text layout
	at, w, h := partBox(p)
	switch name {
	case "at":
		return at, nil
	case "top":
		return Vec{at[0], at[1] + h/2, 0}, nil
	case "bottom":
		return Vec{at[0], at[1] - h/2, 0}, nil
	case "left":
		return Vec{at[0] - w/2, at[1], 0}, nil
	case "right":
		return Vec{at[0] + w/2, at[1], 0}, nil
	case "w":
		return w, nil
	case "h":
		return h, nil
	}
	return nil, fmt.Errorf("part %s has no attribute %q", p.Name, name)
}

func (s *Scope) evalCall(v CallE) (Value, error) {
	// scoped binder functions: plot fn / warp exprs are evaluated via
	// helper evalWith, not here.
	if id, ok := v.Fn.(Ident); ok {
		if fn, isBuiltin := builtins[string(id)]; isBuiltin {
			if _, shadowed := s.binds[string(id)]; !shadowed {
				args := make([]Value, len(v.Args))
				for i, a := range v.Args {
					x, err := s.Eval(a)
					if err != nil {
						return nil, err
					}
					args[i] = x
				}
				return fn(s, args)
			}
		}
	}
	fnv, err := s.Eval(v.Fn)
	if err != nil {
		return nil, err
	}
	if bm, ok := fnv.(boundMethod); ok {
		args := make([]Value, len(v.Args))
		for i, a := range v.Args {
			x, err := s.Eval(a)
			if err != nil {
				return nil, err
			}
			args[i] = x
		}
		return callMethod(s, bm, args)
	}
	return nil, fmt.Errorf("cannot call %T", fnv)
}

func callMethod(s *Scope, bm boundMethod, args []Value) (Value, error) {
	switch bm.name {
	case "point": // axes data coords -> world point
		if len(args) < 2 {
			return nil, fmt.Errorf("point(x, y) needs two args")
		}
		x, err := asFloat(args[0])
		if err != nil {
			return nil, err
		}
		y, err := asFloat(args[1])
		if err != nil {
			return nil, err
		}
		return axesPoint(bm.recv, x, y), nil
	}
	return nil, fmt.Errorf("unknown method %q", bm.name)
}

// ---------- builtins ----------

type builtinFn func(s *Scope, args []Value) (Value, error)

var builtins map[string]builtinFn

const (
	screenTop   = 3.2
	screenEdgeX = 5.4
)

func arg1f(args []Value) (float64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("expected 1 numeric arg")
	}
	return asFloat(args[0])
}

func entOf(v Value) *Entity {
	switch x := v.(type) {
	case *Entity:
		return x
	case *Group:
		if len(x.Items) > 0 {
			return x.Items[0]
		}
	}
	return nil
}

func anchorOf(s *Scope, v Value) (Vec, float64, float64) {
	if e := entOf(v); e != nil {
		w, h := entitySize(e)
		return e.transform().At, w, h
	}
	if p, ok := v.(*PartState); ok {
		at, w, h := partBox(p)
		return at, w, h
	}
	if vec, err := asVec(v); err == nil && v != nil {
		return vec, 0, 0
	}
	return Vec{}, 0, 0
}

func init() {
	builtins = map[string]builtinFn{
		"sin": func(s *Scope, a []Value) (Value, error) {
			f, err := arg1f(a)
			return math.Sin(f), err
		},
		"cos": func(s *Scope, a []Value) (Value, error) {
			f, err := arg1f(a)
			return math.Cos(f), err
		},
		"exp": func(s *Scope, a []Value) (Value, error) {
			f, err := arg1f(a)
			return math.Exp(f), err
		},
		"sinh": func(s *Scope, a []Value) (Value, error) {
			f, err := arg1f(a)
			return math.Sinh(f), err
		},
		"cosh": func(s *Scope, a []Value) (Value, error) {
			f, err := arg1f(a)
			return math.Cosh(f), err
		},
		"abs": func(s *Scope, a []Value) (Value, error) {
			f, err := arg1f(a)
			return math.Abs(f), err
		},
		"sqrt": func(s *Scope, a []Value) (Value, error) {
			f, err := arg1f(a)
			return math.Sqrt(f), err
		},
		"range": func(s *Scope, a []Value) (Value, error) {
			n, err := arg1f(a)
			if err != nil {
				return nil, err
			}
			var out []Value
			for i := 0; i < int(n); i++ {
				out = append(out, float64(i))
			}
			return out, nil
		},
		"fmt": fmtBuiltin,
		"center": func(s *Scope, a []Value) (Value, error) {
			if len(a) == 0 {
				return Vec{}, nil
			}
			at, _, _ := anchorOf(s, a[0])
			return at, nil
		},
		"below": func(s *Scope, a []Value) (Value, error) {
			if len(a) < 1 {
				return nil, fmt.Errorf("below(x, gap)")
			}
			gap := 0.25
			if len(a) > 1 {
				gap, _ = asFloat(a[1])
			}
			at, _, h := anchorOf(s, a[0])
			return Vec{at[0], at[1] - h/2 - gap - 0.3, 0}, nil
		},
		"above": func(s *Scope, a []Value) (Value, error) {
			if len(a) < 1 {
				return nil, fmt.Errorf("above(x, gap)")
			}
			gap := 0.25
			if len(a) > 1 {
				gap, _ = asFloat(a[1])
			}
			at, _, h := anchorOf(s, a[0])
			return Vec{at[0], at[1] + h/2 + gap + 0.3, 0}, nil
		},
		"left": func(s *Scope, a []Value) (Value, error) {
			d, err := arg1f(a)
			return Vec{-d, 0, 0}, err
		},
		"right": func(s *Scope, a []Value) (Value, error) {
			d, err := arg1f(a)
			return Vec{d, 0, 0}, err
		},
		"right_of": func(s *Scope, a []Value) (Value, error) {
			if len(a) < 2 {
				return nil, fmt.Errorf("right_of(x, gap)")
			}
			gap, _ := asFloat(a[1])
			at, w, _ := anchorOf(s, a[0])
			return Vec{at[0] + w/2 + gap, at[1], 0}, nil
		},
		"corner": func(s *Scope, a []Value) (Value, error) {
			if len(a) != 1 {
				return nil, fmt.Errorf("corner(DIR)")
			}
			d, err := asVec(a[0])
			if err != nil {
				return nil, err
			}
			const buff = 0.35
			return AnchorPt{
				P:   Vec{d[0] * (screenEdgeX - buff), d[1] * (screenTop - buff), 0},
				Dir: d,
			}, nil
		},
		"edge": func(s *Scope, a []Value) (Value, error) {
			if len(a) != 1 {
				return nil, fmt.Errorf("edge(DIR)")
			}
			d, err := asVec(a[0])
			if err != nil {
				return nil, err
			}
			return Vec{d[0] * screenEdgeX, d[1] * (screenTop - 0.4), 0}, nil
		},
		"stack": func(s *Scope, a []Value) (Value, error) {
			// a stack region; unresolved names degrade to the screen region
			return stackRegion{}, nil
		},
		// sum(list) — sum of all elements
		"sum": func(s *Scope, a []Value) (Value, error) {
			if len(a) != 1 {
				return nil, fmt.Errorf("sum(list) expects 1 arg")
			}
			list, ok := a[0].([]Value)
			if !ok {
				return nil, fmt.Errorf("sum: arg must be a list, got %T", a[0])
			}
			total := 0.0
			for _, v := range list {
				f, err := asFloat(v)
				if err != nil {
					return nil, fmt.Errorf("sum: %v", err)
				}
				total += f
			}
			return total, nil
		},
		// prod(list) — product of all elements
		"prod": func(s *Scope, a []Value) (Value, error) {
			if len(a) != 1 {
				return nil, fmt.Errorf("prod(list) expects 1 arg")
			}
			list, ok := a[0].([]Value)
			if !ok {
				return nil, fmt.Errorf("prod: arg must be a list, got %T", a[0])
			}
			p := 1.0
			for _, v := range list {
				f, err := asFloat(v)
				if err != nil {
					return nil, fmt.Errorf("prod: %v", err)
				}
				p *= f
			}
			return p, nil
		},
		// pair_sum(list) — sum of all pairwise products (Vieta's c1 coefficient)
		"pair_sum": func(s *Scope, a []Value) (Value, error) {
			if len(a) != 1 {
				return nil, fmt.Errorf("pair_sum(list) expects 1 arg")
			}
			list, ok := a[0].([]Value)
			if !ok {
				return nil, fmt.Errorf("pair_sum: arg must be a list, got %T", a[0])
			}
			floats := make([]float64, len(list))
			for i, v := range list {
				f, err := asFloat(v)
				if err != nil {
					return nil, fmt.Errorf("pair_sum: %v", err)
				}
				floats[i] = f
			}
			total := 0.0
			for i := 0; i < len(floats); i++ {
				for j := i + 1; j < len(floats); j++ {
					total += floats[i] * floats[j]
				}
			}
			return total, nil
		},
		// extern fn spiral(i, n) -> point: the Go-side stub for example 90.
		"spiral": func(s *Scope, a []Value) (Value, error) {
			if len(a) != 2 {
				return nil, fmt.Errorf("spiral(i, n)")
			}
			i, _ := asFloat(a[0])
			n, _ := asFloat(a[1])
			r := 0.6 + 2.4*i/math.Max(n, 1)
			ang := i * 2.399963 // golden angle
			return Vec{r * math.Cos(ang), r * math.Sin(ang), 0}, nil
		},
	}
}

type stackRegion struct{}

func (s *Scope) stackAttr(name string) (Value, error) {
	switch name {
	case "top":
		return Vec{0, screenTop, 0}, nil
	case "bottom":
		return Vec{0, -screenTop, 0}, nil
	case "center":
		return Vec{}, nil
	}
	return nil, fmt.Errorf("stack region has no attribute %q", name)
}

var fmtSpecRe = regexp.MustCompile(`\{(:[^}]*)?\}`)

func fmtBuiltin(s *Scope, args []Value) (Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("fmt(spec, args...)")
	}
	spec, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("fmt: first arg must be a string")
	}
	rest := args[1:]
	i := 0
	out := fmtSpecRe.ReplaceAllStringFunc(spec, func(m string) string {
		if i >= len(rest) {
			return m
		}
		v := rest[i]
		i++
		sub := fmtSpecRe.FindStringSubmatch(m)
		goFmt := "%v"
		if sub[1] != "" {
			goFmt = "%" + strings.TrimPrefix(sub[1], ":")
		}
		if strings.ContainsAny(goFmt, "fFeEgG") {
			f, err := asFloat(v)
			if err == nil {
				return fmt.Sprintf(goFmt, f)
			}
		}
		return fmt.Sprintf("%v", v)
	})
	return out, nil
}

// evalWith evaluates an expression with extra binders (plot fn `x`, warp `p`,
// family `it`, …).
func evalWith(rt *Runtime, e Expr, binds map[string]Value) (Value, error) {
	s := &Scope{rt: rt, binds: binds}
	return s.Eval(e)
}

// ---------- geometry helpers used by attrs & render ----------

func entitySize(e *Entity) (w, h float64) {
	scale := e.transform().Scale
	switch e.Type {
	case "rect":
		return e.fnum("w") * scale, e.fnum("h") * scale
	case "square":
		return e.fnum("side") * scale, e.fnum("side") * scale
	case "dot":
		r := e.fnum("radius")
		if r == 0 {
			r = 0.08
		}
		return 2 * r * scale, 2 * r * scale
	case "arc":
		r := e.fnum("radius")
		if r <= 0 {
			r = 0.5
		}
		return 2 * r * scale, 2 * r * scale
	case "tex", "typst", "text", "decimal":
		lay := textLayoutOf(e)
		if lay != nil {
			return lay.W, lay.H
		}
	case "axes", "plane":
		return 10, 6
	}
	return 1 * scale, 1 * scale
}

func entityColor(e *Entity) Color {
	for _, name := range []string{"color", "fill"} {
		if f, ok := e.Fields[name]; ok && f.Val != nil {
			if c, err := asColor(f.Val); err == nil {
				return c
			}
		}
	}
	return namedColors["white"]
}

func partBox(p *PartState) (at Vec, w, h float64) {
	lay := textLayoutOf(p.E)
	if lay == nil {
		return p.E.fvec("at"), 0.5, 0.5
	}
	return lay.partBox(p)
}

func axesPoint(ax *Entity, x, y float64) Vec {
	return axesLocalPoint(ax, x, y).Add(ax.fvec("at"))
}

func axesLocalPoint(ax *Entity, x, y float64) Vec {
	xr := rangeOf(ax, "x_range", -7, 7)
	yr := rangeOf(ax, "y_range", -4, 4)
	w, h := 10.0, 6.0
	cx, cy := (xr[0]+xr[1])/2, (yr[0]+yr[1])/2
	sx := w / (xr[1] - xr[0])
	sy := h / (yr[1] - yr[0])
	return Vec{(x - cx) * sx, (y - cy) * sy, 0}
}

func rangeOf(e *Entity, name string, lo, hi float64) [3]float64 {
	out := [3]float64{lo, hi, 1}
	f, ok := e.Fields[name]
	if !ok || f.Val == nil {
		return out
	}
	list, ok := f.Val.([]Value)
	if !ok {
		return out
	}
	for i := 0; i < len(list) && i < 3; i++ {
		if v, err := asFloat(list[i]); err == nil {
			out[i] = v
		}
	}
	return out
}
