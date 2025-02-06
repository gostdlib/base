package errors

import "errors"

// Everything below here is a wrapper around the stdlib errors package.
// We do this to prevent having to import the stdlib errors package in every file that needs it.

// New returns an error that formats as the given text.
func New(text string) error {
	return errors.New(text)
}

// Unwrap returns the result of calling the Unwrap method on err, if err's
// type contains an Unwrap method returning error.
// Otherwise, Unwrap returns nil.
func Unwrap(err error) error {
	return errors.Unwrap(err)
}

// Is reports whether any error in err's chain matches target.
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As finds the first error in err's chain that matches target, and if so, sets
// target to that error value and returns true.
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}

// Join returns an error that wraps the given errors. Any nil error values are discarded.
// Join returns nil if every value in errs is nil. The error formats as the concatenation
// of the strings obtained by calling the Error method of each element of errs, with a newline between each string.
// A non-nil error returned by Join implements the Unwrap() []error method.
func Join(err ...error) error {
	return errors.Join(err...)
}
