package pdtt

import "fmt"

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
	case "points":
		if pts, err := resolvePoints(v); err == nil {
			return pointsAsValues(pts)
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
func (r PartColorRef) Key() string { return r.P.E.Name + ".parts." + r.P.key() + ".color" }

type PartOpacityRef struct{ P *PartState }

func (r PartOpacityRef) Get() Value { return r.P.Opacity }
func (r PartOpacityRef) Set(v Value) {
	f, err := asFloat(v)
	if err == nil {
		r.P.Opacity = f
	}
}
func (r PartOpacityRef) Key() string { return r.P.E.Name + ".parts." + r.P.key() + ".opacity" }

// partFloatRef tweens one of a part's scalar emphasis fields (strike, underline,
// wiggle, scale). The attr name doubles as the liveness key suffix.
type partFloatRef struct {
	P    *PartState
	Attr string
}

func (r partFloatRef) Get() Value {
	switch r.Attr {
	case "strike":
		return r.P.Strike
	case "underline":
		return r.P.Underline
	case "wiggle":
		return r.P.Wiggle
	case "scale":
		return r.P.scaleOr1()
	}
	return 0.0
}

func (r partFloatRef) Set(v Value) {
	f, err := asFloat(v)
	if err != nil {
		return
	}
	switch r.Attr {
	case "strike":
		r.P.Strike = f
	case "underline":
		r.P.Underline = f
	case "wiggle":
		r.P.Wiggle = f
	case "scale":
		r.P.Scale = f
	}
}

func (r partFloatRef) Key() string { return r.P.E.Name + ".parts." + r.P.key() + "." + r.Attr }

// partRefFor builds the tweenable Ref for `<text>.sub("...").<attr>`.
func partRefFor(p *PartState, attr string) (Ref, bool) {
	switch attr {
	case "color":
		return PartColorRef{P: p}, true
	case "opacity":
		return PartOpacityRef{P: p}, true
	case "strike", "underline", "wiggle", "scale":
		return partFloatRef{P: p, Attr: attr}, true
	}
	return nil, false
}

type WarpBlendRef struct{ E *Entity }

func (r WarpBlendRef) Get() Value  { return r.E.WarpBlend }
func (r WarpBlendRef) Set(v Value) { f, _ := asFloat(v); r.E.WarpBlend = f }
func (r WarpBlendRef) Key() string { return r.E.Name + ".__warp" }

// ListElemRef tweens one element of a global list variable.
type ListElemRef struct {
	G   *GVar
	Idx int
}

func (r ListElemRef) Get() Value {
	if list, ok := r.G.Val.([]Value); ok && r.Idx >= 0 && r.Idx < len(list) {
		return list[r.Idx]
	}
	return nil
}

func (r ListElemRef) Set(v Value) {
	list, ok := r.G.Val.([]Value)
	if !ok {
		return
	}
	if r.Idx < 0 || r.Idx >= len(list) {
		return
	}
	newList := make([]Value, len(list))
	copy(newList, list)
	newList[r.Idx] = v
	r.G.Val = newList
}
func (r ListElemRef) Key() string { return fmt.Sprintf("%s[%d]", r.G.Name, r.Idx) }
