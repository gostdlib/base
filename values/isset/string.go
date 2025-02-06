package isset

import (
	"fmt"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// String is a type representing a String that can be set or unset.
type String struct {
	v     string
	isSet bool
}

// V returns the value.
func (i String) V() string {
	return i.v
}

// IsSet returns if the value was set.
func (i String) IsSet() bool {
	return i.isSet
}

// Set sets the value and marks it as set.
func (i String) Set(val string) String {
	i.v = val
	i.isSet = true
	return i
}

// Unset retuns the value to its zero value and marks it as unset.
func (i String) Unset() String {
	i.v = ""
	i.isSet = false
	return i
}

// MarshalJSON implements the json.Marshaler interface.
func (i String) MarshalJSON() ([]byte, error) {
	if !i.isSet {
		return []byte{}, nil
	}
	return json.Marshal(i.v)
}

// MarshalJSONV2 implements the json.MarshalerV2 interface.
func (i String) MarshalJSONV2(enc *jsontext.Encoder, opts json.Options) error {
	return enc.WriteToken(jsontext.String(string(i.v)))
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (i *String) UnmarshalJSON(data []byte) error {
	if bytesToStr(data) == "null" {
		i.isSet = false
		i.v = ""
		return nil
	}

	i.isSet = true
	var t string
	if err := json.Unmarshal(data, &t); err != nil {
		return err
	}
	i.v = t
	return nil
}

// UnmarshalJSONV2 implements the json.UnmarshalerV2 interface.
func (v *String) UnmarshalJSONV2(dec *jsontext.Decoder, opts json.Options) error {
	t, err := dec.ReadToken()
	if err != nil {
		return err
	}

	switch t.Kind() {
	case 'n':
		v.isSet = false
		v.v = ""
		return nil
	case '"':
		v.isSet = true
		v.v = string(t.String())
		return nil
	}
	return fmt.Errorf("expected a JSON string, got %string", t.Kind())
}
