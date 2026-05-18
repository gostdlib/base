package queue

import (
	"context"
	"errors"
	"os"
	"sort"
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

// Tests use Number[int] as the Item: V is the value (identity), P the priority. A
// priority backing requires P > 0; a FIFO backing requires P == 0.

// fifoItem builds a pushable item for a FIFO backing (priority must be 0).
func fifoItem(v int) Number[int] { return Number[int]{V: v} }

// prioItem builds a pushable item for a priority backing. P is strictly increasing in v
// and > 0, so pop-by-priority equals ascending value order.
func prioItem(v int) Number[int] { return Number[int]{V: v, P: uint64(v) + 1} }

// queryItem builds an item for Exists/Del lookups; only V (Equal/Hash) is consulted.
func queryItem(v int) Number[int] { return Number[int]{V: v} }

// itemFor builds a pushable item for the given backing kind.
func itemFor(priority bool, v int) Number[int] {
	if priority {
		return prioItem(v)
	}
	return fifoItem(v)
}

type backingConfig struct {
	name     string
	priority bool
	newQ     func(t *testing.T, ctx context.Context) (*Queue[Number[int]], error)
}

func diskRoot(t *testing.T) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatalf("os.OpenRoot: %v", err)
	}
	return root
}

func backingConfigs() []backingConfig {
	base := []struct {
		name     string
		priority bool
		// make builds the backing and returns it with the queue's max size. withIndex
		// selects an indexed (and thus tree/bbolt) backing.
		make func(t *testing.T, ctx context.Context, withIndex bool) (int, Backing[Number[int]], error)
	}{
		{"in-memory FIFO bounded", false, func(t *testing.T, ctx context.Context, idx bool) (int, Backing[Number[int]], error) {
			b, err := memFIFO(idx)
			return 100, b, err
		}},
		{"in-memory FIFO unbounded", false, func(t *testing.T, ctx context.Context, idx bool) (int, Backing[Number[int]], error) {
			b, err := memFIFO(idx)
			return 0, b, err
		}},
		{"in-memory priority bounded", true, func(t *testing.T, ctx context.Context, idx bool) (int, Backing[Number[int]], error) {
			b, err := memPriority(idx)
			return 100, b, err
		}},
		{"in-memory priority unbounded", true, func(t *testing.T, ctx context.Context, idx bool) (int, Backing[Number[int]], error) {
			b, err := memPriority(idx)
			return 0, b, err
		}},
		{"on-disk FIFO", false, func(t *testing.T, ctx context.Context, idx bool) (int, Backing[Number[int]], error) {
			b, err := NewBboltFIFO[Number[int]](ctx, diskRoot(t), indexOpts(idx)...)
			return 100, b, err
		}},
		{"on-disk priority", true, func(t *testing.T, ctx context.Context, idx bool) (int, Backing[Number[int]], error) {
			b, err := NewBboltPriority[Number[int]](ctx, diskRoot(t), indexOpts(idx)...)
			return 100, b, err
		}},
	}

	var cfgs []backingConfig
	for _, b := range base {
		for _, withIndex := range []bool{false, true} {
			b, withIndex := b, withIndex
			name := b.name
			if withIndex {
				name += " +index"
			}
			cfgs = append(cfgs, backingConfig{
				name:     name,
				priority: b.priority,
				newQ: func(t *testing.T, ctx context.Context) (*Queue[Number[int]], error) {
					maxSize, backing, err := b.make(t, ctx, withIndex)
					if err != nil {
						return nil, err
					}
					return New[Number[int]](ctx, backing, maxSize)
				},
			})
		}
	}
	return cfgs
}

// indexOpts returns WithIndex when idx is set, else no options.
func indexOpts(idx bool) []BackingOption {
	if idx {
		return []BackingOption{WithIndex()}
	}
	return nil
}

// memFIFO returns an in-memory FIFO backing; an indexed one uses the B-Tree backing since
// the slice backing has no index.
func memFIFO(idx bool) (Backing[Number[int]], error) {
	if idx {
		return NewBTreeFIFO[Number[int]](WithIndex())
	}
	return NewFIFO[Number[int]]()
}

// memPriority returns an in-memory priority backing; an indexed one uses the B-Tree backing
// since the heap backing has no index.
func memPriority(idx bool) (Backing[Number[int]], error) {
	if idx {
		return NewBTreePriority[Number[int]](WithIndex())
	}
	return NewPriority[Number[int]]()
}

