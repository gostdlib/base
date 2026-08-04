package immutable

import (
	"slices"
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

// copierInt is an int that implements Copier, used to exercise the Copier path
// in CopySlice and CopyMap.
type copierInt int

func (c copierInt) Copy() copierInt {
	return c
}

func TestNewSlice(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want []int
	}{
		{
			name: "Success: nil slice",
			in:   nil,
			want: []int{},
		},
		{
			name: "Success: populated slice",
			in:   []int{1, 2, 3},
			want: []int{1, 2, 3},
		},
	}

	for _, test := range tests {
		s := NewSlice(test.in)
		if diff := pretty.Compare(test.want, s.Copy()); diff != "" {
			t.Errorf("TestNewSlice(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestSliceGet(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		i    int
		want int
	}{
		{
			name: "Success: first element",
			in:   []int{10, 20, 30},
			i:    0,
			want: 10,
		},
		{
			name: "Success: last element",
			in:   []int{10, 20, 30},
			i:    2,
			want: 30,
		},
	}

	for _, test := range tests {
		s := NewSlice(test.in)
		if got := s.Get(test.i); got != test.want {
			t.Errorf("TestSliceGet(%s): got %d, want %d", test.name, got, test.want)
		}
	}
}

func TestSliceLen(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want int
	}{
		{
			name: "Success: empty slice",
			in:   nil,
			want: 0,
		},
		{
			name: "Success: three elements",
			in:   []int{1, 2, 3},
			want: 3,
		},
	}

	for _, test := range tests {
		s := NewSlice(test.in)
		if got := s.Len(); got != test.want {
			t.Errorf("TestSliceLen(%s): got %d, want %d", test.name, got, test.want)
		}
	}
}

func TestSliceAll(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want []int
	}{
		{
			name: "Success: empty slice yields nothing",
			in:   nil,
			want: []int{},
		},
		{
			name: "Success: yields values in index order",
			in:   []int{5, 6, 7},
			want: []int{5, 6, 7},
		},
	}

	for _, test := range tests {
		s := NewSlice(test.in)
		got := []int{}
		wantIdx := 0
		for i, v := range s.All() {
			if i != wantIdx {
				t.Errorf("TestSliceAll(%s): got index %d, want %d", test.name, i, wantIdx)
			}
			wantIdx++
			got = append(got, v)
		}
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestSliceAll(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestUnsafeSlice(t *testing.T) {
	in := []int{1, 2, 3}
	s := NewSlice(in)
	got := UnsafeSlice(s)

	if diff := pretty.Compare(in, got); diff != "" {
		t.Errorf("TestUnsafeSlice: -want/+got:\n%s", diff)
	}

	// UnsafeSlice returns the underlying slice, so a mutation is visible via Get.
	got[0] = 99
	if s.Get(0) != 99 {
		t.Errorf("TestUnsafeSlice: got %d, want 99 (UnsafeSlice must expose the backing array)", s.Get(0))
	}
}

func TestNewMap(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]int
		want map[string]int
	}{
		{
			name: "Success: nil map",
			in:   nil,
			want: map[string]int{},
		},
		{
			name: "Success: populated map",
			in:   map[string]int{"a": 1, "b": 2},
			want: map[string]int{"a": 1, "b": 2},
		},
	}

	for _, test := range tests {
		m := NewMap(test.in)
		if diff := pretty.Compare(test.want, m.Copy()); diff != "" {
			t.Errorf("TestNewMap(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestMapGet(t *testing.T) {
	tests := []struct {
		name   string
		in     map[string]int
		key    string
		want   int
		wantOK bool
	}{
		{
			name:   "Success: present key",
			in:     map[string]int{"a": 1},
			key:    "a",
			want:   1,
			wantOK: true,
		},
		{
			name:   "Success: absent key",
			in:     map[string]int{"a": 1},
			key:    "b",
			want:   0,
			wantOK: false,
		},
	}

	for _, test := range tests {
		m := NewMap(test.in)
		got, ok := m.Get(test.key)
		switch {
		case ok != test.wantOK:
			t.Errorf("TestMapGet(%s): got ok == %t, want ok == %t", test.name, ok, test.wantOK)
		case got != test.want:
			t.Errorf("TestMapGet(%s): got %d, want %d", test.name, got, test.want)
		}
	}
}

func TestMapLen(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]int
		want int
	}{
		{
			name: "Success: empty map",
			in:   nil,
			want: 0,
		},
		{
			name: "Success: two entries",
			in:   map[string]int{"a": 1, "b": 2},
			want: 2,
		},
	}

	for _, test := range tests {
		m := NewMap(test.in)
		if got := m.Len(); got != test.want {
			t.Errorf("TestMapLen(%s): got %d, want %d", test.name, got, test.want)
		}
	}
}

func TestMapAll(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]int
		want map[string]int
	}{
		{
			name: "Success: empty map yields nothing",
			in:   nil,
			want: map[string]int{},
		},
		{
			name: "Success: yields every entry",
			in:   map[string]int{"a": 1, "b": 2, "c": 3},
			want: map[string]int{"a": 1, "b": 2, "c": 3},
		},
	}

	for _, test := range tests {
		m := NewMap(test.in)
		got := map[string]int{}
		for k, v := range m.All() {
			got[k] = v
		}
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestMapAll(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestUnsafeMap(t *testing.T) {
	in := map[string]int{"a": 1}
	m := NewMap(in)
	got := UnsafeMap(m)

	if diff := pretty.Compare(in, got); diff != "" {
		t.Errorf("TestUnsafeMap: -want/+got:\n%s", diff)
	}

	// UnsafeMap returns the underlying map, so a mutation is visible via Get.
	got["a"] = 99
	if v, _ := m.Get("a"); v != 99 {
		t.Errorf("TestUnsafeMap: got %d, want 99 (UnsafeMap must expose the backing map)", v)
	}
}

func TestCopySlice(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want []int
	}{
		{
			name: "Success: nil slice",
			in:   nil,
			want: []int{},
		},
		{
			name: "Success: values copied",
			in:   []int{1, 2, 3},
			want: []int{1, 2, 3},
		},
	}

	for _, test := range tests {
		got := CopySlice(test.in)
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestCopySlice(%s): -want/+got:\n%s", test.name, diff)
		}
	}

	// A copy must be independent of the source.
	src := []int{1, 2, 3}
	cp := CopySlice(src)
	src[0] = 99
	if cp[0] == 99 {
		t.Errorf("TestCopySlice: copy shares backing array with source")
	}
}

func TestCopySliceCopier(t *testing.T) {
	src := []copierInt{1, 2, 3}
	got := CopySlice(src)
	want := []copierInt{1, 2, 3}
	if diff := pretty.Compare(want, got); diff != "" {
		t.Errorf("TestCopySliceCopier: -want/+got:\n%s", diff)
	}
}

func TestCopyMap(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]int
		want map[string]int
	}{
		{
			name: "Success: nil map",
			in:   nil,
			want: map[string]int{},
		},
		{
			name: "Success: entries copied",
			in:   map[string]int{"a": 1, "b": 2},
			want: map[string]int{"a": 1, "b": 2},
		},
	}

	for _, test := range tests {
		got := CopyMap(test.in)
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestCopyMap(%s): -want/+got:\n%s", test.name, diff)
		}
	}

	// A copy must be independent of the source.
	src := map[string]int{"a": 1}
	cp := CopyMap(src)
	src["a"] = 99
	if cp["a"] == 99 {
		t.Errorf("TestCopyMap: copy shares state with source")
	}
}

