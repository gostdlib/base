package promises

import (
	"context"
	"log"
	"runtime"
	syncLib "sync"
	"testing"
	"time"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/tidwall/lotsa"
)

func TestPromiseWithPool(t *testing.T) {
	t.Parallel()

	pool := sync.NewPool(
		context.Background(),
		"",
		func() chan Response[int] {
			return make(chan Response[int], 1)
		},
		sync.WithBuffer(10),
	)

	tests := []struct {
		name string
		pool *sync.Pool[chan Response[int]]
	}{
		{
			name: "With pool",
			pool: pool,
		},
		{
			name: "Without pool",
			pool: nil,
		},
	}

	for _, test := range tests {
		start := time.Now()

		input := make(chan Promise[int, int], 1)

		wg := syncLib.WaitGroup{}

		wg.Add(1)
		go func() {
			defer wg.Done()
			lotsa.Ops(
				1000000,
				runtime.NumCPU(),
				func(i, thread int) {
					p := NewPromise[int, int](i, WithPool(test.pool))
					input <- p
					resp, _ := p.Get(context.Background()) // Can't error on a non-cancelled context.
					if resp.V != i+1 {
						t.Errorf("expected %d, got %d", i+1, resp.V)
					}
				},
			)
		}()

		go func() {
			wg.Wait()
			close(input)
		}()

		wgSetters := syncLib.WaitGroup{}
		for i := 0; i < runtime.NumCPU(); i++ {
			wgSetters.Add(1)
			go func() {
				defer wgSetters.Done()
				for p := range input {
					p.Set(context.Background(), p.In+1, nil)
				}
			}()
		}

		wgSetters.Wait()
		log.Println("Time taken for", test.name, time.Since(start))
	}
}
