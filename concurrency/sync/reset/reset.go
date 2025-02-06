/*
Package reset provides a validator to validate that your Reset() method on your type
works as intended.

Basic use:

	func TestResetMyType(t *testing.T) {
		if err := Validate[*MyType](fields); err != nil {
			t.Fatalf("TestResetMyType: %s", err)
		}
	}
*/
package reset

import (
	"fmt"
	"reflect"
	"slices"
	"unsafe"
)

// Fields stores options for how to check fields in the struct.
type Fields struct {
	// Ignore says that the field is ignored in a reset. That means that calling Reset()
	// cannot change the value.
	Ignore []string
	// HasValue is a map of fields to default values that occur on a reset. These fields
	// do not get zero values.
	HasValue map[string]any
}

func (f *Fields) validate() error {
	for _, field := range f.Ignore {
		if _, ok := f.HasValue[field]; ok {
			return fmt.Errorf("field(%s) was in Ignore and HasValue", field)
		}
	}
	return nil
}

type structResetter interface {
	Reset()
}

// Validate tests a struct resets its fields according to the rules provided in the Fields argument.
// This does not work if fields are an interface type. You must deal with that yourself and utilize Fields.Ignore.
func Validate[T structResetter](f Fields) error {
	if err := f.validate(); err != nil {
		return err
	}

	var instance T
	ptr := reflect.New(reflect.TypeOf(instance).Elem()).Interface().(T)

	val := reflect.ValueOf(ptr).Elem()
	typ := val.Type()

	// Set non-zero values while skipping ignored fields
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)

		fieldValue := val.Field(i)
		if !fieldValue.CanSet() {
			fieldValue = makeWritable(fieldValue)
		}

		// Set field value only if it's not in the Ignore list
		if v, exists := f.HasValue[field.Name]; exists {
			fieldValue.Set(reflect.ValueOf(v))
		} else {
			fieldValue.Set(reflect.ValueOf(getNonZeroValue(field.Type)))
		}
	}

	// log.Printf("original value:\n %s", pretty.Sprint(ptr))

	// Call Reset() on the struct instance
	ptr.Reset()

	// log.Printf("reset value:\n %s", pretty.Sprint(ptr))

	// Validate the reset fields
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldValue := val.Field(i)
		if !fieldValue.CanSet() {
			fieldValue = makeWritable(fieldValue)
		}

		zeroValue := reflect.Zero(field.Type).Interface()

		// If the field is in Ignore list, it should retain the initial value
		if slices.Contains(f.Ignore, field.Name) {
			expectedValue := getNonZeroValue(field.Type)
			if fieldValue.Interface() != expectedValue {
				return fmt.Errorf("field %s should not have changed but was reset(%v)", field.Name, fieldValue.Interface())
			}
			continue
		}

		// Check if the field should have a specific value
		if expectedValue, exists := f.HasValue[field.Name]; exists {
			if !reflect.DeepEqual(fieldValue.Interface(), expectedValue) {
				return fmt.Errorf("field %s expected to have value %v but got %v", field.Name, expectedValue, fieldValue.Interface())
			}
		} else {
			// Verify if the field was reset to zero value
			if field.Type.Kind() == reflect.Map || field.Type.Kind() == reflect.Slice {
				if !fieldValue.IsNil() {
					return fmt.Errorf("field %s should have been reset to zero value but got %v", field.Name, fieldValue.Interface())
				}
			} else if !reflect.DeepEqual(fieldValue.Interface(), zeroValue) {
				return fmt.Errorf("field %s should have been reset to zero value but got %v", field.Name, fieldValue.Interface())
			}
		}
	}

	return nil
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
		return reflect.MakeSlice(t, 1, 1).Interface()
	case reflect.Map:
		return reflect.MakeMap(t).Interface()
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
