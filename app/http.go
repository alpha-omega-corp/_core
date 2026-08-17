package app

import (
	"errors"
	"net/http"

	"github.com/alpha-omega-corp/_core/helpers"
	"github.com/uptrace/bunrouter"
)

/*
HttpResponse collapses the two closures a route used to need.

Written by hand, a controller method nests the router's own closure inside
helpers.Response's — one to receive the request, one to defer the call so the
encoder can run after it — around a single line of actual work:

	return func(w http.ResponseWriter, req bunrouter.Request) error {
		return helpers.Response(w, func() (*[]handlers.Slot, error) {
			slots, err := c.Handler().Available(req.Context(), time.Now())
			if err != nil {
				return nil, err
			}

			return &slots, nil
		})
	}

There is one closure here, and it is the one doing the work:

	return HttpResponse(func(w http.ResponseWriter, req bunrouter.Request) ([]handlers.Slot, error) {
		return c.Handler().Available(req.Context(), time.Now())
	})

T is the response type exactly as the handler returns it, value or pointer.
The old form fixed it at *T, which is why the slice above had to be assigned,
address-taken and returned in three lines the JSON encoder never needed.

fn receives the same pair bunrouter hands a handler, so the helpers that decode
a request take it directly and a route that needs one stays inside this form:

	return HttpResponse(func(w http.ResponseWriter, req bunrouter.Request) (*handlers.Meat, error) {
		body := helpers.GetBody[handlers.MeatBody](w, req)
		return c.Handler().Create(req.Context(), body)
	})

Those helpers report their own failure and still return a zero value, so a
handler that cannot proceed on one should say so — returning HttpFail keeps
the status the caller gets from depending on which write landed last.
*/
func HttpResponse[T any](fn func(w http.ResponseWriter, req bunrouter.Request) (T, error)) bunrouter.HandlerFunc {
	return func(w http.ResponseWriter, req bunrouter.Request) error {
		res, err := fn(w, req)
		if err != nil {
			helpers.Error(w, err, HttpStatus(err))

			// Returning the error as well would have bunrouter write a second
			// response over this one.
			return nil
		}

		return bunrouter.JSON(w, res)
	}
}

// HttpError is an error that carries the status it should be reported with.
//
// Without it every failure out of a handler is a 500, which is right for a
// broken query and wrong for a sold-out item or an unknown reference — the
// caller reads "something went wrong" for something it could have fixed. A
// handler returning HttpFail keeps that distinction without reaching for the
// ResponseWriter, which is the thing controllers dropped out of this form to
// get at.
type HttpError struct {
	Code int
	Err  error
}

func (e *HttpError) Error() string { return e.Err.Error() }

func (e *HttpError) Unwrap() error { return e.Err }

// HttpFail tags an error with the status to answer it with.
//
//	return nil, app.HttpFail(http.StatusNotFound, err)
func HttpFail(code int, err error) error {
	return &HttpError{Code: code, Err: err}
}

// HttpStatus reports the status an error should be answered with: whatever an
// HttpError in its chain asks for, 500 for anything else. Wrapping is
// respected, so an error that has been through fmt.Errorf("%w") on its way up
// still lands on the status it was tagged with.
func HttpStatus(err error) int {
	var httpErr *HttpError
	if errors.As(err, &httpErr) && httpErr.Code != 0 {
		return httpErr.Code
	}

	return http.StatusInternalServerError
}
