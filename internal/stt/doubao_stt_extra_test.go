package stt

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// roundTripFunc lets a test observe or fail a request without a network peer.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// errBody is a response body whose Read always fails.
type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("body exploded") }
func (errBody) Close() error             { return nil }

// TestDoubaoTranscribe_DefaultBaseURL covers the two fallbacks that only apply
// to a provider built by hand: an empty BaseURL and an empty ResourceID. The
// transport fails the request on purpose so the test never touches the network
// — the assertion is on the URL that was about to be called.
func TestDoubaoTranscribe_DefaultBaseURL(t *testing.T) {
	var gotURL, gotResource string
	p := &DoubaoProvider{
		APIKey: "test-key",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotURL = r.URL.String()
			gotResource = r.Header.Get("X-Api-Resource-Id")
			return nil, errors.New("no network in this test")
		})},
	}

	_, err := p.Transcribe(context.Background(), strings.NewReader(string(doubaoMP3)), "zh")
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("want a request error, got %v", err)
	}
	if want := DoubaoBaseURL + doubaoFlashPath; gotURL != want {
		t.Errorf("request URL = %q, want %q", gotURL, want)
	}
	if gotResource != DoubaoDefaultResourceID {
		t.Errorf("X-Api-Resource-Id = %q, want the duration-billed default", gotResource)
	}
}

// TestDoubaoTranscribe_ReadResponseError covers a body that dies mid-read: the
// caller must get an error rather than a truncated transcript.
func TestDoubaoTranscribe_ReadResponseError(t *testing.T) {
	p := &DoubaoProvider{
		APIKey: "test-key",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       errBody{},
			}, nil
		})},
	}

	_, err := p.Transcribe(context.Background(), strings.NewReader(string(doubaoMP3)), "zh")
	if err == nil || !strings.Contains(err.Error(), "read response") {
		t.Fatalf("want a read-response error, got %v", err)
	}
}

// TestDoubaoTranscribe_NonOKWithJSONBody covers the ordering between the body
// code and the HTTP status: a 5xx whose body parses but carries no header code
// must still be rejected on the status alone.
func TestDoubaoTranscribe_NonOKWithJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"result":{"text":"partial"}}`)
	}))
	defer srv.Close()

	_, err := newDoubao(srv.URL).Transcribe(context.Background(), strings.NewReader(string(doubaoMP3)), "zh")
	if err == nil {
		t.Fatal("expected an error for a non-200 response with a parseable body")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("error should mention the status, got %v", err)
	}
}

// TestDoubaoTranscribe_MalformedBaseURL covers a base URL that cannot even be
// turned into a request: it must fail before dialing, not surface as a dial
// timeout.
func TestDoubaoTranscribe_MalformedBaseURL(t *testing.T) {
	p := &DoubaoProvider{APIKey: "test-key", BaseURL: "http://[::1"}

	_, err := p.Transcribe(context.Background(), strings.NewReader(string(doubaoMP3)), "zh")
	if err == nil || !strings.Contains(err.Error(), "create request") {
		t.Fatalf("want a create-request error, got %v", err)
	}
}

// TestDoubaoTranscribe_NilHTTPClient covers a provider assembled by hand (no
// NewDoubaoProvider): a nil client must fall back to a default one instead of
// panicking on a nil dereference.
func TestDoubaoTranscribe_NilHTTPClient(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"result":{"text":"ok"}}`)
	}))
	defer srv.Close()

	p := &DoubaoProvider{APIKey: "test-key", BaseURL: srv.URL}
	text, err := p.Transcribe(context.Background(), strings.NewReader(string(doubaoMP3)), "zh")
	if err != nil {
		t.Fatalf("Transcribe with a nil client: %v", err)
	}
	if text != "ok" {
		t.Errorf("text = %q, want ok", text)
	}
	if calls != 1 {
		t.Errorf("server saw %d request(s), want 1", calls)
	}
}

// TestTruncate covers the error-message cap: an HTML proxy page must not dump
// its whole body into the transcript error.
func TestTruncate(t *testing.T) {
	if got := truncate([]byte("short body")); got != "short body" {
		t.Errorf("truncate(short) = %q", got)
	}

	long := strings.Repeat("x", maxErrBody+40)
	got := truncate([]byte(long))
	if !strings.HasSuffix(got, "...") {
		t.Errorf("a truncated body should be marked with an ellipsis, got %q", got)
	}
	if len(got) != maxErrBody+len("...") {
		t.Errorf("truncated length = %d, want %d", len(got), maxErrBody+len("..."))
	}
}
