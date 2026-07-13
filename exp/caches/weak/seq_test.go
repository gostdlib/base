package weak

import (
	"testing"
	"time"
)

// TestTTLEntryDeadlineTiebreak is a white-box regression for the deadline-collision tiebreak. Two distinct keys
// that share an identical expireAfter deadline must both survive in the expireAfter tree; without a seq tiebreak
// the comparator treats them as equal and tidwall/btree replaces the first with the second, silently dropping a
// key from forced eviction.
func TestTTLEntryDeadlineTiebreak(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: entries with identical expireAfter but distinct seq both survive in the expireAfter tree",
		},
	}

	for _, test := range tests {
		// Build the tree exactly as New builds the expireAfter tree.
		tree := newExpireAfterTree[string, int]()

		deadline := time.Now()
		tree.Set(ttlEntry[string, int]{expireAfter: deadline, key: "a", seq: 1})
		tree.Set(ttlEntry[string, int]{expireAfter: deadline, key: "b", seq: 2})

		got := map[string]bool{}
		tree.Ascend(ttlEntry[string, int]{}, func(item ttlEntry[string, int]) bool {
			got[item.key] = true
			return true
		})

		if len(got) != 2 || !got["a"] || !got["b"] {
			t.Errorf("TestTTLEntryDeadlineTiebreak(%s): got surviving keys %v, want both \"a\" and \"b\"", test.name, got)
		}
	}
}
