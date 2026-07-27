package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNoCache_defaultValue(t *testing.T) {
	handler := NoCache(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	got := rec.Header().Get("Cache-Control")
	if got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
}

func TestNoCache_handlerCanOverride(t *testing.T) {
	handler := NoCache(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("Cache-Control")
	if got != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q, want %q (handler should override middleware default)", got, "public, max-age=3600")
	}
}
