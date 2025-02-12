package immutable

import (
	"testing"

	"github.com/gostdlib/base/values/immutable/unsafe"

	"github.com/kylelemons/godebug/pretty"
)

func TestMapLen(t *testing.T) {
	m := NewMap(map[string]string{"hello": "world", "foo": "bar"})
	if m.Len() != 2 {
		t.Errorf("TestMap: got %v, want 2", m.Len())
	}
}

func TestMapCopy(t *testing.T) {
	m := NewMap(map[string]string{"hello": "world", "foo": "bar"})
	cp := m.Copy()
	for i, v := range m.All() {
		if v != cp[i] {
			t.Fatalf("TestMapCopy: not all values copied")
		}
	}
	cp["hello"] = "modified"
	if v, _ := m.Get("hello"); v == "modified" {
		t.Errorf("TestMapCopy: copy affected original")
	}
}

func TestUnsafeMap(t *testing.T) {
	sl := NewMap(map[string]string{"hello": "world", "foo": "bar"})

	real := unsafe.Map(sl)
	real["hello"] = "modified"
	if v, _ := sl.Get("hello"); v != "modified" {
		t.Errorf("TestUnsafeMap:  Unsafe didn't modify original")
	}
}

func TestSliceLen(t *testing.T) {
	sl := NewSlice([]string{"value1", "value2"})
	if sl.Len() != 2 {
		t.Errorf("TestSlice: got %v, want 2", sl.Len())
	}
}

func TestSliceCopy(t *testing.T) {
	sl := NewSlice([]string{"value1", "value2"})
	cp := sl.Copy()
	for i, v := range sl.All() {
		if v != cp[i] {
			t.Fatalf("TestSliceCopy: not all values copied")
		}
	}
	cp[0] = "modified"
	if sl.Get(0) == cp[0] {
		t.Errorf("TestSliceCopy: copy affected original")
	}
}

func TestUnsafeSlice(t *testing.T) {
	sl := NewSlice([]string{"value1", "value2"})

	real := unsafe.Slice(sl)
	real[0] = "modified"
	if sl.Get(0) != "modified" {
		t.Errorf("TestUnsafeSlice: Unsafe didn't modify original")
	}
}

func TestCopySlice(t *testing.T) {
	s := []string{"value1", "value2"}
	copied := CopySlice(s)

	if diff := pretty.Compare(s, copied); diff != "" {
		t.Errorf("TestCopySlice: -want/+got:\n%s", diff)
	}

	s[0] = "modified"
	if diff := pretty.Compare(s, copied); diff == "" {
		t.Errorf("TestCopySlice: both slices should not be equal after modification")
	}
}

func TestCopyMap(t *testing.T) {
	m := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	copied := CopyMap(m)

	if diff := pretty.Compare(m, copied); diff != "" {
		t.Errorf("TestCopyMap: -want/+got:\n%s", diff)
	}

	m["key1"] = "modified"
	if diff := pretty.Compare(m, copied); diff == "" {
		t.Errorf("TestCopyMap: both maps should not be equal after modification")
	}
}
