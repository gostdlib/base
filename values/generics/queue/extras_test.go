package queue

import (
	"errors"
	"os"
	"testing"
	"time"
)

// mustPanic runs f and fails unless it panics with a message containing want.
func mustPanic(t *testing.T, name, want string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("%s: did not panic, want panic containing %q", name, want)
			return
		}
		if msg, _ := r.(string); msg == "" || !contains(msg, want) {
			t.Errorf("%s: panic = %v, want message containing %q", name, r, want)
		}
	}()
	f()
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestStringItem covers String's Item methods and an end-to-end FIFO queue of String.
func TestStringItem(t *testing.T) {
	a := String{V: "x", P: 1}
	b := String{V: "x", P: 9}
	c := String{V: "y", P: 5}
	switch {
	case !a.Equal(b):
		t.Errorf("TestStringItem: String{x}.Equal(String{x}) = false, want true (Equal is value-based)")
	case a.Equal(c):
		t.Errorf("TestStringItem: String{x}.Equal(String{y}) = true, want false")
	case a.Hash() != b.Hash():
		t.Errorf("TestStringItem: equal values hashed differently (%d vs %d)", a.Hash(), b.Hash())
	case !a.Less(b) && a.P < b.P:
		t.Errorf("TestStringItem: Less must compare P: a.P=%d b.P=%d", a.P, b.P)
	case a.Priority() != 1:
		t.Errorf("TestStringItem: Priority() = %d, want 1", a.Priority())
	}

	ctx := t.Context()
	bk, err := NewFIFO[String]()
	if err != nil {
		t.Fatalf("TestStringItem: NewFIFO got err == %s, want err == nil", err)
	}
	q, err := New[String](ctx, "test", bk, 0)
	if err != nil {
		t.Fatalf("TestStringItem: New got err == %s, want err == nil", err)
	}
	for _, s := range []string{"b", "a", "c"} {
		if ok, err := q.Push(ctx, []String{{V: s}}); err != nil || !ok {
			t.Fatalf("TestStringItem: Push(%q) got (ok=%v err=%v), want (true,nil)", s, ok, err)
		}
	}
	if ex, err := q.Exists(ctx, String{V: "a"}); err != nil || !ex {
		t.Errorf("TestStringItem: Exists(a) got (%v,%v), want (true,nil)", ex, err)
	}
	for _, want := range []string{"b", "a", "c"} { // FIFO insertion order
		items, err := q.Pop(ctx, 1)
		if err != nil || len(items) != 1 || items[0].V != want {
			t.Fatalf("TestStringItem: Pop got (%v,%v), want %q", items, err, want)
		}
	}
	if err := q.Close(ctx); err != nil {
		t.Errorf("TestStringItem: Close got err == %s, want err == nil", err)
	}
}

// TestBytesItem covers Bytes's Item methods and an end-to-end priority queue of Bytes.
func TestBytesItem(t *testing.T) {
	a := Bytes{V: []byte("x"), P: 2}
	b := Bytes{V: []byte("x"), P: 7}
	if !a.Equal(b) {
		t.Errorf("TestBytesItem: equal bytes not Equal")
	}
	if a.Hash() != b.Hash() {
		t.Errorf("TestBytesItem: equal bytes hashed differently (%d vs %d)", a.Hash(), b.Hash())
	}
	if a.Equal(Bytes{V: []byte("y")}) {
		t.Errorf("TestBytesItem: different bytes reported Equal")
	}
	if !a.Less(b) {
		t.Errorf("TestBytesItem: Less must compare P (2 < 7)")
	}

	ctx := t.Context()
	bk, err := NewBTreePriority[Bytes]()
	if err != nil {
		t.Fatalf("TestBytesItem: NewBTreePriority got err == %s, want err == nil", err)
	}
	q, err := New[Bytes](ctx, "test", bk, 0)
	if err != nil {
		t.Fatalf("TestBytesItem: New got err == %s, want err == nil", err)
	}
	// Pushed out of priority order; must pop by ascending P.
	in := []Bytes{{V: []byte("hi"), P: 3}, {V: []byte("lo"), P: 1}, {V: []byte("mid"), P: 2}}
	for _, it := range in {
		if ok, err := q.Push(ctx, []Bytes{it}); err != nil || !ok {
			t.Fatalf("TestBytesItem: Push got (ok=%v err=%v), want (true,nil)", ok, err)
		}
	}
	for _, want := range []string{"lo", "mid", "hi"} {
		items, err := q.Pop(ctx, 1)
		if err != nil || len(items) != 1 || string(items[0].V) != want {
			t.Fatalf("TestBytesItem: Pop got (%v,%v), want %q", items, err, want)
		}
	}
	if err := q.Close(ctx); err != nil {
		t.Errorf("TestBytesItem: Close got err == %s, want err == nil", err)
	}
}

