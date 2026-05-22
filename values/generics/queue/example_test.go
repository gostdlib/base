package queue_test

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"iter"
	"os"

	"github.com/gostdlib/base/values/generics/queue"
)

// Example shows the basic FIFO usage: build a slice-backed FIFO, push a batch, then
// drain it in insertion order. New's maxSize of 0 means unbounded.
func Example() {
	ctx := context.Background()

	b, err := queue.NewFIFO[queue.Number[int]]()
	if err != nil {
		panic(err)
	}
	q, err := queue.New(ctx, "example", b, queue.Unlimited)
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
		fmt.Println(it.V)
	}
	// Output:
	// 1
	// 2
	// 3
}

// Example_priority shows a priority queue: items pop in ascending Item.Priority
// regardless of push order. Items pushed onto a priority queue must have Priority > 0.
func Example_priority() {
	ctx := context.Background()

	b, err := queue.NewPriority[queue.Number[int]]()
	if err != nil {
		panic(err)
	}
	q, err := queue.New(ctx, "example", b, 0)
	if err != nil {
		panic(err)
	}
	defer q.Close(ctx)

	if _, err := q.Push(ctx, []queue.Number[int]{
		{V: 30, P: 30},
		{V: 10, P: 10},
		{V: 20, P: 20},
	}); err != nil {
		panic(err)
	}

	items, err := q.Pop(ctx, 3)
	if err != nil {
		panic(err)
	}
	for _, it := range items {
		fmt.Println(it.V)
	}
	// Output:
	// 10
	// 20
	// 30
}

// Example_peek shows reading the head item without removing it. queue.String wraps a
// string so it satisfies the Item constraint.
func Example_peek() {
	ctx := context.Background()

	b, err := queue.NewFIFO[queue.String]()
	if err != nil {
		panic(err)
	}
	q, err := queue.New(ctx, "example", b, 0)
	if err != nil {
		panic(err)
	}
	defer q.Close(ctx)

	if _, err := q.Push(ctx, []queue.String{{V: "first"}, {V: "second"}}); err != nil {
		panic(err)
	}

	v, ok, err := q.Peek(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println(v.V, ok, q.Len())
	// Output:
	// first true 2
}

// Example_existsDelete shows membership tests and removal. WithIndex makes Exists O(1)
// and Del O(log n); Del removes every item that Equals any element of the argument.
func Example_existsDelete() {
	ctx := context.Background()

	b, err := queue.NewBTreeFIFO[queue.Number[int]](queue.WithIndex())
	if err != nil {
		panic(err)
	}
	q, err := queue.New(ctx, "example", b, 0)
	if err != nil {
		panic(err)
	}
	defer q.Close(ctx)

	if _, err := q.Push(ctx, []queue.Number[int]{{V: 1}, {V: 2}, {V: 3}}); err != nil {
		panic(err)
	}

	got, err := q.Exists(ctx, queue.Number[int]{V: 2})
	if err != nil {
		panic(err)
	}
	fmt.Println("exists(2):", got)

	if err := q.Del(ctx, []queue.Number[int]{{V: 2}}); err != nil {
		panic(err)
	}

	got, err = q.Exists(ctx, queue.Number[int]{V: 2})
	if err != nil {
		panic(err)
	}
	fmt.Println("exists(2):", got, "len:", q.Len())
	// Output:
	// exists(2): true
	// exists(2): false len: 2
}

// Example_rangeAll iterates the live queue without consuming it. RangeAll holds the
// read lock for the whole loop, so do not call a mutating method from inside it.
func Example_rangeAll() {
	ctx := context.Background()

	b, err := queue.NewFIFO[queue.Number[int]]()
	if err != nil {
		panic(err)
	}
	q, err := queue.New(ctx, "example", b, 0)
	if err != nil {
		panic(err)
	}
	defer q.Close(ctx)

	if _, err := q.Push(ctx, []queue.Number[int]{{V: 1}, {V: 2}, {V: 3}}); err != nil {
		panic(err)
	}

	for v, err := range q.RangeAll(ctx) {
		if err != nil {
			panic(err)
		}
		fmt.Println(v.V)
	}
	fmt.Println("len after RangeAll:", q.Len())
	// Output:
	// 1
	// 2
	// 3
	// len after RangeAll: 3
}

// Example_bounded shows a bounded queue. A single Push whose batch exceeds maxSize can
// never fit and returns ErrBatchTooLarge; smaller batches are accepted up to capacity.
func Example_bounded() {
	ctx := context.Background()

	b, err := queue.NewFIFO[queue.Number[int]]()
	if err != nil {
		panic(err)
	}
	q, err := queue.New(ctx, "example", b, 2) // bounded: at most 2 items
	if err != nil {
		panic(err)
	}
	defer q.Close(ctx)

	_, err = q.Push(ctx, []queue.Number[int]{{V: 1}, {V: 2}, {V: 3}})
	fmt.Println("batch of 3 too large:", errors.Is(err, queue.ErrBatchTooLarge))

	if _, err := q.Push(ctx, []queue.Number[int]{{V: 1}, {V: 2}}); err != nil {
		panic(err)
	}
	fmt.Println("len:", q.Len())
	// Output:
	// batch of 3 too large: true
	// len: 2
}

// Example_bbolt shows an on-disk FIFO. The database lives under root and is the source
// of truth on reopen, so the queue survives process restarts.
func Example_bbolt() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "queue-example-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	root, err := os.OpenRoot(dir)
	if err != nil {
		panic(err)
	}

	b, err := queue.NewBboltFIFO[queue.Number[int]](ctx, root)
	if err != nil {
		panic(err)
	}
	q, err := queue.New(ctx, "example", b, 0)
	if err != nil {
		panic(err)
	}
	defer q.Close(ctx)

	if _, err := q.Push(ctx, []queue.Number[int]{{V: 10}, {V: 20}}); err != nil {
		panic(err)
	}
	items, err := q.Pop(ctx, 2)
	if err != nil {
		panic(err)
	}
	for _, it := range items {
		fmt.Println(it.V)
	}
	// Output:
	// 10
	// 20
}

