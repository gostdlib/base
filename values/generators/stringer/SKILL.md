---
name: stringer
description: >-
  Generate go:generate directives for github.com/gostdlib/base/values/generators/stringer.
  TRIGGER when: user defines an integer enum type in Go, asks to add stringer generation,
  or asks about enum String/Valid/Reverse/Marshal/List methods.
---

# Stringer code generation

You help users generate `//go:generate` directives for the `github.com/gostdlib/base/values/generators/stringer` tool,
which creates methods for Go integer enum types.

## When to act

- User creates or modifies an integer enum type (e.g., `type Foo uint8` with `const` block).
- User asks for String(), Valid(), Reverse, JSON marshal, or List methods on an enum.
- User asks to add a `go:generate` directive for stringer.

## How to write the directive

```go
//go:generate go tool github.com/gostdlib/base/values/generators/stringer -type=TypeName [flags...]
```

Place it directly above the type declaration.

## Available flags

| Flag | What it does |
|------|-------------|
| `-type=Name` | **(Required)** Type name(s), comma-separated |
| `-linecomment` | Use line comment text as the string value instead of the constant name |
| `-valid` | Generate `Valid() bool` method |
| `-invalid="..."` | Values/ranges marked invalid (e.g., `"0"`, `"0,<4,>=100"`). Requires `-valid` or `-marshal` |
| `-reverse` | Generate `Reverse<Type>(s string, caseSensitive bool) (<Type>, bool)` |
| `-replace=old,new` | Transform input before reverse lookup; repeatable. Requires `-reverse` |
| `-marshal` | Generate `MarshalJSON`/`UnmarshalJSON`. Requires `-reverse`, enables `-valid` |
| `-marshalinsensitive` | Case-insensitive unmarshal. Enables `-marshal` and `-reverse` automatically |
| `-list` | Generate `List<Type>() iter.Seq[<Type>]` iterator |
| `-output=file.go` | Override output filename (default: `<type>_string.go`) |
| `-trimprefix=Prefix` | Strip prefix from constant names in output |
| `-tags=...` | Build tags (directory input only) |

## Flag dependencies

- `-marshal` requires `-reverse`
- `-marshalinsensitive` automatically enables `-marshal` and `-reverse`
- `-marshal` automatically enables `-valid`
- `-replace` requires `-reverse`
- `-invalid` requires `-valid` or `-marshal`

## Rules

1. The zero value of an enum MUST be `Unknown<TypeName>` (e.g., `UnknownFruit = 0`).
2. Always use `-linecomment` when the constants have line comments, omit it when they don't.
3. If the type will be used in JSON, use `-marshalinsensitive` (which enables `-marshal` and `-reverse`).
4. If the zero value represents "unset", add `-invalid=0` along with `-valid`.
5. Use `-list` when the user needs to iterate over all values.
6. After writing the directive, remind the user to run `go generate` and ensure the tool is installed:
   ```bash
   go get -tool github.com/gostdlib/base/values/generators/stringer
   ```

## Typical directive

For a standard enum with JSON support and validation:

```go
//go:generate go tool github.com/gostdlib/base/values/generators/stringer -type=Fruit -linecomment -valid -invalid=0 -reverse -marshalinsensitive -list

type Fruit uint8

const (
	UnknownFruit Fruit = 0 // Unknown
	Apple        Fruit = 1 // apple
	Orange       Fruit = 2 // orange
	Banana       Fruit = 3 // banana
)
```

## Generated output

The directive above produces:

- `func (i Fruit) String() string` — returns the line comment text
- `func (i Fruit) Valid() bool` — false for 0 and undefined values
- `func ReverseFruit(s string, caseSensitive bool) (Fruit, bool)` — string-to-value lookup (case-insensitive)
- `func (i Fruit) MarshalJSON() ([]byte, error)` — JSON encoding with validation
- `func (i *Fruit) UnmarshalJSON(data []byte) error` — case-insensitive JSON decoding
- `func ListFruit() iter.Seq[Fruit]` — iterator over all defined values