func TestCopyMapCopier(t *testing.T) {
	src := map[string]copierInt{"a": 1, "b": 2}
	got := CopyMap(src)
	want := map[string]copierInt{"a": 1, "b": 2}
	if diff := pretty.Compare(want, got); diff != "" {
		t.Errorf("TestCopyMapCopier: -want/+got:\n%s", diff)
	}
}

func TestNewSet(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "Success: nil slice",
			in:   nil,
			want: nil,
		},
		{
			name: "Success: populated slice",
			in:   []string{"a", "b", "c"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "Success: duplicate values are collapsed",
			in:   []string{"a", "b", "a", "b"},
			want: []string{"a", "b"},
		},
	}

	for _, test := range tests {
		s := NewSet(test.in)
		got := s.Members()
		slices.Sort(got)
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestNewSet(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestSetLen(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want int
	}{
		{
			name: "Success: empty set",
			in:   nil,
			want: 0,
		},
		{
			name: "Success: duplicates only count once",
			in:   []string{"a", "b", "a"},
			want: 2,
		},
	}

	for _, test := range tests {
		s := NewSet(test.in)
		if got := s.Len(); got != test.want {
			t.Errorf("TestSetLen(%s): got %d, want %d", test.name, got, test.want)
		}
	}
}

func TestSetContains(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		v    string
		want bool
	}{
		{
			name: "Success: present value",
			in:   []string{"a", "b"},
			v:    "a",
			want: true,
		},
		{
			name: "Success: absent value",
			in:   []string{"a", "b"},
			v:    "c",
			want: false,
		},
		{
			name: "Success: empty set contains nothing",
			in:   nil,
			v:    "a",
			want: false,
		},
	}

	for _, test := range tests {
		s := NewSet(test.in)
		if got := s.Contains(test.v); got != test.want {
			t.Errorf("TestSetContains(%s): got %t, want %t", test.name, got, test.want)
		}
	}
}