// TestValueItem covers Value[T]'s caller-supplied Equaler/Hasher, the nil-guard panics,
// and an end-to-end FIFO queue of Value[int].
func TestValueItem(t *testing.T) {
	eq := func(a, b int) bool { return a == b }
	hsh := func(a int) uint64 { return uint64(a) }

	a := Value[int]{V: 5, P: 1, Equaler: eq, Hasher: hsh}
	b := Value[int]{V: 5, P: 9, Equaler: eq, Hasher: hsh}
	switch {
	case !a.Equal(b):
		t.Errorf("TestValueItem: Equaler-based Equal got false, want true")
	case a.Hash() != b.Hash():
		t.Errorf("TestValueItem: Hasher-based Hash mismatch (%d vs %d)", a.Hash(), b.Hash())
	case !a.Less(b):
		t.Errorf("TestValueItem: Less must compare P (1 < 9)")
	case a.Priority() != 1:
		t.Errorf("TestValueItem: Priority() = %d, want 1", a.Priority())
	}

	mustPanic(t, "TestValueItem nil Equaler", "queue: Value.Equal: Equaler is nil", func() {
		_ = Value[int]{}.Equal(Value[int]{})
	})
	mustPanic(t, "TestValueItem nil Hasher", "queue: Value.Hash: Hasher is nil", func() {
		_ = Value[int]{}.Hash()
	})

	ctx := t.Context()
	bk, err := NewFIFO[Value[int]]()
	if err != nil {
		t.Fatalf("TestValueItem: NewFIFO got err == %s, want err == nil", err)
	}
	q, err := New[Value[int]](ctx, "test", bk, 0)
	if err != nil {
		t.Fatalf("TestValueItem: New got err == %s, want err == nil", err)
	}
	mk := func(v int) Value[int] { return Value[int]{V: v, Equaler: eq, Hasher: hsh} }
	for _, v := range []int{10, 20, 30} {
		if ok, err := q.Push(ctx, []Value[int]{mk(v)}); err != nil || !ok {
			t.Fatalf("TestValueItem: Push(%d) got (ok=%v err=%v), want (true,nil)", v, ok, err)
		}
	}
	if err := q.Del(ctx, []Value[int]{mk(20)}); err != nil {
		t.Errorf("TestValueItem: Del got err == %s, want err == nil", err)
	}
	if ex, err := q.Exists(ctx, mk(20)); err != nil || ex {
		t.Errorf("TestValueItem: Exists(20) after Del got (%v,%v), want (false,nil)", ex, err)
	}
	for _, want := range []int{10, 30} {
		items, err := q.Pop(ctx, 1)
		if err != nil || len(items) != 1 || items[0].V != want {
			t.Fatalf("TestValueItem: Pop got (%v,%v), want %d", items, err, want)
		}
	}
	if err := q.Close(ctx); err != nil {
		t.Errorf("TestValueItem: Close got err == %s, want err == nil", err)
	}
}