// Example_value adapts an arbitrary type to the Item constraint with queue.Value, which
// takes caller-supplied equality and hash functions (consistent: equal values hash equal).
func Example_value() {
	ctx := context.Background()

	type person struct{ name string }
	equal := func(a, b person) bool { return a.name == b.name }
	hash := func(p person) uint64 {
		h := fnv.New64a()
		h.Write([]byte(p.name))
		return h.Sum64()
	}
	mk := func(name string) queue.Value[person] {
		return queue.Value[person]{V: person{name}, Equaler: equal, Hasher: hash}
	}

	b, err := queue.NewFIFO[queue.Value[person]]()
	if err != nil {
		panic(err)
	}
	q, err := queue.New(ctx, "example", b, 0)
	if err != nil {
		panic(err)
	}
	defer q.Close(ctx)

	if _, err := q.Push(ctx, []queue.Value[person]{mk("alice"), mk("bob")}); err != nil {
		panic(err)
	}
	items, err := q.Pop(ctx, 2)
	if err != nil {
		panic(err)
	}
	for _, it := range items {
		fmt.Println(it.V.name)
	}
	// Output:
	// alice
	// bob
}

// Example_waitNotEmpty shows the consumer wait pattern: block until the queue has at
// least one item, then pull. NotFull is the producer-side counterpart — it blocks until
// a bounded queue has room to push. Both honor context cancellation.
func Example_waitNotEmpty() {
	ctx := context.Background()

	b, err := queue.NewFIFO[queue.Number[int]]()
	if err != nil {
		panic(err)
	}
	q, err := queue.New(ctx, "example", b, queue.Unlimited)
	if err != nil {
		panic(err)
	}
	defer q.Close(ctx)

	if _, err := q.Push(ctx, []queue.Number[int]{{V: 7}}); err != nil {
		panic(err)
	}

	if err := q.NotEmpty(ctx); err != nil {
		panic(err)
	}
	items, err := q.Pop(ctx, 1)
	if err != nil {
		panic(err)
	}
	fmt.Println(items[0].V)
	// Output:
	// 7
}

