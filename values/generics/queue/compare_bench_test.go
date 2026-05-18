package queue

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/beeker1121/goque"
	diskqueue "github.com/nsqio/go-diskqueue"
	"github.com/tidwall/btype"
)

var benchSizes = []int{1000, 5000, 10000, 50000, 100000}

// diskBenchSizes is small: serial fill/drain does durable I/O per item on the
// fsync-per-op types (ours, nsqio syncEvery=1), so its cost is dominated by disk
// latency × N. The concurrent and batched benchmarks are the meaningful disk
// throughput comparisons.
var diskBenchSizes = []int{100, 500}

// boundedBenchMax is the bounded maxSize used for the slice/heap factories, which only make
// sense for a bounded queue; larger working sets use the btree/bbolt factories instead.
const boundedBenchMax = 10_000

// benchQueue is the minimal surface the comparison benchmarks exercise: fill then drain.
// push takes the value; the adapter assigns the priority appropriate to its backing kind.
type benchQueue interface {
	push(n int)
	pop()
	drain(n int)
	cleanup()
}

func enc(n int) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(n))
	return b[:]
}

// ourQueue adapts *Queue[Number[int]]. Pop is only ever called on a non-empty queue here, so
// its blocking behavior never triggers. priority selects per-kind item construction.
type ourQueue struct {
	ctx      context.Context
	q        *Queue[Number[int]]
	priority bool
}

func (o ourQueue) push(n int) {
	if _, err := o.q.Push(o.ctx, []Number[int]{itemFor(o.priority, n)}); err != nil {
		panic(err)
	}
}

func (o ourQueue) pop() {
	if _, err := o.q.PopN(o.ctx, 1); err != nil {
		panic(err)
	}
}

// drain removes exactly n items using as few PopN calls as possible (one bbolt txn).
func (o ourQueue) drain(n int) {
	for n > 0 {
		items, err := o.q.PopN(o.ctx, n)
		if err != nil {
			panic(err)
		}
		n -= len(items)
	}
}

func (o ourQueue) cleanup() { o.q.Close(o.ctx) }

// btypeQueue adapts github.com/tidwall/btype.Queue (in-memory FIFO, no ctx, non-blocking).
type btypeQueue struct {
	q *btype.Queue[Number[int]]
}

func (b btypeQueue) push(n int) { b.q.Push(Number[int]{V: n}) }
func (b btypeQueue) pop()       { b.q.Pop() }
func (b btypeQueue) drain(n int) {
	for i := 0; i < n; i++ {
		b.q.Pop()
	}
}
func (b btypeQueue) cleanup() {}

// lockedBtypeQueue wraps btype.Queue with a sync.RWMutex so it is goroutine-safe and can
// participate in the concurrent-push benchmark (btype.Queue itself is not safe for
// concurrent use). This isolates the cost of the lock the bare structure omits.
type lockedBtypeQueue struct {
	mu sync.RWMutex
	q  *btype.Queue[Number[int]]
}

func (b *lockedBtypeQueue) push(n int) {
	b.mu.Lock()
	b.q.Push(Number[int]{V: n})
	b.mu.Unlock()
}
func (b *lockedBtypeQueue) pop() {
	b.mu.Lock()
	b.q.Pop()
	b.mu.Unlock()
}
func (b *lockedBtypeQueue) drain(n int) {
	b.mu.Lock()
	for i := 0; i < n; i++ {
		b.q.Pop()
	}
	b.mu.Unlock()
}
func (b *lockedBtypeQueue) cleanup() {}

// nsqQueue adapts github.com/nsqio/go-diskqueue (filesystem-backed FIFO). syncEvery=1 makes
// each Put durable, comparable to the per-op fsync of the bbolt backing.
type nsqQueue struct {
	dq diskqueue.Interface
}

func newNSQ(b *testing.B) benchQueue {
	noop := func(lvl diskqueue.LogLevel, f string, args ...interface{}) {}
	dq := diskqueue.New("bench", b.TempDir(), 1<<24, 1, 1<<16, 1, 2*time.Second, noop)
	return &nsqQueue{dq: dq}
}

func (q *nsqQueue) push(n int) {
	if err := q.dq.Put(enc(n)); err != nil {
		panic(err)
	}
}
func (q *nsqQueue) pop() { <-q.dq.ReadChan() }
func (q *nsqQueue) drain(n int) {
	ch := q.dq.ReadChan()
	for i := 0; i < n; i++ {
		<-ch
	}
}
func (q *nsqQueue) cleanup() { q.dq.Close() }

// goqueQueue adapts github.com/beeker1121/goque.Queue (LevelDB-backed FIFO).
type goqueQueue struct {
	q *goque.Queue
}