// TestBackingOptionRejection verifies the shared BackingOption type rejects use with a
// constructor it does not support (the article's call-type validation): WithBTreeWidth is
// only valid for the keyed B-Tree constructors, so passing it to a bbolt constructor
// returns an error.
func TestBackingOptionRejection(t *testing.T) {
	ctx := t.Context()
	if _, err := NewBboltFIFO[Number[int]](ctx, diskRoot(t), WithBTreeWidth(8)); err == nil {
		t.Errorf("TestBackingOptionRejection: NewBboltFIFO(WithBTreeWidth) got err == nil, want err != nil")
	}
	if _, err := NewBboltPriority[Number[int]](ctx, diskRoot(t), WithBTreeWidth(8)); err == nil {
		t.Errorf("TestBackingOptionRejection: NewBboltPriority(WithBTreeWidth) got err == nil, want err != nil")
	}
	// WithIndex is valid for all four indexable constructors.
	if _, err := NewBboltFIFO[Number[int]](ctx, diskRoot(t), WithIndex()); err != nil {
		t.Errorf("TestBackingOptionRejection: NewBboltFIFO(WithIndex) got err == %s, want nil", err)
	}
	// bbolt-only options are rejected by the in-memory btree constructors.
	if _, err := NewBTreeFIFO[Number[int]](WithNoSync()); err == nil {
		t.Errorf("TestBackingOptionRejection: NewBTreeFIFO(WithNoSync) got err == nil, want err != nil")
	}
	if _, err := NewBTreePriority[Number[int]](WithBoltTimeout(time.Second)); err == nil {
		t.Errorf("TestBackingOptionRejection: NewBTreePriority(WithBoltTimeout) got err == nil, want err != nil")
	}
}

func TestBackingOptsApplied(t *testing.T) {
	openFn := func(string, int, os.FileMode) (*os.File, error) { return nil, nil }
	o, err := applyBackingOptions(callBboltFIFO, []BackingOption{
		WithNoSync(), WithNoFreelistSync(), WithNoGrowSync(),
		WithBoltPreLoadFreelist(), WithBoltFreelistMap(), WithBoltMlock(),
		WithBoltMmapFlags(0x40), WithBoltInitialMmapSize(1 << 20),
		WithBoltPageSize(8192), WithBoltTimeout(3 * time.Second),
		WithBoltOpenFile(openFn), WithIndex(),
	})
	if err != nil {
		t.Fatalf("TestBackingOptsApplied: applyBackingOptions got err == %s, want nil", err)
	}
	switch {
	case !o.boltNoSync:
		t.Errorf("TestBackingOptsApplied: boltNoSync not set")
	case !o.boltNoFreelistSync:
		t.Errorf("TestBackingOptsApplied: boltNoFreelistSync not set")
	case !o.boltNoGrowSync:
		t.Errorf("TestBackingOptsApplied: boltNoGrowSync not set")
	case !o.boltPreLoadFreelist:
		t.Errorf("TestBackingOptsApplied: boltPreLoadFreelist not set")
	case !o.boltFreelistMap:
		t.Errorf("TestBackingOptsApplied: boltFreelistMap not set")
	case !o.boltMlock:
		t.Errorf("TestBackingOptsApplied: boltMlock not set")
	case o.boltMmapFlags != 0x40:
		t.Errorf("TestBackingOptsApplied: boltMmapFlags = %d, want 0x40", o.boltMmapFlags)
	case o.boltInitialMmapSize != 1<<20:
		t.Errorf("TestBackingOptsApplied: boltInitialMmapSize = %d, want %d", o.boltInitialMmapSize, 1<<20)
	case o.boltPageSize != 8192:
		t.Errorf("TestBackingOptsApplied: boltPageSize = %d, want 8192", o.boltPageSize)
	case o.boltTimeout != 3*time.Second:
		t.Errorf("TestBackingOptsApplied: boltTimeout = %s, want 3s", o.boltTimeout)
	case o.boltOpenFile == nil:
		t.Errorf("TestBackingOptsApplied: boltOpenFile not set")
	case !o.index:
		t.Errorf("TestBackingOptsApplied: index not set")
	}
}

