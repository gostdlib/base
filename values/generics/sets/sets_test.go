package sets

import "testing"

func TestNew(t *testing.T) {
	s := New(1, 2, 3)
	if len(s) != 3 {
		t.Errorf("Expected set length 3, got %d", len(s))
	}
	if !s.Contains(1) || !s.Contains(2) || !s.Contains(3) {
		t.Errorf("Set does not contain expected elements")
	}
}

func TestAdd(t *testing.T) {
	s := New[int]()
	s.Add(1, 2, 3)
	if len(s) != 3 {
		t.Errorf("Expected set length 3, got %d", len(s))
	}
	if !s.Contains(1) || !s.Contains(2) || !s.Contains(3) {
		t.Errorf("Set does not contain expected elements")
	}
}

func TestRemove(t *testing.T) {
	s := New(1, 2, 3)
	s.Remove(2)
	if len(s) != 2 {
		t.Errorf("Expected set length 2, got %d", len(s))
	}
	if s.Contains(2) {
		t.Errorf("Set still contains removed element")
	}
}

func TestContains(t *testing.T) {
	s := New(1, 2, 3)
	if !s.Contains(1) || !s.Contains(2) || !s.Contains(3) {
		t.Errorf("Set does not contain expected elements")
	}
	if s.Contains(4) {
		t.Errorf("Set contains unexpected element")
	}
}

func TestMembers(t *testing.T) {
	s := New(1, 2, 3)
	members := s.Members()
	if len(members) != 3 {
		t.Errorf("Expected members length 3, got %d", len(members))
	}
	expected := map[int]bool{1: true, 2: true, 3: true}
	for _, v := range members {
		if !expected[v] {
			t.Errorf("Unexpected member %d", v)
		}
	}
}

func TestString(t *testing.T) {
	s := New(1, 2, 3)
	str := s.String()
	expected := "[1 2 3]"
	if str != expected {
		t.Errorf("Expected string %s, got %s", expected, str)
	}
}

func TestUnion(t *testing.T) {
	s1 := New(1, 2, 3)
	s2 := New(3, 4, 5)
	union := s1.Union(s2)
	if len(union) != 5 {
		t.Errorf("Expected union length 5, got %d", len(union))
	}
	expected := map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true}
	for _, v := range union.Members() {
		if !expected[v] {
			t.Errorf("Unexpected member %d in union", v)
		}
	}
}

func TestIntersection(t *testing.T) {
	s1 := New(1, 2, 3)
	s2 := New(3, 4, 5)
	intersection := s1.Intersection(s2)
	if len(intersection) != 1 {
		t.Errorf("Expected intersection length 1, got %d", len(intersection))
	}
	if !intersection.Contains(3) {
		t.Errorf("Intersection does not contain expected element 3")
	}
}
