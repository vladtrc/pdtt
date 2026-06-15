# 03 — Magic-string types (`"__family_proxy__"`)

## The assumption

Structural facts about an entity are encoded as its `Type` string and compared
literally:

```go
// internal/pdtt/compile.go
if e.Type == "__family_proxy__" && len(trail) >= 1 {
```

`"__family_proxy__"` appears as a sentinel in both `compile.go` and `model.go`.
This is a type system implemented in string comparisons — the same shape as the
`closed` flag the morph rewrite removed: a structural concept (this entity is a
proxy, not a real shape) living as free-form data.

## Why it's a problem

- **Typos are silent.** `"__family_proxy__"` vs `"__family-proxy__"` compiles
  and just never matches. No exhaustiveness, no go-to-definition.
- **Mixed namespaces.** Real types (`path`, `dot`, `typst`) and pseudo-types
  (`__family_proxy__`) share one string field, so every `switch e.Type` has to
  silently ignore the sentinels, and adding a real type risks colliding with a
  pseudo one.
- **Invariants are unwritten.** Nothing says a proxy never reaches the renderer;
  it's enforced by scattered string checks that are easy to forget in new code.

## The thing to question

Should "is this a layout proxy?" be a value of the same field that says "is this
a circle?" They are different axes.

## Sketch of a fix

- Promote the sentinels to a real boolean/enum on the entity (`IsProxy bool`, or
  a `Kind` enum distinct from the drawable `Type`), so the compiler tracks them.
- Or model proxies as their own type that never enters the entity list the
  renderer walks.

Either way the goal is: a misspelled or forgotten check fails to compile instead
of failing to match.
