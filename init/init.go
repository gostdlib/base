/*
Package init provides service initialization and close functions. These functions represent a
set of standards around services.

When building a project using the genproject binary, an "init" package will be setup that refers to this one.

The Service() function does things like setup logging, tracing, audit logging and signal handling. It also
sets the GOMAXPROCS based on the real CPUs available and will set the GOMEMLIMIT based on the container memory limit
if running in a Linux container and will honor a pre-set GOMEMLIMIT if set. Defaults for other packages in
the base/ packages are set like the default worker pool and background tasks.

It determines what should and shouldn't run based on the environment the service is running in.
Users can register their own initialization functions to run at the end of the Service() function to
centralize service initialization.

The Close() function does things like close various clients that were setup, flush logs and handle panics
so that we ensure they are written to the log before existing (it does not stop a panic).

Serivce() and Close() should always be called in the main function of the service if using directly (if using
the one generated by genproject, this is slightly different).

Example of a basic service setup:

	package main

	import (
		"github.com/gostdlib/base/init"
	)

	var serviceMeta = init.Meta{
		Service: "[your service name]",
		Build:   "[your image tag]",
	}

	var initArgs = int.Args{Meta: serviceMeta}

	func main() {
		init.Service(initArgs)
		defer init.Close(initArgs)

		// Insert the rest of your code
	}

Sometimes you want other registrations to be done, but you want them to be done after other init() functions
and after things like logging and tracing are setup. This is where the InitFunc type comes in. You can register
an init to happen after stadard init.Service() functions are called.

This is normally done in a package init(), but if you need to control exact ordering you can do it in main() before
init.Service() is called.

Example RegisterInit:

	package expvar

	import (
		"net/http"
		"expvar"

		"github.com/gostdlib/base/init"
	)

	func init() {
		init.RegisterInit(Init)
	}

	// Init is the function that will be called after init.Service() has done all its setups.
	func Init(i init.InitArgs) error {
		var mux *http.ServeMux

		// Extract a mux from the InitArgs. If not provided, return an error.
		v := i.Opaque("httpMuxer")
		if v == nil {
			return fmt.Errorf("httpMuxer key not found in InitArgs")
		}
		mux.Handle("/debug/vars", expvar.Handler())
	}

In some cases, you may want to close resources that were opened in the Service() function. This is
where RegisterClose() can be used. Below is an example, though I probably wouldn't use this for this
purpose, however it shows a case where you might want to close something.

Example RegisterClose:

		package storage

		import (
			...
			"github.com/gostdlib/base/init"
			...
		)

		var db *sql.DB

		// We use init here to register our Init() function that will happen after microsoft.Init() has
		// done all its setups.
		func init() {
			init.RegisterInit(Init)
			init.RegisterClose(Close)
		}

		// Init is the function that will be called after init.Service() has done all its setups.
		func Init(init.InitArgs) error {
			// Extract a mux from the InitArgs. If not provided, return an error.
			v := i.Opaque("storageDB")
			if v == nil {
				return fmt.Errorf("storageDB key not found in InitArgs")
			}
			ptr := v.(*sql.DB)

			var err error
			db, err = sql.Open("mysql", "username:password@tcp(127.0.0.1:3306)/jazzrecords")
			if err != nil {
	  			return err
			}
			ptr = *db

			return nil
		}

		func Close(i IntArgs) {
			v := i.Opaque("storageDB").(*sql.DB)
			if err := db.Close; err != nil {
				log.Default().Error(fmt.Sprintf("could not close db: %v", err))
			}
		}
*/
package init

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gostdlib/base/concurrency/background"
	"github.com/gostdlib/base/concurrency/worker"
	"github.com/gostdlib/base/env/detect"
	"github.com/gostdlib/base/telemetry/log"
	"github.com/gostdlib/base/telemetry/otel/metrics"
	"github.com/gostdlib/base/telemetry/otel/trace"

	"github.com/google/uuid"
	"github.com/gostdlib/concurrency/prim/wait"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"

	_ "go.uber.org/automaxprocs"
)

var called atomic.Bool

// Called returns true if Service() has been called.
func Called() bool {
	return called.Load()
}

// InitArgs are the arguments that are passed to Service(). These are filtered down to
// customer initialization functions and closers. Custom initializers and closers
// should treat this as readonly.
type InitArgs struct {
	// Meta provides metadata about the service that will be used for telemetry.
	Meta Meta

	// SignalHandlers provides handlers for signals that are caught by Service().
	// This can only be used to deal with syscall.SIGQUIT, syscall.SIGINT or syscall.SIGTERM.
	// Registering any other signals will cause a panic.
	// All of these will have Close() called after the signal handler is called.
	// Once handling is done, panic is called.
	SignalHandlers map[os.Signal]func()

	values []keyValue
}