func newGoque(b *testing.B) benchQueue {
	q, err := goque.OpenQueue(b.TempDir())
	if err != nil {
		b.Fatalf("goque.OpenQueue: %v", err)
	}
	return &goqueQueue{q: q}
}

func (q *goqueQueue) push(n int) {
	if _, err := q.q.Enqueue(enc(n)); err != nil {
		panic(err)
	}
}
func (q *goqueQueue) pop() {
	if _, err := q.q.Dequeue(); err != nil {
		panic(err)
	}
}
func (q *goqueQueue) drain(n int) {
	for i := 0; i < n; i++ {
		if _, err := q.q.Dequeue(); err != nil {
			panic(err)
		}
	}
}
func (q *goqueQueue) cleanup() { q.q.Close() }

// goquePrioQueue adapts github.com/beeker1121/goque.PriorityQueue (LevelDB-backed priority).
// goque priorities are uint8; the benchmark only measures throughput, not ordering, so the
// value is reused (mod 256) as the priority level.
type goquePrioQueue struct {
	pq *goque.PriorityQueue
}

func newGoquePrio(b *testing.B) benchQueue {
	pq, err := goque.OpenPriorityQueue(b.TempDir(), goque.ASC)
	if err != nil {
		b.Fatalf("goque.OpenPriorityQueue: %v", err)
	}
	return &goquePrioQueue{pq: pq}
}

func (q *goquePrioQueue) push(n int) {
	if _, err := q.pq.Enqueue(uint8(n), enc(n)); err != nil {
		panic(err)
	}
}
func (q *goquePrioQueue) pop() {
	if _, err := q.pq.Dequeue(); err != nil {
		panic(err)
	}
}
func (q *goquePrioQueue) drain(n int) {
	for i := 0; i < n; i++ {
		if _, err := q.pq.Dequeue(); err != nil {
			panic(err)
		}
	}
}
func (q *goquePrioQueue) cleanup() { q.pq.Close() }

type benchFactory struct {
	name string
	disk bool
	// skip reports sizes this type cannot represent (e.g. the []T slice backing only
	// exists for maxSize in (size, boundedBenchMax]).
	skip func(size int) bool
	make func(b *testing.B, size int) benchQueue
}

func ourMaker(maxSize int, priority bool, backing func() (Backing[Number[int]], error)) func(*testing.B, int) benchQueue {
	return func(b *testing.B, size int) benchQueue {
		ctx := b.Context()
		bk, err := backing()
		if err != nil {
			b.Fatalf("backing: %v", err)
		}
		q, err := New[Number[int]](ctx, bk, maxSize)
		if err != nil {
			b.Fatalf("New: %v", err)
		}
		return ourQueue{ctx: ctx, q: q, priority: priority}
	}
}

func diskRootB(b *testing.B) *os.Root {
	root, err := os.OpenRoot(b.TempDir())
	if err != nil {
		b.Fatalf("os.OpenRoot: %v", err)
	}
	return root
}

func ourBboltMaker(priority bool) func(*testing.B, int) benchQueue {
	return func(b *testing.B, size int) benchQueue {
		ctx := b.Context()
		var bk Backing[Number[int]]
		var err error
		if priority {
			bk, err = NewBboltPriority[Number[int]](ctx, diskRootB(b))
		} else {
			bk, err = NewBboltFIFO[Number[int]](ctx, diskRootB(b))
		}
		if err != nil {
			b.Fatalf("bbolt backing: %v", err)
		}
		q, err := New[Number[int]](ctx, bk, 0)
		if err != nil {
			b.Fatalf("New: %v", err)
		}
		return ourQueue{ctx: ctx, q: q, priority: priority}
	}
}

// memoryFactories are the in-memory queue types.
func memoryFactories() []benchFactory {
	return []benchFactory{
		{
			name: "ours-fifo-slice",
			skip: func(size int) bool { return size >= boundedBenchMax },
			make: ourMaker(boundedBenchMax, false, func() (Backing[Number[int]], error) { return NewFIFO[Number[int]]() }),
		},
		{
			name: "ours-fifo-btree",
			make: ourMaker(0, false, func() (Backing[Number[int]], error) { return NewBTreeFIFO[Number[int]]() }),
		},
		{
			name: "ours-fifo-btype",
			make: ourMaker(0, false, func() (Backing[Number[int]], error) { return NewBtypeFIFO[Number[int]]() }),
		},
		{
			name: "ours-fifo-index",
			make: ourMaker(0, false, func() (Backing[Number[int]], error) { return NewBTreeFIFO[Number[int]](WithIndex()) }),
		},
		{
			name: "ours-priority-heap",
			skip: func(size int) bool { return size >= boundedBenchMax },
			make: ourMaker(boundedBenchMax, true, func() (Backing[Number[int]], error) { return NewPriority[Number[int]]() }),
		},
		{
			name: "ours-priority-btree",
			make: ourMaker(0, true, func() (Backing[Number[int]], error) { return NewBTreePriority[Number[int]]() }),
		},
		{
			name: "tidwall-btype",
			make: func(b *testing.B, size int) benchQueue { return btypeQueue{q: &btype.Queue[Number[int]]{}} },
		},
		{
			name: "tidwall-btype-locked",
			make: func(b *testing.B, size int) benchQueue {
				return &lockedBtypeQueue{q: &btype.Queue[Number[int]]{}}
			},
		},
	}
}

