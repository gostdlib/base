package dynamic

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/retry/exponential"
	"github.com/kylelemons/godebug/pretty"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name         string
		options      []Option
		wantPrealloc int
		wantErr      bool
	}{
		{
			name: "Success: no options",
		},
		{
			name:         "Success: WithPreallocate with a positive size",
			options:      []Option{WithPreallocate(10)},
			wantPrealloc: 10,
		},
		{
			name:    "Success: WithPreallocate with a zero size",
			options: []Option{WithPreallocate(0)},
		},
		{
			name:    "Error: WithPreallocate with a negative size",
			options: []Option{WithPreallocate(-1)},
			wantErr: true,
		},
	}

	for _, test := range tests {
		s, err := New[int](test.options...)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestNew(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestNew(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		if s.Len() != 0 {
			t.Errorf("TestNew(%s): got Len() == %d, want Len() == 0", test.name, s.Len())
		}
		// Without this the prealloc rows assert nothing the zero-option row does not.
		if got := cap(s.scratch); got < fixedCases+test.wantPrealloc {
			t.Errorf("TestNew(%s): got cap(scratch) == %d, want >= %d", test.name, got, fixedCases+test.wantPrealloc)
		}
	}
}

func TestAdd(t *testing.T) {
	// Each error case breaks exactly one input off the valid baseline: either the channel is nil, or the
	// channel is already registered. Never both.
	tests := []struct {
		name    string
		send    bool
		nilCh   bool
		preAdd  bool
		wantErr bool
	}{
		{
			name: "Success: AddRecv with a valid channel",
		},
		{
			name: "Success: AddSend with a valid channel",
			send: true,
		},
		{
			name:    "Error: AddRecv with a nil channel",
			nilCh:   true,
			wantErr: true,
		},
		{
			name:    "Error: AddRecv with a channel already in use",
			preAdd:  true,
			wantErr: true,
		},
		{
			name:    "Error: AddSend with a nil channel",
			send:    true,
			nilCh:   true,
			wantErr: true,
		},
		{
			name:    "Error: AddSend with a channel already in use",
			send:    true,
			preAdd:  true,
			wantErr: true,
		},
	}

	for _, test := range tests {
		s, err := New[int]()
		if err != nil {
			t.Fatalf("TestAdd(%s): New: got err == %s, want err == nil", test.name, err)
		}

		ch := make(chan int, 1)
		if test.nilCh {
			ch = nil
		}
		if test.preAdd {
			if err := s.AddRecv(ch, nil); err != nil {
				t.Fatalf("TestAdd(%s): setup AddRecv: got err == %s, want err == nil", test.name, err)
			}
		}

		if test.send {
			err = s.AddSend(ch, 1, nil)
		} else {
			err = s.AddRecv(ch, nil)
		}

		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestAdd(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestAdd(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		if s.Len() != 1 {
			t.Errorf("TestAdd(%s): got Len() == %d, want Len() == 1", test.name, s.Len())
		}
	}
}

func TestSelect(t *testing.T) {
	// Every row registers its own case on a fresh Select and asserts the Kind, the Value, the surviving case
	// count and which values reached the Action. Asserting actions on every row turns "a close must not run the
	// Action" from a one-off test into a property checked for each kind.
	tests := []struct {
		name        string
		setup       func(t *testing.T, s *Select[int], action Action[int]) chan int
		cancel      bool
		wantKind    ResultKind
		wantValue   int
		wantLen     int
		wantActions []int
	}{
		{
			name: "Success: a value on a receive channel is Received",
			setup: func(t *testing.T, s *Select[int], action Action[int]) chan int {
				ch := make(chan int, 1)
				if err := s.AddRecv(ch, action); err != nil {
					t.Fatalf("TestSelect: AddRecv: got err == %s, want err == nil", err)
				}
				ch <- 42
				return ch
			},
			wantKind:    Received,
			wantValue:   42,
			wantLen:     1,
			wantActions: []int{42},
		},
		{
			name: "Success: a completed send is Sent and the case is removed",
			setup: func(t *testing.T, s *Select[int], action Action[int]) chan int {
				ch := make(chan int, 1)
				if err := s.AddSend(ch, 7, action); err != nil {
					t.Fatalf("TestSelect: AddSend: got err == %s, want err == nil", err)
				}
				return ch
			},
			wantKind:    Sent,
			wantValue:   7,
			wantLen:     0,
			wantActions: []int{7},
		},
		{
			name: "Success: a closed receive channel is Closed and runs no Action",
			setup: func(t *testing.T, s *Select[int], action Action[int]) chan int {
				ch := make(chan int, 1)
				if err := s.AddRecv(ch, action); err != nil {
					t.Fatalf("TestSelect: AddRecv: got err == %s, want err == nil", err)
				}
				close(ch)
				return ch
			},
			wantKind: Closed,
			wantLen:  0,
		},
		{
			name: "Success: a cancelled Context is CtxDone and runs no Action",
			setup: func(t *testing.T, s *Select[int], action Action[int]) chan int {
				ch := make(chan int, 1)
				if err := s.AddRecv(ch, action); err != nil {
					t.Fatalf("TestSelect: AddRecv: got err == %s, want err == nil", err)
				}
				return nil
			},
			cancel:   true,
			wantKind: CtxDone,
			wantLen:  1,
		},
	}

	for _, test := range tests {
		s, err := New[int]()
		if err != nil {
			t.Fatalf("TestSelect(%s): New: got err == %s, want err == nil", test.name, err)
		}

		var gotActions []int
		action := func(ctx context.Context, v int) {
			gotActions = append(gotActions, v)
		}
		wantCh := test.setup(t, s, action)

		ctx := t.Context()
		if test.cancel {
			cancelCtx, cancel := context.WithCancel(ctx)
			cancel()
			ctx = cancelCtx
		}

		got := s.Select(ctx)
		if got.Kind != test.wantKind {
			t.Errorf("TestSelect(%s): got Kind == %v, want %v", test.name, got.Kind, test.wantKind)
			continue
		}
		if got.Value != test.wantValue {
			t.Errorf("TestSelect(%s): got Value == %d, want %d", test.name, got.Value, test.wantValue)
		}
		// Received and Closed report RecvCh; Sent reports SendCh. wantCh is bidirectional, so convert it to the
		// direction under test. A nil wantCh converts to a nil directional channel, which is what CtxDone and
		// Defaulted report.
		switch got.Kind {
		case Sent:
			if got.SendCh != (chan<- int)(wantCh) {
				t.Errorf("TestSelect(%s): got SendCh == %v, want %v", test.name, got.SendCh, wantCh)
			}
		default:
			if got.RecvCh != (<-chan int)(wantCh) {
				t.Errorf("TestSelect(%s): got RecvCh == %v, want %v", test.name, got.RecvCh, wantCh)
			}
		}
		if s.Len() != test.wantLen {
			t.Errorf("TestSelect(%s): got Len() == %d, want %d", test.name, s.Len(), test.wantLen)
		}
		if diff := pretty.Compare(test.wantActions, gotActions); diff != "" {
			t.Errorf("TestSelect(%s): Actions: -want/+got:\n%s", test.name, diff)
		}

		// A send must actually put the value on the wire, not just report Sent.
		if test.wantKind == Sent {
			if v := <-wantCh; v != test.wantValue {
				t.Errorf("TestSelect(%s): got sent value == %d, want %d", test.name, v, test.wantValue)
			}
		}
	}
}

// TestActionCanMutate proves an Action runs with no lock held, so it may call back into the Select. Under the old
// design the Action ran under the mutex and this self-deadlocked.
func TestActionCanMutate(t *testing.T) {
	ctx := t.Context()

	s, err := New[int]()
	if err != nil {
		t.Fatalf("TestActionCanMutate: New: got err == %s, want err == nil", err)
	}

	first := make(chan int, 1)
	second := make(chan int, 1)
	action := func(ctx context.Context, v int) {
		if err := s.AddRecv(second, nil); err != nil {
			t.Errorf("TestActionCanMutate: AddRecv from Action: got err == %s, want err == nil", err)
		}
	}
	if err := s.AddRecv(first, action); err != nil {
		t.Fatalf("TestActionCanMutate: AddRecv: got err == %s, want err == nil", err)
	}

	first <- 1
	s.Select(ctx)

	if s.Len() != 2 {
		t.Errorf("TestActionCanMutate: got Len() == %d, want Len() == 2", s.Len())
	}
}

func TestTrySelect(t *testing.T) {
	ctx := t.Context()

	s, err := New[int]()
	if err != nil {
		t.Fatalf("TestTrySelect: New: got err == %s, want err == nil", err)
	}

	ch := make(chan int, 1)
	if err := s.AddRecv(ch, nil); err != nil {
		t.Fatalf("TestTrySelect: AddRecv: got err == %s, want err == nil", err)
	}

	// Nothing ready.
	got := s.TrySelect(ctx)
	if got.Kind != Defaulted {
		t.Errorf("TestTrySelect: with nothing ready, got Kind == %v, want Kind == %v", got.Kind, Defaulted)
	}

	// Something ready.
	ch <- 3
	got = s.TrySelect(ctx)
	if got.Kind != Received {
		t.Fatalf("TestTrySelect: with a value ready, got Kind == %v, want Kind == %v", got.Kind, Received)
	}
	if got.Value != 3 {
		t.Errorf("TestTrySelect: got Value == %d, want Value == 3", got.Value)
	}

	// TrySelect must not leave its default case behind and turn later blocking Selects into a spin.
	got = s.TrySelect(ctx)
	if got.Kind != Defaulted {
		t.Errorf("TestTrySelect: on the second empty try, got Kind == %v, want Kind == %v", got.Kind, Defaulted)
	}
}

// TestAddWhileBlocked is the regression test for removing the polling ticker. A Select is genuinely blocked in
// reflect.Select on a channel that never fires; adding a new channel from another goroutine must be picked up
// immediately rather than on the next tick. The old design waited up to 100ms for its ticker case to fire.
func TestAddWhileBlocked(t *testing.T) {
	ctx := t.Context()

	s, err := New[int]()
	if err != nil {
		t.Fatalf("TestAddWhileBlocked: New: got err == %s, want err == nil", err)
	}

	// A channel that is never written to, so the Select below truly blocks inside reflect.Select.
	idle := make(chan int)
	if err := s.AddRecv(idle, nil); err != nil {
		t.Fatalf("TestAddWhileBlocked: AddRecv: got err == %s, want err == nil", err)
	}

	late := make(chan int, 1)
	results := make(chan Result[int], 1)

	g := sync.Group{}
	g.Go(ctx, func(ctx context.Context) error {
		results <- s.Select(ctx)
		return nil
	})

	// Give the consumer time to actually park in reflect.Select.
	time.Sleep(20 * time.Millisecond)

	if err := s.AddRecv(late, nil); err != nil {
		t.Fatalf("TestAddWhileBlocked: late AddRecv: got err == %s, want err == nil", err)
	}
	late <- 99

	var got Result[int]
	select {
	case got = <-results:
	case <-time.After(5 * time.Second):
		t.Fatalf("TestAddWhileBlocked: Select never returned")
	}

	if err := g.Wait(ctx); err != nil {
		t.Fatalf("TestAddWhileBlocked: Wait: got err == %s, want err == nil", err)
	}

	if got.Kind != Received {
		t.Fatalf("TestAddWhileBlocked: got Kind == %v, want Kind == %v", got.Kind, Received)
	}
	if got.Value != 99 {
		t.Errorf("TestAddWhileBlocked: got Value == %d, want Value == 99", got.Value)
	}
	// Deliberately no wall-clock bound here: a latency assertion inside a correctness test fails under load for
	// reasons unrelated to the code. TestNoSpuriousWakeups covers "the ticker is gone" and only fails in the
	// safe direction, and BenchmarkAddLatency measures the number.
}

// TestRemoveWhileBlocked proves Remove() reaches a Select that is parked in reflect.Select.
func TestRemoveWhileBlocked(t *testing.T) {
	ctx := t.Context()

	s, err := New[int]()
	if err != nil {
		t.Fatalf("TestRemoveWhileBlocked: New: got err == %s, want err == nil", err)
	}

	idle := make(chan int)
	if err := s.AddRecv(idle, nil); err != nil {
		t.Fatalf("TestRemoveWhileBlocked: AddRecv: got err == %s, want err == nil", err)
	}

	done := make(chan struct{})
	g := sync.Group{}
	g.Go(ctx, func(ctx context.Context) error {
		defer close(done)
		s.Select(ctx)
		return nil
	})

	time.Sleep(20 * time.Millisecond)

	if !s.Remove(idle) {
		t.Errorf("TestRemoveWhileBlocked: got Remove() == false, want Remove() == true")
	}
	if s.Len() != 0 {
		t.Errorf("TestRemoveWhileBlocked: got Len() == %d, want Len() == 0", s.Len())
	}
	if s.Remove(idle) {
		t.Errorf("TestRemoveWhileBlocked: on a second Remove, got true, want false")
	}

	// The Select must still be parked: Remove() woke it, but nothing is ready, so it re-armed on the smaller
	// case set rather than returning a bogus Result or wedging.
	select {
	case <-done:
		t.Errorf("TestRemoveWhileBlocked: Select returned after Remove, want it to stay blocked")
	case <-time.After(50 * time.Millisecond):
	}

	// Now give it something so the goroutine can exit cleanly.
	other := make(chan int, 1)
	if err := s.AddRecv(other, nil); err != nil {
		t.Fatalf("TestRemoveWhileBlocked: AddRecv: got err == %s, want err == nil", err)
	}
	other <- 1

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("TestRemoveWhileBlocked: Select never returned")
	}
	if err := g.Wait(ctx); err != nil {
		t.Fatalf("TestRemoveWhileBlocked: Wait: got err == %s, want err == nil", err)
	}
}

// TestNoSpuriousWakeups proves the polling is gone. With no traffic and no mutations, a Select must stay parked
// rather than returning on a timer. The old design returned every 100ms.
func TestNoSpuriousWakeups(t *testing.T) {
	ctx := t.Context()

	s, err := New[int]()
	if err != nil {
		t.Fatalf("TestNoSpuriousWakeups: New: got err == %s, want err == nil", err)
	}

	idle := make(chan int)
	if err := s.AddRecv(idle, nil); err != nil {
		t.Fatalf("TestNoSpuriousWakeups: AddRecv: got err == %s, want err == nil", err)
	}

	returned := make(chan Result[int], 1)
	g := sync.Group{}
	g.Go(ctx, func(ctx context.Context) error {
		returned <- s.Select(ctx)
		return nil
	})

	// Several old tick intervals worth of quiet. Any return here is a spurious wakeup.
	select {
	case r := <-returned:
		t.Fatalf("TestNoSpuriousWakeups: Select returned %v during a quiet window, want it to stay parked", r.Kind)
	case <-time.After(350 * time.Millisecond):
	}

	idle <- 1
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatalf("TestNoSpuriousWakeups: Select never returned")
	}
	if err := g.Wait(ctx); err != nil {
		t.Fatalf("TestNoSpuriousWakeups: Wait: got err == %s, want err == nil", err)
	}
}

func TestAll(t *testing.T) {
	ctx := t.Context()

	s, err := New[int]()
	if err != nil {
		t.Fatalf("TestAll: New: got err == %s, want err == nil", err)
	}

	ch := make(chan int, 3)
	if err := s.AddRecv(ch, nil); err != nil {
		t.Fatalf("TestAll: AddRecv: got err == %s, want err == nil", err)
	}
	ch <- 1
	ch <- 2
	ch <- 3
	close(ch)

	var got []int
	var sawClosed bool
	for r := range s.All(ctx) {
		switch r.Kind {
		case Received:
			got = append(got, r.Value)
		case Closed:
			sawClosed = true
		}
		if sawClosed {
			break
		}
	}

	want := []int{1, 2, 3}
	if diff := pretty.Compare(want, got); diff != "" {
		t.Errorf("TestAll: -want/+got:\n%s", diff)
	}
	if !sawClosed {
		t.Errorf("TestAll: got sawClosed == false, want true")
	}
}

// TestAllStopsOnCancel proves All() terminates on a cancelled Context rather than racing the random case choice.
func TestAllStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	s, err := New[int]()
	if err != nil {
		t.Fatalf("TestAllStopsOnCancel: New: got err == %s, want err == nil", err)
	}

	ch := make(chan int, 1)
	if err := s.AddRecv(ch, nil); err != nil {
		t.Fatalf("TestAllStopsOnCancel: AddRecv: got err == %s, want err == nil", err)
	}
	cancel()

	var last ResultKind
	count := 0
	for r := range s.All(ctx) {
		last = r.Kind
		count++
		if count > 10 {
			t.Fatalf("TestAllStopsOnCancel: All did not stop on a cancelled Context")
		}
	}

	if last != CtxDone {
		t.Errorf("TestAllStopsOnCancel: got last Kind == %v, want %v", last, CtxDone)
	}
}

