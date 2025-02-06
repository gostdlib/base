// Package grpc contains a filter for use in our sampler.Filtered sampler that will match
// gRPC calls based on metadata.
package grpc

import (
	"context"
	"fmt"
	"regexp"

	grpcContext "github.com/gostdlib/base/context/grpc"
)

// Matcher is used to match a call based on attributes and is used in decision making on
// if a call should be sampled outside of the normal sampling process. Fields that
// are empty will not be used in the matching process.
type Matcher struct {
	// Op is a regular expression that will be used to match the operation name.
	// An empty string will match all operations.
	Op string
	// CustomerID is a regular expression that will be used to match the customer ID.
	CustomerID string

	op         *regexp.Regexp
	customerID *regexp.Regexp
}

// Match returns true if the filter matches the given context. This will cause the call to be
// sampled.
func (m Matcher) Match(ctx context.Context) bool {
	md := grpcContext.GetMetadata(ctx)
	if md.IsZero() {
		return false
	}

	if m.op != nil {
		if m.op.MatchString(md.Op) {
			return true
		}
	}
	if m.customerID != nil {
		if m.customerID.MatchString(md.CustomerID) {
			return true
		}
	}
	return false
}

// Compile compiles the regular expressions for the filter. This returns a new filter with the
// compiled regular expressions. This should be called before using the filter.
func (m Matcher) Compile() (Matcher, error) {
	var err error
	m.op, err = regexp.Compile(m.Op)
	if err != nil {
		return Matcher{}, fmt.Errorf("sampler Matcher had bad regular expression(%s) for Op: %w", m.Op, err)
	}
	m.customerID, err = regexp.Compile(m.CustomerID)
	if err != nil {
		return Matcher{}, fmt.Errorf("sampler Matcher had bad regular expression(%s) for CustomerID: %w", m.CustomerID, err)
	}
	return m, nil
}

// MustCompile compiles the regular expressions for the filter. This returns a new filter with the
// compiled regular expressions. This should be called before using the filter. If there is an error
// this will panic.
func (m Matcher) MustCompile() Matcher {
	m, err := m.Compile()
	if err != nil {
		panic(err)
	}
	return m
}
