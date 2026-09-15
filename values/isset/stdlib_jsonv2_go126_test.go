//go:build goexperiment.jsonv2 && !go1.27

package isset

import "encoding/json/v2"

// The standard library json v2 API is reached through these names so stdlib_jsonv2_test.go runs on Go 1.26 and Go 1.27.
// This file is the Go 1.26 side, where encoding/json/v2 only exists under GOEXPERIMENT=jsonv2.
// stdlib_jsonv2_go127_test.go holds the same names for Go 1.27.
type (
	stdlibMarshalerTo     = json.MarshalerTo
	stdlibUnmarshalerFrom = json.UnmarshalerFrom
)

var (
	stdlibMarshal   = json.Marshal
	stdlibUnmarshal = json.Unmarshal
)
