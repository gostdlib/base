package queue

import (
	"math"
	"testing"
)

// hashDefinedInt and hashDefinedFloat are defined (named) numeric types. NumberConstraint
// is constraints.Integer | constraints.Float, whose type sets use ~, so these satisfy it
// and Number[hashDefinedInt]/Number[hashDefinedFloat] are legal instantiations.
type hashDefinedInt int64
type hashDefinedFloat float64

// TestNumberHashDefinedTypes is a regression test: Number.Hash must key off the
// underlying numeric kind so defined types work. A concrete-type switch matches the
// exact dynamic type (hashDefinedInt != int64) and falls through to a panic; the
// reflect-based implementation (reflect.Kind reports the underlying kind) is correct.
func TestNumberHashDefinedTypes(t *testing.T) {
	a := Number[hashDefinedInt]{V: 5}
	b := Number[hashDefinedInt]{V: 5}
	c := Number[hashDefinedInt]{V: 6}
	if !a.Equal(b) {
		t.Fatalf("TestNumberHashDefinedTypes: a.Equal(b) == false, want true")
	}
	if a.Hash() != b.Hash() {
		t.Errorf("TestNumberHashDefinedTypes: equal defined-int values hashed differently: %d vs %d", a.Hash(), b.Hash())
	}
	if a.Hash() == c.Hash() {
		t.Errorf("TestNumberHashDefinedTypes: distinct defined-int values 5 and 6 hashed equally (%d)", a.Hash())
	}

	// -0.0 and +0.0 are Equal, so they must hash equally, for a defined float type too.
	negZero := Number[hashDefinedFloat]{V: hashDefinedFloat(math.Copysign(0, -1))}
	posZero := Number[hashDefinedFloat]{V: 0}
	if !negZero.Equal(posZero) {
		t.Fatalf("TestNumberHashDefinedTypes: negZero.Equal(posZero) == false, want true")
	}
	if negZero.Hash() != posZero.Hash() {
		t.Errorf("TestNumberHashDefinedTypes: defined-float -0.0 and +0.0 hashed differently: %d vs %d", negZero.Hash(), posZero.Hash())
	}

	// Exercise the index path (which calls Hash) end-to-end with a defined type.
	ctx := t.Context()
	bk, err := NewBTreeFIFO[Number[hashDefinedInt]](WithIndex())
	if err != nil {
		t.Fatalf("TestNumberHashDefinedTypes: NewBTreeFIFO got err == %s, want err == nil", err)
	}
	q, err := New[Number[hashDefinedInt]](ctx, "test", bk, Unlimited)
	if err != nil {
		t.Fatalf("TestNumberHashDefinedTypes: New got err == %s, want err == nil", err)
	}
	if ok, err := q.Push(ctx, []Number[hashDefinedInt]{{V: 99}}); err != nil || !ok {
		t.Fatalf("TestNumberHashDefinedTypes: Push got (ok=%v err=%v), want (true,nil)", ok, err)
	}
	exists, err := q.Exists(ctx, Number[hashDefinedInt]{V: 99})
	if err != nil {
		t.Fatalf("TestNumberHashDefinedTypes: Exists got err == %s, want err == nil", err)
	}
	if !exists {
		t.Errorf("TestNumberHashDefinedTypes: Exists(99) == false, want true")
	}
	if err := q.Close(ctx); err != nil {
		t.Errorf("TestNumberHashDefinedTypes: Close got err == %s, want err == nil", err)
	}
}