func TestSetAll(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "Success: empty set yields nothing",
			in:   nil,
			want: []string{},
		},
		{
			name: "Success: yields every member",
			in:   []string{"a", "b", "c"},
			want: []string{"a", "b", "c"},
		},
	}

	for _, test := range tests {
		s := NewSet(test.in)
		got := []string{}
		for v := range s.All() {
			got = append(got, v)
		}
		slices.Sort(got)
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestSetAll(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestSetMembers(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "Success: empty set returns nil",
			in:   nil,
			want: nil,
		},
		{
			name: "Success: all members returned",
			in:   []string{"a", "b", "c"},
			want: []string{"a", "b", "c"},
		},
	}

	for _, test := range tests {
		s := NewSet(test.in)
		got := s.Members()
		slices.Sort(got)
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestSetMembers(%s): -want/+got:\n%s", test.name, diff)
		}
	}

	// Members returns a copy, so mutating it must not affect the Set.
	s := NewSet([]string{"a"})
	members := s.Members()
	members[0] = "z"
	if !s.Contains("a") || s.Contains("z") {
		t.Errorf("TestSetMembers: mutating the returned slice affected the Set")
	}
}

func TestSetString(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{
			name: "Success: empty set",
			in:   nil,
			want: "[]",
		},
		{
			name: "Success: single member",
			in:   []string{"a"},
			want: "[a]",
		},
	}

	for _, test := range tests {
		s := NewSet(test.in)
		if got := s.String(); got != test.want {
			t.Errorf("TestSetString(%s): got %q, want %q", test.name, got, test.want)
		}
	}
}

func TestSetUnion(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []string
	}{
		{
			name: "Success: disjoint sets",
			a:    []string{"a", "b"},
			b:    []string{"c", "d"},
			want: []string{"a", "b", "c", "d"},
		},
		{
			name: "Success: overlapping sets",
			a:    []string{"a", "b"},
			b:    []string{"b", "c"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "Success: union with empty set",
			a:    []string{"a"},
			b:    nil,
			want: []string{"a"},
		},
	}

	for _, test := range tests {
		a := NewSet(test.a)
		b := NewSet(test.b)
		got := a.Union(b).Members()
		slices.Sort(got)
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestSetUnion(%s): -want/+got:\n%s", test.name, diff)
		}
	}

	// Union must not modify its operands.
	a := NewSet([]string{"a"})
	b := NewSet([]string{"b"})
	a.Union(b)
	switch {
	case a.Len() != 1 || a.Contains("b"):
		t.Errorf("TestSetUnion: Union modified the receiver")
	case b.Len() != 1 || b.Contains("a"):
		t.Errorf("TestSetUnion: Union modified the argument")
	}
}

func TestSetIntersection(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []string
	}{
		{
			name: "Success: overlapping sets",
			a:    []string{"a", "b", "c"},
			b:    []string{"b", "c", "d"},
			want: []string{"b", "c"},
		},
		{
			name: "Success: disjoint sets yield empty set",
			a:    []string{"a"},
			b:    []string{"b"},
			want: nil,
		},
		{
			name: "Success: intersection with empty set",
			a:    []string{"a"},
			b:    nil,
			want: nil,
		},
		{
			name: "Success: larger set as receiver",
			a:    []string{"a", "b", "c", "d"},
			b:    []string{"b"},
			want: []string{"b"},
		},
	}

	for _, test := range tests {
		a := NewSet(test.a)
		b := NewSet(test.b)
		got := a.Intersection(b).Members()
		slices.Sort(got)
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestSetIntersection(%s): -want/+got:\n%s", test.name, diff)
		}
	}

	// Intersection must not modify its operands.
	a := NewSet([]string{"a", "b"})
	b := NewSet([]string{"b"})
	a.Intersection(b)
	switch {
	case a.Len() != 2:
		t.Errorf("TestSetIntersection: Intersection modified the receiver")
	case b.Len() != 1 || b.Contains("a"):
		t.Errorf("TestSetIntersection: Intersection modified the argument")
	}
}
