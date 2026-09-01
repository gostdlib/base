---
name: immutable
description: >-
  Generate go:generate directives for github.com/gostdlib/base/values/generators/immutable.
  TRIGGER when: user wants an immutable (read-only) version of a Go struct, asks to
  prevent a struct from being mutated after construction, wants copy-on-write
  setters, or asks to add immutable generation.
---

# Immutable code generation

You help users generate `//go:generate` directives for the
`github.com/gostdlib/base/values/generators/immutable` tool, which creates an immutable
(read-only) twin of an existing Go struct: a value type whose fields cannot be
mutated after construction, with getters for every exported field, copy-on-write
setters that return a new value, and conversions to and from the original
mutable struct.

## When to act

- User wants a struct that callers cannot mutate after it is built (config,
  shared/cached state, a value object passed across goroutines).
- User asks for copy-on-write / "with"-style setters that return a new value
  instead of mutating in place.
- User has a struct with `map` or slice fields and wants the immutable `Map` /
  `Slice` types instead of handing out a mutable reference.
- User asks to add a `go:generate` directive for the immutable tool.

## How to write the directive

```go
//go:generate go tool github.com/gostdlib/base/values/generators/immutable -type StructName
```

Place it in the package that declares the struct (commonly above the struct
declaration). The target struct must already be declared in the same package.
The tool scans every non-generated `*.go` file in its own directory, so run it
from that directory.

## Available flags

| Flag | What it does |
|------|-------------|
| `-type=StructName` | **(Required)** Name of the struct to make immutable. Generates `Im<StructName>` into `<StructName>_immutable.go`. |
| `-copy` | Makes `Immutable()` copy the maps and slices it wraps instead of sharing them. Off by default. |

The tool takes a single struct per invocation. Add one directive per struct you
want an immutable version of.

`Immutable()` shares its wrapped maps and slices by default, so calling it hands
ownership over and the original struct must not be used afterwards. Add `-copy`
when the caller needs to keep using the original. `Mutable()` copies in both
modes.

Copying is **one level deep** in both directions: the map or slice itself is
copied, but a pointer, interface, or nested map/slice element is still shared
unless its type implements `immutable.Copier`. Two shapes the generator does not
handle: a named map/slice type (`type Tags map[string]string`, field `Tags Tags`)
is not wrapped, and an aliased import of the immutable package is not carried
into the generated file.

## Rules

1. `-type` must name a struct declared in the same directory as the directive.
2. The output file is `<StructName>_immutable.go` — the struct name is used
   verbatim (not lower-cased), so `-type NonGeneric` writes
   `NonGeneric_immutable.go`.
3. The generated type is `Im<StructName>`; all of its fields are unexported, so
   they can only be read through getters and replaced through setters.
4. **Methods that mutate a receiver field cause generation to fail.** The tool
   copies the original struct's methods onto the immutable type; a method that
   assigns to `r.<field>` would break immutability, so it is rejected. Make such
   a method return a new value (or drop it) before generating.
5. **A public field that collides with an existing private field once
   lower-cased causes generation to fail** (e.g. having both `Hello` and
   `hello`), because both would map to the same unexported field.
6. Only exported fields get a `Get<Field>` / `Set<Field>` pair. Unexported
   fields are carried over and preserved through conversions, but have no
   accessors.
7. Generics are supported; the type parameters are copied to `Im<StructName>`.
8. After writing the directive, remind the user to run `go generate` and ensure
   the tool is available:
   ```bash
   go get -tool github.com/gostdlib/base/values/generators/immutable
   ```

## Typical directive

```go
//go:generate go tool github.com/gostdlib/base/values/generators/immutable -type User

type User struct {
	ID    uint64
	Name  string
	Tags  map[string]string
	Roles []string
	secret string
}
```

## Generated output

The directive above produces `User_immutable.go` containing:

- `type ImUser struct { ... }` — the immutable twin; every field is unexported.
  `map[string]string` becomes `immutable.Map[string, string]` and `[]string`
  becomes `immutable.Slice[string]` (from
  `github.com/gostdlib/base/values/immutable`).
- `func (r *ImUser) GetID() uint64`, `GetName()`, `GetTags()`, `GetRoles()` —
  a getter per exported field. (`secret` is unexported, so it gets none.)
- `func (r *ImUser) SetID(value uint64) ImUser` and one `Set<Field>` per
  exported field — each returns a **copy** of the value with that one field
  changed; the receiver is never mutated.
- `func (r *ImUser) Mutable() User` — converts back to the original mutable
  struct (copying the immutable `Map`/`Slice` fields).
- `func (r *User) Immutable() ImUser` — converts a mutable `User` into its
  immutable twin.

Any methods declared on the original struct are also copied onto `ImUser`.

## Usage example

```go
u := User{ID: 1, Name: "Alice"}.Immutable() // hand out an ImUser; callers can't mutate it

fmt.Println(u.GetName()) // "Alice"

u2 := u.SetName("Bob")   // u is unchanged; u2 is a new value
fmt.Println(u.GetName())  // "Alice"
fmt.Println(u2.GetName()) // "Bob"

m := u.Mutable() // back to a mutable User when you need one
m.Name = "Carol"
```
