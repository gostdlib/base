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

// handleSignals registers signal handlers for the given signals. Only SIGQUIT, SIGINT and
// SIGTERM may be registered; any other signal, or a nil handler, is rejected with an error.
// When a registered signal is received, its handler is called, then Close() is called and
// the process panics.
func handleSignals(args InitArgs) error {
	for sig, f := range args.SignalHandlers {
		if f == nil {
			return fmt.Errorf("signal(%s) was registered with a nil handler", sig)
		}
		switch sig {
		case syscall.SIGQUIT, syscall.SIGINT, syscall.SIGTERM:
		default:
			return fmt.Errorf("signal(%s) is not supported; only SIGQUIT, SIGINT and SIGTERM may be registered", sig)
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

// handleSignal handles an individual signal by calling its registered handler and returning an
// error so the caller initiates graceful shutdown (Close() then panic). Only SIGQUIT, SIGINT and
// SIGTERM can reach here, since handleSignals rejects any other signal at registration.
func handleSignal(sig os.Signal, handlers map[os.Signal]func()) error {
	f := handlers[sig]
	f()
	return fmt.Errorf("signal(%s)", sig)
}