// TestSendNilInterface covers the reflect.ValueOf trap: for an interface T holding nil, reflect.ValueOf(value) is
// the invalid Value and reflect.Select would panic with "SendDir case missing Send value".
func TestSendNilInterface(t *testing.T) {
	ctx := t.Context()

	s, err := New[any]()
	if err != nil {
		t.Fatalf("TestSendNilInterface: New: got err == %s, want err == nil", err)
	}

	ch := make(chan any, 1)
	if err := s.AddSend(ch, nil, nil); err != nil {
		t.Fatalf("TestSendNilInterface: AddSend: got err == %s, want err == nil", err)
	}

	got := s.Select(ctx)
	if got.Kind != Sent {
		t.Fatalf("TestSendNilInterface: got Kind == %v, want Kind == %v", got.Kind, Sent)
	}
	if v := <-ch; v != nil {
		t.Errorf("TestSendNilInterface: got sent value == %v, want nil", v)
	}
}

// TestRecvNilInterface covers the receive side of the same trap: a plain assertion on a nil interface panics.
func TestRecvNilInterface(t *testing.T) {
	ctx := t.Context()

	s, err := New[any]()
	if err != nil {
		t.Fatalf("TestRecvNilInterface: New: got err == %s, want err == nil", err)
	}

	ch := make(chan any, 1)
	if err := s.AddRecv(ch, nil); err != nil {
		t.Fatalf("TestRecvNilInterface: AddRecv: got err == %s, want err == nil", err)
	}
	ch <- nil

	got := s.Select(ctx)
	if got.Kind != Received {
		t.Fatalf("TestRecvNilInterface: got Kind == %v, want Kind == %v", got.Kind, Received)
	}
	if got.Value != nil {
		t.Errorf("TestRecvNilInterface: got Value == %v, want nil", got.Value)
	}
}

