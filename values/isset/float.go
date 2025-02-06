package isset

import (
	"fmt"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// Float32 is a type that represents an float32 that can be set or unset.
type Float32 = floatType[float32]

// Float64 is a type that represents an float64 that can be set or unset.
type Float64 = floatType[float64]

type floatType[T ~float32 | ~float64] struct {
	v     T
	isSet bool
}

// V returns the value.
func (i floatType[T]) V() T {
	return i.v
}

// IsSet returns if the value was set.
func (i floatType[T]) IsSet() bool {
	return i.isSet
}

// Set sets the value and marks it as set.
func (i floatType[T]) Set(val T) floatType[T] {
	i.v = val
	i.isSet = true
	return i
}

// Unset retuns the value to its zero value and marks it as unset.
func (i floatType[T]) Unset() floatType[T] {
	var zero T
	i.v = zero
	i.isSet = false
	return i
}

// MarshalJSON implements the json.Marshaler interface.
func (i floatType[T]) MarshalJSON() ([]byte, error) {
	if !i.isSet {
		return []byte{}, nil
	}
	return json.Marshal(i.v)
}

// MarshalJSONV2 implements the json.MarshalerV2 interface.
func (i floatType[T]) MarshalJSONV2(enc *jsontext.Encoder, opts json.Options) error {
	return enc.WriteToken(jsontext.Float(float64(i.v)))
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (i *floatType[T]) UnmarshalJSON(data []byte) error {
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
func (v *floatType[T]) UnmarshalJSONV2(dec *jsontext.Decoder, opts json.Options) error {
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
		v.v = T(t.Float())
		return nil
	}
	return fmt.Errorf("expected a JSON number, got %T", t.Kind())
}