// memBackup is a minimal in-memory queue.Backup[queue.Number[int]] used by
// Example_backup. A real backup would persist to durable storage instead.
type memBackup struct {
	items  []queue.Number[int]
	onLoad []int
}

func (m *memBackup) Push(_ context.Context, vs []queue.Number[int]) error {
	m.items = append(m.items, vs...)
	return nil
}

func (m *memBackup) Del(_ context.Context, vs []queue.Number[int]) error {
	for _, v := range vs {
		for i, it := range m.items {
			if it.Equal(v) {
				m.items = append(m.items[:i], m.items[i+1:]...)
				break
			}
		}
	}
	return nil
}

func (m *memBackup) Restore(_ context.Context, vs []queue.Number[int]) error {
	m.items = append(append([]queue.Number[int]{}, vs...), m.items...)
	return nil
}

func (m *memBackup) Len() int64                  { return int64(len(m.items)) }
func (m *memBackup) Close(context.Context) error { return nil }
func (m *memBackup) Clear(context.Context) error { m.items = nil; return nil }

func (m *memBackup) RangeAll(context.Context) iter.Seq2[queue.Number[int], error] {
	return func(yield func(queue.Number[int], error) bool) {
		for _, it := range m.items {
			if !yield(it, nil) {
				return
			}
		}
	}
}

func (m *memBackup) OnLoad(_ context.Context, v queue.Number[int]) error {
	m.onLoad = append(m.onLoad, v.V) // rebuild external state here (e.g., an index/map)
	return nil
}

// Example_backup attaches a Backup with WithBackup. On New the queue hydrates from the
// backup (calling OnLoad per item so external state can be rebuilt), and every later
// mutation is mirrored to the backup so it stays a true copy for crash recovery.
func Example_backup() {
	ctx := context.Background()

	mb := &memBackup{items: []queue.Number[int]{{V: 1}, {V: 2}}}

	b, err := queue.NewFIFO[queue.Number[int]]()
	if err != nil {
		panic(err)
	}
	q, err := queue.New(ctx, "example", b, queue.Unlimited, queue.WithBackup(mb))
	if err != nil {
		panic(err)
	}
	defer q.Close(ctx)

	fmt.Println("hydrated len:", q.Len())
	fmt.Println("onload:", mb.onLoad)

	if _, err := q.Pop(ctx, 2); err != nil {
		panic(err)
	}
	fmt.Println("backup len after drain:", mb.Len())
	// Output:
	// hydrated len: 2
	// onload: [1 2]
	// backup len after drain: 0
}

// Example_sideEffect runs a WithSideEffect on Pop: it executes after the pop succeeds,
// as part of the same call (if it returns an error, Pop returns that error). It takes
// no arguments, so it suits cross-cutting bookkeeping like metrics; to key external
// state by the popped values, use the slice Pop returns.
func Example_sideEffect() {
	ctx := context.Background()

	b, err := queue.NewFIFO[queue.Number[int]]()
	if err != nil {
		panic(err)
	}
	q, err := queue.New(ctx, "example", b, queue.Unlimited)
	if err != nil {
		panic(err)
	}
	defer q.Close(ctx)

	if _, err := q.Push(ctx, []queue.Number[int]{{V: 1}, {V: 2}}); err != nil {
		panic(err)
	}

	stats := map[string]int{}
	items, err := q.Pop(ctx, 2, queue.WithSideEffect(func() error {
		stats["popOps"]++
		return nil
	}))
	if err != nil {
		panic(err)
	}

	processed := map[int]bool{}
	for _, it := range items {
		processed[it.V] = true
	}
	fmt.Println("popped:", len(items), "popOps:", stats["popOps"], "processed[1]:", processed[1])
	// Output:
	// popped: 2 popOps: 1 processed[1]: true
}