func TestBboltMlock(t *testing.T) {
	ctx := t.Context()
	bk, err := NewBboltFIFO[Number[int]](ctx, diskRoot(t), WithBoltMlock(), WithBoltMmapFlags(0))
	if err != nil {
		t.Fatalf("TestBboltMlock: NewBboltFIFO got err == %s, want err == nil", err)
	}
	q, err := New[Number[int]](ctx, "test", bk, 0)
	if err != nil {
		t.Fatalf("TestBboltMlock: New got err == %s, want err == nil", err)
	}
	if ok, err := q.Push(ctx, []Number[int]{fifoItem(1)}); err != nil || !ok {
		t.Fatalf("TestBboltMlock: Push got (ok=%v err=%v)", ok, err)
	}
	if rem := pop(t, ctx, "mlock", q, 1); rem[0] != 1 {
		t.Errorf("TestBboltMlock: pop got %v, want [1]", rem)
	}
	if err := q.Close(ctx); err != nil {
		t.Errorf("TestBboltMlock: Close got err == %s, want err == nil", err)
	}
}

func TestBboltNoSync(t *testing.T) {
	ctx := t.Context()
	bk, err := NewBboltFIFO[Number[int]](ctx, diskRoot(t),
		WithNoSync(), WithNoFreelistSync(), WithNoGrowSync(),
		WithBoltPreLoadFreelist(), WithBoltFreelistMap(), WithBoltMmapFlags(0),
		WithBoltInitialMmapSize(1<<20), WithBoltPageSize(4096))
	if err != nil {
		t.Fatalf("TestBboltNoSync: NewBboltFIFO got err == %s, want err == nil", err)
	}
	q, err := New[Number[int]](ctx, "test", bk, 0)
	if err != nil {
		t.Fatalf("TestBboltNoSync: New got err == %s, want err == nil", err)
	}
	for i := 0; i < 50; i++ {
		if ok, err := q.Push(ctx, []Number[int]{fifoItem(i)}); err != nil || !ok {
			t.Fatalf("TestBboltNoSync: Push(%d) got (ok=%v err=%v)", i, ok, err)
		}
	}
	got := pop(t, ctx, "nosync", q, 50)
	for i, v := range got {
		if v != i {
			t.Fatalf("TestBboltNoSync: pop[%d] got %d, want %d", i, v, i)
		}
	}
	if err := q.Close(ctx); err != nil {
		t.Errorf("TestBboltNoSync: Close got err == %s, want err == nil", err)
	}
}

// TestBboltOpenFile verifies WithBoltOpenFile routes bbolt's file open through the
// caller-supplied function and the queue works end-to-end.
func TestBboltOpenFile(t *testing.T) {
	ctx := t.Context()
	calls := 0
	opener := func(name string, flag int, mode os.FileMode) (*os.File, error) {
		calls++
		return os.OpenFile(name, flag, mode)
	}
	bk, err := NewBboltFIFO[Number[int]](ctx, diskRoot(t), WithBoltOpenFile(opener))
	if err != nil {
		t.Fatalf("TestBboltOpenFile: NewBboltFIFO got err == %s, want err == nil", err)
	}
	q, err := New[Number[int]](ctx, "test", bk, 0)
	if err != nil {
		t.Fatalf("TestBboltOpenFile: New got err == %s, want err == nil", err)
	}
	if calls == 0 {
		t.Errorf("TestBboltOpenFile: custom OpenFile was never invoked")
	}
	for i := 0; i < 10; i++ {
		if ok, err := q.Push(ctx, []Number[int]{fifoItem(i)}); err != nil || !ok {
			t.Fatalf("TestBboltOpenFile: Push(%d) got (ok=%v err=%v)", i, ok, err)
		}
	}
	if got := pop(t, ctx, "openfile", q, 10); got[0] != 0 || got[9] != 9 {
		t.Errorf("TestBboltOpenFile: drained %v, want 0..9", got)
	}
	if err := q.Close(ctx); err != nil {
		t.Errorf("TestBboltOpenFile: Close got err == %s, want err == nil", err)
	}
}

