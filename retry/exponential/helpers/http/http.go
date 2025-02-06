/*
Package http provides an ErrTransformer for http.Client from the standard library.
Other third-party HTTP clients are not supported by this package.

Example that handle HTTP non-temporary error codes:

		httpTransform := http.New()

		backoff := exponential.WithErrTransformer(httpTransform)
	    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	    var resp *http.Response

	    err := backoff.Retry(
	    	ctx,
	     	func(ctx context.Context, r Record) error {
	      		var err error
	        	resp, err = httpClient.Do(someRequest)
	         	return err
	        },
	    )
	    cancel()

Example with custom errors:

		bodyHasErr := func(r *http.Response) error {
	 		b, err :io.ReadAll(r.Body)
	 		if err != nil {
	 			return fmt.Errorf("response body had error: %s", err)
	    	}

			s := strings.TrimSpace(string(b))
	 		if strings.HasPrefix(s, "error") {
	 			if strings.Contains(s, "errors: permament") {
	 				return fmt.Errorf("error: %w: %w", s, errors.ErrPermanent)
	 			}
	 			return fmt.Errorf("error: %s", s)
			}
	 		return nil
	   }

	   httpTransform := http.New(bodyHasErr)

	   backoff := exponential.WithErrTransformer(httpTransform)
	   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	   var resp *http.Response

	   err := backoff.Retry(
	   		ctx,
	     	func(ctx context.Context, r Record) error {
	      		var err error
	        	resp, err = httpTransform.RespToErr(httpClient.Do(someRequest)) // <- note the call wrapper
	         	return err
	        },
	    )
	    cancel()
*/
package http

import (
	"github.com/Azure/retry/exponential/helpers/http"
)

// Transformer provides an ErrTransformer method that can be used to detect non-retriable errors.
// The following codes are retriable: StatusRequestTimeout, StatusConflict, StatusLocked, StatusTooEarly,
// StatusTooManyRequests, StatusInternalServerError and StatusGatewayTimeout.
// Any other code is not.
type Transformer = http.Transformer

// RespToErr allows you to inspect a Response and determine if the result is really an error.
// If you want to make that type of error non-retriable, wrap the error with errors.ErrPermanent, like
// so: return fmt.Errorf("had some error condition: %w", errors.ErrPermanent) . This should return
// nil if the Response was fine.
type RespToErr = http.RespToErr

// New returns a new Transformer. This implements exponential.ErrTransformer with the method ErrTransformer.
func New(respToErrs ...RespToErr) *Transformer {
	return http.New(respToErrs...)
}