// Value returns the value for a key that was set with WithOpaque(). If the key is not found,
// then nil is returned. This is used to retrieve values for custom initializers and closers.
func (i InitArgs) Value(key any) any {
	for _, kv := range i.values {
		if kv.key == key {
			return kv.value
		}
	}
	return nil
}

// WithValue adds an opaque key value pair to the InitArgs. This is used to pass
// values to custom initializers and closers. The key must be comparable or this panics.
// Returns a new InitArgs with the key value pair added. This works similar to context.WithValue().
// You should use typed keys to avoid collisions.
func WithValue(i InitArgs, key, value any) InitArgs {
	if key == nil {
		panic("nil key")
	}
	if !reflect.TypeOf(key).Comparable() {
		panic("key is not comparable")
	}
	i.values = append(i.values, keyValue{key: key, value: value})
	return i
}

type keyValue struct {
	key   any
	value any
}

// Meta is metadata about the service.
type Meta struct {
	// Service is the name of the service. If provided, will be used in log output.
	Service string
	// Build is the build tag. Usually the image version or commit hash.
	// If provided, will be used in log output.
	Build string
}

// InitFunc is a function that is called during Service() in order to setup various needs
// for the service. These happen in the order they are registered, so if one has a dependency
// on another, you have to register them in the correct order. For those that do not have a
// dependency, these are usually done via a package init() function. If this function returns
// an error, then Service() will panic.
type InitFunc func(args InitArgs) error

type registry[T any] struct {
	mu     sync.Mutex
	values []T
}

var initReg registry[InitFunc]

// CloseFunc is a function that is called during Close() in order to close various clients or other
// resources that were setup during Service(). These happen in parallel.
type CloseFunc func(args InitArgs)

var closeReg registry[CloseFunc]

// Register registers a function to be called during Service(). These functions are called in the order
// they are registered. If one fails, then Service() will panic. These functions are called after
// all other setup has been done by Service().
// Normal use is within a package init() function. Often side effect imported.
func RegisterInit(f InitFunc) {
	initReg.mu.Lock()
	defer initReg.mu.Unlock()
	initReg.values = append(initReg.values, f)
}

// RegisterClose registers a function to be called during Close(). They are called in parallel.
// All closers must be completed within 30 seconds. This is usually called along with RegisterInit()
// within a package init() function and side effect imported.
func RegisterClose(f CloseFunc) {
	closeReg.mu.Lock()
	defer closeReg.mu.Unlock()
	closeReg.values = append(closeReg.values, f)
}

type initOpts struct {
	logger      *slog.Logger
	extraFields []any

	metricProvider metric.MeterProvider
	metricsPort    uint16
	disableTrace   bool
	traceProvider  *sdkTrace.TracerProvider
	sampleRate     float64

	pool *worker.Pool
}

// Option is an optional argument to Init.
type Option func(*initOpts) error

// WithExtraFields sets extra fields to be added to the logger. These fields will always
// be logged with every log message. These are not logged in non-production environments.
func WithExtraFields(fieldPairs []any) Option {
	return func(opts *initOpts) error {
		if len(fieldPairs)%2 != 0 {
			return fmt.Errorf("extra fields must be key-value pairs")
		}
		opts.extraFields = fieldPairs
		return nil
	}
}

// WithLogger sets the logger in the base/log package to use the provided logger.
// By default there is a JSON logger to stderr that records the source and uses an
// adjustable level from the base/telemetry/log package.
// If you require zap or zerolog, you can use the log/adapters package to
// convert them to the slog.Logger type. If you provide a logger that outside of those,
// you need to set your logger to use the LogLevel defined in the base/telemetry/log package.
func WithLogger(l *slog.Logger) Option {
	return func(opts *initOpts) error {
		opts.logger = l
		return nil
	}
}

// WithMeterProvider sets the metric provider to use for the service. By default this will
// be created for you. You may use the ("go.opentelemetry.io/otel/metric/noop") noop.NewMeterProvider()
// to disable metrics.
func WithMeterProvider(m metric.MeterProvider) Option {
	return func(opts *initOpts) error {
		opts.metricProvider = m
		return nil
	}
}

// WithDisableTrace disables tracing for the service.
func WithDisableTrace() Option {
	return func(opts *initOpts) error {
		opts.disableTrace = true
		return nil
	}
}

