# queue

`package queue` provides a generic, thread-safe ordered list + queue with OTEL integration that supports
multiple interchangeable backings. It supports blocking and non-blocking operations, 
bounded and unbounded queues, FIFO and priority ordering, in-memory and on-disk storage, 
and an optional backup interface for crash recovery.

```go
import "github.com/gostdlib/base/values/generics/queue"
```

The runnable versions of every snippet below live in `example_test.go` and render on the
package's [pkg.go.dev](https://pkg.go.dev) page (run them with `go test`).

## Features

- One `Queue[T]` type over a caller-chosen `Backing[T]`.
- FIFO or priority ordering.
- In-memory (slice, B-Tree, heap) or on-disk (bbolt) backings.
- Bounded (`maxSize`) or unbounded (`queue.Unlimited`) queues with blocking
  `Push`/`Pop` and `NotEmpty`/`NotFull` wait primitives.
- Batch `Push`, `Pop`, and `Del`.
- Optional in-memory hash index (`WithIndex`) for O(1) `Exists` / O(log n) `Del`.
- Optional `Backup` mirror for recovery, with hydrate-on-startup (`OnLoad`).
- Per-operation `WithSideEffect` for bookkeeping that commits with the operation.

## Choosing a backing

| Constructor | Ordering | Storage | Notes |
|---|---|---|---|
| `NewFIFO` | FIFO | in-memory slice | best under ~10K items |
| `NewBTreeFIFO` | FIFO | in-memory B-Tree | large/unbounded; `WithIndex` adds a keyed tree + hash index |
| `NewPriority` | priority | in-memory heap | items pop in `Item.Less` order |
| `NewBTreePriority` | priority | in-memory B-Tree | preferred for large/unbounded priority queues; accepts `WithIndex` |
| `NewBboltFIFO` | FIFO | on-disk (bbolt) | survives restarts; DB is source of truth on reopen |
| `NewBboltPriority` | priority | on-disk (bbolt) | survives restarts |

`WithIndex` enables an in-memory hash index for O(1) `Exists` / O(log n) `Del`, honored by
the keyed B-Tree and bbolt backings. Priority backings order by `Item.Less`; FIFO backings
by insertion. Items pushed onto a priority queue must report `Priority() > 0`; items pushed
onto a FIFO queue must report `Priority() == 0`.

## Item types

Anything stored must satisfy the `Item` constraint (`Less`, `Equal`, `Priority`, `Hash`).
Rather than implement it yourself, wrap your value in one of the built-in types. Every
wrapper has a `V` field (the value) and a `P uint64` field (the priority — set `P > 0`
for priority queues, leave `P == 0` for FIFO); `Less` compares `P`.

| Wrapper | `V` type | `Equal` | `Hash` | Backings |
|---|---|---|---|---|
| `Number[T]` | `T` constrained to `constraints.Integer \| constraints.Float` | `==` on `V` | value-derived (canonical; `-0.0`/`+0.0` hash equal, `NaN` never `Equal`) | all (in-memory and on-disk) |
| `String` | `string` | `==` on `V` | `maphash` of `V` | all |
| `Bytes` | `[]byte` | `bytes.Equal` | `maphash` of `V` | all |
| `Value[T]` | `T any` | caller's `Equaler(a, b)` | caller's `Hasher(v)` | in-memory; on-disk with `WithCodec` |

Notes:

- `Value[T]` requires non-nil `Equaler` and `Hasher` that are mutually consistent: if
  `Equaler(a, b)` then `Hasher(a) == Hasher(b)`. Its function fields cannot be JSON-encoded,
  so an on-disk `Value` queue must be built with `WithCodec(enc, dec)` on
  `NewBboltFIFO`/`NewBboltPriority` (the constructor returns `ErrCodecRequired` otherwise).
  `JSONEncode[T]`/`JSONDecode[T]` are provided helpers for JSON-serializable payloads;
  `Equaler`/`Hasher` are not persisted, so a decode closure that needs `Del`/`Exists`/
  `WithIndex` after reload must re-attach them.
