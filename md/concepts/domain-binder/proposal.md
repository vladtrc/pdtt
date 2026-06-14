# Domain Binder Proposal

Status: proposal. Refines `indexed-group` by making its declaration binder and the score's
`[*]` index **one** construct: `domain as name`. Reuses two things the language already has
— the `as` binder (`each record as name`) and the half-open range (`0..n`) — instead of
inventing a header-only `[i: count]` form and implicit `i j k l`.

## TL;DR

One binding rule, everywhere a name is bound over a domain:

```
DOMAIN as NAME
```

- A **domain** is a collection of ints — a range (`0..val.len`) or a list's index domain
  (`val.indices`). Live, like any value.
- **`NAME`** is bound to each element, scoped to its bracket / its tween row.
- Every `[*]` introduces a new **independent** unnamed binder. Multiple occurrences expand
  as nested loops, never as an implicit positional zip.
- `[* as i]` names that binder so the same element can be addressed elsewhere in the row;
  the name is in scope from that occurrence through the rest of the tween row.

That is the whole feature. Declaration and score now read the same way.

## Example (same scene as `indexed-group`)

```pdtt
val:       [-3, -1, 2]
color:     [color.red, color.green, color.blue]
factor_at: [[-3.85, 3.25], [-1.70, 3.25], [0.45, 3.25]]

roots[val.indices as i]:            # one rule: bind i over a domain
  dot mark:
    at: ax.point(val[i], 0)
    radius: 0.095
    color: color[i]

  arrow hit:
    from: ax.point(val[i], -1.45)
    to: mark.at                     # same instance — no index
    color: color[i]

  typst(fmt("(x {:+.2f})", 0 - val[i])) factor:
    at: factor_at[i]
    color: color[i]
```

Score uses the **same** binder; the index is named only where it is actually read:

```pdtt
| roots[*].mark.opacity -> 0                               # no index needed
| roots[* as i].mark{opacity: 0, scale: 0.2} -> roots[i].mark
| roots[* as i].factor{opacity: 0} -> roots[i].factor      # index named for the RHS
| val[* as i] -> [-2, 1, 3][i]                            # RHS reads the named index
```

## Multiple binders are nested loops

Every syntactic occurrence of `[*]` or `[* as name]` introduces one independent domain.
Binders expand left to right as nested loops; equal source collections do not make them the
same binder.

```pdtt
| a[*] -> b[*]
```

Conceptually lowers to:

```text
for each element of a:
  for each element of b:
    emit one tween
```

The row therefore emits `a.len * b.len` tweens. This is a Cartesian product, not a zip.
Use one named binder when both sides must refer to the same position:

```pdtt
| a[* as i] -> b[i]
```

Nested binders are valid when the Cartesian product is intended and the expanded tweens
write distinct target paths:

```pdtt
| grid[* as row][* as col].color -> palette[row][col]
```

Expansion is rejected when two emitted tweens write the same target path during the same
score block. For example:

```pdtt
| roots[*].mark{opacity: 0} -> roots[*].mark
```

With `n` roots this emits `n * n` tweens, and each left-hand `roots[k].mark` is written `n`
times. That is a compile error for conflicting writes. The intended entry tween is:

```pdtt
| roots[* as i].mark{opacity: 0} -> roots[i].mark
```

An unbound index is also a compile error:

```pdtt
| a[*] -> b[i]   # `i` is not bound in this row
```

## Why this is better than `indexed-group`'s `[i: count]`

`indexed-group` is right that there should be **one binder, always the index, scoped to its
bracket**. But it only applies that to the declaration. The score still binds with `[*]`'s
implicit `it` / `i j k l` — a *second*, magic mechanism. This proposal finishes the job:

| | `indexed-group` | **domain-binder** |
|---|---|---|
| declaration binder | `[i: count]` | `[domain as i]` |
| score index binder | implicit `i j k l` | `[* as i]` (named) / `[*]` (unnamed) |
| binder keyword | new colon form | **reuses `as`** (`each … as name`) |
| header domain | int coerced to `0..count` | a real collection (`0..n`, `.indices`) |
| constructs to learn | header form **+** `[*]` magic | **one** rule for both |

