package hashmap

import (
	"reflect"
	"testing"
	"time"
)

// hmBase is a fixed instant far in the future used to build stored/passed deadlines in FuzzDeleteIfMaxTTL. It sits
// far past time.Now so Get is never consulted here; only the stored<=passed comparison in DeleteIfMaxTTL matters.
var hmBase = time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)

// FuzzDeleteIfMaxTTL exercises Map.DeleteIfMaxTTL against its documented contract: it deletes iff the stored deadline
// is non-zero and not after the passed deadline (stored <= passed). storedOff/passedOff are folded into a small range
// so stored==passed happens often, and zeroStored selects the zero-deadline (never-delete) case.
func FuzzDeleteIfMaxTTL(f *testing.F) {
	f.Add(int64(0), int64(0), false) // stored==passed, non-zero: deletes.
	f.Add(int64(2), int64(1), false) // stored after passed: kept.
	f.Add(int64(1), int64(2), false) // stored before passed: deletes.
	f.Add(int64(0), int64(0), true)  // zero stored deadline: never deletes.

	f.Fuzz(func(t *testing.T, storedOff int64, passedOff int64, zeroStored bool) {
		// Fold offsets into a small range so stored==passed is common. Non-negative to keep hours sane.
		so := ((storedOff % 5) + 5) % 5
		po := ((passedOff % 5) + 5) % 5

		var stored time.Time
		if !zeroStored {
			stored = hmBase.Add(time.Duration(so) * time.Hour)
		}
		passed := hmBase.Add(time.Duration(po) * time.Hour)

		wantDeleted := !stored.IsZero() && !stored.After(passed)

		const key = "k"
		val := int(storedOff*31 + passedOff) // arbitrary but recoverable value to check prev on delete.

		m := New[string, int](0)
		m.Set(key, val, stored)
		if m.Len() != 1 {
			t.Fatalf("FuzzDeleteIfMaxTTL: after Set got Len()=%d, want 1 (stored=%v passed=%v)", m.Len(), stored, passed)
		}

		prev, deleted := m.DeleteIfMaxTTL(key, passed)

		switch {
		case deleted != wantDeleted:
			t.Fatalf("FuzzDeleteIfMaxTTL: stored=%v passed=%v zero=%v: got deleted=%v, want %v", stored, passed, zeroStored, deleted, wantDeleted)
		case deleted && prev != val:
			t.Fatalf("FuzzDeleteIfMaxTTL: stored=%v passed=%v: got prev=%d, want %d", stored, passed, prev, val)
		}

		wantLen := 1
		if wantDeleted {
			wantLen = 0
		}
		if m.Len() != wantLen {
			t.Fatalf("FuzzDeleteIfMaxTTL: stored=%v passed=%v deleted=%v: got Len()=%d, want %d", stored, passed, deleted, m.Len(), wantLen)
		}
	})
}

const (
	hmKeySpace = 16  // small key space to force hash collisions, probe chains, resize and shrink.
	hmMaxOps   = 256 // cap per-input work.
)

// hmFarFuture and hmFarPast bracket time.Now so a farPast deadline always reads as expired on Get and a farFuture one
// never does. The zero deadline never expires. All/Len/physical count do NOT honor expiry; only Get does. The
// reference model below mirrors exactly that: expired keys remain physically present (counted, iterated) and are only
// hidden from Get.
var (
	hmFarFuture = time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
	hmFarPast   = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
)