// TestWithBTreeWidth verifies WithBTreeWidth is validated on the keyed-btree paths
// (indexed FIFO and priority): width < 2 is rejected there. On a plain (positional/btype)
// FIFO WithBTreeWidth is a no-op, so width < 2 is accepted.
func TestWithBTreeWidth(t *testing.T) {
	if _, err := NewBTreeFIFO[Number[int]](WithBTreeWidth(1)); err != nil {
		t.Errorf("TestWithBTreeWidth: NewBTreeFIFO(width=1) (no index, btype) got err == %s, want nil", err)
	}
	if _, err := NewBTreeFIFO[Number[int]](WithBTreeWidth(1), WithIndex()); err == nil {
		t.Errorf("TestWithBTreeWidth: NewBTreeFIFO(width=1, WithIndex) got err == nil, want err != nil")
	}
	if _, err := NewBTreePriority[Number[int]](WithBTreeWidth(1)); err == nil {
		t.Errorf("TestWithBTreeWidth: NewBTreePriority(width=1) got err == nil, want err != nil")
	}

	ctx := t.Context()
	bk, err := NewBTreeFIFO[Number[int]](WithBTreeWidth(8))
	if err != nil {
		t.Fatalf("TestWithBTreeWidth: NewBTreeFIFO(width=8) got err == %s, want err == nil", err)
	}
	q, err := New[Number[int]](ctx, "test", bk, 0)
	if err != nil {
		t.Fatalf("TestWithBTreeWidth: New got err == %s, want err == nil", err)
	}
	for i := 0; i < 5; i++ {
		if ok, err := q.Push(ctx, []Number[int]{fifoItem(i)}); err != nil || !ok {
			t.Fatalf("TestWithBTreeWidth: Push(%d) got (ok=%v err=%v), want (true,nil)", i, ok, err)
		}
	}
	got := pop(t, ctx, "width=8", q, 5)
	for i, v := range got {
		if v != i {
			t.Fatalf("TestWithBTreeWidth: pop[%d] got %d, want %d", i, v, i)
		}
	}
	if err := q.Close(ctx); err != nil {
		t.Errorf("TestWithBTreeWidth: Close got err == %s, want err == nil", err)
	}
}

// TestWithSideEffect verifies WithSideEffect runs after a successful op and that its error
// is surfaced without rolling back the op (the item is still pushed/popped).
func TestWithSideEffect(t *testing.T) {
	ctx := t.Context()
	bk, err := NewFIFO[Number[int]]()
	if err != nil {
		t.Fatalf("TestWithSideEffect: NewFIFO got err == %s, want err == nil", err)
	}
	q, err := New[Number[int]](ctx, "test", bk, 0)
	if err != nil {
		t.Fatalf("TestWithSideEffect: New got err == %s, want err == nil", err)
	}

	ran := false
	ok, err := q.Push(ctx, []Number[int]{fifoItem(1)}, WithSideEffect(func() error { ran = true; return nil }))
	switch {
	case err != nil || !ok:
		t.Errorf("TestWithSideEffect: Push got (ok=%v err=%v), want (true,nil)", ok, err)
	case !ran:
		t.Errorf("TestWithSideEffect: side effect did not run")
	}

	sentinel := errors.New("side effect failed")
	ok, err = q.Push(ctx, []Number[int]{fifoItem(2)}, WithSideEffect(func() error { return sentinel }))
	switch {
	case !errors.Is(err, sentinel):
		t.Errorf("TestWithSideEffect: Push got err == %v, want %v", err, sentinel)
	case !ok:
		t.Errorf("TestWithSideEffect: Push got ok == false, want true (item still pushed despite side-effect error)")
	}
	if q.Len() != 2 {
		t.Errorf("TestWithSideEffect: Len got %d, want 2 (both items pushed)", q.Len())
	}

	// Pop side effect runs and its error is surfaced; the item is still popped.
	items, err := q.Pop(ctx, 1, WithSideEffect(func() error { return sentinel }))
	switch {
	case !errors.Is(err, sentinel):
		t.Errorf("TestWithSideEffect: Pop got err == %v, want %v", err, sentinel)
	case len(items) != 1 || items[0].V != 1:
		t.Errorf("TestWithSideEffect: Pop got items == %v, want [1] (item still popped)", items)
	}
	if q.Len() != 1 {
		t.Errorf("TestWithSideEffect: Len after Pop got %d, want 1", q.Len())
	}
	if err := q.Close(ctx); err != nil {
		t.Errorf("TestWithSideEffect: Close got err == %s, want err == nil", err)
	}
}
