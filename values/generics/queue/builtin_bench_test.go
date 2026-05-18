package queue

import "testing"

// hashSink keeps Hash results from being optimized away.
var hashSink uint64

// BenchmarkNumberHash measures Number.Hash (the reflect.ValueOf kind dispatch) across a
// signed int, unsigned int, and float — the three encoding paths.
func BenchmarkNumberHash(b *testing.B) {
	b.Run("int64", func(b *testing.B) {
		n := Number[int64]{V: -123456789}
		for i := 0; i < b.N; i++ {
			hashSink = n.Hash()
		}
	})

	b.Run("uint64", func(b *testing.B) {
		n := Number[uint64]{V: 123456789}
		for i := 0; i < b.N; i++ {
			hashSink = n.Hash()
		}
	})

	b.Run("float64", func(b *testing.B) {
		n := Number[float64]{V: -3.14159}
		for i := 0; i < b.N; i++ {
			hashSink = n.Hash()
		}
	})
}
