package slices

import (
	"strconv"
	"testing"

	"github.com/gostdlib/base/context"
)

func addOne(ctx context.Context, i int, v int) (int, error) {
	return v + 1, nil
}

func BenchmarkTransform(b *testing.B) {
	ctx := context.Background()
	p := context.Pool(ctx).Limited(b.Context(), "", -1)

	bench := []struct {
		num int
	}{
		{100},
		{1000},
		{10000},
		{100000},
		{1000000},
		{10000000},
	}

	for _, bm := range bench {
		x := make([]int, bm.num)
		for i := 0; i < bm.num; i++ {
			x[i] = i
		}

		for z := 0; z < 2; z++ {
			testName := strconv.Itoa(bm.num)
			if z%2 == 0 {
				testName = testName + "(Transform)"
			} else {
				testName = testName + "(Loop)"
			}
			b.Run(
				testName,
				func(b *testing.B) {
					for i := 0; i < b.N; i++ {
						if z%2 == 0 {
							Transform(
								ctx,
								p,
								addOne,
								x,
							)
						} else {
							for a := 0; a < len(x); a++ {
								x[a] = x[a] + 1
							}
						}
					}
				},
			)
		}
	}
}
