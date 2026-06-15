# 02 — `Vec` is 3D in a 2D system

## The assumption

```go
// internal/pdtt/model.go
type Vec [3]float64
```

Everything — points, centroids, lerps, resampling — carries a `z`. But the
renderer is 2D: the camera projects with `c.sx(p)` using only x/y, and the
third component is almost always 0 and almost never meaningfully interpolated.

## Why it's a problem

- **Dead weight in the hot path.** Resampling and lerping run per-frame over ~192
  points per contour; a third of that arithmetic moves a coordinate nobody draws.
- **Constant low-grade noise.** Every `Vec{...}` literal carries a trailing `0`,
  every lerp has a third line, every helper must decide whether to touch `[2]`.
  It invites "did I handle z?" mistakes that don't matter but cost attention.
- **Half-committed.** The system is neither honestly 2D (then drop the axis) nor
  genuinely 3D (then the camera, fills, and morphing would need real depth
  handling). The current state is the worst of both: 3D cost, 2D capability.

## The thing to question

Is there any feature on the roadmap that needs the z axis? If not, it is
pure tax. If yes, it is unfinished — z is currently ignored by the renderer.

## Sketch of a fix

- If 2D is the truth: make `Vec [2]float64`, delete the trailing components, let
  the compiler find every site. Large but mechanical.
- If 3D is wanted: that is a real project (depth-aware projection, z-sorting),
  not a type change — track it as a feature, not a cleanup.

## Caution

This is **wide, not deep** — `Vec` touches nearly every file. Don't do it
casually or alongside other changes; it deserves its own PR and its own review.