- `Hash` only matters when `WithIndex` is used; it must stay consistent with `Equal`
  (equal items hash equal — collisions are fine, `Equal` still confirms the match).
- All wrappers are comparable by value; construct them inline, e.g.
  `queue.Number[int]{V: 5}` or `queue.String{V: "x", P: 3}`.

## Quick start (FIFO)

```go
ctx := context.Background()

b, err := queue.NewFIFO[queue.Number[int]]()
if err != nil { 
	panic(err) 
}

q, err := queue.New(ctx, b, queue.Unlimited) // Unlimited == 0 == unbounded
if err != nil { 
	panic(err) 
}
defer q.Close(ctx)

if _, err := q.Push(ctx, []queue.Number[int]{{V: 1}, {V: 2}, {V: 3}}); err != nil {
	panic(err)
}

items, err := q.Pop(ctx, 3)
if err != nil { 
	panic(err) 
}

for _, it := range items {
	fmt.Println(it.V) // 1, 2, 3
}
```

## Priority queue

Items pop in ascending `Priority` regardless of push order; priority items must have
`Priority > 0`.

```go
b, _ := queue.NewPriority[queue.Number[int]]()
q, _ := queue.New(ctx, b, queue.Unlimited)
defer q.Close(ctx)

q.Push(ctx, []queue.Number[int]{{V: 30, P: 30}, {V: 10, P: 10}, {V: 20, P: 20}})
items, _ := q.Pop(ctx, 3)
// items pop as 10, 20, 30
```

## Peek

`Peek` returns the head without removing it.

```go
v, ok, err := q.Peek(ctx) // ok == false when empty
```

## Membership and delete (with index)

`Del` removes every item that `Equal`s any element of its argument (batch). `WithIndex`
makes `Exists` O(1) and `Del` O(log n).

```go
b, _ := queue.NewBTreeFIFO[queue.Number[int]](queue.WithIndex())
q, _ := queue.New(ctx, b, queue.Unlimited)
defer q.Close(ctx)

q.Push(ctx, []queue.Number[int]{{V: 1}, {V: 2}, {V: 3}})
ok, _ := q.Exists(ctx, queue.Number[int]{V: 2})       // true
q.Del(ctx, []queue.Number[int]{{V: 2}})
ok, _ = q.Exists(ctx, queue.Number[int]{V: 2})        // false, Len() == 2
```

## Iterate without consuming (`RangeAll`)

`RangeAll` ranges over the live queue without removing anything. It holds the read lock
for the **entire loop**, so writers (`Push`/`Pop`/`Del`/`Clear`) block until iteration
finishes or is abandoned. Do not call a mutating `Queue` method from inside the loop on
the same goroutine — that self-deadlocks.

```go
for v, err := range q.RangeAll(ctx) {
	if err != nil { panic(err) }
	fmt.Println(v.V)
}
// Len() unchanged
```

## Snapshot iteration under contention (`RangeAllCOW`)

`RangeAllCOW` is like `RangeAll` but does not block writers for the whole iteration. It
holds the read lock only until a writer is waiting; at that point it copies the remaining
items, releases the lock, and finishes iterating over the copy while the writer proceeds.
The snapshot is taken at the moment of contention, so items yielded after that reflect the
queue state at that instant, not later mutations. For on-disk backings the remainder is
copied into memory, which can be large.

```go
for v, err := range q.RangeAllCOW(ctx) {
	if err != nil { panic(err) }
	fmt.Println(v.V)
}
// Len() unchanged; concurrent writers are not blocked for the whole loop
```

## Bounded queues

A single `Push` whose batch exceeds `maxSize` can never fit and returns
`ErrBatchTooLarge`; otherwise `Push` blocks until there is room (or the context is
canceled).

```go
q, _ := queue.New(ctx, b, 2) // at most 2 items
_, err := q.Push(ctx, []queue.Number[int]{{V: 1}, {V: 2}, {V: 3}})
errors.Is(err, queue.ErrBatchTooLarge) // true
```

