---
name: tuple
description: >-
  Generate go:generate directives for github.com/gostdlib/base/values/generators/tuple.
  TRIGGER when: user has a nested map (map[K1]map[K2]V) or a composite map key built by
  concatenating/formatting strings, wants a small named multi-field map key, or asks to
  add tuple generation.
---

# Tuple code generation

You help users generate `//go:generate` directives for the
`github.com/gostdlib/base/values/generators/tuple` tool, which creates a small named
**value struct** — a tuple — with a constructor, accessors, `Len()` and `String()`.

Its one real use is a **map key made of more than one part**: it turns
`map[K1]map[K2]V` and `m[a+"|"+b]` into a flat `map[keyTuple]V`. That is one hash lookup
instead of two, no inner-map allocation or nil-map panic path, no delimiter collisions,
and key parts that keep their types instead of being smashed into a string.

> **Status:** the generator is marked **EXPERIMENTAL** by its own README and may be
> removed. Recommend it freely inside a repo that already depends on `gostdlib/base` (or
> `go.goms.io/aks/base`), and prefer keeping the generated key type unexported so a future
> removal is a local fix. If the module does not already depend on base, say so rather than
> adding a new dependency just for a key type.

## When to act

- User declares or maintains a `map[K1]map[K2]V` that is only ever accessed as a full
  two-level `m[k1][k2]` get/set — the nesting is a compound lookup, not a grouping.
- User builds a composite key by concatenation or formatting: `m[a+":"+b]`,
  `m[fmt.Sprintf("%s/%d", ns, id)]`, `m[strings.Join(parts, "|")]`. Rank this highest: if a
  key part can contain the delimiter, two distinct pairs silently collide — a real bug, not
  a cleanup.
- User has an anonymous struct key (`map[struct{ a, b string }]V`) they want named.
- User keeps two or more maps in lockstep under the same pair of keys — one tuple type
  serves both and documents the relationship.
- User asks to add a `go:generate` directive for the tuple tool.

For finding these across an existing codebase rather than writing one directive, use the
`tuple-review` skill.

## When NOT to suggest it

The nesting is real whenever the inner map is a value in its own right — a flat map has no
cheap way to answer "everything under `k1`". Do not propose a tuple key if the code does
any of these:

- Ranges a group: `for k2, v := range m[k1]`.
- Extracts or hands off a group: `inner, ok := m[k1]` then returns/stores/passes `inner`.
- Deletes a whole group: `delete(m, k1)`.
- Asks a group's size or existence: `len(m[k1])`, `if _, ok := m[k1]; ok`.
- Ranges the outer map for its key set to list or dispatch on groups.
- Marshals the map. `encoding/json` cannot marshal a struct-keyed map unless the key
  implements `encoding.TextMarshaler`, and the generated tuple does **not**. Hard blocker.
- Guards, swaps, or shards inner maps individually — there the nesting *is* the locking.

Also skip it for a single-part key, and for a plain struct key with no methods that nobody
needs accessors on.

## How to write the directive

```go
//go:generate go tool github.com/gostdlib/base/values/generators/tuple -p lastFirst last:string, first:string
```

Place it above the type or map field that uses the key. Fields are a comma-separated list
of `name:type` (or bare `type`) after the tuple name.

## Available flags

| Flag | What it does |
|------|-------------|
| `-p` | Write a complete Go file (package clause and `import "fmt"`) into the current directory as `<name>.go`, lower-cased. Without it, only the type and its methods print to stdout for pasting into an existing file. |

Everything else is positional: the name first, then the fields.

## Rules

1. **The name is used verbatim.** `lastFirst` generates an unexported type, `LastFirst` an
   exported one; accessors are exported either way. Export only if the key type appears in
   an exported signature.
2. **Prefer `name:type` over bare `type`.** `V0()`/`V1()` tells a reader nothing at the call
   site; `Region()`/`Zone()` does. Bare positional fields are acceptable only for an obvious
   same-type pair. The two forms can be mixed.
3. **Name the tuple for what the key *is*** (`regionZone`, `userRepo`) — not for the value it
   looks up, and never with "tuple"/"key"/the map's name in it.
4. **Every field type must be comparable** for the tuple to work as a map key. The generator
   does **not** check this: a `[]string`, `map`, or func field generates a file that compiles
   on its own and then fails at the map with `invalid map key type`. Normalize such a part to
   a comparable form (string, array) first.
5. **Keep fields small.** The tuple is copied on every lookup and hashed field by field, so
   use scalars and strings, not big structs.
6. **With `-p`, field types must be builtin or declared in the same package.** The generated
   file imports only `fmt`, so a qualified type like `time.Duration` produces
   `undefined: time`. Either use a same-package type, or run without `-p` and paste the
   output into a file that already has the import.
7. Field names must be valid Go identifiers and may not repeat; at least one field is
   required.
8. **The generated file is generated.** Never hand-edit `<name>.go` — change the
   `//go:generate` line and regenerate.
9. **`len()` changes meaning** when a nested map is flattened: nested `len(m)` counts
   *groups*, flat `len(m)` counts *entries*. Check every `len()` on a converted map.
10. After writing the directive, remind the user to run `go generate` and ensure the tool is
   available:
   ```bash
   go get -tool github.com/gostdlib/base/values/generators/tuple
   ```
   Since a fixed-size key rarely changes, running the command once and checking the file in
   is also fine — the tool's own README suggests it.

## Typical directive

```go
//go:generate go tool github.com/gostdlib/base/values/generators/tuple -p regionZone region:string, zone:string

type registry struct {
	nodes map[regionZone]*node
}
```

## Generated output

The directive above produces `regionzone.go` containing:

```go
// regionZone is a tuple holding 2 values: region string, zone string.
type regionZone struct {
	region string
	zone   string
}

func newRegionZone(region string, zone string) regionZone
func (t regionZone) Region() string
func (t regionZone) Zone() string
func (t regionZone) Len() int
func (t regionZone) String() string // "(us-east, 1a)"
```

The constructor follows the type's visibility: an unexported tuple yields `newRegionZone` and an exported
`RegionZone` yields `NewRegionZone`.

## Usage example

Flattening the nested map the tuple replaces:

```go
// m[r][z]                     → m[newRegionZone(r, z)]
// m[r][z] = v (+ inner init)  → m[newRegionZone(r, z)] = v
// delete(m[r], z)             → delete(m, newRegionZone(r, z))
// for r, inner := range m { for z, v := range inner { … } }
//                             → for k, v := range m { k.Region(); k.Zone() }
```

Convert one map at a time, fix **every** access site, then `go build ./...` and
`go vet ./...`.

## Related

- `tuple-review` — audit a codebase for nested maps and composite keys that should become
  tuple keys.
- If the flattened map is handed out read-only, wrap it in
  [`immutable.Map[keyTuple, V]`](../../immutable).
