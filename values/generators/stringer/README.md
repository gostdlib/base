# Stringer

An enhanced version of Go's [`stringer`](https://pkg.go.dev/golang.org/x/tools/cmd/stringer) tool that generates `String()` methods for integer enum types, plus additional methods for validation, reverse lookup, and JSON marshaling.

The standard `stringer` tool is one of the most useful tools the Go team has provided. But after years of use, there are common patterns that developers end up maintaining by hand. This tool consolidates those patterns into code generation.

## Why integer enums over string enums?

- **Size**: Most enumerators fit in a `uint8` (1 byte). A string enum is rarely less than 4 bytes.
- **Allocation**: Integers don't allocate; strings do.
- **Comparison flexibility**: Numeric types enable range checks (e.g., codes 10-20 mean success, >20 means failure) rather than equality-only comparisons.
- **Large dataset efficiency**: In the era of large datasets, AI training, and cloud compute, poor data type choices add up.

## Install

```bash
go get -tool github.com/gostdlib/base/values/generators/stringer
```

## Basic Usage

```go
//go:generate go tool github.com/gostdlib/base/values/generators/stringer -type=Fruit -linecomment

type Fruit uint8

const (
	UnknownFruit Fruit = 0 // Unknown
	Apple        Fruit = 1 // apple
	Orange       Fruit = 2 // orange
)
```

Running `go generate` produces a `String()` method so that `Apple.String()` returns `"apple"`.

## Flags

| Flag | Description |
|------|-------------|
| `-type` | **(Required)** Comma-separated list of type names (e.g., `-type=Pill,Color`) |
| `-output` | Output file name; defaults to `<type>_string.go` |
| `-trimprefix` | Prefix to strip from constant names (e.g., `-trimprefix=Fruit` makes `FruitApple` output `"Apple"`) |
| `-linecomment` | Use the line comment text as the string value instead of the constant name |
| `-tags` | Comma-separated build tags (directory input only) |
| `-valid` | Generate a `Valid() bool` method |
| `-invalid` | Comma-separated values/ranges to mark invalid (e.g., `-invalid="0,<4,>=100"`) |
| `-reverse` | Generate a `Reverse<Type>(s string, caseSensitive bool) (<Type>, bool)` function |
| `-replace` | String replacement pairs for reverse lookup (e.g., `-replace=-,_`); repeatable |
| `-marshal` | Generate `MarshalJSON()` / `UnmarshalJSON()` methods (requires `-reverse`, enables `-valid`) |
| `-marshalinsensitive` | Case-insensitive `UnmarshalJSON` (enables `-marshal` and `-reverse`) |
| `-list` | Generate a `List<Type>() iter.Seq[<Type>]` function yielding all values in order |

### Flag dependencies

- `-marshal` requires `-reverse`
- `-marshalinsensitive` automatically enables `-marshal` and `-reverse`
- `-marshal` automatically enables `-valid`
- `-replace` requires `-reverse`
- `-invalid` requires `-valid` or `-marshal`

## Features

### Validation (`-valid`, `-invalid`)

When receiving enum values from the network, a client/server version mismatch can introduce unexpected values. The `Valid()` method tells you if a value is a defined constant.

The zero value in Go defaults to 0, which typically represents an "Unknown" state. Use `-invalid=0` to treat it as invalid even though it is a defined constant. The `-invalid` flag supports values and range operators:

```
-invalid="0"          # single value
-invalid="0,<4,>=100" # combined: 0, less than 4, and >= 100
```

```go
//go:generate go tool github.com/gostdlib/base/values/generators/stringer -type=Status -linecomment -valid -invalid=0

type Status uint8

const (
	UnknownStatus Status = 0 // Unknown
	Active        Status = 1 // active
	Inactive      Status = 2 // inactive
)
```

Generated:

```go
func (i Status) Valid() bool { ... }

Active.Valid()        // true
UnknownStatus.Valid() // false (invalid=0)
Status(99).Valid()    // false
```

### Reverse lookup (`-reverse`)

Convert strings back to enum values, similar to what Protocol Buffers provide. Useful for parsing user input or data from external systems.

```go
//go:generate go tool github.com/gostdlib/base/values/generators/stringer -type=Status -linecomment -reverse
```

Generated:

```go
func ReverseStatus(s string, caseSensitive bool) (Status, bool)
```

```go
val, ok := ReverseStatus("active", true)  // case-sensitive
val, ok := ReverseStatus("Active", false) // case-insensitive
```

#### String replacement (`-replace`)

When external data uses different conventions (e.g., hyphens vs. underscores), `-replace` transforms the input string before lookup:

```bash
-replace=-,_    # replace dashes with underscores before lookup
```

Use `\,` to escape a literal comma and `\\` for a literal backslash.

### JSON marshaling (`-marshal`, `-marshalinsensitive`)

Generate `MarshalJSON()` and `UnmarshalJSON()` methods for JSON encoding/decoding of enum values as strings.

```go
//go:generate go tool github.com/gostdlib/base/values/generators/stringer -type=Color -linecomment -marshalinsensitive
```

`MarshalJSON` validates the value before marshaling and returns an error for undefined values. `UnmarshalJSON` converts the JSON string back to the enum value.

With `-marshalinsensitive`, unmarshaling accepts any case variation: `"red"`, `"Red"`, and `"RED"` all resolve to the same value.

### Listing values (`-list`)

Generate a `List<Type>() iter.Seq[<Type>]` function that yields all defined constant values in order.

```go
//go:generate go tool github.com/gostdlib/base/values/generators/stringer -type=Fruit -linecomment -list
```

Generated:

```go
func ListFruit() iter.Seq[Fruit]
```

```go
for fruit := range ListFruit() {
	fmt.Println(fruit)
}
// Output:
// apple
// orange
// banana
```

## Full example

```go
//go:generate go tool github.com/gostdlib/base/values/generators/stringer -type=Fruit -linecomment -valid -invalid=0 -reverse -marshal -list

type Fruit uint8

const (
	UnknownFruit Fruit = 0 // Unknown
	Apple        Fruit = 1 // apple
	Orange       Fruit = 2 // orange
	Banana       Fruit = 3 // banana
)
```

This generates:

- `func (i Fruit) String() string` - returns the line comment text
- `func (i Fruit) Valid() bool` - returns false for 0 and undefined values
- `func ReverseFruit(s string, caseSensitive bool) (Fruit, bool)` - string-to-value lookup
- `func (i Fruit) MarshalJSON() ([]byte, error)` - JSON encoding
- `func (i *Fruit) UnmarshalJSON(data []byte) error` - JSON decoding
- `func ListFruit() iter.Seq[Fruit]` - iterate over all values in order

## Claude Code skill

This repo includes a [SKILL.md](SKILL.md) file that teaches [Claude Code](https://claude.ai/code) how to generate `go:generate` directives for this tool. When installed, Claude will automatically suggest the correct directive and flags when you define integer enum types.

To install, copy the file into your personal or project skills directory:

```bash
# Personal (applies to all your projects)
mkdir -p ~/.claude/skills/stringer
cp SKILL.md ~/.claude/skills/stringer/SKILL.md

# Or project-level (applies to one project)
mkdir -p .claude/skills/stringer
cp SKILL.md .claude/skills/stringer/SKILL.md
```

You can also invoke it manually in Claude Code with `/stringer`.

## License

See [LICENSE](LICENSE) for details. This tool is a fork of the original Go `stringer` tool.