## Wait before consuming

A consumer can block until the queue has an item; `NotFull` is the producer-side
counterpart (blocks until a bounded queue has room). Both honor context cancellation.

```go
if err := q.NotEmpty(ctx); err != nil { panic(err) }
items, _ := q.Pop(ctx, 1)
```

## On-disk (bbolt)

The database lives under `root` and is the source of truth on reopen, so the queue
survives process restarts.

```go
root, _ := os.OpenRoot(dir)
b, _ := queue.NewBboltFIFO[queue.Number[int]](ctx, root)
q, _ := queue.New(ctx, b, queue.Unlimited)
defer q.Close(ctx)

q.Push(ctx, []queue.Number[int]{{V: 10}, {V: 20}})
items, _ := q.Pop(ctx, 2) // 10, 20
```

For delete/exists-heavy on-disk workloads, add `WithIndex()`. See the bbolt constructors
for tuning options (`WithNoSync`, `WithBoltFreelistMap`, `WithNoFreelistSync`, …).

## Backup and recovery

`WithBackup` attaches a `Backup[T]`. On `New` the queue hydrates from the backup —
`OnLoad` is called once per item so you can rebuild external state — and every later
mutation is mirrored to the backup so it stays a true copy.

```go
type memBackup struct { /* implements queue.Backup[queue.Number[int]] */ }

mb := &memBackup{items: []queue.Number[int]{{V: 1}, {V: 2}}}
q, _ := queue.New(ctx, b, queue.Unlimited, queue.WithBackup(mb))
// q.Len() == 2 (hydrated); OnLoad fired for 1 and 2
q.Pop(ctx, 2)
// mb mirrors the deletes; mb.Len() == 0
```

`OnLoad` runs during `New`, before the queue exists, so it must not operate on this queue
or its backing — restrict it to external state. For an on-disk backing restarting against
an already-populated store, items are recovered from the database (not the backup) but
`OnLoad` still fires once per persisted item. A full `Backup` implementation
(`Push`/`Del`/`Restore`/`Len`/`Close`/`Clear`/`RangeAll`/`OnLoad`) is in
`example_test.go`.

## Side effects on an operation

`WithSideEffect` runs after the operation succeeds, as part of the same call; if it
returns an error, the operation returns that error. It takes no arguments (suited to
cross-cutting bookkeeping like metrics). To key external state by the popped values, use
the slice `Pop` returns.

```go
stats := map[string]int{}
items, err := q.Pop(ctx, 2, queue.WithSideEffect(func() error {
	stats["popOps"]++
	return nil
}))
```

## Custom item types

`Value[T]` adapts an arbitrary type with caller-supplied equality and hash functions
(consistent: equal values must hash equal).

```go
type person struct{ name string }
mk := func(n string) queue.Value[person] {
	return queue.Value[person]{
		V:       person{n},
		Equaler: func(a, b person) bool { return a.name == b.name },
		Hasher:  func(p person) uint64 { h := fnv.New64a(); h.Write([]byte(p.name)); return h.Sum64() },
	}
}
q.Push(ctx, []queue.Value[person]{mk("alice"), mk("bob")})
```

## Notes

- `Push` returns `(ok bool, err error)`; `Pop` returns `([]T, error)`; `Del` takes a
  batch `[]T`.
- The priority-heap backing's `All` holds the write lock for the iteration (it sorts the
  heap in place, which keeps the min-heap valid); every other backing's `All` holds the
  read lock. `AllCOW` is the read-lock, snapshot variant.
- Run the examples: `go test ./...` in this directory executes every `Example*` and
  verifies its `// Output:`.

## Third Party source

- Bbolt - https://pkg.go.dev/go.etcd.io/bbolt
- Tidwall/btree - https://github.com/tidwall/btree
- Tidwall/btype (btree fifo borrows this code) - https://github.com/tidwall/btype
