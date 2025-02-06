package isset

import (
	"bytes"
	"io"
	"testing"

	"github.com/go-json-experiment/json"
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
	b.Run("MarshalJSONV2", func(b *testing.B) {
		var v Int
		v = v.Set(42)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			err = v.MarshalJSONV2(enc, json.DefaultOptionsV2())
		}
	})

	b.Run("UnmarshalJSON", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var x Int
			err = x.UnmarshalJSON([]byte("42"))
		}
	})

	dec := jsontext.NewDecoder(bytes.NewReader([]byte("42")))
	b.Run("UnmarshalJSONV2", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			dec.Reset(bytes.NewReader([]byte("42")))
			b.StartTimer()

			var x Int
			err = x.UnmarshalJSONV2(dec, json.DefaultOptionsV2())
		}
	})
}
