package queue

import (
	"context"
	"slices"
	"testing"
)

// The fuzz tests drive a queue with an operation script decoded from the fuzz input and
// check every observable result against a trivial in-memory reference model. The queue is
// unbounded so Push never blocks, and Pop/Peek-removing ops are only issued when the model
// is non-empty so the queue never blocks waiting for an item.

// fifoModel is the reference for a FIFO queue: values in insertion order.
type fifoModel struct {
	items []int
}

func (m *fifoModel) push(v int)  { m.items = append(m.items, v) }
func (m *fifoModel) len() int    { return len(m.items) }
func (m *fifoModel) empty() bool { return len(m.items) == 0 }
func (m *fifoModel) head() int   { return m.items[0] }
func (m *fifoModel) pop() int {
	v := m.items[0]
	m.items = m.items[1:]
	return v
}
func (m *fifoModel) exists(v int) bool {
	if slices.Contains(m.items, v) {
		return true
	}
	return false
}
func (m *fifoModel) del(v int) {
	out := m.items[:0:0]
	for _, it := range m.items {
		if it != v {
			out = append(out, it)
		}
	}
	m.items = out
}
func (m *fifoModel) delMany(vs []int) {
	for _, v := range vs {
		m.del(v)
	}
}

// prioModel is the reference for a priority queue using prioItem's encoding (P = v+1), so
// the pop order is ascending value with insertion order breaking ties between equal values.
type prioModel struct {
	vals []int
	seqs []int
	next int
}

func (m *prioModel) push(v int) {
	m.vals = append(m.vals, v)
	m.seqs = append(m.seqs, m.next)
	m.next++
}
func (m *prioModel) len() int    { return len(m.vals) }
func (m *prioModel) empty() bool { return len(m.vals) == 0 }

// minIdx returns the index that pops next: lowest value, earliest sequence on ties.
func (m *prioModel) minIdx() int {
	best := 0
	for i := 1; i < len(m.vals); i++ {
		switch {
		case m.vals[i] < m.vals[best]:
			best = i
		case m.vals[i] == m.vals[best] && m.seqs[i] < m.seqs[best]:
			best = i
		}
	}
	return best
}

func (m *prioModel) head() int { return m.vals[m.minIdx()] }
func (m *prioModel) pop() int {
	i := m.minIdx()
	v := m.vals[i]
	m.vals = append(m.vals[:i], m.vals[i+1:]...)
	m.seqs = append(m.seqs[:i], m.seqs[i+1:]...)
	return v
}
func (m *prioModel) exists(v int) bool {
	for _, it := range m.vals {
		if it == v {
			return true
		}
	}
	return false
}
func (m *prioModel) del(v int) {
	var vals, seqs []int
	for i, it := range m.vals {
		if it != v {
			vals = append(vals, it)
			seqs = append(seqs, m.seqs[i])
		}
	}
	m.vals, m.seqs = vals, seqs
}
func (m *prioModel) delMany(vs []int) {
	for _, v := range vs {
		m.del(v)
	}
}

// model is the reference interface both fifoModel and prioModel satisfy.
type model interface {
	push(v int)
	pop() int
	head() int
	len() int
	empty() bool
	exists(v int) bool
	del(v int)
	delMany(vs []int)
}

// runScript replays the op script in data against q (built for the maker's kind) and the
// reference model m, failing on the first divergence.
func runScript(t *testing.T, ctx context.Context, name string, q *Queue[Number[int]], priority bool, m model, data []byte) {
	t.Helper()
	mk := func(v int) Number[int] { return itemFor(priority, v) }

	for i := 0; i+1 < len(data); i += 2 {
		op := data[i] % 7
		v := int(data[i+1])
		switch op {
		case 0: // push
			if ok, err := q.Push(ctx, []Number[int]{mk(v)}); err != nil || !ok {
				t.Fatalf("Fuzz%s: Push(%d) got (ok=%v err=%v), want (true,nil)", name, v, ok, err)
			}
			m.push(v)
		case 1: // pop (only when non-empty so the queue never blocks)
			if m.empty() {
				continue
			}
			items, err := q.Pop(ctx, 1)
			if err != nil || len(items) != 1 {
				t.Fatalf("Fuzz%s: Pop got (items=%v err=%v), want 1 item, nil", name, items, err)
			}
			if want := m.pop(); items[0].V != want {
				t.Fatalf("Fuzz%s: Pop got %d, want %d", name, items[0].V, want)
			}
		case 2: // peek
			pv, ok, err := q.Peek(ctx)
			if err != nil {
				t.Fatalf("Fuzz%s: Peek got err == %s, want err == nil", name, err)
			}
			if ok != !m.empty() {
				t.Fatalf("Fuzz%s: Peek ok == %v, want %v", name, ok, !m.empty())
			}
			if ok && pv.V != m.head() {
				t.Fatalf("Fuzz%s: Peek got %d, want %d", name, pv.V, m.head())
			}
		case 3: // len
			if got := q.Len(); got != int64(m.len()) {
				t.Fatalf("Fuzz%s: Len got %d, want %d", name, got, m.len())
			}
		case 4: // exists
			got, err := q.Exists(ctx, queryItem(v))
			if err != nil {
				t.Fatalf("Fuzz%s: Exists(%d) got err == %s, want err == nil", name, v, err)
			}
			if got != m.exists(v) {
				t.Fatalf("Fuzz%s: Exists(%d) got %v, want %v", name, v, got, m.exists(v))
			}
		case 5: // del
			if err := q.Del(ctx, []Number[int]{queryItem(v)}); err != nil {
				t.Fatalf("Fuzz%s: Del(%d) got err == %s, want err == nil", name, v, err)
			}
			m.del(v)
		case 6: // batch del with a duplicate element (exercises the dedup path)
			v2 := int(data[i])
			if err := q.Del(ctx, []Number[int]{queryItem(v), queryItem(v), queryItem(v2)}); err != nil {
				t.Fatalf("Fuzz%s: batch Del(%d,%d) got err == %s, want err == nil", name, v, v2, err)
			}
			m.delMany([]int{v, v, v2})
		}
		if got := q.Len(); got != int64(m.len()) {
			t.Fatalf("Fuzz%s: Len after op %d got %d, want %d", name, op, got, m.len())
		}
	}

	// Drain whatever remains and confirm the order matches the model exactly.
	for !m.empty() {
		items, err := q.Pop(ctx, 1)
		if err != nil || len(items) != 1 {
			t.Fatalf("Fuzz%s: drain Pop got (items=%v err=%v), want 1 item, nil", name, items, err)
		}
		if want := m.pop(); items[0].V != want {
			t.Fatalf("Fuzz%s: drain Pop got %d, want %d", name, items[0].V, want)
		}
	}
	if got := q.Len(); got != 0 {
		t.Fatalf("Fuzz%s: Len after drain got %d, want 0", name, got)
	}
	if err := q.Close(ctx); err != nil {
		t.Fatalf("Fuzz%s: Close got err == %s, want err == nil", name, err)
	}
}

