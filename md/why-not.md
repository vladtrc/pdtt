# pdtt — what doesn't exist

Terms people reach for, and where to go instead.

- **`bind` / `track` / updater callbacks** — tween to a dynamic expression instead:
  `| label.at -> center(dot)` keeps following after the window.
- **liveness analysis / "live field"** — not a concept. A field is its expression; it
  updates iff the expression references something time-varying.
- **`state`** — use a record. Snapshot-and-restore is `=` capture + a tween back.
- **`table`** — use `data name:` with a `for:` row source.
- **`sibling` / `item` / `count` / `row`** — one binder, `it` (`.i`, `.n`, `.field`).
  Disambiguate nested `each` with `each … as name`.
- **`morph` as a verb** — it's a modifier (a transition strategy), spelled `transition:morph` before the `->`.
- **uppercase constants (`UL`, `RED`, …)** — namespaced globals: `corner.ul`, `color.red`.
- **`tween x to y` / `during(a,b){…}`** — the `->` line with modifier cells is the only form.
- **`extern updater` / `extern gen`** — only pure `extern fn` exists; per-frame logic is refused.
- **camera class / `MovingCameraScene`** — the builtin `frame` record is the camera.
- **plural type names (`dots`)** — a `for:` makes a record plural; the name stays singular.
