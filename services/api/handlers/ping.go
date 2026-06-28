package handlers

import (
	"context"

	"github.com/primaryrutabaga/ruby-core/services/api/oas"
)

// Ping confirms the API surface is reachable and the caller's bearer token was
// accepted (the security check runs before this handler). It performs no I/O.
func (s *Service) Ping(_ context.Context) (oas.PingRes, error) {
	return &oas.PingOK{Status: "ok", Service: s.service}, nil
}
