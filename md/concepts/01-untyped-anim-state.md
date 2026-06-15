# 01 — Untyped animation state (`a.start []Value`)

## The assumption

Every animation captures its start state in a single untyped slice:

```go
// internal/pdtt/compile.go
start         []Value // captured start values per target
```

and reads it back by position, with the meaning of each slot implicit and
branch-dependent:

```go
s0, _ := asFloat(a.start[0])
d0, _ := asFloat(a.start[1])
off, _ := asVec(a.start[2])
if srcMove != nil && len(a.start) > 3 {
    srcOff, _ := asVec(a.start[3])
}
```

This is a struct wearing an `[]interface{}` costume. Slot 0 might be an opacity,
a float, or a vector depending on which verb wrote it.

## Why it's a problem

- **No compiler help.** A wrong index, a missing append, or a type mismatch
  between the `Start` writer and the `Update` reader compiles fine and fails at
  render time (or silently, via the `_` on every `asFloat`/`asVec`).
- **Slot collisions.** Every verb that grows a new piece of state risks reusing
  an index another branch already means something else by. The
  `len(a.start) > 3` guard is the smell: the slice's shape is conditional.
- **The morph closure was 100 lines partly because of this** — packing and
  unpacking positional state inline instead of naming it.

## The thing to question

Should animation state be positional and untyped at all? Each verb knows exactly
what it needs to remember.

## Sketch of a fix

Give each verb a typed state struct captured in a closure or a small interface,
instead of a shared `[]Value`:

```go
type morphState struct {
    srcOp, dstOp float64
    offset       Vec
    srcOffset    Vec
}
```

The `Anim` holds `state any` (or a generic), each verb type-asserts once. Most of
the `asFloat(a.start[i])` noise disappears, and slot collisions become
impossible.