// WithTraceProvider sets the trace provider to use for the service. By default this will
// be created for you. If the environment variable "TracingEndpoint" is set, this will be
// used to send traces to the OTEL provider endpoint. Otherwise it uses the stdout exporter
// that is set to use stderr. You cannot use this and WithTraceSampleRate together or it will cause a panic.
func WithTraceProvider(t *sdkTrace.TracerProvider) Option {
	return func(opts *initOpts) error {
		if opts.sampleRate != 0 {
			panic("cannot use WithTraceProvider and WithTraceSampleRate together")
		}
		opts.traceProvider = t
		return nil
	}
}

// WithTraceSampleRate sets the sample rate for traces. This only applies if using the default trace provider
// when the environmental variable "TracingEndpoint" is set. If using WithTraceProvider, using this will cause
// a panic.
func WithTraceSampleRate(r float64) Option {
	return func(opts *initOpts) error {
		if opts.traceProvider != nil {
			panic("cannot use WithTraceProvider and WithTraceSampleRate together")
		}
		opts.sampleRate = r
		return nil
	}
}

// WithMetricsPort sets the port to use for the metrics server. If not provided, then this defaults to
// port 2223.
func WithMetricsPort(p uint16) Option {
	return func(opts *initOpts) error {
		opts.metricsPort = p
		return nil
	}
}

// WithPool sets the worker pool to use for the service. If not provided, then this defaults to
// a worker.Pool with runtime.NumCPUs() workers (this number is based on the Uber gomaxprocs package).
// The pool grows and shrinks with use. See package worker documentation for more.  If you provide a pool,
// this will set the default pool to this pool unless you set noDefault to true.
func WithPool(p *worker.Pool, noDefault bool) Option {
	return func(opts *initOpts) error {
		opts.pool = p
		return nil
	}
}

// Service is the service initialization function for initing services.
// This will set the logger with the service name and build tag if provided.
//
// Init CAN panic if something required for a production service or a bad option is provided.
// To panic in production, the cause of the panic (outside a bad option passed to Service() which always panics)
// should be an absolute no-go for the service to run, such as a critical service requirement.
//
// This will do the following (not inclusive):
// - Set google/uuid to use a random pool
// - Setup the logger
// - Setup the audit client
// - Setup tracing
// - Integrates various clients and error packages with each other
// - Run user provided initializers
func Service(args InitArgs, options ...Option) {
	// In case of panic in the Service().
	defer func() {
		if r := recover(); r != nil {
			log.Fatal(r)
		}
		called.Store(true)
	}()

	uuid.EnableRandPool()

	opts := initOpts{}

	for _, o := range options {
		if err := o(&opts); err != nil {
			panic(err)
		}
	}

	sm := newSetup(args, opts)

	if err := sm.run(); err != nil {
		panic(err)
	}
}

type stateFn func() (stateFn, error)

// setup is a state machine that is used to setup the service.
type setup struct {
	args   InitArgs
	opts   initOpts
	inits  []InitFunc
	inProd bool

	logMsgs []string

	detectInit  func()
	traceInit   func(bool, float64) error
	metricsInit func(*resource.Resource, uint16) error
}

// newSetup creates a new setup state machine.
func newSetup(args InitArgs, opts initOpts) setup {
	return setup{
		args:  args,
		opts:  opts,
		inits: initReg.values,

		detectInit:  detect.Init,
		traceInit:   trace.Init,
		metricsInit: metrics.Init,
	}
}

// run runs the setup state machine.
func (s setup) run() error {
	state := s.start
	for state != nil {
		var err error
		state, err = state()
		if err != nil {
			return err
		}
	}
	return nil
}

// start is the first state in the setup state machine.
func (s setup) start() (stateFn, error) {
	return s.gcSetup, nil
}

// gcSetup will setup the the garbage collector to use the memory limit of the container.
func (s setup) gcSetup() (stateFn, error) {
	limit, err := containerMemory()
	if err != nil {
		s.logMsgs = append(s.logMsgs, fmt.Sprintf("could not get container memory limit: %v", err))
		return s.packageInits, nil
	}
	if limit <= 0 {
		return s.packageInits, nil
	}
	debug.SetMemoryLimit(limit)

	return s.packageInits, nil
}

// packageInits is the first state in the setup state machine. This state is responsible for
// setting up the package level initializations. This includes  detect initialization,
// and audit initialization.
func (s setup) packageInits() (stateFn, error) {
	s.detectInit()
	s.inProd = detect.Env().Prod()

	if s.opts.traceProvider != nil {
		trace.Set(s.opts.traceProvider)
	}
	if err := s.traceInit(s.opts.disableTrace, s.opts.sampleRate); err != nil {
		return nil, err
	}
	rsc, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String(string(semconv.ServiceNameKey), s.args.Meta.Service),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("could not create a OTEL resource for service name: %w", err)
	}

	if s.opts.metricProvider != nil {
		metrics.Set(s.opts.metricProvider)
	}

	if err := s.metricsInit(rsc, s.opts.metricsPort); err != nil {
		return nil, err
	}

	// This ensures that the default background tasks gets the init'd metrics.
	background.Set(background.New(context.Background()))
	p, err := worker.New(context.Background(), "defaultPool")
	if err != nil {
		return nil, fmt.Errorf("could not create default worker pool: %w", err)
	}
	worker.Set(p)
	return s.loggerSetup, nil
}

