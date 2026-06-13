# pdtt — the tween (`->`)

## What it is

`path -> expr` is the tween operator.

During the window: interpolates `path` from its current value to `expr`.
After the window: `path` is `expr`. Done. The previous value is forgotten.

## Both sides are just values

The tween does not care what either side is.
Static literal, dynamic expression, captured snapshot, field of another object — anything.

```
| 2s smooth
| dot.at -> [3, 1]              # after: dot.at is [3, 1], static
| label.at -> center(dot)       # after: label.at follows center(dot)
| frame.at -> home.at           # after: frame.at follows home.at
```

If the target expression is dynamic, the field keeps updating after the window.
If the target is a literal, the field stays fixed.

## Ease

The modifier cell before the tween sets the easing curve. Default: `linear`.

## Transition strategy

A transition modifier (`morph`, `fade_in`, `draw`, `write`) specifies how the
interpolation is visually rendered. Without one, default is direct value interpolation.

```
| 1.5s
| morph | title -> transform_title
| fade_in | grid
```

## What the tween is not

Not a binding declaration, not a detach operation, not a query about what the field
"used to be". It is an assignment with an animated transition.
After `x -> y`, x is y. Everything before that is irrelevant.
