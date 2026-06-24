# union

`union` generates type-safe union (sum) types in Go: a value that holds exactly
one of a fixed set of member types, with a discriminator enum, typed setters,
and typed accessors.

## Install

The tool is registered in this module's `go.mod` `tool` block, so it runs via:

```bash
go tool github.com/gostdlib/base/values/generators/union -n Name -t Type1,Type2
```

In another module, add it as a tool dependency first:

```bash
go get -tool github.com/gostdlib/base/values/generators/union
```

## Usage

Add a `//go:generate` directive in the package that declares the member types,
then run `go generate ./...`:

```go
//go:generate go tool github.com/gostdlib/base/values/generators/union -n Candy -t Twix,ThreeMuskateers

type Twix struct{ String string }

type ThreeMuskateers struct{ String string }
```

### Flags

| Flag | Description |
|------|-------------|
| `-n` | **(required)** Name of the union type to generate. |
| `-t` | **(required)** Comma-separated member type names. |
| `-output` | Output file name. Defaults to `<name>_union.go` (lower-cased). |
| `-noAny` | Store each member in its own typed field instead of an `any` field. |

### `-noAny`

By default the union holds its value in an `any` field. With `-noAny`, the
generated struct has one typed field per member instead:

```go
type Candy struct {
	t                CandyType
	vTwix            Twix
	vThreeMuskateers ThreeMuskateers
}
```

This avoids boxing non-pointer members into an `any` (no heap allocation) and
lets accessors return the field directly with no type assertion. The public API
is unchanged — `-noAny` only affects the internal representation.

If `-n` names an unexported type (lower-case first letter, e.g. `candy`), the
discriminator (`candyType`) follows the same casing. The methods are exported
either way.

## Generated code

The directive above produces `candy_union.go`:

```go
type CandyType uint8

const (
	CandyTypeNotSet          CandyType = 0
	CandyTypeTwix            CandyType = 1
	CandyTypeThreeMuskateers CandyType = 2
)

func (t CandyType) String() string { /* ... */ }

type Candy struct { /* ... */ }

func (c *Candy) Type() CandyType
func (c *Candy) SetTwix(v Twix) Twix
func (c *Candy) SetThreeMuskateers(v ThreeMuskateers) ThreeMuskateers
func (c *Candy) Twix() Twix
func (c *Candy) ThreeMuskateers() ThreeMuskateers
```

A union starts at its zero value (no member held) and is populated with a `Set`
method, which replaces any current member and returns the value it set. Each
accessor returns the held value, or the zero value if a different member is
held. The discriminator's `String()` method uses the same name-string +
index-array form as [stringer](https://github.com/johnsiilver/stringer); do
**not** point stringer at `<Name>Type`, as it would generate a colliding
`String()`.

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
