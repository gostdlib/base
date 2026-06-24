package init

import (
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// fakeNotifier is a fake implementation of signal.Notify to intercept signals for testing.
var fakeNotifier = func(c chan<- os.Signal, sig ...os.Signal) {
	go func() {
		for _, s := range sig {
			c <- s
		}
	}()
}

func TestHandleSignals(t *testing.T) {
	originalNotifier := notifier
	notifier = fakeNotifier
	t.Cleanup(
		func() { notifier = originalNotifier },
	)

	originalClose := closeCall
	closeCount := 0
	closeCall = func(InitArgs) { closeCount++ }
	t.Cleanup(
		func() { closeCall = originalClose },
	)

	originalPanic := panicCall
	didPanic := atomic.Pointer[bool]{}
	bFalse := false
	didPanic.Store(&bFalse)
	panicCall = func(v any) {
		t := true
		didPanic.Store(&t)
	}
	t.Cleanup(
		func() { panicCall = originalPanic },
	)

	// Create a map to track which handlers were called.
	calledHandlers := make(map[os.Signal]bool)
	mu := sync.Mutex{}
	handlers := map[os.Signal]func(){
		syscall.SIGQUIT: func() { mu.Lock(); defer mu.Unlock(); calledHandlers[syscall.SIGQUIT] = true },
		syscall.SIGINT:  func() { mu.Lock(); defer mu.Unlock(); calledHandlers[syscall.SIGINT] = true },
		syscall.SIGTERM: func() { mu.Lock(); defer mu.Unlock(); calledHandlers[syscall.SIGTERM] = true },
	}

	for sig, f := range handlers {
		didPanic.Store(&bFalse)
		done := make(chan struct{})
		var err error

		// Call handleSignals in a separate goroutine to simulate asynchronous signal handling.
		go func() {
			defer close(done)
			args := InitArgs{
				SignalHandlers: map[os.Signal]func(){
					sig: f,
				},
			}
			err = handleSignals(args)
			time.Sleep(1 * time.Second)
		}()

		<-done

		if err != nil {
			t.Errorf("TestHandleSignals(%s): did not expect error, got: %s", sig, err)
		}
		if !(*didPanic.Load()) {
			t.Errorf("TestHandleSignals(%s): expected panic, got no panic", sig)
		}
	}

	// Verify that the handlers were called.
	for sig, called := range calledHandlers {
		if !called {
			t.Errorf("Handler for signal %s was not called", sig)
		}
	}
	if closeCount != len(handlers) {
		t.Errorf("TestHandleSignals: expected close to be called %d, was called %d", len(handlers), closeCount)
	}
}

// TestSignalsInitWiring verifies that the setup state machine reaches signalsInit and that it
// only registers OS signal notification when SignalHandlers were actually provided. This guards
// against a regression where the handlers were documented but never wired into Service().
func TestSignalsInitWiring(t *testing.T) {
	originalNotifier := notifier
	t.Cleanup(
		func() { notifier = originalNotifier },
	)

	tests := []struct {
		name       string
		handlers   map[os.Signal]func()
		wantNotify bool
	}{
		{
			name:       "Success: no handlers skips signal registration",
			handlers:   nil,
			wantNotify: false,
		},
		{
			name: "Success: handlers register signal notification",
			handlers: map[os.Signal]func(){
				syscall.SIGTERM: func() {},
			},
			wantNotify: true,
		},
	}

	for _, test := range tests {
		notified := atomic.Bool{}
		notifier = func(c chan<- os.Signal, sig ...os.Signal) { notified.Store(true) }

		s := setup{args: InitArgs{SignalHandlers: test.handlers}}
		next, err := s.signalsInit()
		switch {
		case err != nil:
			t.Errorf("TestSignalsInitWiring(%s): got err == %s, want err == nil", test.name, err)
			continue
		case next == nil:
			t.Errorf("TestSignalsInitWiring(%s): got next state == nil, want next state != nil", test.name)
			continue
		}
		if notified.Load() != test.wantNotify {
			t.Errorf("TestSignalsInitWiring(%s): got notifier called == %v, want %v", test.name, notified.Load(), test.wantNotify)
		}
	}
}

// TestHandleSignalsValidation verifies that handleSignals rejects nil handlers and any signal
// other than SIGQUIT, SIGINT or SIGTERM, matching the documented contract that registering an
// unsupported signal causes Service() to panic.
func TestHandleSignalsValidation(t *testing.T) {
	originalNotifier := notifier
	notifier = func(c chan<- os.Signal, sig ...os.Signal) {}
	t.Cleanup(
		func() { notifier = originalNotifier },
	)

	tests := []struct {
		name     string
		handlers map[os.Signal]func()
		wantErr  bool
	}{
		{
			name:     "Success: SIGTERM is a supported signal",
			handlers: map[os.Signal]func(){syscall.SIGTERM: func() {}},
			wantErr:  false,
		},
		{
			name:     "Error: nil handler is rejected",
			handlers: map[os.Signal]func(){syscall.SIGTERM: nil},
			wantErr:  true,
		},
		{
			name:     "Error: unsupported signal is rejected",
			handlers: map[os.Signal]func(){syscall.SIGHUP: func() {}},
			wantErr:  true,
		},
	}

	for _, test := range tests {
		err := handleSignals(InitArgs{SignalHandlers: test.handlers})
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestHandleSignalsValidation(%s): got err == nil, want err != nil", test.name)
		case err != nil && !test.wantErr:
			t.Errorf("TestHandleSignalsValidation(%s): got err == %s, want err == nil", test.name, err)
		}
	}
}

func TestHandleSignal(t *testing.T) {
	handled := false

	tests := []struct {
		name      string
		sig       os.Signal
		handlers  map[os.Signal]func()
		expectErr bool
	}{
		{
			name: "Handle SIGQUIT",
			sig:  syscall.SIGQUIT,
			handlers: map[os.Signal]func(){
				syscall.SIGQUIT: func() {
					handled = true
				},
			},
			expectErr: true,
		},
		{
			name: "Handle SIGINT",
			sig:  syscall.SIGINT,
			handlers: map[os.Signal]func(){
				syscall.SIGINT: func() {
					handled = true
				},
			},
			expectErr: true,
		},
		{
			name: "Handle SIGTERM",
			sig:  syscall.SIGTERM,
			handlers: map[os.Signal]func(){
				syscall.SIGTERM: func() {
					handled = true
				},
			},
			expectErr: true,
		},
	}

	for _, test := range tests {
		handled = false
		err := handleSignal(test.sig, test.handlers)
		switch {
		case test.expectErr && err == nil:
			t.Errorf("TestHandleSignal(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.expectErr && err != nil:
			t.Errorf("TestHandleSignal(%s): got err == %v, want err == nil", test.name, err)
			continue
		}
		if !handled {
			t.Errorf("TestHandleSignal(%s): signal not handled", test.name)
		}
	}
}