func TestConcurrentAdds(t *testing.T) {
	ctx := t.Context()

	s, err := New[int](WithPreallocate(64))
	if err != nil {
		t.Fatalf("TestConcurrentAdds: New: got err == %s, want err == nil", err)
	}

	const total = 64
	chans := make([]chan int, total)
	for i := 0; i < total; i++ {
		chans[i] = make(chan int, 1)
	}

	g := sync.Group{}
	for i := 0; i < total; i++ {
		g.Go(ctx, func(ctx context.Context) error {
			return s.AddRecv(chans[i], nil)
		})
	}
	if err := g.Wait(ctx); err != nil {
		t.Fatalf("TestConcurrentAdds: Wait: got err == %s, want err == nil", err)
	}

	if s.Len() != total {
		t.Errorf("TestConcurrentAdds: got Len() == %d, want Len() == %d", s.Len(), total)
	}

	// Drain one value from every channel to prove all cases are live.
	for i := 0; i < total; i++ {
		chans[i] <- i
	}
	seen := map[int]bool{}
	for i := 0; i < total; i++ {
		r := s.Select(ctx)
		if r.Kind != Received {
			t.Fatalf("TestConcurrentAdds: got Kind == %v, want Kind == %v", r.Kind, Received)
		}
		seen[r.Value] = true
	}
	if len(seen) != total {
		t.Errorf("TestConcurrentAdds: got %d distinct values, want %d", len(seen), total)
	}
}

