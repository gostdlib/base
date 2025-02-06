package init

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gostdlib/base/telemetry/log"
)

// These can be overridden in tests.
var (
	notifier  = signal.Notify
	closeCall = Close
	panicCall = panicer
)

// panicer panics. You can't assign panic as a variable, so you need a wrapper.
func panicer(v any) {
	panic(v)
}

// handleSignals registers signal handlers for the given signals.
// If the signal is SIGQUIT, SIGINT, or SIGTERM this will panic after the handler is called.
// In that case it will also call Close().
func handleSignals(args InitArgs) error {
	for sig, f := range args.SignalHandlers {
		if f == nil {
			return fmt.Errorf("signal(%s) was registered with a nil handler", sig)
		}
	}

	notifyCh := make(chan os.Signal, 1)
	sigs := []os.Signal{}
	for sig := range args.SignalHandlers {
		sigs = append(sigs, sig)
	}

	notifier(notifyCh, sigs...)

	go func() {
		for sig := range notifyCh {
			log.Default().Error(fmt.Sprintf("Received signal: %s", sig))
			if err := handleSignal(sig, args.SignalHandlers); err != nil {
				// We are already shutting down, so ignore any further signals
				// and just wait for the process to exit.
				go func() {
					for range notifyCh {
					}
				}()
				closeCall(args)
				panicCall(fmt.Sprintf("signal(%s)", sig))
				return // For tests where panic is overridden
			}
		}
	}()
	return nil
}

// handleSignal handles an individual signal. If the signal is SIGQUIT, SIGINT, or SIGTERM
// it will call the handler function for that signal and return an error. Otherwise, it will
// return nil.
func handleSignal(sig os.Signal, handlers map[os.Signal]func()) error {
	switch sig {
	case syscall.SIGQUIT, syscall.SIGINT, syscall.SIGTERM:
		f := handlers[sig]
		f()
		return fmt.Errorf("signal(%s)", sig)
	default:
		f := handlers[sig]
		f()
	}
	return nil
}
