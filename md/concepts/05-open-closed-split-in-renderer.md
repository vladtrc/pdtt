# 05 — Open/closed split still lives in the renderer

## The assumption

The morph pipeline was unified on one idea (see
`internal/pdtt/morph.go`): every shape is a closed loop of points; openness is
geometric (an open path is folded there-and-back), never a flag. That removed the
`resampleClosed`/`resampleOpen` branching and the per-contour closed flag from
*morphing*.

But the **static** renderer still branches:

```go
// internal/pdtt/render.go — outlinePoints
if e.fnum("closed") != 0 {
    return resampleClosed(pathPts, n)
}
return resampleOpen(pathPts, n)
```

So there are now two different notions of "a shape's points": the morph one
(always a loop) and the static-draw one (open or closed).

## Why it's a problem

- **Two sources of truth** for the same geometry, which can drift. A shape can be
  sampled one way for a frozen frame and another way mid-morph.
- It is the **last remaining instance** of the open/closed split we deliberately
  removed elsewhere — leaving it half-done is the inconsistency, not the split
  itself.

## Why it's low severity

The static path is correct today and visually fine; an open polyline genuinely
*should* draw open when frozen. This is a consistency/tidiness question, not a
bug.

## The thing to question

Should there be exactly one "points of a shape" function repo-wide, with open/
closed handled the same way the morph code handles it — or is the static case
legitimately different (a frozen open line is not a degenerate loop)?

## Sketch of a fix

Either route `outlinePoints` through the same loop extraction `morphLoops` uses
(and let the renderer decide closure once), or document explicitly *why* static
draw keeps a separate notion — so the divergence is intentional, not accidental.