// popN pops exactly n items. PopN blocks until >=1 and returns up to the requested count,
// so loop until n are collected (the caller knows n items will arrive).
func popN(t *testing.T, ctx context.Context, name string, q *Queue[Number[int]], n int) []int {
	t.Helper()
	var got []int
	for len(got) < n {
		items, err := q.PopN(ctx, n-len(got))
		if err != nil {
			t.Fatalf("TestBackingsConformance(%s): PopN got err == %s, want err == nil", name, err)
		}
		for _, v := range items {
			got = append(got, v.V)
		}
	}
	return got
}

func wantOrder(priority bool, pushed []int) []int {
	out := append([]int(nil), pushed...)
	if priority {
		sort.Ints(out)
	}
	return out
}

func TestBackingsConformance(t *testing.T) {
	for _, test := range backingConfigs() {
		ctx := t.Context()

		q, err := test.newQ(t, ctx)
		if err != nil {
			t.Fatalf("TestBackingsConformance(%s): New got err == %s, want err == nil", test.name, err)
		}

		pushed := []int{3, 1, 2, 2}
		for _, n := range pushed {
			ok, err := q.Push(ctx, []Number[int]{itemFor(test.priority, n)})
			switch {
			case err != nil:
				t.Fatalf("TestBackingsConformance(%s): Push(%d) got err == %s, want err == nil", test.name, n, err)
			case !ok:
				t.Fatalf("TestBackingsConformance(%s): Push(%d) got ok == false, want true", test.name, n)
			}
		}

		if got := q.Len(); got != int64(len(pushed)) {
			t.Errorf("TestBackingsConformance(%s): Len got %d, want %d", test.name, got, len(pushed))
		}

		head, ok, err := q.Peek(ctx)
		wantHead := wantOrder(test.priority, pushed)[0]
		switch {
		case err != nil:
			t.Errorf("TestBackingsConformance(%s): Peek got err == %s, want err == nil", test.name, err)
		case !ok:
			t.Errorf("TestBackingsConformance(%s): Peek got ok == false, want true", test.name)
		case head.V != wantHead:
			t.Errorf("TestBackingsConformance(%s): Peek got %d, want %d", test.name, head.V, wantHead)
		}

		for _, tc := range []struct {
			v    int
			want bool
		}{{2, true}, {3, true}, {99, false}} {
			got, err := q.Exists(ctx, queryItem(tc.v))
			switch {
			case err != nil:
				t.Errorf("TestBackingsConformance(%s): Exists(%d) got err == %s, want err == nil", test.name, tc.v, err)
			case got != tc.want:
				t.Errorf("TestBackingsConformance(%s): Exists(%d) got %v, want %v", test.name, tc.v, got, tc.want)
			}
		}

		want := wantOrder(test.priority, pushed)
		got := popN(t, ctx, test.name, q, len(want))
		if diff := pretty.Compare(want, got); diff != "" {
			t.Errorf("TestBackingsConformance(%s): pop order -want +got:\n%s", test.name, diff)
		}

		if n := q.Len(); n != 0 {
			t.Errorf("TestBackingsConformance(%s): Len after drain got %d, want 0", test.name, n)
		}
		if _, ok, err := q.Peek(ctx); err != nil || ok {
			t.Errorf("TestBackingsConformance(%s): Peek on empty got (ok=%v err=%v), want (false,nil)", test.name, ok, err)
		}

		// Del removes ALL Equal matches.
		for _, n := range []int{7, 7, 8} {
			if _, err := q.Push(ctx, []Number[int]{itemFor(test.priority, n)}); err != nil {
				t.Fatalf("TestBackingsConformance(%s): Push(%d) got err == %s", test.name, n, err)
			}
		}
		if err := q.Del(ctx, []Number[int]{queryItem(7)}); err != nil {
			t.Errorf("TestBackingsConformance(%s): Del(7) got err == %s, want err == nil", test.name, err)
		}
		if n := q.Len(); n != 1 {
			t.Errorf("TestBackingsConformance(%s): Len after Del got %d, want 1", test.name, n)
		}
		if ex, err := q.Exists(ctx, queryItem(7)); err != nil || ex {
			t.Errorf("TestBackingsConformance(%s): Exists(7) after Del got (%v,%v), want (false,nil)", test.name, ex, err)
		}
		if rem := popN(t, ctx, test.name, q, 1); len(rem) != 1 || rem[0] != 8 {
			t.Errorf("TestBackingsConformance(%s): remaining after Del got %v, want [8]", test.name, rem)
		}

		// Batch Del removes every item Equal to any element of v; a duplicate element
		// (7 listed twice) is idempotent and still removes all matching items.
		for _, n := range []int{7, 7, 8, 9, 9} {
			if _, err := q.Push(ctx, []Number[int]{itemFor(test.priority, n)}); err != nil {
				t.Fatalf("TestBackingsConformance(%s): Push(%d) got err == %s", test.name, n, err)
			}
		}
		if err := q.Del(ctx, []Number[int]{queryItem(7), queryItem(9), queryItem(7)}); err != nil {
			t.Errorf("TestBackingsConformance(%s): batch Del got err == %s, want err == nil", test.name, err)
		}
		if n := q.Len(); n != 1 {
			t.Errorf("TestBackingsConformance(%s): Len after batch Del got %d, want 1", test.name, n)
		}
		for _, miss := range []int{7, 9} {
			if ex, err := q.Exists(ctx, queryItem(miss)); err != nil || ex {
				t.Errorf("TestBackingsConformance(%s): Exists(%d) after batch Del got (%v,%v), want (false,nil)", test.name, miss, ex, err)
			}
		}

		// Empty and nil v are no-ops that do not error or change Len.
		for _, empty := range [][]Number[int]{nil, {}} {
			if err := q.Del(ctx, empty); err != nil {
				t.Errorf("TestBackingsConformance(%s): Del(empty) got err == %s, want err == nil", test.name, err)
			}
		}
		if n := q.Len(); n != 1 {
			t.Errorf("TestBackingsConformance(%s): Len after empty Del got %d, want 1", test.name, n)
		}
		if rem := popN(t, ctx, test.name, q, 1); len(rem) != 1 || rem[0] != 8 {
			t.Errorf("TestBackingsConformance(%s): remaining after batch Del got %v, want [8]", test.name, rem)
		}

		// RangeAll yields in queue order.
		rangePush := []int{11, 10}
		for _, n := range rangePush {
			if _, err := q.Push(ctx, []Number[int]{itemFor(test.priority, n)}); err != nil {
				t.Fatalf("TestBackingsConformance(%s): Push(%d) got err == %s", test.name, n, err)
			}
		}
		var ranged []int
		for v, err := range q.RangeAll(ctx) {
			if err != nil {
				t.Fatalf("TestBackingsConformance(%s): RangeAll got err == %s", test.name, err)
			}
			ranged = append(ranged, v.V)
		}
		if diff := pretty.Compare(wantOrder(test.priority, rangePush), ranged); diff != "" {
			t.Errorf("TestBackingsConformance(%s): RangeAll order -want +got:\n%s", test.name, diff)
		}

		if err := q.Clear(ctx); err != nil {
			t.Errorf("TestBackingsConformance(%s): Clear got err == %s, want err == nil", test.name, err)
		}
		if n := q.Len(); n != 0 {
			t.Errorf("TestBackingsConformance(%s): Len after Clear got %d, want 0", test.name, n)
		}

		if err := q.Close(ctx); err != nil {
			t.Errorf("TestBackingsConformance(%s): Close got err == %s, want err == nil", test.name, err)
		}
	}
}

