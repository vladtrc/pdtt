# pdtt — why not X

Common terms people reach for that don't exist in pdtt, and where to go instead.

---

### `bind` / `track` / updater callbacks
**Gone.** Tween to a dynamic expression does this.
`| label.at -> center(dot)` — after the window, label follows dot. No separate bind needed.

### "live field" / liveness analysis
**Not a concept.** Fields are just expressions.
If the expression references something time-varying, it updates. If not, it doesn't.
No classification, no compiler pass, no special rules.

### `state`
**Gone.** Use `type name:` (a record). Static initial values are just field declarations.
Snapshot-and-restore uses `=` capture + record arrow.

### `table`
**Gone.** Use `data name:` (a record of type `data`, which renders nothing).
Row source goes in `for:`.

### `sibling` / `item` / `i` / `count` / `row`
**Gone.** One binder: `it`. Access index as `it.i`, count as `it.n`, columns as `it.field`.
To avoid shadowing in nested `each`, use `each … as name`.

### `morph` as a standalone verb
`morph` is a **modifier** (sets the transition strategy), not a verb.
It sits between pipes like any other modifier, before the tween.

```
| morph | a -> b
| .5- | stagger .1s | morph | by name | parts[*] -> targets[it.i]
```

### Uppercase constants (`UL`, `DOWN`, `RED`, …)
**Gone.** Use namespaced globals: `corner.ul`, `approx.above`, `color.red`.

### `tween x to y` / `during(a,b) { … }`
**Gone.** The tween `x -> y` with modifier cells is the only syntax.

### `extern updater` / `extern gen`
**Gone.** Only `extern fn` (pure, eval-time) remains.
Per-frame logic and lazy scene generation are refused by design.

### Camera class / `MovingCameraScene`
**Gone.** The camera is a builtin record named `frame` with fields `at`, `w`, `h`, `angle`.

### Plural type names (`dots`, `lines`, …)
**Gone.** A record with `for:` is plural. Name stays singular.
