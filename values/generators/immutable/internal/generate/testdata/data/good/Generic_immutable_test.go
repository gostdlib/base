package data

import (
	"log"
	"reflect"
	"testing"
	"unsafe"

	"github.com/kr/pretty"
)

func TestImGeneric(t *testing.T) {
	g := Generic[int, string]{}
	setAllFields(&g)
	log.Println(pretty.Sprint(g))

	im := g.Immutable()

	if im.GetID() != g.ID {
		t.Errorf("TestImGeneric(Immutable): expected ID to be 1, got %d", im.GetID())
	}
	if im.GetName() != g.Name {
		t.Errorf("TestImGeneric(Immutable): expected Name to be 'name', got '%s'", im.GetName())
	}
	if im.GetEmail() != g.Email {
		t.Errorf("TestImGeneric(Immutable): expected Email to be 'email', got '%s'", im.GetEmail())
	}
	if v, ok := im.GetTags().Get("tag"); !ok || v != struct{}{} {
		t.Errorf("TestImGeneric(Immutable): expected Tags to contain 'tag', got '%v'", im.GetTags())
	}
	for i, v := range im.GetSlices().All() {
		if v != g.Slices[i] {
			t.Errorf("TestImGeneric(Immutable): expected Slices to contain '%d', got '%d'", g.Slices[i], v)
		}
	}
	if im.GetSubData() != g.SubData {
		t.Errorf("TestImGeneric(Immutable): expected SubData to be 1, got %d", im.GetSubData())
	}
	if im.GetComp() != g.Comp {
		t.Errorf("TestImGeneric(Immutable): expected Comp to be 'comp', got '%s'", im.GetComp())
	}
	if im.private != g.private {
		t.Errorf("TestImGeneric(Immutable): expected Private to be 'private', got '%s'", im.private)
	}

	// Prove that setting something on the original doesn't alter the immutable.
	g.ID = 2
	if im.GetID() == g.ID {
		t.Errorf("TestImGeneric(Immutable): expected ID to be 1, got %d", im.GetID())
	}

	// Prove that a setter on the immutable makes a copy.
	imCopy := im.SetID(3)
	if imCopy.GetID() == im.GetID() {
		t.Errorf("TestImGeneric(Immutable): expected ID to be 3, got %d", imCopy.GetID())
	}
}

func setAllFields(v any) {
	val := reflect.ValueOf(v).Elem()
	typ := val.Type()

	// Set non-zero values while skipping ignored fields
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)

		fieldValue := val.Field(i)
		if !fieldValue.CanSet() {
			fieldValue = makeWritable(fieldValue)
		}

		fieldValue.Set(reflect.ValueOf(getNonZeroValue(field.Type)))
	}
}

func getNonZeroValue(t reflect.Type) any {
	switch t.Kind() {
	case reflect.Int:
		return 1
	case reflect.String:
		return "nonzero"
	case reflect.Bool:
		return true
	case reflect.Slice:
		return []int{1, 2, 3}
	case reflect.Map:
		return map[string]struct{}{
			"tag": {},
		}
	case reflect.Ptr:
		return reflect.New(t.Elem()).Interface()
	default:
		return reflect.Zero(t).Interface()
	}
}

func makeWritable(v reflect.Value) reflect.Value {
	if v.CanSet() {
		return v
	}
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
}
