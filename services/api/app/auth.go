package app

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/primaryrutabaga/ruby-core/services/api/oas"
)

// errInvalidBearer is returned by the security handler on a missing/incorrect
// token. ogen wraps it in an *ogenerrors.SecurityError, which handlers.NewError
// maps to a 401 Problem.
var errInvalidBearer = errors.New("invalid or missing bearer token")

// tokenAuth enforces the in-app Vault-issued bearer token — the defense-in-depth
// layer behind Traefik edge auth + mTLS (ADR-0040). It implements both the ogen
// SecurityHandler (for the generated API) and an http middleware (for the docs and
// raw-spec routes, which are not part of the generated surface).
type tokenAuth struct {
	expected []byte
}

func newTokenAuth(token string) *tokenAuth {
	return &tokenAuth{expected: []byte(token)}
}

// valid compares in constant time to avoid leaking the token via timing.
func (a *tokenAuth) valid(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), a.expected) == 1
}

// HandleBearerAuth implements oas.SecurityHandler.
func (a *tokenAuth) HandleBearerAuth(ctx context.Context, _ oas.OperationName, t oas.BearerAuth) (context.Context, error) {
	if !a.valid(t.Token) {
		return ctx, errInvalidBearer
	}
	return ctx, nil
}

// middleware guards non-generated routes (docs, raw spec) with the same bearer.
func (a *tokenAuth) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.valid(bearerFromHeader(r)) {
			writeProblem(w, http.StatusUnauthorized, "A valid bearer token is required.", r.URL.Path)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bearerFromHeader extracts the token from an "Authorization: Bearer <token>" header.
func bearerFromHeader(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return h[len(prefix):]
	}
	return ""
}