Concretely:

1. **One rule, declaration and score.** `domain as name` binds an index in the header and in
   a tween row identically. No `[i: count]` here and implicit `i j k l` there.
2. **No new keyword.** The language already binds with `as` (`each record as name dur`).
   Binding an index is the same act, so it is the same word — not a fresh `via`/colon form.
3. **No magic-number coercion.** `[i: count]` quietly reads an int as the range `0..count`.
   Here the bracket takes an actual collection; `val.indices` / `0..val.len` say "index
   domain" in the type, not by a header-only rule.
4. **`[*]` keeps its short form.** When the binder is used only to expand one collection,
   write plain `[*]`. Name it with `as i` when another expression must address that same
   position.

## Two spellings of a domain — keep both

```pdtt
roots[val.indices as i]:   # index domain of a list — drift-free, preferred
roots[0..n as i]:          # an explicit range — when there is no backing list
```

`val.indices` is the index domain of `val` (`0 .. val.len`); it cannot drift from the data.
`0..n` is the existing half-open range, for a purely synthetic count. Both are live: animate
`val` and the family re-sizes. (`val.len` stays a plain `int`; the domain is `val.indices` /
`0..val.len`, never a bare number — so a number is never silently a collection.)

## Why not `enumerate` / `via` (the rejected alternative)

A sketch proposed `roots[val.len.enumerate via i]`. Rejected on two counts:

- **`enumerate`** conventionally yields `(index, value)` pairs (Python, Rust). Using it for
  indices-only misleads, and it invents an `int.enumerate` method where a plain range already
  exists. Use `val.indices` / `0..val.len`.
- **`via`** is a second binder keyword for a job `as` already does — it adds the very
  non-uniformity this concept removes.

## Live by default, one binder `:` (`snapshot` freezes)

Bundled here because the binder is pointless if derived names can't track the data. Every
binding uses the same `:` at every level — raw value, derived value, or record field — and
every one is **live**: it re-evaluates each frame. The whole file reads as one uniform,
YAML-shaped map; `:` either takes an inline value or opens a block. Freezing is the explicit
opt-in via the **`snapshot`** operator, which samples its argument *at that point in the
timeline* and returns a constant:

```pdtt
c2:   0 - sum(val)        # live — tracks val through the animation
home: snapshot frame      # frozen — the camera as it is here, not "always now"
at:   snapshot grid.at    # the same operator inside a field
```

This drops the language's last asymmetry — fields were already `:` and live, only top-level
data used a separate, frozen `=`. Now there is one rule: every `:` binding is live; `snapshot`
is the only thing that introduces a constant, and it is an ordinary expression. It supersedes
`indexed-group`'s "live by default; `=` to freeze": there is no frozen capture form at all,
just `snapshot`.

The payoff for this example: coefficients of the expanded polynomial can be **named** without
desyncing from the graph — `c2: 0 - sum(val)` stays correct as `val` animates, where a frozen
value would have pinned it to its declaration-time value.

## Lowering

Unchanged from `indexed-group`. `NAME[domain as i]:` lowers to one plural record `NAME`
keyed by the domain; each instance shares the bound integer `i`; list reads (`val[i]`) are
ordinary indexing; same-instance member refs (`hit.to: mark.at`) lower to field access.
Each `[*]` and `[* as i]` creates an independent broadcast domain; `[* as i]` names the
index that domain carries. Multiple domains lower to nested loops in lexical order.

```
parse → merge records → run `from` commands → evaluate fields
      → expand domain binders / [*] → resolve `=` captures → emit score → render
```

## Guardrails

Same short list as `indexed-group` (length drift, member-name clashes, member cycles,
snapshot-vs-track), plus two score checks:

- reading a binder name outside its scope is a compile error;
- expanded tweens that write the same target path in one score block are a compile error.

There is no header-only "int means `0..count`" coercion, because the bracket always takes a
real domain.