// diskFactories are the on-disk queue types: ours (bbolt) vs nsqio/go-diskqueue and
// beeker1121/goque.
func diskFactories() []benchFactory {
	return []benchFactory{
		{name: "ours-fifo-bbolt", disk: true, make: ourBboltMaker(false)},
		{name: "ours-priority-bbolt", disk: true, make: ourBboltMaker(true)},
		{name: "nsqio-diskqueue-fifo", disk: true, make: func(b *testing.B, size int) benchQueue { return newNSQ(b) }},
		{name: "goque-fifo", disk: true, make: func(b *testing.B, size int) benchQueue { return newGoque(b) }},
		{name: "goque-priority", disk: true, make: func(b *testing.B, size int) benchQueue { return newGoquePrio(b) }},
	}
}

// runFillDrain measures one fill-of-N then drain-of-N cycle per op.
func runFillDrain(b *testing.B, fs []benchFactory, sizes []int) {
	for _, f := range fs {
		for _, size := range sizes {
			if f.skip != nil && f.skip(size) {
				continue
			}
			b.Run(fmt.Sprintf("%s/%d", f.name, size), func(b *testing.B) {
				q := f.make(b, size)
				defer q.cleanup()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for j := 0; j < size; j++ {
						q.push(j)
					}
					q.drain(size)
				}
			})
		}
	}
}

// runConcurrentPush times pushBenchTotal single-item pushes spread across P producers.
func runConcurrentPush(b *testing.B, fs []benchFactory) {
	for _, f := range fs {
		if f.name == "tidwall-btype" { // not goroutine-safe
			continue
		}
		for _, p := range []int{8, 50} {
			b.Run(fmt.Sprintf("%s/producers=%d", f.name, p), func(b *testing.B) {
				bq := f.make(b, pushBenchTotal)
				defer bq.cleanup()
				per := pushBenchTotal / p
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					done := make(chan struct{}, p)
					for g := 0; g < p; g++ {
						go func(base int) {
							for k := 0; k < per; k++ {
								bq.push(base + k)
							}
							done <- struct{}{}
						}(g * per)
					}
					for g := 0; g < p; g++ {
						<-done
					}
					b.StopTimer()
					bq.drain(per * p)
					b.StartTimer()
				}
			})
		}
	}
}

// pushBenchTotal is the number of single-item pushes timed by the concurrent push benchmark.
const pushBenchTotal = 2000

// --- Memory comparisons ---

func BenchmarkMemoryFillDrain(b *testing.B)      { runFillDrain(b, memoryFactories(), benchSizes) }
func BenchmarkMemoryConcurrentPush(b *testing.B) { runConcurrentPush(b, memoryFactories()) }

// --- Disk comparisons ---

func BenchmarkDiskFillDrain(b *testing.B)      { runFillDrain(b, diskFactories(), diskBenchSizes) }
func BenchmarkDiskConcurrentPush(b *testing.B) { runConcurrentPush(b, diskFactories()) }

// BenchmarkOursBatchFillDrain measures our batched Push path (one txn / fsync for the
// whole batch) for the *Queue backings only — nsqio/goque have no batch API.
func BenchmarkOursBatchFillDrain(b *testing.B) {
	fs := append(memoryFactories(), diskFactories()...)
	for _, f := range fs {
		for _, size := range benchSizes {
			if f.skip != nil && f.skip(size) {
				continue
			}
			if f.disk && size > diskBenchSizes[len(diskBenchSizes)-1] {
				continue
			}
			b.Run(fmt.Sprintf("%s/%d", f.name, size), func(b *testing.B) {
				bq := f.make(b, size)
				defer bq.cleanup()
				oq, ok := bq.(ourQueue)
				if !ok {
					b.Skip("not a *Queue backing")
				}
				batch := make([]Number[int], size)
				for j := range batch {
					batch[j] = itemFor(oq.priority, j)
				}
				const chunk = 1000
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for off := 0; off < size; off += chunk {
						end := off + chunk
						if end > size {
							end = size
						}
						if _, err := oq.q.Push(oq.ctx, batch[off:end]); err != nil {
							b.Fatalf("Push: %v", err)
						}
					}
					oq.drain(size)
				}
			})
		}
	}
}
