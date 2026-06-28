package app

import (
	"encoding/json"
	"net/http"
)

// writeProblem renders an RFC 9457 Problem Details response for the routes served
// outside the ogen surface (docs, raw spec). The generated API renders its own
// problems via handlers.NewError; this keeps the same shape for the rest.
func writeProblem(w http.ResponseWriter, status int, detail, instance string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":     "about:blank",
		"title":    http.StatusText(status),
		"status":   status,
		"detail":   detail,
		"instance": instance,
	})
}