// loggerSetup is the state responsible for setting up the logger. This includes setting the
// logger to the provided logger, adding extra fields to the logger, and setting the service name
// and build tag if provided.
func (s setup) loggerSetup() (stateFn, error) {
	logger := log.Default()
	if s.opts.logger != nil {
		logger = s.opts.logger
	}

	// We only need extra logging in production.
	if s.inProd {
		if s.args.Meta.Service != "" {
			logger = logger.With("serviceName", s.args.Meta.Service)
		}
		if s.args.Meta.Build != "" {
			logger = logger.With("serviceBuild", s.args.Meta.Build)
		}
		for i := 0; i < len(s.opts.extraFields); i += 2 {
			logger = logger.With(s.opts.extraFields[i].(string), s.opts.extraFields[i+1])
		}
	}
	log.Set(logger)
	return s.poolInit, nil
}

// poolInit is responsible for setting up the worker pool. If a pool is provided, it will be set as the default pool.
// If not provided, it will be set to a pool with runtime.NumCPUs() workers when Default() is called the first time.
func (s setup) poolInit() (stateFn, error) {
	if s.opts.pool != nil {
		if p := worker.Default(); p != nil {
			log.Default().Error("something is setting the default worker pool before init.Service() and also passing WithPool to Service(), shutting down the old pool")
			p.Close(context.Background())
			log.Default().Error("old pool closed")
		}
		worker.Set(s.opts.pool)
	}
	return s.customInits, nil
}

// customInits is responsible for running any custom initializations that were registered via
// RegisterInit(). This will run all the registered initializations in parallel. An error will be
// returned if any of the initializations fail.
func (s setup) customInits() (stateFn, error) {
	ctx := context.Background()
	g := wait.Group{}
	for _, f := range s.inits {
		g.Go(
			ctx,
			func(ctx context.Context) error {
				return f(s.args)
			},
		)
	}
	if err := g.Wait(ctx); err != nil {
		return nil, err
	}
	return s.log, nil
}

// log is responsible for logging any messages that were collected during the setup process
// before the logger was setup.
func (s setup) log() (stateFn, error) {
	for _, msg := range s.logMsgs {
		log.Default().Info(msg)
	}
	return nil, nil
}

// Close is used as a defer function in the main function of a service after Init.
// This will recover from a panics in order to log it via the base/log.Default() logger to
// avoid any panics from escaping logs. However, it will still exit after logging the error.
// This also closes the audit client in audit.Default(). All other closers that are registered
// via RegisterClose() will be called in parallel. This will return in 30 seconds no matter
// if all closers are done or not.
func Close(args InitArgs) {
	defer func() {
		if r := recover(); r != nil {
			log.Fatal(r)
		}
	}()

	c := closer{
		closers: closeReg.values,
		trace:   trace.Close,
		metrics: metrics.Close,
	}
	c.callClosers(args)
}

// closer provides a way to call various client closers and registered closers.
type closer struct {
	closers []CloseFunc
	audit   func()
	trace   func()
	metrics func()
}

func (c closer) callClosers(args InitArgs) {
	ctx := context.Background()
	g := wait.Group{}

	when := 30 * time.Second

	// Audit client close.
	if c.audit != nil {
		g.Go(
			ctx,
			func(ctx context.Context) error {
				in(
					args,
					func(args InitArgs) { c.audit() },
					when,
				)
				return nil
			},
		)
	}

	// Trace client close.
	if c.trace != nil {
		g.Go(
			ctx,
			func(ctx context.Context) error {
				in(
					args,
					func(args InitArgs) { c.trace() },
					when,
				)
				return nil
			},
		)
	}

	// Close the metrics client.
	g.Go(
		ctx,
		func(ctx context.Context) error {
			in(
				args,
				func(args InitArgs) { c.metrics() },
				when,
			)
			return nil
		},
	)

	for _, f := range c.closers {
		f := f
		if f != nil {
			g.Go(
				ctx,
				func(ctx context.Context) error {
					in(args, f, when)
					return nil
				},
			)
		}
	}

	g.Wait(ctx)
}

// in calls a function and waits for it to complete. If it does not complete within the given time,
// it will return.
func in(args InitArgs, f func(args InitArgs), t time.Duration) {
	if f == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		f(args)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(t):
	}
}
