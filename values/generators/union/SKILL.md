---
name: union
description: >-
  Generate go:generate directives for github.com/gostdlib/base/values/generators/union.
  TRIGGER when: user wants a type-safe union/sum/variant type in Go, asks to hold
  "one of" several Go types in a single value, or asks to add union generation.
---

# Union code generation

You help users generate `//go:generate` directives for the
`github.com/gostdlib/base/values/generators/union` tool, which creates a type-safe
union (sum) type: a value that holds exactly one of a fixed set of member types,
with a discriminator enum, typed setters, and typed accessors.

## When to act

- User wants a single value to hold "one of" several Go types (a tagged union,
  sum type, or variant).
- User has a set of related types (e.g. message kinds, event variants, result
  cases) and wants a type-safe wrapper that tracks which one is held.
- User asks to add a `go:generate` directive for the union tool.

## How to write the directive

```go
//go:generate go tool github.com/gostdlib/base/values/generators/union -n Name -t Type1,Type2,...
```

Place it anywhere in the package (commonly above the member type declarations).
The member types must already be declared in the same package.

## Available flags

| Flag | What it does |
|------|-------------|
| `-n=Name` | **(Required)** Name of the union type to generate |
| `-t=Type1,Type2` | **(Required)** Comma-separated member type names |
| `-output=file.go` | Override output filename (default: `<name>_union.go`, lower-cased) |
| `-noAny` | Store each member in its own typed field instead of an `any` field (no boxing allocation for non-pointer members, no type assertion on access). The public API is unchanged. Suggest it when members are non-pointer value types and allocation or access cost matters. |

## Rules

1. `-n` and every `-t` member must be valid Go identifiers.
2. No member may share the union name, and members may not repeat.
3. The member types must be declared in the same package as the directive.
4. If `-n` is unexported (lower-case first letter, e.g. `candy`), the
   discriminator (`candyType`) follows the same casing. The methods are exported
   either way.
5. Do **not** also point `stringer` at the generated `<Name>Type` — the tool
   already generates its `String()` method, so a second one would collide.
6. The zero value of the union holds no member; its discriminator is
   `<Name>TypeNotSet`. Populate a union by declaring its zero value and calling a
   `Set<Type>` method.
7. After writing the directive, remind the user to run `go generate` and ensure
   the tool is available:
   ```bash
   go get -tool github.com/gostdlib/base/values/generators/union
   ```

## Typical directive

```go
//go:generate go tool github.com/gostdlib/base/values/generators/union -n Candy -t Twix,ThreeMuskateers

type Twix struct{ String string }

type ThreeMuskateers struct{ String string }
```

## Generated output

The directive above produces `candy_union.go` containing:

- `type CandyType uint8` — the discriminator, with constants
  `CandyTypeNotSet` (= 0), `CandyTypeTwix`, `CandyTypeThreeMuskateers`.
- `func (t CandyType) String() string` — returns the constant name, or
  `CandyType(N)` for an unknown value.
- `type Candy struct{ ... }` — the union value; its zero value holds no member.
- `func (c *Candy) Type() CandyType` — reports which member is held.
- `func (c *Candy) SetTwix(v Twix) Twix` and
  `func (c *Candy) SetThreeMuskateers(v ThreeMuskateers) ThreeMuskateers` —
  replace any current member with `v` and return it.
- `func (c *Candy) Twix() Twix` and `func (c *Candy) ThreeMuskateers() ThreeMuskateers`
  — return the held value, or the zero value if a different member is held.

## Usage example

```go
var u Candy
u.SetTwix(Twix{"hello"})
switch u.Type() {
case CandyTypeTwix:
	fmt.Println(u.Twix().String) // "hello"
case CandyTypeThreeMuskateers:
	fmt.Println(u.ThreeMuskateers().String)
}
```
