package stt

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTencentEffectiveAppID_ReadsAccountAppID: the realtime API needs the
// account APPID, and the console prints it next to the UIN. Reading it from the
// key pair is what keeps a pasted UIN from failing every recording.
func TestTencentEffectiveAppID_ReadsAccountAppID(t *testing.T) {
	var gotAction, gotRegion, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAction = r.Header.Get("X-TC-Action")
		gotRegion = r.Header.Get("X-TC-Region")
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"Response":{"AppId":1253717851}}`))
	}))
	defer srv.Close()

	p := NewTencentProvider("AKIDtest", "secretkey", "100055003327") // a UIN
	p.CAMBaseURL = srv.URL

	got, err := p.EffectiveAppID(context.Background())
	if err != nil {
		t.Fatalf("EffectiveAppID: %v", err)
	}
	if got != "1253717851" {
		t.Fatalf("appid = %q, want the account's 1253717851", got)
	}
	if gotAction != tencentCAMAction {
		t.Errorf("X-TC-Action = %q, want %q", gotAction, tencentCAMAction)
	}
	if gotRegion == "" {
		t.Error("X-TC-Region is empty; the account API rejects that")
	}
	if !strings.HasPrefix(gotAuth, "TC3-HMAC-SHA256 Credential=AKIDtest/") {
		t.Errorf("Authorization = %q, want a TC3 signature for the key pair", gotAuth)
	}
}

// TestTencentEffectiveAppID_Caches: one lookup per provider, not one per
// recording.
func TestTencentEffectiveAppID_Caches(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"Response":{"AppId":1253717851}}`))
	}))
	defer srv.Close()

	p := NewTencentProvider("AKIDtest", "secretkey", "")
	p.CAMBaseURL = srv.URL
	for range 2 {
		if _, err := p.EffectiveAppID(context.Background()); err != nil {
			t.Fatalf("EffectiveAppID: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("lookup calls = %d, want 1", calls)
	}
}

// TestTencentEffectiveAppID_FallsBackToTypedValue: a key scoped to ASR alone
// cannot read account information, so the typed value still has to work.
func TestTencentEffectiveAppID_FallsBackToTypedValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Response":{"Error":{"Code":"AuthFailure.UnauthorizedOperation","Message":"no cam read"}}}`))
	}))
	defer srv.Close()

	p := NewTencentProvider("AKIDtest", "secretkey", "1250000000")
	p.CAMBaseURL = srv.URL

	got, err := p.EffectiveAppID(context.Background())
	if err != nil {
		t.Fatalf("EffectiveAppID: %v", err)
	}
	if got != "1250000000" {
		t.Fatalf("appid = %q, want the configured value", got)
	}
}

// TestTencentEffectiveAppID_ErrorsWithoutValue: no lookup and nothing typed
// must name the field instead of dialing with an empty path segment.
func TestTencentEffectiveAppID_ErrorsWithoutValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"Response":{"Error":{"Code":"AuthFailure","Message":"denied"}}}`)
	}))
	defer srv.Close()

	p := NewTencentProvider("AKIDtest", "secretkey", "")
	p.CAMBaseURL = srv.URL

	_, err := p.EffectiveAppID(context.Background())
	if err == nil {
		t.Fatal("EffectiveAppID succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "AppID") {
		t.Fatalf("err = %v, want it to name the AppID", err)
	}
}

// TestTencentEffectiveAppID_RequiresCredentials covers the missing-key guard
// without touching the network.
func TestTencentEffectiveAppID_RequiresCredentials(t *testing.T) {
	p := NewTencentProvider("", "", "")
	_, err := p.EffectiveAppID(context.Background())
	if err == nil || !strings.Contains(err.Error(), "SecretId") {
		t.Fatalf("err = %v, want it to name the missing SecretId", err)
	}
}
