package isset

import (
	"fmt"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// Uint is a type that represents an uint that can be set or unset.
type Uint = uintType[uint]

// Uint8 is a type that represents an uint8 that can be set or unset.
type Uint8 = uintType[uint8]

// Uint16 is a type that represents an uint16 that can be set or unset.
type Uint16 = uintType[uint16]

// Uint32 is a type that represents an uint32 that can be set or unset.
type Uint32 = uintType[uint32]

// Uint64 is a type that represents an uint64 that can be set or unset.
type Uint64 = uintType[uint64]

type uintType[T ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64] struct {
	v     T
	isSet bool
}

// V returns the value.
func (i uintType[T]) V() T {
	return i.v
}

// IsSet returns if the value was set.
func (i uintType[T]) IsSet() bool {
	return i.isSet
}

// Set sets the value and marks it as set.
func (i uintType[T]) Set(val T) uintType[T] {
	i.v = val
	i.isSet = true
	return i
}

// Unset retuns the value to its zero value and marks it as unset.
func (i uintType[T]) Unset() uintType[T] {
	var zero T
	i.v = zero
	i.isSet = false
	return i
}

// MarshalJSON implements the json.Marshaler interface.
func (i uintType[T]) MarshalJSON() ([]byte, error) {
	if !i.isSet {
		return []byte{}, nil
	}
	return json.Marshal(i.v)
}

// MarshalJSONV2 implements the json.MarshalerV2 interface.
func (i uintType[T]) MarshalJSONV2(enc *jsontext.Encoder, opts json.Options) error {
	return enc.WriteToken(jsontext.Uint(uint64(i.v)))
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (i *uintType[T]) UnmarshalJSON(data []byte) error {
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
func (v *uintType[T]) UnmarshalJSONV2(dec *jsontext.Decoder, opts json.Options) error {
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
		v.v = T(t.Uint())
		return nil
	}
	return fmt.Errorf("expected a JSON number, got %T", t.Kind())
}
