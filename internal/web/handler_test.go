package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(Deps{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "ok" {
		t.Errorf("body = %q, want ok", got)
	}
}

func TestHealthz_MethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(Deps{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestUnknownAPIPathIsNotSPA(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(Deps{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nothing", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestReadyz(t *testing.T) {
	tests := []struct {
		name  string
		ready func(context.Context) error
		code  int
		body  string
	}{
		{"no checks", nil, http.StatusOK, "ok"},
		{"healthy", func(context.Context) error { return nil }, http.StatusOK, "ok"},
		{"db down", func(context.Context) error { return errors.New("down") }, http.StatusServiceUnavailable, "not ready"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewHandler(Deps{Ready: tt.ready}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if rec.Code != tt.code || rec.Body.String() != tt.body {
				t.Fatalf("got %d %q, want %d %q", rec.Code, rec.Body.String(), tt.code, tt.body)
			}
		})
	}
}

func TestAPIMounted(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	rec := httptest.NewRecorder()
	NewHandler(Deps{API: api}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tierlists/x", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rec.Code)
	}
}
