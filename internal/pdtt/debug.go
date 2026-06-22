package pdtt

import "fmt"

// DebugVar is one field of one entity, frozen at a frame, formatted for display.
type DebugVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// DebugEntity is an entity's visible field values at a single frame.
type DebugEntity struct {
	Name   string     `json:"name"`
	Type   string     `json:"type"`
	Active bool       `json:"active"`
	Fields []DebugVar `json:"fields"`
}

// DebugFrame snapshots every entity's fields at the current runtime state. Call
// after rt.Step(t) to capture the variable values the user sees on that frame.
func (rt *Runtime) DebugFrame() []DebugEntity {
	out := make([]DebugEntity, 0, len(rt.Entities))
	for _, e := range rt.Entities {
		if e.Type == "data" || e.IsFamilyProxy {
			continue
		}
		de := DebugEntity{Name: e.Name, Type: e.Type, Active: e.Active}
		for _, name := range e.Order {
			f, ok := e.Fields[name]
			if !ok || f.Val == nil {
				continue
			}
			de.Fields = append(de.Fields, DebugVar{Name: name, Value: formatDebugValue(f.Val)})
		}
		out = append(out, de)
	}
	return out
}

// formatDebugValue renders a runtime Value as a short human string. ponytail:
// covers the value types that actually appear in fields; unknowns fall back to
// %v, which is fine for a debug panel.
func formatDebugValue(v Value) string {
	switch x := v.(type) {
	case float64:
		return trimFloat(x)
	case Num:
		return trimFloat(float64(x))
	case int:
		return fmt.Sprintf("%d", x)
	case bool:
		return fmt.Sprintf("%t", x)
	case string:
		return x
	case Vec:
		return "[" + trimFloat(x[0]) + ", " + trimFloat(x[1]) + "]"
	case AnchorPt:
		return "[" + trimFloat(x.P[0]) + ", " + trimFloat(x.P[1]) + "]"
	case Color:
		return fmt.Sprintf("rgba(%s, %s, %s, %s)", trimFloat(x.R), trimFloat(x.G), trimFloat(x.B), trimFloat(x.A))
	case ItVal:
		return formatDebugValue(x.Val)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func trimFloat(f float64) string {
	return fmt.Sprintf("%.4g", f)
}
