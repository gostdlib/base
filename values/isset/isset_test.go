package isset

import (
	"bytes"
	"io"
	"testing"

	"github.com/go-json-experiment/json/jsontext"
)

var out []byte
var err error

func BenchmarkInt(b *testing.B) {
	b.Run("Set", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var v Int
			v = v.Set(42)
		}
	})

	b.Run("Unset", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var v Int
			v = v.Unset()
		}
	})

	b.Run("V", func(b *testing.B) {
		var v Int
		v = v.Set(42)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = v.V()
		}
	})

	b.Run("IsSet", func(b *testing.B) {
		var v Int
		v = v.Set(42)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = v.IsSet()
		}
	})

	b.Run("MarshalJSON", func(b *testing.B) {
		var v Int
		v = v.Set(42)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			out, err = v.MarshalJSON()
		}
	})

	enc := jsontext.NewEncoder(io.Discard)
	b.Run("MarshalJSONTo", func(b *testing.B) {
		var v Int
		v = v.Set(42)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			err = v.MarshalJSONTo(enc)
		}
	})

	b.Run("UnmarshalJSON", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var x Int
			err = x.UnmarshalJSON([]byte("42"))
		}
	})

	b.Run("UnmarshalJSONFrom", func(b *testing.B) {
		data := []byte("42")
		r := bytes.NewReader(data)
		dec := jsontext.NewDecoder(r)
		b.ReportAllocs()
		// The decoder is reset inside the timed loop because StopTimer/StartTimer with ReportAllocs reads memory
		// stats on every call, which costs far more than the decode and makes the benchmark take minutes.
		for i := 0; i < b.N; i++ {
			r.Reset(data)
			dec.Reset(r)

			var x Int
			err = x.UnmarshalJSONFrom(dec)
		}
	})
}
