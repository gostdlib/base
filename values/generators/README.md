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

## Usage

Each tool is registered in this module's `go.mod` `tool` block, so it runs via
`go tool <import-path>`. To use one from another module, add it as a tool
dependency first:

```bash
go get -tool github.com/gostdlib/base/values/generators/stringer
go get -tool github.com/gostdlib/base/values/generators/union
go get -tool github.com/gostdlib/base/values/generators/immutable
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