// BenchmarkSelect measures the steady state receive path. Our own code allocates nothing here, but reflect.Select
// allocates one object per receive case per call, so allocs/op should scale with the case count. This is the cost
// that keeps this package a "not needed often" tool.
func BenchmarkSelect(b *testing.B) {
	for _, cases := range []int{1, 4, 16, 64} {
		b.Run(strconv.Itoa(cases), func(b *testing.B) {
			ctx := b.Context()

			s, err := New[int](WithPreallocate(cases))
			if err != nil {
				b.Fatalf("BenchmarkSelect: New: got err == %s, want err == nil", err)
			}

			chans := make([]chan int, cases)
			for i := 0; i < cases; i++ {
				chans[i] = make(chan int, 1)
				if err := s.AddRecv(chans[i], nil); err != nil {
					b.Fatalf("BenchmarkSelect: AddRecv: got err == %s, want err == nil", err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				chans[i%cases] <- i
				s.Select(ctx)
			}
		})
	}
}

// BenchmarkAddLatency measures how long it takes a case added from another goroutine to become visible to a Select
// that is already parked. The polling design this replaced paid up to a full tick interval here.
func BenchmarkAddLatency(b *testing.B) {
	ctx := b.Context()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s, err := New[int]()
		if err != nil {
			b.Fatalf("BenchmarkAddLatency: New: got err == %s, want err == nil", err)
		}
		idle := make(chan int)
		if err := s.AddRecv(idle, nil); err != nil {
			b.Fatalf("BenchmarkAddLatency: AddRecv: got err == %s, want err == nil", err)
		}

		results := make(chan Result[int], 1)
		g := sync.Group{}
		g.Go(ctx, func(ctx context.Context) error {
			results <- s.Select(ctx)
			return nil
		})
		time.Sleep(2 * time.Millisecond)

		late := make(chan int, 1)
		late <- 1
		b.StartTimer()

		if err := s.AddRecv(late, nil); err != nil {
			b.Fatalf("BenchmarkAddLatency: late AddRecv: got err == %s, want err == nil", err)
		}
		<-results
		b.StopTimer()

		if err := g.Wait(ctx); err != nil {
			b.Fatalf("BenchmarkAddLatency: Wait: got err == %s, want err == nil", err)
		}
		b.StartTimer()
	}
}

// TestSelectPrioritizesCtxDone is the regression test for Select() ignoring a cancelled Context whenever another
// case is also ready. reflect.Select picks uniformly at random among ready cases, so without an explicit ctx
// pre-check Select returns Received (and runs the Action) about half the time after cancellation, contradicting
// its own doc.
func TestSelectPrioritizesCtxDone(t *testing.T) {
	tests := []struct {
		name string
		try  bool
	}{
		{name: "Success: Select returns CtxDone even when a channel is ready"},
		{name: "Success: TrySelect returns CtxDone even when a channel is ready", try: true},
	}

	const iters = 200

	for _, test := range tests {
		s, err := New[int]()
		if err != nil {
			t.Fatalf("TestSelectPrioritizesCtxDone(%s): New: got err == %s, want err == nil", test.name, err)
		}

		var actions int
		ch := make(chan int, 1)
		action := func(ctx context.Context, v int) { actions++ }
		if err := s.AddRecv(ch, action); err != nil {
			t.Fatalf("TestSelectPrioritizesCtxDone(%s): AddRecv: got err == %s, want err == nil", test.name, err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		got := 0
		for i := 0; i < iters; i++ {
			select {
			case ch <- 1:
			default:
			}

			var r Result[int]
			switch {
			case test.try:
				r = s.TrySelect(ctx)
			default:
				r = s.Select(ctx)
			}
			if r.Kind != CtxDone {
				got++
			}
		}

		if got != 0 {
			t.Errorf("TestSelectPrioritizesCtxDone(%s): got %d/%d non-CtxDone results, want 0", test.name, got, iters)
		}
		if actions != 0 {
			t.Errorf("TestSelectPrioritizesCtxDone(%s): got %d Actions run after cancel, want 0", test.name, actions)
		}
	}
}

// TestSingleConsumerGuard is the regression test for the guard being held only inside each Select() call. All()
// used to acquire it per call and release it before yielding, so a loop body could re-enter undetected and two
// goroutines ranging All() would interleave instead of panicking.
func TestSingleConsumerGuard(t *testing.T) {
	tests := []struct {
		name      string
		reenter   func(s *Select[int], ctx context.Context)
		wantPanic bool
	}{
		{
			name:      "Success: a loop body that does not re-enter is fine",
			reenter:   func(s *Select[int], ctx context.Context) {},
			wantPanic: false,
		},
		{
			name:      "Error: calling Select from inside an All loop body panics",
			reenter:   func(s *Select[int], ctx context.Context) { s.Select(ctx) },
			wantPanic: true,
		},
		{
			name:      "Error: calling TrySelect from inside an All loop body panics",
			reenter:   func(s *Select[int], ctx context.Context) { s.TrySelect(ctx) },
			wantPanic: true,
		},
	}

	for _, test := range tests {
		func() {
			gotPanic := false
			defer func() {
				if r := recover(); r != nil {
					gotPanic = true
				}
				if gotPanic != test.wantPanic {
					t.Errorf("TestSingleConsumerGuard(%s): got panic == %t, want %t", test.name, gotPanic, test.wantPanic)
				}
			}()

			s, err := New[int]()
			if err != nil {
				t.Fatalf("TestSingleConsumerGuard(%s): New: got err == %s, want err == nil", test.name, err)
			}
			// Two buffered values so the re-entrant call has one ready and returns instead of blocking.
			// Without this a regression fails by hanging rather than by reporting the missing panic.
			ch := make(chan int, 2)
			if err := s.AddRecv(ch, nil); err != nil {
				t.Fatalf("TestSingleConsumerGuard(%s): AddRecv: got err == %s, want err == nil", test.name, err)
			}
			ch <- 1
			ch <- 2

			ctx := t.Context()
			for range s.All(ctx) {
				test.reenter(s, ctx)
				break
			}
		}()
	}
}

// TestRemove covers the documented no-op paths alongside a real removal.
func TestRemove(t *testing.T) {
	tests := []struct {
		name    string
		nilCh   bool
		absent  bool
		want    bool
		wantLen int
	}{
		{name: "Success: removing a registered channel reports true", want: true},
		{name: "Error: removing a nil channel reports false", nilCh: true, wantLen: 1},
		{name: "Error: removing an unregistered channel reports false", absent: true, wantLen: 1},
	}

	for _, test := range tests {
		s, err := New[int]()
		if err != nil {
			t.Fatalf("TestRemove(%s): New: got err == %s, want err == nil", test.name, err)
		}

		registered := make(chan int, 1)
		if err := s.AddRecv(registered, nil); err != nil {
			t.Fatalf("TestRemove(%s): AddRecv: got err == %s, want err == nil", test.name, err)
		}

		target := registered
		switch {
		case test.nilCh:
			target = nil
		case test.absent:
			target = make(chan int, 1)
		}

		if got := s.Remove(target); got != test.want {
			t.Errorf("TestRemove(%s): got %t, want %t", test.name, got, test.want)
		}
		if s.Len() != test.wantLen {
			t.Errorf("TestRemove(%s): got Len() == %d, want %d", test.name, s.Len(), test.wantLen)
		}
	}
}

// TestAddErrorIdentity pins the exported sentinels and their permanent marking. Without this the table in
// TestAdd would still pass if AddRecv returned the wrong sentinel, or stopped marking errors permanent.
func TestAddErrorIdentity(t *testing.T) {
	tests := []struct {
		name   string
		call   func(s *Select[int], ch chan int) error
		nilCh  bool
		preAdd bool
		wantIs error
	}{
		{
			name:   "Success: a valid AddRecv returns no error",
			call:   func(s *Select[int], ch chan int) error { return s.AddRecv(ch, nil) },
			wantIs: nil,
		},
		{
			name:   "Error: AddRecv with a nil channel is ErrNilChan",
			call:   func(s *Select[int], ch chan int) error { return s.AddRecv(ch, nil) },
			nilCh:  true,
			wantIs: ErrNilChan,
		},
		{
			name:   "Error: AddRecv with a duplicate channel is ErrDupChan",
			call:   func(s *Select[int], ch chan int) error { return s.AddRecv(ch, nil) },
			preAdd: true,
			wantIs: ErrDupChan,
		},
		{
			name:   "Error: AddSend with a nil channel is ErrNilChan",
			call:   func(s *Select[int], ch chan int) error { return s.AddSend(ch, 1, nil) },
			nilCh:  true,
			wantIs: ErrNilChan,
		},
	}

	for _, test := range tests {
		s, err := New[int]()
		if err != nil {
			t.Fatalf("TestAddErrorIdentity(%s): New: got err == %s, want err == nil", test.name, err)
		}

		ch := make(chan int, 1)
		if test.nilCh {
			ch = nil
		}
		if test.preAdd {
			if err := s.AddRecv(ch, nil); err != nil {
				t.Fatalf("TestAddErrorIdentity(%s): setup: got err == %s, want err == nil", test.name, err)
			}
		}

		err = test.call(s, ch)
		if test.wantIs == nil {
			if err != nil {
				t.Errorf("TestAddErrorIdentity(%s): got err == %s, want err == nil", test.name, err)
			}
			continue
		}
		if !errors.Is(err, test.wantIs) {
			t.Errorf("TestAddErrorIdentity(%s): got err == %v, want errors.Is(err, %v)", test.name, err, test.wantIs)
		}
		if !errors.Is(err, exponential.ErrPermanent) {
			t.Errorf("TestAddErrorIdentity(%s): got err == %v, want it wrapped with ErrPermanent", test.name, err)
		}
	}
}

// TestNewErrorIdentity pins that a bad Option surfaces as ErrBadOption and is marked permanent.
func TestNewErrorIdentity(t *testing.T) {
	if _, err := New[int](WithPreallocate(10)); err != nil {
		t.Errorf("TestNewErrorIdentity: got err == %s, want err == nil", err)
	}

	_, err := New[int](WithPreallocate(-1))
	if !errors.Is(err, ErrBadOption) {
		t.Errorf("TestNewErrorIdentity: got err == %v, want errors.Is(err, ErrBadOption)", err)
	}
	if !errors.Is(err, exponential.ErrPermanent) {
		t.Errorf("TestNewErrorIdentity: got err == %v, want it wrapped with ErrPermanent", err)
	}
}

// events exercises Remove against a defined channel type, which a type switch on the concrete channel types would
// miss even though AddRecv accepts it.
type events chan int

// TestRemoveChannelTypes covers Remove's any parameter: it matches by channel identity, so any direction or defined
// type naming the same channel removes it, while a non-channel or a channel of the wrong element type is a
// programming error and panics.
func TestRemoveChannelTypes(t *testing.T) {
	tests := []struct {
		name      string
		target    func(registered chan int) any
		want      bool
		wantLen   int
		wantPanic bool
	}{
		{
			name:   "Success: bidirectional channel",
			target: func(registered chan int) any { return registered },
			want:   true,
		},
		{
			name:   "Success: receive only view of the registered channel",
			target: func(registered chan int) any { return (<-chan int)(registered) },
			want:   true,
		},
		{
			name:   "Success: send only view of the registered channel",
			target: func(registered chan int) any { return (chan<- int)(registered) },
			want:   true,
		},
		{
			name:    "Success: untyped nil reports false",
			target:  func(chan int) any { return nil },
			wantLen: 1,
		},
		{
			name:      "Error: not a channel",
			target:    func(chan int) any { return 42 },
			wantLen:   1,
			wantPanic: true,
		},
		{
			name:      "Error: channel of the wrong element type",
			target:    func(chan int) any { return make(chan string) },
			wantLen:   1,
			wantPanic: true,
		},
	}

	for _, test := range tests {
		func() {
			defer func() {
				r := recover()
				switch {
				case r == nil && test.wantPanic:
					t.Errorf("TestRemoveChannelTypes(%s): got no panic, want panic", test.name)
				case r != nil && !test.wantPanic:
					t.Errorf("TestRemoveChannelTypes(%s): got panic == %v, want no panic", test.name, r)
				}
			}()

			s, err := New[int]()
			if err != nil {
				t.Fatalf("TestRemoveChannelTypes(%s): New: got err == %s, want err == nil", test.name, err)
			}
			registered := make(chan int, 1)
			if err := s.AddRecv(registered, nil); err != nil {
				t.Fatalf("TestRemoveChannelTypes(%s): AddRecv: got err == %s, want err == nil", test.name, err)
			}

			if got := s.Remove(test.target(registered)); got != test.want {
				t.Errorf("TestRemoveChannelTypes(%s): got %t, want %t", test.name, got, test.want)
			}
			if s.Len() != test.wantLen {
				t.Errorf("TestRemoveChannelTypes(%s): got Len() == %d, want Len() == %d", test.name, s.Len(), test.wantLen)
			}
		}()
	}
}

// TestAddRecvDefinedType pins that a defined channel type can be both added and removed, which is the pairing the
// pointer keyed exists map exists to preserve.
func TestAddRecvDefinedType(t *testing.T) {
	s, err := New[int]()
	if err != nil {
		t.Fatalf("TestAddRecvDefinedType: New: got err == %s, want err == nil", err)
	}
	ev := make(events, 1)
	if err := s.AddRecv(ev, nil); err != nil {
		t.Fatalf("TestAddRecvDefinedType: AddRecv: got err == %s, want err == nil", err)
	}
	if !s.Remove(ev) {
		t.Errorf("TestAddRecvDefinedType: got Remove == false, want true")
	}
	if s.Len() != 0 {
		t.Errorf("TestAddRecvDefinedType: got Len() == %d, want Len() == 0", s.Len())
	}
}

// TestAddDuplicateAcrossDirections pins that the duplicate check is by channel identity, so the same channel cannot
// be registered twice even when the two adds name it with different directional types.
func TestAddDuplicateAcrossDirections(t *testing.T) {
	s, err := New[int]()
	if err != nil {
		t.Fatalf("TestAddDuplicateAcrossDirections: New: got err == %s, want err == nil", err)
	}
	ch := make(chan int, 1)
	if err := s.AddRecv(ch, nil); err != nil {
		t.Fatalf("TestAddDuplicateAcrossDirections: AddRecv: got err == %s, want err == nil", err)
	}
	if err := s.AddSend(ch, 1, nil); err == nil {
		t.Errorf("TestAddDuplicateAcrossDirections: AddSend of the same channel: got err == nil, want err != nil")
	}
	if s.Len() != 1 {
		t.Errorf("TestAddDuplicateAcrossDirections: got Len() == %d, want Len() == 1", s.Len())
	}
}

// TestAddRecvAll covers the batch registration path. It is all or nothing, so a rejected batch must leave the Select
// exactly as it was, which is what wantLen pins on the error rows.
func TestAddRecvAll(t *testing.T) {
	tests := []struct {
		name    string
		chans   func(registered chan int) []<-chan int
		preAdd  bool
		wantLen int
		wantIs  error
	}{
		{
			name:    "Success: several channels in one call",
			chans:   func(chan int) []<-chan int { return []<-chan int{make(chan int), make(chan int), make(chan int)} },
			wantLen: 3,
		},
		{
			name:    "Success: a single channel",
			chans:   func(chan int) []<-chan int { return []<-chan int{make(chan int)} },
			wantLen: 1,
		},
		{
			name:    "Success: no channels is a no-op",
			chans:   func(chan int) []<-chan int { return nil },
			wantLen: 0,
		},
		{
			name: "Error: a nil channel rejects the whole batch",
			chans: func(chan int) []<-chan int {
				return []<-chan int{make(chan int), nil, make(chan int)}
			},
			wantIs: ErrNilChan,
		},
		{
			name: "Error: a channel repeated within the batch rejects the whole batch",
			chans: func(chan int) []<-chan int {
				ch := make(chan int)
				return []<-chan int{ch, make(chan int), ch}
			},
			wantIs: ErrDupChan,
		},
		{
			name:    "Error: a channel already registered rejects the whole batch",
			chans:   func(registered chan int) []<-chan int { return []<-chan int{make(chan int), registered} },
			preAdd:  true,
			wantLen: 1,
			wantIs:  ErrDupChan,
		},
	}

	for _, test := range tests {
		s, err := New[int]()
		if err != nil {
			t.Fatalf("TestAddRecvAll(%s): New: got err == %s, want err == nil", test.name, err)
		}
		registered := make(chan int, 1)
		if test.preAdd {
			if err := s.AddRecv(registered, nil); err != nil {
				t.Fatalf("TestAddRecvAll(%s): setup AddRecv: got err == %s, want err == nil", test.name, err)
			}
		}

		err = s.AddRecvAll(nil, test.chans(registered)...)
		switch {
		case err == nil && test.wantIs != nil:
			t.Errorf("TestAddRecvAll(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && test.wantIs == nil:
			t.Errorf("TestAddRecvAll(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil && !errors.Is(err, test.wantIs):
			t.Errorf("TestAddRecvAll(%s): got err == %s, want errors.Is(err, %s)", test.name, err, test.wantIs)
			continue
		}

		if s.Len() != test.wantLen {
			t.Errorf("TestAddRecvAll(%s): got Len() == %d, want Len() == %d", test.name, s.Len(), test.wantLen)
		}
	}
}

// TestAddRecvAllDelivers pins that channels registered in a batch actually receive, which Len() alone does not show.
func TestAddRecvAllDelivers(t *testing.T) {
	s, err := New[int]()
	if err != nil {
		t.Fatalf("TestAddRecvAllDelivers: New: got err == %s, want err == nil", err)
	}

	const n = 8
	chs := make([]<-chan int, 0, n)
	for i := 0; i < n; i++ {
		ch := make(chan int, 1)
		ch <- i
		close(ch)
		chs = append(chs, ch)
	}
	if err := s.AddRecvAll(nil, chs...); err != nil {
		t.Fatalf("TestAddRecvAllDelivers: AddRecvAll: got err == %s, want err == nil", err)
	}

	got := []int{}
	for result := range s.All(t.Context()) {
		if result.Kind == Received {
			got = append(got, result.Value)
		}
		if s.Len() == 0 {
			break
		}
	}
	slices.Sort(got)

	want := []int{0, 1, 2, 3, 4, 5, 6, 7}
	if diff := pretty.Compare(want, got); diff != "" {
		t.Errorf("TestAddRecvAllDelivers: -want/+got:\n%s", diff)
	}
}

// TestAddTooManyCases exercises ErrTooManyCases, which is only practical to reach through AddRecvAll: the batch is
// rejected on the count before any snapshot copy happens, so this costs the channels and nothing else.
func TestAddTooManyCases(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		wantLen int
		wantIs  error
	}{
		{
			name:    "Success: exactly the maximum number of cases",
			count:   maxCases,
			wantLen: maxCases,
		},
		{
			name:   "Error: one case past the maximum",
			count:  maxCases + 1,
			wantIs: ErrTooManyCases,
		},
	}

	for _, test := range tests {
		s, err := New[int]()
		if err != nil {
			t.Fatalf("TestAddTooManyCases(%s): New: got err == %s, want err == nil", test.name, err)
		}
		chs := make([]<-chan int, test.count)
		for i := range chs {
			chs[i] = make(chan int)
		}

		err = s.AddRecvAll(nil, chs...)
		switch {
		case err == nil && test.wantIs != nil:
			t.Errorf("TestAddTooManyCases(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && test.wantIs == nil:
			t.Errorf("TestAddTooManyCases(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil && !errors.Is(err, test.wantIs):
			t.Errorf("TestAddTooManyCases(%s): got err == %s, want errors.Is(err, %s)", test.name, err, test.wantIs)
			continue
		}

		if s.Len() != test.wantLen {
			t.Errorf("TestAddTooManyCases(%s): got Len() == %d, want Len() == %d", test.name, s.Len(), test.wantLen)
		}
	}
}
