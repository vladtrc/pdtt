package pdtt

import "fmt"

// Step advances the runtime to absolute time t, resolving one frame's worth of
// events, live fields, animations, and rate integration. The phase order is
// load-bearing: posts settle before live fields, anims run against settled
// inputs, then a final live/post pass lets dynamic tweens converge.
func (rt *Runtime) Step(t float64) error {
	rt.Dt = t - rt.T
	rt.T = t

	s := &runtimeStep{rt: rt, t: t}
	if err := s.runEvents(); err != nil {
		return err
	}
	s.applyPosts()
	if err := s.evalLive(true); err != nil {
		return err
	}
	if err := s.runAnims(); err != nil {
		return err
	}
	if err := s.evalLive(false); err != nil {
		return err
	}
	s.applyPosts()
	if err := s.evalLive(false); err != nil {
		return err
	}
	return s.integrateRates()
}

type runtimeStep struct {
	rt        *Runtime
	t         float64
	liveDirty bool
}

func (s *runtimeStep) runEvents() error {
	for _, ev := range s.rt.Events {
		if !ev.done && ev.T <= s.t+1e-9 {
			ev.done = true
			if err := ev.Run(s.rt); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *runtimeStep) evalLiveGlobals() error {
	for _, g := range s.rt.liveGlobals {
		if g.Def == nil || s.rt.globalHasWriter(g.Name) {
			continue
		}
		val, err := (&Scope{rt: s.rt}).Eval(g.Def)
		if err != nil {
			return fmt.Errorf("live global %s: %v", g.Name, err)
		}
		g.Val = val
	}
	return nil
}

func (s *runtimeStep) evalLiveFields(allowActiveWritten bool) error {
	s.rt.clearLocalBindCache()
	for _, sl := range s.rt.liveFields {
		if sl.F.Frozen {
			continue
		}
		key := sl.E.Name + "." + sl.F.Name
		if s.rt.fieldHasWriter(key) && (!allowActiveWritten || !s.rt.fieldHasActiveWriter(key, s.t)) {
			continue
		}
		scope := s.rt.fieldEvalScope(sl.E.It)
		val, err := scope.Eval(sl.F.Def)
		if err != nil {
			return fmt.Errorf("live field %s.%s: %v", sl.E.Name, sl.F.Name, err)
		}
		sl.F.Val = coerceField(sl.F.Name, val)
	}
	return nil
}

func (s *runtimeStep) evalLive(allowActiveWritten bool) error {
	if err := s.evalLiveGlobals(); err != nil {
		return err
	}
	return s.evalLiveFields(allowActiveWritten)
}

func (s *runtimeStep) applyPosts() {
	for key, post := range s.rt.post {
		cur := post.Ref.Get()
		goal, err := evalTweenGoal(s.rt, post.RHS, post.Binds, post.ElemIdx, post.ElemN, cur)
		if err != nil {
			s.rt.warnOnce(fmt.Sprintf("post-tween `%s`: %v", key, err))
			continue
		}
		post.Ref.Set(goal)
	}
}

func (s *runtimeStep) runAnims() error {
	for _, a := range s.rt.Anims {
		if a.done || s.t+1e-9 < a.T0 {
			continue
		}
		if s.liveDirty && s.rt.animNeedsLiveInput(a) {
			if err := s.evalLive(false); err != nil {
				return err
			}
			s.liveDirty = false
		}
		if !a.started {
			a.started = true
			if a.Start != nil {
				if err := a.Start(a, s.rt); err != nil {
					return err
				}
			}
		}
		u := 1.0
		if a.T1 > a.T0 {
			u = (s.t - a.T0) / (a.T1 - a.T0)
		}
		if u >= 1 {
			u = 1
			a.done = true
		}
		if a.Update != nil {
			a.Update(a, s.rt, a.Ease(u))
		}
		if s.rt.animNeedsLiveRefresh(a) {
			s.liveDirty = true
		}
	}
	return nil
}

func (s *runtimeStep) integrateRates() error {
	if s.rt.Dt <= 0 {
		return nil
	}
	for _, e := range s.rt.Entities {
		for _, n := range e.Order {
			f := e.Fields[n]
			if !f.Rate || f.Def == nil {
				continue
			}
			scope := s.rt.fieldEvalScope(e.It)
			scope.binds["self"] = f.Val
			rate, err := scope.Eval(f.Def)
			if err != nil {
				return fmt.Errorf("rate field %s.%s: %v", e.Name, n, err)
			}
			switch rv := rate.(type) {
			case Vec:
				cur, _ := asVec(f.Val)
				f.Val = cur.Add(rv.Mul(s.rt.Dt))
			default:
				rf, err := asFloat(rate)
				if err != nil {
					return err
				}
				cur, _ := asFloat(f.Val)
				f.Val = cur + rf*s.rt.Dt
			}
		}
	}
	return nil
}
