# immutable

`immutable` generates an immutable (read-only) twin of an existing Go struct: a
value type whose fields cannot be mutated after construction, with getters for
every exported field, copy-on-write setters that return a new value, and
conversions to and from the original mutable struct.

It is the code-generation companion to the
[`github.com/gostdlib/base/values/immutable`](../../immutable) package, whose
`Map` and `Slice` types it uses for map and slice fields so they can't be
mutated through a shared reference.

## Install

The tool is registered in this module's `go.mod` `tool` block, so it runs via:

```bash
go tool github.com/gostdlib/base/values/generators/immutable -type StructName
```

In another module, add it as a tool dependency first:

```bash
go get -tool github.com/gostdlib/base/values/generators/immutable
```

## Usage

Add a `//go:generate` directive in the package that declares the struct, then
run `go generate ./...`:

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

The tool scans every non-generated `*.go` file in its directory for the named
struct, so run `go generate` from that directory (or `./...` from the module
root).

### Flags

| Flag | Description |
|------|-------------|
| `-type` | **(required)** Name of the struct to make immutable. Output is written to `<StructName>_immutable.go`. |

One struct per invocation — add a directive for each struct you want an
immutable version of.

## Generated code

The directive above produces `User_immutable.go`:

```go
type ImUser struct {
	id     uint64
	name   string
	tags   immutable.Map[string, string]
	roles  immutable.Slice[string]
	secret string
}

func (r *ImUser) GetID() uint64
func (r *ImUser) SetID(value uint64) ImUser
func (r *ImUser) GetName() string
func (r *ImUser) SetName(value string) ImUser
func (r *ImUser) GetTags() immutable.Map[string, string]
func (r *ImUser) SetTags(value immutable.Map[string, string]) ImUser
func (r *ImUser) GetRoles() immutable.Slice[string]
func (r *ImUser) SetRoles(value immutable.Slice[string]) ImUser

func (r *ImUser) Mutable() User    // back to the mutable struct
func (r *User) Immutable() ImUser  // into the immutable twin
```

Key properties of the generated type:

- **Every field is unexported**, so it can only be read through a `Get<Field>`
  method and replaced through a `Set<Field>` method. There is no way to mutate
  an `ImUser` in place.
- **Setters are copy-on-write.** `Set<Field>` returns a new value with that one
  field changed and leaves the receiver untouched.
- **`map` and slice fields become immutable.** A `map[K]V` field becomes an
  `immutable.Map[K, V]` and a `[]T` field becomes an `immutable.Slice[T]`, so
  callers can't reach in and mutate the backing collection. `Mutable()` copies
  them back out into ordinary maps and slices.
- **Unexported fields are preserved** through `Immutable()` / `Mutable()` but
  get no accessors (e.g. `secret` above).
- **Methods are carried over.** Any method declared on the original struct is
  copied onto the immutable type.
- **Generics are supported** — the struct's type parameters are copied to the
  generated type.

### Constraints

The tool refuses to generate (and exits with an error) when it can't preserve
immutability:

- A copied method that **mutates a receiver field** (assigns to `r.<field>`) is
  rejected — it would defeat the immutability guarantee. Make the method return
  a new value, or remove it, before generating.
- A **public field that collides with an existing private field** once
  lower-cased (e.g. both `Hello` and `hello`) is rejected, since both would map
  to the same unexported field.

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

## Claude Code skill

This package includes a [SKILL.md](SKILL.md) that teaches
[Claude Code](https://claude.ai/code) how to generate `go:generate` directives
for this tool. When installed, Claude suggests the correct directive when you
ask for an immutable version of a struct.

To install, copy the file into your personal or project skills directory:

```bash
# Personal (applies to all your projects)
mkdir -p ~/.claude/skills/immutable
cp SKILL.md ~/.claude/skills/immutable/SKILL.md

# Or project-level (applies to one project)
mkdir -p .claude/skills/immutable
cp SKILL.md .claude/skills/immutable/SKILL.md
```

You can also invoke it manually in Claude Code with `/immutable`.
