package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Jov3c/Pondly/apps/api/internal/platform"
)

func TestHealthAndReadinessContract(t *testing.T) {
	cfg := platform.Config{MaxBodyBytes: 1024, MaxInFlight: 4}
	h := New(cfg, Dependencies{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"status":"alive"`)) ||
		rec.Header().Get("X-Request-ID") == "" {
		t.Fatalf("health: %d %s headers=%v", rec.Code, rec.Body.Bytes(), rec.Header())
	}

	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 503 || !bytes.Contains(rec.Body.Bytes(), []byte("dependencies_unavailable")) ||
		!bytes.Contains(rec.Body.Bytes(), []byte("request_id")) {
		t.Fatalf("ready fail-closed: %d %s", rec.Code, rec.Body.Bytes())
	}

	ready := New(cfg, Dependencies{Readiness: func(context.Context) bool { return true }})
	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec = httptest.NewRecorder()
	ready.ServeHTTP(rec, req)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"status":"ready"`)) {
		t.Fatalf("ready: %d %s", rec.Code, rec.Body.Bytes())
	}
}
