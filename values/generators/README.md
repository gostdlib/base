# generators

Code-generation tools for the `gostdlib/base` value types. Each is a small,
self-contained `go run`/`go tool` binary driven by a `//go:generate` directive,
and each ships a [Claude Code](https://claude.ai/code) skill (`SKILL.md`) that
teaches the assistant to write the right directive for you.

## Tools

| Tool | What it generates | Directive |
|------|-------------------|-----------|
| [`stringer`](stringer) | `String()` plus `Valid()`, reverse lookup, JSON marshaling, and `List()` for integer enum types — an enhanced fork of the Go team's `stringer`. | `//go:generate go tool github.com/gostdlib/base/values/generators/stringer -type=Fruit -linecomment` |
| [`union`](union) | A type-safe union (sum) type: a value holding exactly one of a fixed set of member types, with a discriminator enum and typed setters/accessors. | `//go:generate go tool github.com/gostdlib/base/values/generators/union -n Candy -t Twix,ThreeMuskateers` |
| [`immutable`](immutable) | An immutable (read-only) twin of an existing struct: unexported fields, getters, copy-on-write setters, and conversions to/from the mutable struct. | `//go:generate go tool github.com/gostdlib/base/values/generators/immutable -type User` |
| [`sets`](sets) | A `var <Type>Set = immutable.NewSet(...)` holding a type's constants, or an explicit `-v` list of values, for membership checks against a fixed set. | `//go:generate go tool github.com/gostdlib/base/values/generators/sets -t Color` |
| [`tuple`](tuple) | **Experimental.** A small named tuple value type with a constructor, positional or named accessors, `Len()` and `String()` — mainly to flatten `map[K1]map[K2]V` into one map keyed by a struct. | `//go:generate go tool github.com/gostdlib/base/values/generators/tuple -p lastFirst last:string, first:string` |

## Usage

Each tool is registered in this module's `go.mod` `tool` block, so it runs via
`go tool <import-path>`. To use one from another module, add it as a tool
dependency first:

```bash
go get -tool github.com/gostdlib/base/values/generators/stringer
go get -tool github.com/gostdlib/base/values/generators/union
go get -tool github.com/gostdlib/base/values/generators/immutable
go get -tool github.com/gostdlib/base/values/generators/sets
go get -tool github.com/gostdlib/base/values/generators/tuple
```

Then add the relevant `//go:generate` directive in your package and run:

```bash
go generate ./...
```

See each tool's `README.md` for its full flag set and generated output, and its
`SKILL.md` for the Claude Code skill.

## Why these exist

- **`stringer`** — turns integer enums (cheaper and more flexible than string
  enums) into ergonomic types with string conversion, validation, reverse
  lookup, and JSON support, removing the hand-maintained boilerplate that
  usually grows around an enum.
- **`union`** — gives Go a real tagged-union / sum type with a discriminator,
  so a value can safely hold "one of" several types and you can `switch` on
  which one it is.
- **`immutable`** — produces read-only value objects whose `map`/slice fields
  become [`immutable.Map`/`immutable.Slice`](../immutable), so shared or cached
  state can't be mutated out from under you; mutation happens only through
  copy-on-write setters that return new values.
- **`sets`** — replaces hand-maintained `map[T]struct{}` lookup tables and long
  `switch`/`||` chains of allowed values with an
  [`immutable.Set`](../immutable) built from the type's own constants, so the
  set can't drift from the constants it describes or be mutated at runtime.
- **`tuple`** — experimental. Builds the small struct you would otherwise
  hand-write to key a map on more than one value, replacing
  `map[K1]map[K2]V` with a single map keyed by a named tuple. See its
  [README](tuple) for why its scope is deliberately narrow.
