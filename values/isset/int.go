package isset

import (
	"fmt"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// Int is a type that represents an int that can be set or unset.
type Int = intType[int]

// Int8 is a type that represents an int8 that can be set or unset.
type Int8 = intType[int8]

// Int16 is a type that represents an int16 that can be set or unset.
type Int16 = intType[int16]

// Int32 is a type that represents an int32 that can be set or unset.
type Int32 = intType[int32]

// Int64 is a type that represents an int64 that can be set or unset.
type Int64 = intType[int64]

type intType[T ~int | ~int8 | ~int16 | ~int32 | ~int64] struct {
	v     T
	isSet bool
}

// V returns the value.
func (i intType[T]) V() T {
	return i.v
}

// IsSet returns if the value was set.
func (i intType[T]) IsSet() bool {
	return i.isSet
}

// Set sets the value and marks it as set.
func (i intType[T]) Set(val T) intType[T] {
	i.v = val
	i.isSet = true
	return i
}

// Unset retuns the value to its zero value and marks it as unset.
func (i intType[T]) Unset() intType[T] {
	var zero T
	i.v = zero
	i.isSet = false
	return i
}

// MarshalJSON implements the json.Marshaler interface.
func (i intType[T]) MarshalJSON() ([]byte, error) {
	if !i.isSet {
		return []byte{}, nil
	}
	return json.Marshal(i.v)
}

// MarshalJSONV2 implements the json.MarshalerV2 interface.
func (i intType[T]) MarshalJSONV2(enc *jsontext.Encoder, opts json.Options) error {
	return enc.WriteToken(jsontext.Int(int64(i.v)))
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (i *intType[T]) UnmarshalJSON(data []byte) error {
	if bytesToStr(data) == "null" {
		var zero T
		i.isSet = false
		i.v = zero
		return nil
	}

	i.isSet = true
	var t T
	if err := json.Unmarshal(data, &t); err != nil {
		return err
	}
	i.v = t
	return nil
}

// UnmarshalJSONV2 implements the json.UnmarshalerV2 interface.
func (v *intType[T]) UnmarshalJSONV2(dec *jsontext.Decoder, opts json.Options) error {
	t, err := dec.ReadToken()
	if err != nil {
		return err
	}

	switch t.Kind() {
	case 'n':
		v.isSet = false
		v.v = 0
		return nil
	case '0':
		v.isSet = true
		v.v = T(t.Int())
		return nil
	}
	return fmt.Errorf("expected a JSON number, got %T", t.Kind())
}
