# Base - Your foundational packages

[![GoDoc](https://godoc.org/github.com/gostdlib/base?status.svg)](https://pkg.go.dev/github.com/gostdlib/base)
[![Go Report Card](https://goreportcard.com/badge/github.com/gostdlib/base)](https://goreportcard.com/report/github.com/gostdlib/base)

Since you've made it this far, why don't you hit that :star: up in the right corner.

Note: These packages are going through there paces, so some changes may occur.

# Introduction

This is a set of base packages for use in Go projects.

These sets of packages provide out of the box errors, logs, metrics, traces, concurrency primitives, AAA, ...

It then ties these all together for use in projects that provide gRPC, REST or HTTP services providing these packages with interceptors that handle much of the work for you.

This helps you use tracing instead of logs, logging only errors at the RPC level (but with all the relevant information), automatic conversion of errors to error types safe for customer consumption, automatic or helpful setup of other requirements, etc...

It also includes support for dealing with both prod and non-prod environments without thinking about how you need to change initalization. And allows sane overrides for use cases that want to use this but need different defaults.

It provides primatives for concurrency such as worker pools, limited pools, safer group objects instead of WaitGroup, type safe sync.Pool, etc... Sharded maps for speed increases and non-blocking read/write types like WProtect.

Go doesn't have immutable types other than `string`, or does it?  Meet `values/immutable` with a tool for generating immutable types.

Need to write configurations where zero values can't be used to detect if something is set or not? The `isset` package is there for you.

We provide a `retry` package for exponential retries that is integrated into several of the other packages.

On the list goes on.


## Why?

Centralization of best practices with a view on integration between packages to reduce work by the developer.

## Getting started

### Generating a company init package

If you are planning to use this inside a company, you will want to make a company specific version of the init package.

First, install our generation tool:
`go install github.com/gostdlib/base/init/geninit@latest`

Then in whatever repo you want to set this up at, run:
`geninit -pkg company -init Init > company.go`

This would make a pakcage named `company` that you can use to call `company.Init()`. If you want to name that differently, change the flags.

You can customize this with your own extra inits for your company.

Otherwise you can simply use the `base/init` package we provide. The generated one is a wrapper around that one.

### Initalize your project

We provide an initalizer for your project. This create a custom `context` and `error` package for you. These both wrap the standard library packages so you don't have to import both your custom package and the standard libary one.

To use this, run:
`go install github.com/gostdlib/base/genproject@latest`

Then simply go to the root of your project and run:
`genproject`

Now create your `main.go` and call your package init generated in the last step:

```go
var initArgs = company.InitArgs{
	Meta: company.Meta{
		Service: "my-service",
		Build: "1.0.0",
	},
}

func main() {
	company.Init(initArgs)
	defer company.Close(initArgs)

	...
}
```

If you did not generate a company init package, you can just use the general one:

```go
import (
	...
	goinit "github.com/gostdlib/base/init"
	...
)
...

func main() {
	goinit.Service(initArgs)
	defer goinit.Close(initArgs)
	...
}
```

There are a lot of initialization options here that can be provided in `InitArgs` and various `With*()` option funcs.

`Init` provides smart defaults for prod vs non-prod environments (detected by the `base/env/detect` package).

In this case, a prod environment will get the logging package setup(`base/log/`) using an `*slog.Logger` that includes the line it is logged at, the `serviceName` and `serviceBuild` keys using the meta data. These will not be provided in non-prod, to make errors easier to read.

An `audit.Client` will be setup for use via `base/audit` that will either user a domain socket from one of the default locations or a no-op client in non-prod.

`Close` handles closing the default `audit.Client`, calling other designated close procedures and capturing any `panic` that occurs by sending it out to be logged. This should help prevent lost `panic` messages.

Each of these settings can be overridden.  You can provide your own audit.Client or return an NoOp audit.Client even in production. You can change the `slog.Logger` to use the `zerolog` or `zap` underneath via adapters we provide via log/adapters. There are other overrides that can be provided by using sub-packages.

In other words, this should be reconfigurable to meet your needs within reason. This should drive towards an **easy** to use and common method of doing things.

## How to use these packages to ease burden

### Errors and logging

This is not going to go into how to customize your `errors` package, as that is beyond this guide, see the `errors` package for more information.

However, let's talk about how to make `errors` work for you.

You can continue to use the standard Go `error` type as you see fit. But in your service packages, those errors should be created with `errors.E()` (which you can customize). This will generate an `errors.Error` type that wraps your error and provides additional fields.

That type automatically records the file and line the error occurred on. If you pass an `errors.Error` to `errors.E()`, it just returns the previous `Error` to avoid extra wrapping that isn't needed.

When using with our `grpc` service, when the `Error` reaches the top of the call stack, it is automatically logged to the configured logger. It is also automatically added to an OTEL trace if it exists on the `Context` object.

If using a different framework, you only need to call the `.Log()` method at the top of the call stack. This prevents unnecessary logging either due to transient errors that succeed or duplicate errors from the common:

```go
if err != nil {
	log.Println(err)
	return err // And then the next function does the same
}
```

You can also follow the examples in the `base/errors` to do `error` conversions to errors you want to return to a user, do filtering, etc.  You can still wrap your own custom errors with attributes so that `errors.Is()` and `errors.As()` work as you would expect.

You can access logging either through the logging package's `Default()` function or via the `Context` object generated by `context.Background()`. In `grpc` these are automatically attached via an interceptor.

As we use tracing, there is not much use for logging other than for errors or in background goroutines. If you don't have the ability to capture traces, you can configure tracing to output to stdout or stderr.

Logging can be accessed via `telemetry/log`.

#### Why not log immediately when calling E()

Because errors might get handled. By using this method, we guarantee that a relevant error will get logged if it is in the RPC path if it bubbles up (which means it was relevant) vs logging errors that don't mean anything because they were transient (like ARM retries).

Tracing on the other hand, which happens only at set intervals can still capture that this is occuring for analysis for problems. You can also capture these via metrics to look for statistical changes that are significant for alarms. These are much better diagnostic tools than the red herrings that occuring by looking at log entries that don't matter.

Background tasks errors must be manually logged, but this is a tiny fraction of code in comparison.

Without seeing transient errors, how can you know if something is having an overall problem?  The answer to that is `Metrics`.

`Metrics` are for counting. If you need to count errors, count the types of errors with metrics, not log entries.

The base packages assumes that:

* Logs are for errors or information that cannot be captured in a trace
* Traces are for debugging information
* Metrics are for counting and trending

You will be unhappy if you try to use these sets of packages in other ways.

### Metrics

Metrics can be gotten by either using the "telemetry/metrics.Default()" function or via the `Context` and the accessor `context.Meter()` function.

The `context.Meter()` will return a meter that is scoped to your package. From there you setup your meter for use. A good example of this is in "base/concurrency/worker/metrics.go".

There is also `context.MeterProvider` which you can use to have a more manual method that gives more control.

This will be served on port 2223 unless you have overriden the OTEL provider.

Many of the types in this package automatically provide metrics.

### Tracing

Tracing objects can be retrieved via a `Context` object using the the `telemetry/otel/trace` set of packages.

The normal way is to use the `span` package to create a trace span in your current function.

Generally you will want to create a Span for each function in a call chain and do any logging you find necessary. Remember that simply creating an `errors.Error` will cause the error to be logged to the span.

Creating a new span is easy:

```go
func myFunc(ctx context.Context) error {
	ctx, spanner := span.New(ctx, opts...)
	defer spanner.End()
	...
}
```

This automatically names the span and you can use the `spanner` object to do additional spans.

If you want to use an existing parent span instead of a new Span, you can do `span.Get()`.

If you need to name things yourself or have some other preferred method of dealing with tracing, you can simply use the `trace` package's `Default()` to get a raw `*sdkTrace.TracerProvider`.

## Concurrency

There are a set of concurrency packages here. There are a few notes on use here, but you should dig into the individual packages.

The base package of interest is `worker`. This package allows for the creation of worker pools. However, this is not usually necessary as `context.Background()` gives access to a default worker pool via the `context.Pool()` function.

This allows goroutine reuse, metrics and provides access to other primatives. There is a `Limited` type that can be made off a `Pool` to allow only X goroutines to run at a time. There is a `Group` type that can give you a `WaitGroup` like type that you can't forget to increment or decrement (similar to the errgroup type in the x packages).

All the other concurrency primatives use the `Pool` type to leverage metrics and reuse capabilities.

Want a fanout/fanin that isn't so prone to deadlocks?  Use the `patterns/fan` package.

There are also supersets of the `sync` package that provide generic typed `sync.Pool` types, `shardedmaps` that scale better and `WProtect` for non-blocking read/write types.

Finally there is a `background` package for keeping track of background tasks instead of random goroutines you don't know are even running.

## Immutable

Check out the `values/immutable` for an immutable type generator that leverages our immutable Map and Slice types. More in that package.

## Isset

Don't use `*bool` or other basic types to indicate if something is set or not. This creates a lot of junk that the garbage collector has to deal with. Overuse of pointers is one of the biggest time wasters and causes panics without good reason. Lots of small pointers are just junk that cause long GC pauses in heavily trafficed services.

Instead, use the `values/isset` pacakge.

## RPC

Checkout the `grpc` package for an RPC experience that automatically ties in your metrics/logging/tracing.

## Much, much more...

Look around and have fun!