// TestKindValidation verifies a priority backing rejects items with Priority() == 0 and a
// FIFO backing rejects items with Priority() > 0.
func TestKindValidation(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name    string
		backing func() (Backing[Number[int]], error)
		item    Number[int]
		wantErr error
	}{
		{
			name:    "Error: priority backing rejects Priority()==0",
			backing: func() (Backing[Number[int]], error) { return NewPriority[Number[int]]() },
			item:    Number[int]{V: 1, P: 0},
			wantErr: ErrPriorityRequired,
		},
		{
			name:    "Error: btree priority backing rejects Priority()==0",
			backing: func() (Backing[Number[int]], error) { return NewBTreePriority[Number[int]]() },
			item:    Number[int]{V: 1, P: 0},
			wantErr: ErrPriorityRequired,
		},
		{
			name:    "Error: FIFO backing rejects Priority()>0",
			backing: func() (Backing[Number[int]], error) { return NewFIFO[Number[int]]() },
			item:    Number[int]{V: 1, P: 5},
			wantErr: ErrPriorityNotAllowed,
		},
		{
			name:    "Error: btree FIFO backing rejects Priority()>0",
			backing: func() (Backing[Number[int]], error) { return NewBTreeFIFO[Number[int]]() },
			item:    Number[int]{V: 1, P: 5},
			wantErr: ErrPriorityNotAllowed,
		},
	}

	for _, test := range tests {
		b, err := test.backing()
		if err != nil {
			t.Fatalf("TestKindValidation(%s): backing got err == %s, want err == nil", test.name, err)
		}
		q, err := New[Number[int]](ctx, b, 0)
		if err != nil {
			t.Fatalf("TestKindValidation(%s): New got err == %s, want err == nil", test.name, err)
		}
		_, err = q.Push(ctx, []Number[int]{test.item})
		if !errors.Is(err, test.wantErr) {
			t.Errorf("TestKindValidation(%s): Push got err == %v, want %v", test.name, err, test.wantErr)
		}
		q.Close(ctx)
	}
}
