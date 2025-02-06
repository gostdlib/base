package isset

import (
	"fmt"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// Bool is a type representing a bool that can be set or unset.
type Bool struct {
	v     bool
	isSet bool
}

// V returns the value.
func (i Bool) V() bool {
	return i.v
}

// IsSet returns if the value was set.
func (i Bool) IsSet() bool {
	return i.isSet
}

// Set sets the value and marks it as set.
func (i Bool) Set(val bool) Bool {
	i.v = val
	i.isSet = true
	return i
}

// Unset retuns the value to its zero value and marks it as unset.
func (i Bool) Unset() Bool {
	i.v = false
	i.isSet = false
	return i
}

// MarshalJSON implements the json.Marshaler interface.
func (i Bool) MarshalJSON() ([]byte, error) {
	if !i.isSet {
		return []byte{}, nil
	}
	return json.Marshal(i.v)
}

// MarshalJSONV2 implements the json.MarshalerV2 interface.
func (i Bool) MarshalJSONV2(enc *jsontext.Encoder, opts json.Options) error {
	return enc.WriteToken(jsontext.Bool(bool(i.v)))
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (i *Bool) UnmarshalJSON(data []byte) error {
	if bytesToStr(data) == "null" {
		i.isSet = false
		i.v = false
		return nil
	}

	i.isSet = true
	var t bool
	if err := json.Unmarshal(data, &t); err != nil {
		return err
	}
	i.v = t
	return nil
}

// UnmarshalJSONV2 implements the json.UnmarshalerV2 interface.
func (v *Bool) UnmarshalJSONV2(dec *jsontext.Decoder, opts json.Options) error {
	t, err := dec.ReadToken()
	if err != nil {
		return err
	}

	switch t.Kind() {
	case 'n':
		v.isSet = false
		v.v = false
		return nil
	case 'f', 't':
		v.isSet = true
		v.v = t.Bool()
		return nil
	}
	return fmt.Errorf("expected a JSON bool, got %T", t.Kind())
}
