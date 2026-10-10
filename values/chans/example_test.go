package chans_test

import (
	"fmt"

	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/values/chans"
)

// ExampleGet shows the two results a live context can produce: a value, then the channel closing.
func ExampleGet() {
	ctx := context.Background()

	ch := make(chan int, 1)
	ch <- 1
	close(ch)

	v, r := chans.Get(ctx, ch)
	fmt.Println(v, r)

	_, r = chans.Get(ctx, ch)
	fmt.Println(r)
	// Output:
	// 1 OK
	// Closed
}

// ExampleGet_stopOnCancel is for a loop that must stop as soon as shutdown begins without losing a value it already
// took off the channel. The value that comes back with ResultOKCanceled is handled, then the loop stops; anything still
// queued is left on the channel for someone else.
func ExampleGet_stopOnCancel() {
	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan int, 3)
	ch <- 1
	ch <- 2
	ch <- 3

	for {
		v, r := chans.Get(ctx, ch)
		if r.OK() {
			fmt.Println("handled", v)
			cancel() // Shutdown begins while the loop is running.
		}
		if r.CanceledOrClosed() {
			fmt.Println("stopped:", r)
			break
		}
	}
	fmt.Println("left on channel:", len(ch))
	// Output:
	// handled 1
	// handled 2
	// stopped: OKCanceled
	// left on channel: 1
}

// ExampleGet_drainAfterCancel is for a loop that must finish all queued work after shutdown, where the producer has
// already stopped sending. Stopping only on !r.OK() keeps the loop going through every ResultOKCanceled value until the
// channel is empty. This drain ends only once producers stop sending; if a producer may outlive ctx, use
// ExampleGet_drainBounded instead.
func ExampleGet_drainAfterCancel() {
	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan int, 3)
	ch <- 1
	ch <- 2
	ch <- 3

	for {
		v, r := chans.Get(ctx, ch)
		if !r.OK() {
			fmt.Println("stopped:", r)
			break
		}
		fmt.Println("handled", v, r)
		cancel() // Shutdown begins while the loop is running.
	}
	// Output:
	// handled 1 OK
	// handled 2 OKCanceled
	// handled 3 OKCanceled
	// stopped: CtxDone
}

// ExampleGet_drainBounded is for a loop that must finish the work queued when shutdown begins, while a producer keeps
// sending past shutdown. A !r.OK() drain would never end here, so the loop stops on cancel and then handles only the
// number of values that were on the channel at that moment.
func ExampleGet_drainBounded() {
	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan int, 3)
	ch <- 1
	ch <- 2
	ch <- 3

	// produce stands in for a producer that ignores ctx and refills the channel after every receive.
	next := 100
	produce := func() {
		if chans.TryPut(ch, next) {
			next++
		}
	}

	for {
		v, r := chans.Get(ctx, ch)
		if r.OK() {
			fmt.Println("handled", v)
			produce()
			cancel() // Shutdown begins while the loop is running.
		}
		if r.CanceledOrClosed() {
			fmt.Println("stopped:", r)
			break
		}
	}

	n := len(ch)
	for i := 0; i < n; i++ {
		v, r := chans.Get(ctx, ch)
		if !r.OK() {
			break
		}
		fmt.Println("drained", v)
		produce()
	}
	fmt.Println("left on channel:", len(ch))
	// Output:
	// handled 1
	// handled 2
	// stopped: OKCanceled
	// drained 3
	// drained 100
	// drained 101
	// left on channel: 3
}

// ExampleGet_closedAfterCancel shows that a closed channel is still reported as ResultClosed when the context is also
// done, so the caller can tell the stream ended rather than being canceled.
func ExampleGet_closedAfterCancel() {
	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan int, 1)
	ch <- 1
	close(ch)
	cancel()

	v, r := chans.Get(ctx, ch)
	fmt.Println(v, r)

	_, r = chans.Get(ctx, ch)
	fmt.Println(r)
	// Output:
	// 1 OKCanceled
	// Closed
}

// ExampleResult_OK shows which results carry a value.
func ExampleResult_OK() {
	results := []chans.Result{
		chans.ResultOK,
		chans.ResultOKCanceled,
		chans.ResultClosed,
		chans.ResultCtxDone,
	}
	for _, r := range results {
		fmt.Println(r, r.OK())
	}
	// Output:
	// OK true
	// OKCanceled true
	// Closed false
	// CtxDone false
}

// ExampleResult_CanceledOrClosed shows which results mean a loop over Get should stop, alongside OK and Closed.
// ResultOKCanceled is in both OK and CanceledOrClosed: it carries a value that should be handled, and it also means
// the context is done.
func ExampleResult_CanceledOrClosed() {
	results := []chans.Result{
		chans.ResultOK,
		chans.ResultOKCanceled,
		chans.ResultClosed,
		chans.ResultCtxDone,
	}
	for _, r := range results {
		fmt.Println(r, r.OK(), r.CanceledOrClosed(), r.Closed())
	}
	// Output:
	// OK true false false
	// OKCanceled true true false
	// Closed false true true
	// CtxDone false true false
}

// ExampleResult_Closed is for a loop that must consume the whole stream, where the producer closing the channel is
// success and cancellation is an error the caller has to see. Closed is true only for ResultClosed, so a done context
// is never mistaken for the end of the stream.
func ExampleResult_Closed() {
	consume := func(ctx context.Context, ch <-chan int) error {
		for {
			v, r := chans.Get(ctx, ch)
			if r.OK() {
				fmt.Println("handled", v)
			}
			switch {
			case r.Closed():
				return nil
			case r.CanceledOrClosed():
				return ctx.Err()
			}
		}
	}

	// The producer finishes and closes the channel.
	finished := make(chan int, 2)
	finished <- 1
	finished <- 2
	close(finished)
	fmt.Println("err:", consume(context.Background(), finished))

	// Shutdown begins before the producer finishes.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	unfinished := make(chan int, 1)
	unfinished <- 3
	fmt.Println("err:", consume(ctx, unfinished))
	// Output:
	// handled 1
	// handled 2
	// err: <nil>
	// handled 3
	// err: context canceled
}
