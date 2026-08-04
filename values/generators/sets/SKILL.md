---
name: sets
description: >-
  Generate go:generate directives for github.com/gostdlib/base/values/generators/sets.
  TRIGGER when: user wants an immutable set holding a type's constants, asks for a
  lookup/membership set of valid enum values, or asks to add set generation.
---

# Sets code generation

You help users generate `//go:generate` directives for the
`github.com/gostdlib/base/values/generators/sets` tool (the `sets` command), which creates a package-level
**value** — `var <Type>Set = immutable.NewSet(...)` — holding the constants of a type,
or an explicit list of values. The generated var is an
`immutable.Set` from `github.com/gostdlib/base/values/immutable`, so it is read-only
after construction and safe for concurrent reads.

Like `stringer`, this tool builds off an existing type. It does **not** declare a new
type, a constructor, or any wrapper methods — only the var.

## When to act

- User has a type with constants (commonly an enum) and wants the set of its valid
  values for membership checks.
- User is writing repeated `switch`/`==` chains or a hand-built `map[T]struct{}` /
  `map[T]bool` of allowed values, and a generated set would replace it.
- User wants a fixed set of literal values as an immutable set.
- User asks to add a `go:generate` directive for the `sets` tool.

## How to write the directive

```go
//go:generate go tool github.com/gostdlib/base/values/generators/sets -t TypeName
```

Place it directly above the type declaration. The type must already be declared in the
same package.

## Available flags

| Flag | What it does |
|------|-------------|
| `-t=Type` | **(Required)** Comma-separated type names. Each gets its own `<Type>Set` var in one output file. |
| `-v=val,val` | Use these literal values instead of the package's constants. Requires exactly **one** `-t` type. Each value is validated against the type's underlying type. |
| `-output=file.go` | Override output filename (default: `<type>_set.go`, lower-cased from the first type listed) |

There is no `-type` flag; the flag is `-t` only.

## Rules

1. Every `-t` name must be a valid Go identifier, must not repeat, and must be a type
   declared in the same package as the directive.
2. The type's **underlying** type must be `string`, `int`/`int8`/`int16`/`int32`/`int64`,
   `uint`/`uint8`/`uint16`/`uint32`/`uint64`, or `float32`/`float64`. `byte` and `rune`
   qualify (they resolve to `uint8`/`int32`). A `bool`, struct, slice, map, or pointer
   based type is rejected.
3. Without `-v`, the type must have at least one constant **declared with that type**.
   An untyped constant (`const Foo = "bar"`) is not collected — write `const Foo T = "bar"`.
4. `-v` requires exactly one `-t` type. Values are range- and syntax-checked against the
   underlying type, so `-v 300` on an `int8` type or `-v -1` on a `uint` type is an error.
   Strings are quoted and escaped automatically; do not add quotes yourself.
5. The generated var is always named `<Type>Set`, so generate **at most one set per type
   per package** — two runs for the same type into different `-output` files collide.
6. **Every** constant of the type is collected, including an enum's `Unknown<Type> = 0`
   zero value. If the user wants a set of only the *valid* values, either exclude it at
   the call site (`v != UnknownLevel && LevelSet.Contains(v)`) or list the values
   explicitly with `-v`.
7. An unexported type yields an unexported var (`color` → `colorSet`), and unexported
   constants are collected just like exported ones.
8. The set is immutable. `Union` and `Intersection` return new sets; nothing mutates the
   generated var.
9. The package does **not** need to compile for generation to succeed. Writing the
   directive and the code that uses the set at the same time is fine — the tool tolerates
   the "undefined: `<Type>Set`" error and generates anyway.
10. After writing the directive, remind the user to run `go generate` and ensure the tool
   is available:
   ```bash
   go get -tool github.com/gostdlib/base/values/generators/sets
   ```

## Pairs well with stringer

For an **integer** enum type, `stringer` gives the values names and `sets` gives you the
set of valid values. Both directives can sit above the same type:

```go
//go:generate go tool github.com/gostdlib/base/values/generators/stringer -type=Level
//go:generate go tool github.com/gostdlib/base/values/generators/sets -t Level

type Level int

const (
	UnknownLevel Level = iota // Unknown
	Low                       // Low
	High                      // High
)
```

`stringer` only handles integer types; `sets` also handles string- and float-based types,
so a string enum gets a `sets` directive alone.

## Typical directive

```go
//go:generate go tool github.com/gostdlib/base/values/generators/sets -t Color

type Color string

const (
	Blue Color = "blue"
	Red  Color = "red"
)
```

## Generated output

The directive above produces `color_set.go` containing:

```go
// ColorSet is an immutable set holding the constant values of type Color.
var ColorSet = immutable.NewSet([]Color{Blue, Red})
```

With `-v` instead (`sets -t Color -v teal,mauve`):

```go
// ColorSet is an immutable set of Color values.
var ColorSet = immutable.NewSet([]Color{"teal", "mauve"})
```

The var carries `immutable.Set`'s methods: `Len`, `Contains`, `All`, `Members`, `String`,
`Union`, and `Intersection`. `All` and `Members` yield members in random order.

## Usage example

```go
func valid(c Color) bool {
	return ColorSet.Contains(c)
}

for c := range ColorSet.All() {
	fmt.Println(c)
}
```
