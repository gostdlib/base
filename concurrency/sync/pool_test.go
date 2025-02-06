package sync

import (
	"context"
	"runtime"
	"testing"

	"github.com/tidwall/lotsa"
)

type intType int

func (i *intType) Reset() {
	*i = 0
}

func TestPool(t *testing.T) {
	ctx := context.Background()

	pool := NewPool[*intType](ctx, "", func() *intType { var x intType; return &x }, WithBuffer(10))
	lotsa.Ops(
		1000000,
		runtime.NumCPU(),
		func(i, thread int) {
			x := pool.Get(ctx)
			defer pool.Put(ctx, x)
			if x == nil || *x != 0 {
				panic("bad")
			}
		},
	)
}