// FuzzHashmap is a differential fuzz of Map against a reference map[string]int plus a per-key deadline table. The
// input is an op stream: a header byte selects the deadline mode (all-zero, or per-Set deadline chosen by the value
// byte) and each following 3-byte chunk is one op (opcode, key, value) over a small key space. After every op the
// harness checks Len, All and touched-key Get against the reference, and that Get never mutates the map.
func FuzzHashmap(f *testing.F) {
	f.Add([]byte{0, 0, 0, 5, 2, 3, 1})                   // mode 0: Set then Delete.
	f.Add([]byte{1, 0, 1, 2, 2, 1, 0, 0, 3, 5})          // mode 1: per-Set deadlines including farPast/farFuture.
	f.Add([]byte{0, 0, 1, 1, 0, 2, 2, 0, 3, 3, 0, 4, 4}) // several distinct keys.
	f.Add([]byte{1, 0, 0, 2})                            // mode 1: single farPast Set (expired but physically present).
	f.Add([]byte{})                                      // empty.

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		mode := data[0] & 1
		rest := data[1:]

		m := New[string, int](0)
		ref := map[string]int{}          // physical presence: mirrors what Set stores and Delete removes, ignoring expiry.
		refTTL := map[string]time.Time{} // per-key deadline so the reference can model Get's expiry check.

		// refGet models Map.Get: a physically present key is a miss iff its deadline is non-zero and already past.
		refGet := func(key string) (int, bool) {
			v, ok := ref[key]
			if !ok {
				return 0, false
			}
			d := refTTL[key]
			if !d.IsZero() && time.Now().After(d) {
				return 0, false
			}
			return v, true
		}

		// deadlineFor picks a Set deadline. Mode 0 is always zero (no expiry); mode 1 varies by the value byte.
		deadlineFor := func(valByte byte) time.Time {
			if mode == 0 {
				return time.Time{}
			}
			switch valByte % 3 {
			case 1:
				return hmFarFuture
			case 2:
				return hmFarPast
			default:
				return time.Time{}
			}
		}

		// checkInvariants runs the after-every-op oracles: Len, All and a no-mutate Get probe on the touched key.
		checkInvariants := func(opIdx int, key string) {
			if m.Len() != len(ref) {
				t.Fatalf("FuzzHashmap: op %d: got Len()=%d, want %d", opIdx, m.Len(), len(ref))
			}

			got := map[string]int{}
			for k, v := range m.All() {
				if _, dup := got[k]; dup {
					t.Fatalf("FuzzHashmap: op %d: All() yielded key %q twice", opIdx, k)
				}
				got[k] = v
			}
			if !reflect.DeepEqual(got, ref) {
				t.Fatalf("FuzzHashmap: op %d: All() = %v, want %v", opIdx, got, ref)
			}

			lenBefore := m.Len()
			gv, gok := m.Get(key)
			if m.Len() != lenBefore {
				t.Fatalf("FuzzHashmap: op %d: Get(%q) mutated Len (%d -> %d)", opIdx, key, lenBefore, m.Len())
			}
			wv, wok := refGet(key)
			switch {
			case gok != wok:
				t.Fatalf("FuzzHashmap: op %d: Get(%q) ok=%v, want %v", opIdx, key, gok, wok)
			case gok && gv != wv:
				t.Fatalf("FuzzHashmap: op %d: Get(%q) value=%d, want %d", opIdx, key, gv, wv)
			}
		}

		opIdx := 0
		for p := 0; p+2 < len(rest) && opIdx < hmMaxOps; p += 3 {
			op := rest[p] % 5
			key := k(int(rest[p+1]) % hmKeySpace)
			valByte := rest[p+2]

			switch op {
			case 0: // Set
				val := int(valByte)
				deadline := deadlineFor(valByte)
				_, hadRef := ref[key]
				prev, replaced := m.Set(key, val, deadline)
				switch {
				case replaced != hadRef:
					t.Fatalf("FuzzHashmap: op %d: Set(%q) replaced=%v, want %v", opIdx, key, replaced, hadRef)
				case replaced && prev != ref[key]:
					t.Fatalf("FuzzHashmap: op %d: Set(%q) prev=%d, want %d", opIdx, key, prev, ref[key])
				}
				ref[key] = val
				refTTL[key] = deadline
			case 1: // Delete
				want, hadRef := ref[key]
				prev, deleted := m.Delete(key)
				switch {
				case deleted != hadRef:
					t.Fatalf("FuzzHashmap: op %d: Delete(%q) deleted=%v, want %v", opIdx, key, deleted, hadRef)
				case deleted && prev != want:
					t.Fatalf("FuzzHashmap: op %d: Delete(%q) prev=%d, want %d", opIdx, key, prev, want)
				}
				delete(ref, key)
				delete(refTTL, key)
			case 2: // Get (agreement checked via the invariant probe below)
			case 3: // Len-check (verified by the invariant probe)
			case 4: // All-check (verified by the invariant probe)
			}

			checkInvariants(opIdx, key)
			opIdx++
		}
	})
}
