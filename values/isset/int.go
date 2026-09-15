package isset

import (
	"fmt"
	"strconv"
	"unsafe"

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

// Unset happens the value to its zero value and marks it as unset.
func (i intType[T]) Unset() intType[T] {
	var zero T
	i.v = zero
	i.isSet = false
	return i
}

// MarshalJSON implements the json.Marshaler interface.
func (i intType[T]) MarshalJSON() ([]byte, error) {
	if !i.isSet {
		return []byte("null"), nil
	}
	return json.Marshal(i.v)
}

// MarshalJSONTo implements the json.MarshalerTo interface.
func (i intType[T]) MarshalJSONTo(enc *jsontext.Encoder) error {
	if !i.isSet {
		return enc.WriteToken(jsontext.Null)
	}
	return enc.WriteToken(jsontext.Int(int64(i.v)))
}

// MarshalJSONV2 calls MarshalJSONTo.
//
// Deprecated: Use MarshalJSONTo. The json v2 marshaler interface is json.MarshalerTo, which this method name
// never satisfied.
func (i intType[T]) MarshalJSONV2(enc *jsontext.Encoder, _ json.Options) error {
	return i.MarshalJSONTo(enc)
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

// UnmarshalJSONFrom implements the json.UnmarshalerFrom interface.
func (v *intType[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	// ReadValue is used instead of ReadToken because jsontext.Token's Int, Uint and Float methods return a
	// (value, error) pair in github.com/go-json-experiment/json but only a saturated value in the standard library's
	// encoding/json/jsontext, which github.com/go-json-experiment/json aliases to under GOEXPERIMENT=jsonv2.
	val, err := dec.ReadValue()
	if err != nil {
		return err
	}

	switch val.Kind() {
	case 'n':
		v.isSet = false
		v.v = 0
		return nil
	case '0':
		i, err := strconv.ParseInt(bytesToStr(val), 10, int(unsafe.Sizeof(v.v))*8)
		if err != nil {
			return fmt.Errorf("expected a JSON number that fits %T: %w", v.v, err)
		}
		v.isSet = true
		v.v = T(i)
		return nil
	}
	return fmt.Errorf("expected a JSON number, got %v", val.Kind())
}

// UnmarshalJSONV2 calls UnmarshalJSONFrom.
//
// Deprecated: Use UnmarshalJSONFrom. The json v2 unmarshaler interface is json.UnmarshalerFrom, which this
// method name never satisfied.
func (v *intType[T]) UnmarshalJSONV2(dec *jsontext.Decoder, _ json.Options) error {
	return v.UnmarshalJSONFrom(dec)
}
