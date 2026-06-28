package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/primaryrutabaga/ruby-core/services/api/oas"
)

// apiError carries an explicit HTTP status from a handler (e.g. a 400 for a bad
// range). NewError renders it as the Problem status; the detail is caller-supplied.
type apiError struct {
	status int
	detail string
}

func (e *apiError) Error() string { return e.detail }

// badRequest returns a handler error that NewError renders as a 400 Problem.
func badRequest(detail string) error { return &apiError{status: http.StatusBadRequest, detail: detail} }

// NewError maps any error returned by a handler or by the security/decoding layer
// to an RFC 9457 Problem Details response (ADR-0041). ogen calls this for the
// operation's `default` response. Explicit apiErrors keep their status; security
// failures become 401; request/parameter decode failures become 400; everything
// else is a 500 with a non-leaking detail.
func (s *Service) NewError(_ context.Context, err error) *oas.ProblemStatusCode {
	status := http.StatusInternalServerError
	detail := "An unexpected error occurred."

	var apiErr *apiError
	var secErr *ogenerrors.SecurityError
	var decParams *ogenerrors.DecodeParamsError
	var decReq *ogenerrors.DecodeRequestError
	switch {
	case errors.As(err, &apiErr):
		status = apiErr.status
		detail = apiErr.detail
	case errors.As(err, &secErr):
		status = http.StatusUnauthorized
		detail = "A valid bearer token is required."
	case errors.As(err, &decParams), errors.As(err, &decReq):
		status = http.StatusBadRequest
		detail = "The request could not be processed as submitted."
	}

	return &oas.ProblemStatusCode{
		StatusCode: status,
		Response: oas.Problem{
			Type:   oas.NewOptString("about:blank"),
			Title:  http.StatusText(status),
			Status: int32(status), //nolint:gosec // G115: HTTP status code, always within int32 range
			Detail: oas.NewOptString(detail),
		},
	}
}
