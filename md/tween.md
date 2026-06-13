# pdtt — the tween (`->`)

`path -> expr` is the one operation in the language.

- **During the window:** interpolate `path` from its current value toward `expr`.
- **After the window:** `path` *is* `expr`. The previous value is forgotten.

Both sides are just values — literal, dynamic expression, captured snapshot, another
record's field. The tween does not care which.

```
| 1.5s | smooth
| dot.at -> [3, 1]            # after: dot.at is [3, 1], fixed
| label.at -> center(dot)     # after: label.at follows center(dot)
| frame.at -> home.at         # after: the camera follows home.at
```

If the target is dynamic, the field keeps tracking it after the window; if it's a
literal, the field stays put. This subsumes bind / track / updater callbacks — there
is no separate "follow" construct.

Easing and the transition strategy (`morph`, `fade_in`, …) come from modifier cells
before the `->`; default is linear, direct interpolation.

`x -> y` is an assignment with an animated transition — not a binding, detach, or a
query about the old value. After it, `x` is `y`.
