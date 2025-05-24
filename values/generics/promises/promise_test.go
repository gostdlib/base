package promises

import (
	"context"
	"log"
	"runtime"
	"strconv"
	syncLib "sync"
	"testing"
	"time"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/tidwall/lotsa"
)

func TestNewPromise(t *testing.T) {
	t.Parallel()

	input := make(chan Promise[int, string], 1)

	ctx := context.Background()
	wg := syncLib.WaitGroup{}

	start := time.Now()
	wg.Add(1)
	go func() {
		defer wg.Done()
		lotsa.Ops(
			1000000,
			runtime.NumCPU(),
			func(i, thread int) {
				p := New[int, string](ctx, i)
				input <- p
				resp, _ := p.Get(context.Background()) // Can't error on a non-cancelled context.
				if resp.V != "hello"+strconv.Itoa(i) {
					t.Errorf("expected %s, got %s", "hello"+strconv.Itoa(i), resp.V)
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
				p.Set(context.Background(), "hello"+strconv.Itoa(p.In), nil)
			}
		}()
	}

	wgSetters.Wait()
	log.Println("TestNewPromise: Time taken for", time.Since(start))
}

func TestMaker(t *testing.T) {
	t.Parallel()

	input := make(chan Promise[int, string], 1)

	ctx := context.Background()
	wg := syncLib.WaitGroup{}
	maker := Maker[int, string]{PoolOptions: []sync.Option{sync.WithBuffer(10)}}

	start := time.Now()
	wg.Add(1)
	go func() {
		defer wg.Done()
		lotsa.Ops(
			1000000,
			runtime.NumCPU(),
			func(i, thread int) {
				p := maker.New(ctx, i)
				input <- p
				resp, _ := p.Get(context.Background()) // Can't error on a non-cancelled context.
				if resp.V != "hello"+strconv.Itoa(i) {
					t.Errorf("expected %s, got %s", "hello"+strconv.Itoa(i), resp.V)
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
				p.Set(context.Background(), "hello"+strconv.Itoa(p.In), nil)
			}
		}()
	}

	wgSetters.Wait()
	log.Println("TestMaker: Time taken for", time.Since(start))
}

func BenchmarkNew(b *testing.B) {
	b.ReportAllocs()

	input := make(chan Promise[int, string], 1)

	ctx := context.Background()
	wg := syncLib.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		lotsa.Ops(
			1000000,
			runtime.NumCPU(),
			func(i, thread int) {
				p := New[int, string](ctx, i)
				input <- p
				resp, _ := p.Get(context.Background()) // Can't error on a non-cancelled context.
				if resp.V != "hello"+strconv.Itoa(i) {
					b.Errorf("expected %s, got %s", "hello"+strconv.Itoa(i), resp.V)
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
				p.Set(context.Background(), "hello"+strconv.Itoa(p.In), nil)
			}
		}()
	}

	wgSetters.Wait()

}

func BenchmarkMaker(b *testing.B) {
	b.ReportAllocs()

	input := make(chan Promise[int, string], 1)
	ctx := context.Background()
	wg := syncLib.WaitGroup{}
	maker := Maker[int, string]{PoolOptions: []sync.Option{sync.WithBuffer(10)}}

	b.ResetTimer()
	wg.Add(1)
	go func() {
		defer wg.Done()
		lotsa.Ops(
			1000000,
			runtime.NumCPU(),
			func(i, thread int) {
				p := maker.New(ctx, i)
				input <- p
				resp, _ := p.Get(context.Background()) // Can't error on a non-cancelled context.
				if resp.V != "hello"+strconv.Itoa(i) {
					b.Errorf("expected %s, got %s", "hello"+strconv.Itoa(i), resp.V)
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
				p.Set(context.Background(), "hello"+strconv.Itoa(p.In), nil)
			}
		}()
	}
}