func fuzzSeeds(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 1, 0, 2, 0, 2, 1, 0, 3, 0})             // push,push,push,pop,len
	f.Add([]byte{0, 9, 0, 9, 4, 9, 5, 9, 4, 9, 3, 0})       // dup push, exists, del, exists, len
	f.Add([]byte{2, 0, 1, 0, 0, 5, 2, 5, 1, 0, 0, 7, 0, 3}) // peek/pop on empty, then activity
	f.Add([]byte{0, 0, 7, 0, 7, 0, 8, 6, 7, 3, 0})          // push 7,7,8 then batch-del (dup 7), len
}

// split returns the backing selector (first byte) and the op script (the rest). Every
// backing is exercised, including the WithIndex Hash-keyed path and the on-disk bbolt
// path. The bbolt backings use WithNoSync so per-exec fsync does not tank the fuzz rate;
// durability is irrelevant to the logical model check.
func split(data []byte) (sel int, script []byte) {
	if len(data) == 0 {
		return 0, nil
	}
	return int(data[0]), data[1:]
}

// fifoFuzzBackings are the FIFO backings the fuzzer rotates through.
var fifoFuzzBackings = []func(t *testing.T, ctx context.Context) (Backing[Number[int]], error){
	func(*testing.T, context.Context) (Backing[Number[int]], error) { return NewFIFO[Number[int]]() },
	func(*testing.T, context.Context) (Backing[Number[int]], error) { return NewBTreeFIFO[Number[int]]() },
	func(*testing.T, context.Context) (Backing[Number[int]], error) {
		return NewBTreeFIFO[Number[int]](WithIndex())
	},
	func(*testing.T, context.Context) (Backing[Number[int]], error) { return newBtypeFIFO[Number[int]]() },
	func(t *testing.T, ctx context.Context) (Backing[Number[int]], error) {
		return NewBboltFIFO[Number[int]](ctx, diskRoot(t), WithNoSync())
	},
	func(t *testing.T, ctx context.Context) (Backing[Number[int]], error) {
		return NewBboltFIFO[Number[int]](ctx, diskRoot(t), WithNoSync(), WithIndex())
	},
}

// prioFuzzBackings are the priority backings the fuzzer rotates through.
var prioFuzzBackings = []func(t *testing.T, ctx context.Context) (Backing[Number[int]], error){
	func(*testing.T, context.Context) (Backing[Number[int]], error) { return NewPriority[Number[int]]() },
	func(*testing.T, context.Context) (Backing[Number[int]], error) {
		return NewBTreePriority[Number[int]]()
	},
	func(*testing.T, context.Context) (Backing[Number[int]], error) {
		return NewBTreePriority[Number[int]](WithIndex())
	},
	func(t *testing.T, ctx context.Context) (Backing[Number[int]], error) {
		return NewBboltPriority[Number[int]](ctx, diskRoot(t), WithNoSync())
	},
	func(t *testing.T, ctx context.Context) (Backing[Number[int]], error) {
		return NewBboltPriority[Number[int]](ctx, diskRoot(t), WithNoSync(), WithIndex())
	},
}

// FuzzFIFO drives a fuzz-selected FIFO backing and checks it against fifoModel.
func FuzzFIFO(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		sel, script := split(data)
		ctx := context.Background()
		b, err := fifoFuzzBackings[sel%len(fifoFuzzBackings)](t, ctx)
		if err != nil {
			t.Fatalf("FuzzFIFO: backing build got err == %s, want err == nil", err)
		}
		q, err := New[Number[int]](ctx, "test", b, 0)
		if err != nil {
			t.Fatalf("FuzzFIFO: New got err == %s, want err == nil", err)
		}
		runScript(t, ctx, "FIFO", q, false, &fifoModel{}, script)
	})
}

// FuzzPriority drives a fuzz-selected priority backing and checks it against prioModel.
func FuzzPriority(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		sel, script := split(data)
		ctx := context.Background()
		b, err := prioFuzzBackings[sel%len(prioFuzzBackings)](t, ctx)
		if err != nil {
			t.Fatalf("FuzzPriority: backing build got err == %s, want err == nil", err)
		}
		q, err := New[Number[int]](ctx, "test", b, 0)
		if err != nil {
			t.Fatalf("FuzzPriority: New got err == %s, want err == nil", err)
		}
		runScript(t, ctx, "Priority", q, true, &prioModel{}, script)
	})
}
