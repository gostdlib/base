package weak

import (
	"fmt"
	"testing"
	"time"
)

// fuzzBase is a fixed instant that ttlEntry deadlines are derived from. Offsets are added to it so the fuzzer can
// create many entries that share an identical expireAfter deadline (forcing the seq tiebreak to do work).
var fuzzBase = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

// fuzzDeadlineOffsets is a small set of offsets. Drawing deadlines from a small set makes equal deadlines common,
// which is exactly the case the seq tiebreak in ttlEntry.less exists to handle.
var fuzzDeadlineOffsets = [4]time.Duration{0, 1 * time.Hour, 2 * time.Hour, 3 * time.Hour}

// FuzzTTLEntryLess exercises ttlEntry.less and the expireAfter btree built exactly as New builds it. Each input byte
// becomes one ttlEntry (capped at 64) whose deadline is drawn from a small offset set (so equal deadlines are common)
// and whose seq comes from a monotonic counter, mirroring how set() assigns m.seq.Add(1). It checks that less is a
// strict weak order with the seq tiebreak (trichotomy), that all distinct-seq entries survive in the tree, and that
// ascending iteration yields non-decreasing deadlines.
func FuzzTTLEntryLess(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0})       // all-equal deadlines: every entry collides on expireAfter, seq must break every tie.
	f.Add([]byte{0, 1, 2, 3})       // all-distinct deadlines: comparator decides on expireAfter alone.
	f.Add([]byte{0, 0, 1, 1, 2, 2}) // mixed: pairs share deadlines.
	f.Add([]byte{})                 // empty: no entries.

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64 {
			data = data[:64]
		}

		entries := make([]ttlEntry[string, int], 0, len(data))
		for i, b := range data {
			offset := fuzzDeadlineOffsets[int(b)%len(fuzzDeadlineOffsets)]
			entries = append(entries, ttlEntry[string, int]{
				key:         fmt.Sprintf("k%d", i),
				expireAfter: fuzzBase.Add(offset),
				seq:         uint64(i + 1), // mirrors m.seq.Add(1): a distinct, monotonically increasing sequence.
				value:       nil,
			})
		}

		// (a) Trichotomy: for identical (expireAfter, seq) neither is less; otherwise exactly one is less.
		for i := range entries {
			for j := range entries {
				a, b := entries[i], entries[j]
				identical := a.expireAfter.Equal(b.expireAfter) && a.seq == b.seq
				lessAB := a.less(b)
				lessBA := b.less(a)
				switch {
				case identical && (lessAB || lessBA):
					t.Fatalf("FuzzTTLEntryLess: i=%d j=%d identical: got less(a,b)=%v less(b,a)=%v, want both false", i, j, lessAB, lessBA)
				case !identical && lessAB == lessBA:
					t.Fatalf("FuzzTTLEntryLess: i=%d j=%d distinct: got less(a,b)=%v less(b,a)=%v, want exactly one true", i, j, lessAB, lessBA)
				}
			}
		}

		// (b) Survival: insert every entry into a tree built exactly as New builds expireAfter. Distinct seqs mean no
		// comparator-equality replacement, so every entry survives and is retrievable.
		tree := newExpireAfterTree[string, int]()
		for i := range entries {
			tree.Set(entries[i])
		}
		if tree.Len() != len(entries) {
			t.Fatalf("FuzzTTLEntryLess: got tree.Len()=%d, want %d (a distinct-seq entry was dropped)", tree.Len(), len(entries))
		}
		for i := range entries {
			got, ok := tree.Get(entries[i])
			switch {
			case !ok:
				t.Fatalf("FuzzTTLEntryLess: entry i=%d (%v/%d) not found in tree", i, entries[i].expireAfter, entries[i].seq)
			case got.seq != entries[i].seq:
				t.Fatalf("FuzzTTLEntryLess: entry i=%d got seq=%d, want seq=%d", i, got.seq, entries[i].seq)
			}
		}

		// (c) Order: ascending iteration yields non-decreasing expireAfter.
		var prev time.Time
		first := true
		tree.Ascend(ttlEntry[string, int]{}, func(item ttlEntry[string, int]) bool {
			if !first && item.expireAfter.Before(prev) {
				t.Fatalf("FuzzTTLEntryLess: ascending walk out of order: %v before previous %v", item.expireAfter, prev)
			}
			prev = item.expireAfter
			first = false
			return true
		})
	})
}
