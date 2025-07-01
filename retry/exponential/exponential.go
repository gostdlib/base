package exponential

import (
	"github.com/Azure/retry/exponential"
)

// Backoff provides a mechanism for retrying operations with exponential backoff. This can be used in
// tests without a fake/mock interface to simulate retries either by using the WithTesting()
// option or by setting a Policy that works with your test. This keeps code leaner, avoids
// dynamic dispatch, unneeded allocations and is easier to test.
type Backoff = exponential.Backoff

// Options are used to configure the backoff policy.
type Option = exponential.Option

// WithPolicy sets the backoff policy to use. If not specified, then DefaultPolicy is used.
func WithPolicy(policy Policy) Option {
	return exponential.WithPolicy(policy)
}

// and is not used at this time.
type TestOption = exponential.TestOption

// WithTesting invokes the backoff policy with no actual delay.
// Cannot be used outside of a test or this will panic.
func WithTesting(options ...TestOption) Option {
	return exponential.WithTesting(options...)
}

// ErrTransformer is a function that can be used to transform an error before it is returned.
// The typical case is to make an error a permanent error based on some criteria in order to
// stop retries. The other use is to use errors.ErrRetryAfter as a wrapper to specify the minimum
// time the retry must wait based on a response from a service. This type allows packaging of custom
// retry logic in one place for reuse instead of in the Op. As ErrTransformrers are applied in order,
// the last one to change an error will be the error returned.
type ErrTransformer = exponential.ErrTransformer

// WithErrTransformer sets the error transformers to use. If not specified, then no transformers are used.
// Passing multiple transformers will apply them in order. If WithErrTransformer is passed multiple times,
// only the final transformers are used (aka don't do that).
func WithErrTransformer(transformers ...ErrTransformer) Option {
	return exponential.WithErrTransformer(transformers...)
}

// New creates a new Backoff instance with the given options.
func New(options ...Option) (*Backoff, error) {
	return exponential.New(options...)
}

// Must returns b if err == nil, otherwise it panics.
// Use: Must(New(options)) .
func Must(b *exponential.Backoff, err error) *exponential.Backoff {
	return exponential.Must(b, err)
}

// Record is the record of a Retry attempt.
type Record = exponential.Record

// Op is a function that can be retried.
type Op = exponential.Op

// RetryOption is an option for the Retry method. Functions that implement RetryOption
// provide an override on a single call.
type RetryOption = exponential.RetryOption

// WithMaxAttempts sets the maximum number of attempts to retry the operation. If not specified,
// the maximum number of attempts is determined by the policy. If the policy has a MaxAttempts
// greater than 0, that is used. If the policy has a MaxAttempts of 0, then there is no limit.
func WithMaxAttempts(max int) RetryOption {
	return exponential.WithMaxAttempts(max)
}
