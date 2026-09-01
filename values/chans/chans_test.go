package chans

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/kylelemons/godebug/pretty"
)

// TestGet exercises the context branch with an empty channel on purpose. Get blocks forever on an empty channel with
// a live context, and when a buffered value and a done context are both ready select picks between them at random, so
// an empty channel is the only deterministic way to reach the context branch. Get returns no error, so every case but
// the nil channel is a success case: ok == false covers both a closed channel and a done context, and the caller is
// expected to check the context to tell them apart.
func TestGet(t *testing.T) {
	tests := []struct {
		name      string
		buffer    int
		prefill   []int
		closed    bool
		cancel    bool
		nilChan   bool
		producer  func(ch chan int)
		wantV     int
		wantOK    bool
		wantPanic bool
	}{
		{
			name:    "Success: receives a buffered value",
			buffer:  1,
			prefill: []int{7},
			wantV:   7,
			wantOK:  true,
		},
		{
			name:   "Success: closed and drained channel reports not ok",
			buffer: 1,
			closed: true,
		},
		{
			name:   "Success: context is done reports not ok",
			buffer: 1,
			cancel: true,
		},
		{
			name:     "Success: receives a value sent by another goroutine over an unbuffered channel",
			producer: func(ch chan int) { ch <- 7 },
			wantV:    7,
			wantOK:   true,
		},
		{
			name:     "Success: a producer closing while the receive is blocked reports not ok",
			producer: func(ch chan int) { close(ch) },
		},
		{
			name:      "Error: channel is nil",
			nilChan:   true,
			wantPanic: true,
		},
	}

	for _, test := range tests {
		func() {
			defer func() {
				r := recover()
				switch {
				case r == nil && test.wantPanic:
					t.Errorf("TestGet(%s): got no panic, want panic", test.name)
				case r != nil && !test.wantPanic:
					t.Errorf("TestGet(%s): got panic == %v, want no panic", test.name, r)
				}
			}()

			var ch chan int
			if !test.nilChan {
				ch = make(chan int, test.buffer)
				for _, v := range test.prefill {
					ch <- v
				}
				if test.closed {
					close(ch)
				}
				if test.producer != nil {
					go test.producer(ch)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}

			v, ok := Get(ctx, ch)
			if v != test.wantV {
				t.Errorf("TestGet(%s): got v == %d, want v == %d", test.name, v, test.wantV)
			}
			if ok != test.wantOK {
				t.Errorf("TestGet(%s): got ok == %t, want ok == %t", test.name, ok, test.wantOK)
			}
		}()
	}
}

// TestPut fills the channel in the error case for the same reason TestGet empties it: a done context racing a send
// that can succeed is decided at random, so the send must be unable to proceed for the context branch to be reliable.
func TestPut(t *testing.T) {
	tests := []struct {
		name      string
		buffer    int
		prefill   int
		nilChan   bool
		cancel    bool
		consumer  bool
		wantOK    bool
		wantPanic bool
	}{
		{
			name:   "Success: sends to a channel with room",
			buffer: 1,
			wantOK: true,
		},
		{
			name:     "Success: sends to an unbuffered channel with a waiting receiver",
			consumer: true,
			wantOK:   true,
		},
		{
			name:    "Success: context is done and the channel is full reports not ok",
			buffer:  1,
			prefill: 1,
			cancel:  true,
		},
		{
			name:      "Error: channel is nil",
			nilChan:   true,
			wantPanic: true,
		},
	}

	for _, test := range tests {
		func() {
			defer func() {
				r := recover()
				switch {
				case r == nil && test.wantPanic:
					t.Errorf("TestPut(%s): got no panic, want panic", test.name)
				case r != nil && !test.wantPanic:
					t.Errorf("TestPut(%s): got panic == %v, want no panic", test.name, r)
				}
			}()

			var ch chan int
			if !test.nilChan {
				ch = make(chan int, test.buffer)
				for i := 0; i < test.prefill; i++ {
					ch <- 0
				}
			}
			received := make(chan int, 1)
			if test.consumer {
				go func() { received <- <-ch }()
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}

			ok := Put(ctx, ch, 42)
			if ok != test.wantOK {
				t.Errorf("TestPut(%s): got ok == %t, want ok == %t", test.name, ok, test.wantOK)
			}
			if !ok {
				return
			}

			got := 0
			if test.consumer {
				got = <-received
			} else {
				got = <-ch
			}
			if got != 42 {
				t.Errorf("TestPut(%s): got %d delivered, want 42", test.name, got)
			}
		}()
	}
}

func TestTryPut(t *testing.T) {
	tests := []struct {
		name      string
		prefill   int
		nilChan   bool
		wantOK    bool
		wantPanic bool
	}{
		{
			name:   "Success: channel has room",
			wantOK: true,
		},
		{
			name:    "Success: channel is full reports not ok",
			prefill: 1,
		},
		{
			name:      "Error: channel is nil",
			nilChan:   true,
			wantPanic: true,
		},
	}

	for _, test := range tests {
		func() {
			defer func() {
				r := recover()
				switch {
				case r == nil && test.wantPanic:
					t.Errorf("TestTryPut(%s): got no panic, want panic", test.name)
				case r != nil && !test.wantPanic:
					t.Errorf("TestTryPut(%s): got panic == %v, want no panic", test.name, r)
				}
			}()

			var ch chan int
			if !test.nilChan {
				ch = make(chan int, 1)
				for i := 0; i < test.prefill; i++ {
					ch <- 0
				}
			}

			ok := TryPut(ch, 42)
			if ok != test.wantOK {
				t.Errorf("TestTryPut(%s): got ok == %t, want ok == %t", test.name, ok, test.wantOK)
			}
			if !ok {
				return
			}

			if got := <-ch; got != 42 {
				t.Errorf("TestTryPut(%s): got %d on the channel, want 42", test.name, got)
			}
		}()
	}
}

func TestTryGet(t *testing.T) {
	tests := []struct {
		name       string
		prefill    []int
		closed     bool
		nilChan    bool
		wantV      int
		wantOK     bool
		wantClosed bool
		wantPanic  bool
	}{
		{
			name:    "Success: value is available",
			prefill: []int{7},
			wantV:   7,
			wantOK:  true,
		},
		{
			name: "Success: channel is empty and open",
		},
		{
			name:       "Success: channel is closed and drained",
			closed:     true,
			wantClosed: true,
		},
		{
			name:    "Success: channel is closed but still holding a value",
			prefill: []int{7},
			closed:  true,
			wantV:   7,
			wantOK:  true,
		},
		{
			name:      "Error: channel is nil",
			nilChan:   true,
			wantPanic: true,
		},
	}

	for _, test := range tests {
		func() {
			defer func() {
				r := recover()
				switch {
				case r == nil && test.wantPanic:
					t.Errorf("TestTryGet(%s): got no panic, want panic", test.name)
				case r != nil && !test.wantPanic:
					t.Errorf("TestTryGet(%s): got panic == %v, want no panic", test.name, r)
				}
			}()

			var ch chan int
			if !test.nilChan {
				ch = make(chan int, 1)
				for _, v := range test.prefill {
					ch <- v
				}
				if test.closed {
					close(ch)
				}
			}

			v, ok, closed := TryGet(ch)
			if v != test.wantV {
				t.Errorf("TestTryGet(%s): got v == %d, want v == %d", test.name, v, test.wantV)
			}
			if ok != test.wantOK {
				t.Errorf("TestTryGet(%s): got ok == %t, want ok == %t", test.name, ok, test.wantOK)
			}
			if closed != test.wantClosed {
				t.Errorf("TestTryGet(%s): got closed == %t, want closed == %t", test.name, closed, test.wantClosed)
			}
		}()
	}
}

// TestIter uses an empty channel in the context case so the context branch is not racing a ready receive. Iter
// yields no error, so a cancelled context simply ends the sequence; every case here is a success case. The stopAfter
// case pins that abandoning the range terminates the sequence, and the closed cases pin that a closed channel ends
// the sequence without yielding a trailing zero value.
func TestIter(t *testing.T) {
	tests := []struct {
		name      string
		buffer    int
		prefill   []int
		closed    bool
		cancel    bool
		producer  func(ch chan int)
		stopAfter int
		nilChan   bool
		want      []int
		wantPanic bool
	}{
		{
			name:    "Success: drains a closed channel",
			buffer:  3,
			prefill: []int{1, 2, 3},
			closed:  true,
			want:    []int{1, 2, 3},
		},
		{
			name:   "Success: closed and empty channel yields nothing",
			buffer: 3,
			closed: true,
		},
		{
			name:      "Success: consumer stops early",
			buffer:    3,
			prefill:   []int{1, 2, 3},
			closed:    true,
			stopAfter: 2,
			want:      []int{1, 2},
		},
		{
			name:   "Success: context is done yields nothing",
			buffer: 3,
			cancel: true,
		},
		{
			name:      "Error: channel is nil",
			nilChan:   true,
			wantPanic: true,
		},
		{
			name: "Success: consumes from a producer that closes when it is done",
			producer: func(ch chan int) {
				for i := 1; i <= 3; i++ {
					ch <- i
				}
				close(ch)
			},
			want: []int{1, 2, 3},
		},
	}

	for _, test := range tests {
		func() {
			defer func() {
				r := recover()
				switch {
				case r == nil && test.wantPanic:
					t.Errorf("TestIter(%s): got no panic, want panic", test.name)
				case r != nil && !test.wantPanic:
					t.Errorf("TestIter(%s): got panic == %v, want no panic", test.name, r)
				}
			}()

			var ch chan int
			if !test.nilChan {
				ch = make(chan int, test.buffer)
				for _, v := range test.prefill {
					ch <- v
				}
				if test.closed {
					close(ch)
				}
				if test.producer != nil {
					go test.producer(ch)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}

			var got []int
			for v := range Iter(ctx, ch) {
				got = append(got, v)
				if test.stopAfter > 0 && len(got) == test.stopAfter {
					break
				}
			}

			if diff := pretty.Compare(test.want, got); diff != "" {
				t.Errorf("TestIter(%s): -want/+got:\n%s", test.name, diff)
			}
		}()
	}
}

func TestWithLimit(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		wantLimit int
		wantPanic bool
	}{
		{
			name:      "Success: positive limit",
			limit:     3,
			wantLimit: 3,
		},
		{
			name:      "Error: limit below one",
			limit:     0,
			wantPanic: true,
		},
	}

	for _, test := range tests {
		func() {
			defer func() {
				r := recover()
				switch {
				case r == nil && test.wantPanic:
					t.Errorf("TestWithLimit(%s): got no panic, want panic", test.name)
				case r != nil && !test.wantPanic:
					t.Errorf("TestWithLimit(%s): got panic == %v, want no panic", test.name, r)
				}
			}()

			opts := WithLimit(test.limit)(collectOptions{})
			if opts.limit != test.wantLimit {
				t.Errorf("TestWithLimit(%s): got limit == %d, want limit == %d", test.name, opts.limit, test.wantLimit)
			}
		}()
	}
}

// TestCollect leaves the channel open in the limit case to prove the limit alone stops the collection. The partial
// case pins that a cancelled context still returns everything already taken off the channel, with done false. Collect
// returns no error, so every case here is a success case; done distinguishes "drained the channel" from "stopped
// early", and stopping early is a normal outcome rather than a failure.
func TestCollect(t *testing.T) {
	tests := []struct {
		name       string
		buffer     int
		prefill    []int
		closed     bool
		precancel  bool
		timeout    bool
		producer   func(ch chan int)
		options    []CollectOption
		nilChan    bool
		want       []int
		wantInChan int
		wantDone   bool
		wantPanic  bool
	}{
		{
			name:     "Success: collects every value from a closed channel",
			buffer:   3,
			prefill:  []int{1, 2, 3},
			closed:   true,
			want:     []int{1, 2, 3},
			wantDone: true,
		},
		{
			name:     "Success: closed and empty channel collects nothing",
			buffer:   3,
			closed:   true,
			wantDone: true,
		},
		{
			name:       "Success: WithLimit stops before the channel is closed",
			buffer:     3,
			prefill:    []int{1, 2, 3},
			options:    []CollectOption{WithLimit(2)},
			want:       []int{1, 2},
			wantInChan: 1,
		},
		{
			name:      "Success: context is done before anything is collected",
			buffer:    3,
			precancel: true,
		},
		{
			name:    "Success: context is done after collecting some values",
			buffer:  3,
			prefill: []int{1, 2},
			timeout: true,
			want:    []int{1, 2},
		},
		{
			name:      "Error: channel is nil",
			nilChan:   true,
			wantPanic: true,
		},
		{
			name: "Success: collects from a producer that closes when it is done",
			producer: func(ch chan int) {
				for i := 1; i <= 3; i++ {
					ch <- i
				}
				close(ch)
			},
			want:     []int{1, 2, 3},
			wantDone: true,
		},
	}

	for _, test := range tests {
		func() {
			defer func() {
				r := recover()
				switch {
				case r == nil && test.wantPanic:
					t.Errorf("TestCollect(%s): got no panic, want panic", test.name)
				case r != nil && !test.wantPanic:
					t.Errorf("TestCollect(%s): got panic == %v, want no panic", test.name, r)
				}
			}()

			var ch chan int
			if !test.nilChan {
				ch = make(chan int, test.buffer)
				for _, v := range test.prefill {
					ch <- v
				}
				if test.closed {
					close(ch)
				}
				if test.producer != nil {
					go test.producer(ch)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.precancel {
				cancel()
			}
			if test.timeout {
				// The channel stays open with values buffered, so Collect takes both immediately (the context is not
				// yet done, so its select arm is not ready) and only then blocks until the deadline. That pins the
				// partial result deterministically.
				timeoutCtx, timeoutCancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
				defer timeoutCancel()
				ctx = timeoutCtx
			}

			got, done := Collect(ctx, ch, test.options...)
			if done != test.wantDone {
				t.Errorf("TestCollect(%s): got done == %t, want done == %t", test.name, done, test.wantDone)
			}

			if diff := pretty.Compare(test.want, got); diff != "" {
				t.Errorf("TestCollect(%s): -want/+got:\n%s", test.name, diff)
			}
			if len(ch) != test.wantInChan {
				t.Errorf("TestCollect(%s): got %d values left in the channel, want %d", test.name, len(ch), test.wantInChan)
			}
		}()
	}
}

// TestFill covers both context branches. The precancel case reaches the ctx.Err() check before the first send. The
// blocked case gives the channel room for exactly one value so the second send cannot proceed: Fill clears the
// ctx.Err() check microseconds in, blocks in the select, and the deadline fires while it is parked there. That is the
// only way to reach the select's ctx.Done arm and the documented undelivered value from a blocked send. Fill returns
// no error, so every case here is a success case; done separates a fully sent sequence from one cut short.
func TestFill(t *testing.T) {
	tests := []struct {
		name            string
		seq             []int
		buffer          int
		precancel       bool
		blocked         bool
		drain           bool
		nilChan         bool
		wantDelivered   []int
		wantUndelivered int
		wantDone        bool
		wantPanic       bool
	}{
		{
			name:          "Success: delivers the whole sequence",
			seq:           []int{1, 2, 3},
			buffer:        3,
			wantDelivered: []int{1, 2, 3},
			wantDone:      true,
		},
		{
			name:     "Success: empty sequence delivers nothing",
			buffer:   3,
			wantDone: true,
		},
		{
			name:            "Success: context is done before the first send",
			seq:             []int{1, 2, 3},
			buffer:          3,
			precancel:       true,
			wantUndelivered: 1,
		},
		{
			name:            "Success: context is done while a send is blocked",
			seq:             []int{1, 2, 3},
			buffer:          1,
			blocked:         true,
			wantDelivered:   []int{1},
			wantUndelivered: 2,
		},
		{
			name:      "Error: channel is nil",
			seq:       []int{1},
			nilChan:   true,
			wantPanic: true,
		},
		{
			name:          "Success: blocks on a full channel until a consumer drains it",
			seq:           []int{1, 2, 3, 4, 5},
			buffer:        1,
			drain:         true,
			wantDelivered: []int{1, 2, 3, 4, 5},
			wantDone:      true,
		},
	}

	for _, test := range tests {
		func() {
			defer func() {
				r := recover()
				switch {
				case r == nil && test.wantPanic:
					t.Errorf("TestFill(%s): got no panic, want panic", test.name)
				case r != nil && !test.wantPanic:
					t.Errorf("TestFill(%s): got panic == %v, want no panic", test.name, r)
				}
			}()

			var ch chan int
			if !test.nilChan {
				ch = make(chan int, test.buffer)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.precancel {
				cancel()
			}
			if test.blocked {
				timeoutCtx, timeoutCancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
				defer timeoutCancel()
				ctx = timeoutCtx
			}
			drained := make(chan []int, 1)
			if test.drain {
				// A buffer of 1 against a 5 element sequence means Fill blocks on nearly every send, so this pins
				// that a blocked send resumes once a consumer makes room rather than dropping the value.
				go func() {
					var got []int
					for v := range ch {
						got = append(got, v)
					}
					drained <- got
				}()
			}

			undelivered, done := Fill(ctx, ch, slices.Values(test.seq))
			if undelivered != test.wantUndelivered {
				t.Errorf("TestFill(%s): got undelivered == %d, want undelivered == %d", test.name, undelivered, test.wantUndelivered)
			}
			if done != test.wantDone {
				t.Errorf("TestFill(%s): got done == %t, want done == %t", test.name, done, test.wantDone)
			}

			close(ch)
			var got []int
			if test.drain {
				got = <-drained
			} else {
				collected, drainedOK := Collect(t.Context(), ch)
				if !drainedOK {
					t.Errorf("TestFill(%s): draining the channel: got done == false, want done == true", test.name)
					return
				}
				got = collected
			}
			if diff := pretty.Compare(test.wantDelivered, got); diff != "" {
				t.Errorf("TestFill(%s): delivered -want/+got:\n%s", test.name, diff)
			}
		}()
	}
}

// closedWith returns a closed channel already holding vs.
func closedWith(vs ...int) <-chan int {
	ch := make(chan int, len(vs))
	for _, v := range vs {
		ch <- v
	}
	close(ch)
	return ch
}

// TestMerge sorts what it collects because Merge interleaves its inputs in arrival order, which is not deterministic.
// The duplicate case pins current behavior: dynamic.AddRecv rejects a channel already in use and Merge discards that
// error, so the same channel passed twice is registered once rather than read twice.
func TestMerge(t *testing.T) {
	tests := []struct {
		name      string
		inputs    func() []<-chan int
		want      []int
		wantPanic bool
	}{
		{
			name:   "Success: merges several closed channels",
			inputs: func() []<-chan int { return []<-chan int{closedWith(1, 2), closedWith(3), closedWith(4, 5)} },
			want:   []int{1, 2, 3, 4, 5},
		},
		{
			name:   "Success: a single channel",
			inputs: func() []<-chan int { return []<-chan int{closedWith(1, 2, 3)} },
			want:   []int{1, 2, 3},
		},
		{
			name:   "Success: values buffered in an already closed channel are not lost",
			inputs: func() []<-chan int { return []<-chan int{closedWith(1, 2, 3, 4, 5)} },
			want:   []int{1, 2, 3, 4, 5},
		},
		{
			name:   "Success: closed and empty channels yield nothing",
			inputs: func() []<-chan int { return []<-chan int{closedWith(), closedWith()} },
		},
		{
			name: "Success: live producers on unbuffered channels",
			inputs: func() []<-chan int {
				a, b := make(chan int), make(chan int)
				go func() {
					for i := 1; i <= 3; i++ {
						a <- i
					}
					close(a)
				}()
				go func() {
					for i := 4; i <= 6; i++ {
						b <- i
					}
					close(b)
				}()
				return []<-chan int{a, b}
			},
			want: []int{1, 2, 3, 4, 5, 6},
		},
		{
			name: "Success: the same channel passed twice is read once",
			inputs: func() []<-chan int {
				ch := closedWith(1, 2)
				return []<-chan int{ch, ch}
			},
			want: []int{1, 2},
		},
		{
			name:      "Error: no channels provided",
			inputs:    func() []<-chan int { return nil },
			wantPanic: true,
		},
		{
			name: "Error: more channels than dynamic.Select can hold",
			inputs: func() []<-chan int {
				// Real channels, so this cannot pass on the nil-channel panic instead. They are never registered,
				// so the cost is allocating them, not 65534 select cases.
				chs := make([]<-chan int, 65534)
				for i := range chs {
					chs[i] = make(chan int)
				}
				return chs
			},
			wantPanic: true,
		},
		{
			name:      "Error: an input channel is nil",
			inputs:    func() []<-chan int { return []<-chan int{closedWith(1), nil} },
			wantPanic: true,
		},
	}

	for _, test := range tests {
		func() {
			defer func() {
				r := recover()
				switch {
				case r == nil && test.wantPanic:
					t.Errorf("TestMerge(%s): got no panic, want panic", test.name)
				case r != nil && !test.wantPanic:
					t.Errorf("TestMerge(%s): got panic == %v, want no panic", test.name, r)
				}
			}()

			var got []int
			for v := range Merge(test.inputs()...) {
				got = append(got, v)
			}
			slices.Sort(got)

			if diff := pretty.Compare(test.want, got); diff != "" {
				t.Errorf("TestMerge(%s): -want/+got:\n%s", test.name, diff)
			}
		}()
	}
}

// TestTee uses unbuffered outputs with a concurrent reader on each, which is the shape that actually exercises Tee's
// blocking send. Tee's documented panic for a repeated output channel is not covered here: it fires inside the pool
// worker, where no caller can recover it, so triggering it would take the test binary down with it.
func TestTee(t *testing.T) {
	tests := []struct {
		name      string
		values    []int
		outputs   int
		nilInput  bool
		nilOutput bool
		want      []int
		wantPanic bool
	}{
		{
			name:    "Success: copies every value to every output",
			values:  []int{1, 2, 3},
			outputs: 3,
			want:    []int{1, 2, 3},
		},
		{
			name:    "Success: a single output",
			values:  []int{1, 2},
			outputs: 1,
			want:    []int{1, 2},
		},
		{
			name:    "Success: a closed and empty input closes the outputs without sending",
			outputs: 2,
		},
		{
			name:      "Error: input channel is nil",
			nilInput:  true,
			outputs:   1,
			wantPanic: true,
		},
		{
			name:      "Error: an output channel is nil",
			values:    []int{1},
			outputs:   2,
			nilOutput: true,
			wantPanic: true,
		},
		{
			name:      "Error: no output channels provided",
			values:    []int{1},
			wantPanic: true,
		},
	}

	for _, test := range tests {
		func() {
			defer func() {
				r := recover()
				switch {
				case r == nil && test.wantPanic:
					t.Errorf("TestTee(%s): got no panic, want panic", test.name)
				case r != nil && !test.wantPanic:
					t.Errorf("TestTee(%s): got panic == %v, want no panic", test.name, r)
				}
			}()

			var in chan int
			if !test.nilInput {
				in = make(chan int, len(test.values))
				for _, v := range test.values {
					in <- v
				}
				close(in)
			}

			outs := make([]chan int, test.outputs)
			sendOnly := make([]chan<- int, test.outputs)
			for i := range outs {
				outs[i] = make(chan int)
				sendOnly[i] = outs[i]
			}
			if test.nilOutput && len(sendOnly) > 0 {
				sendOnly[0] = nil
			}

			// Readers are only safe to start when Tee will actually run; on a panic they would park forever.
			results := make(chan []int, len(outs))
			if !test.wantPanic {
				for _, o := range outs {
					go func() {
						var got []int
						for v := range o {
							got = append(got, v)
						}
						results <- got
					}()
				}
			}

			Tee(in, sendOnly...)

			for i := 0; i < len(outs); i++ {
				got := <-results
				if diff := pretty.Compare(test.want, got); diff != "" {
					t.Errorf("TestTee(%s): output %d: -want/+got:\n%s", test.name, i, diff)
				}
			}
		}()
	}
}
