package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/primaryrutabaga/ruby-core/services/api/oas"
)

// NewError maps any error returned by a handler or by the security/decoding layer
// to an RFC 9457 Problem Details response (ADR-0041). ogen calls this for the
// operation's `default` response. Security failures become 401; everything else is
// a 500 with a non-leaking detail. Request/parameter validation mapping (400) is
// added when endpoints with parameters land (Slice C).
func (s *Service) NewError(_ context.Context, err error) *oas.ProblemStatusCode {
	status := http.StatusInternalServerError
	detail := "An unexpected error occurred."

	var secErr *ogenerrors.SecurityError
	if errors.As(err, &secErr) {
		status = http.StatusUnauthorized
		detail = "A valid bearer token is required."
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
