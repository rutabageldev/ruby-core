//go:build fast

package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apispec "github.com/primaryrutabaga/ruby-core/api"
)

const testToken = "test-bearer-token-0123456789"

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	// pool is nil: the only endpoint in this slice (/ping) performs no DB I/O.
	a, err := New(nil, testToken, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	return a.Handler()
}

func do(t *testing.T, h http.Handler, method, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealth_Unauthenticated200(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodGet, "/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("health content-type = %q, want application/json", ct)
	}
}

func TestPing_MissingBearer_401Problem(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodGet, "/v1/ping", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ping (no auth) status = %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "problem+json") {
		t.Fatalf("ping (no auth) content-type = %q, want application/problem+json", ct)
	}
	var prob struct {
		Title  string `json:"title"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
		t.Fatalf("decode problem: %v (body=%s)", err, rec.Body.String())
	}
	if prob.Status != http.StatusUnauthorized {
		t.Fatalf("problem.status = %d, want 401", prob.Status)
	}
}

func TestPing_WrongBearer_401(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodGet, "/v1/ping", "not-the-token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ping (wrong token) status = %d, want 401", rec.Code)
	}
}

func TestPing_ValidBearer_200(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodGet, "/v1/ping", testToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("ping status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode ping: %v", err)
	}
	if body.Status != "ok" || body.Service != "api" {
		t.Fatalf("ping body = %+v, want {ok api}", body)
	}
}

func TestOpenAPISpec_RequiresBearer(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodGet, "/openapi.yaml", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/openapi.yaml (no auth) status = %d, want 401", rec.Code)
	}
}

func TestOpenAPISpec_ServesEmbeddedBundle(t *testing.T) {
	if len(apispec.Bundled) == 0 {
		t.Fatal("embedded bundle is empty — generation/embed broken")
	}
	rec := do(t, newTestHandler(t), http.MethodGet, "/openapi.yaml", testToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("/openapi.yaml status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != string(apispec.Bundled) {
		t.Fatal("/openapi.yaml body does not match embedded bundle")
	}
}
