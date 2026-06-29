package push

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJPushClient_SetBaseURL(t *testing.T) {
	client := NewJPushClient(JPushConfig{Enabled: true, AppKey: "key", MasterSecret: "secret"})
	originalURL := client.baseURL
	client.SetBaseURL("http://localhost:9999")
	if client.baseURL != "http://localhost:9999" {
		t.Errorf("expected baseURL to be overridden, got %q", client.baseURL)
	}
	if originalURL == client.baseURL {
		t.Error("baseURL should have changed")
	}
}

func TestJPushClient_SendNotification_Disabled(t *testing.T) {
	client := NewJPushClient(JPushConfig{Enabled: false})
	err := client.SendNotification("test-reg-id", "Title", "Alert", map[string]string{"event_type": "task_update"})
	if err != nil {
		t.Fatalf("disabled client should return nil error, got: %v", err)
	}
}

func TestJPushClient_SendNotification_Success(t *testing.T) {
	var receivedBody json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v3/push" {
			t.Errorf("expected /v3/push, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth == "" {
			t.Error("expected Authorization header")
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected application/json, got %s", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sendno":"1","msg_id":"12345"}`))
	}))
	defer server.Close()

	client := NewJPushClient(JPushConfig{
		Enabled:      true,
		AppKey:       "test-app-key",
		MasterSecret: "test-master-secret",
	})
	client.baseURL = server.URL

	err := client.SendNotification("reg-123", "AI任务完成", "Task completed", map[string]string{"event_type": "task_update", "task_id": "1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(receivedBody) == 0 {
		t.Error("expected request body")
	}
}

func TestJPushClient_SendNotification_EmptyRegID(t *testing.T) {
	client := NewJPushClient(JPushConfig{Enabled: true, AppKey: "key", MasterSecret: "secret"})
	err := client.SendNotification("", "Title", "Alert", nil)
	if err == nil {
		t.Error("expected error for empty registration ID")
	}
}

func TestJPushClient_SendNotification_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":1002,"message":"invalid registration id"}}`))
	}))
	defer server.Close()

	client := NewJPushClient(JPushConfig{
		Enabled:      true,
		AppKey:       "test-app-key",
		MasterSecret: "test-master-secret",
	})
	client.baseURL = server.URL

	err := client.SendNotification("bad-reg", "Title", "Alert", nil)
	if err == nil {
		t.Error("expected error for server error response")
	}
}

func TestJPushClient_Reconfigure(t *testing.T) {
	client := NewJPushClient(JPushConfig{Enabled: false, AppKey: "", MasterSecret: ""})

	// Reconfigure to enable
	client.Reconfigure(JPushConfig{Enabled: true, AppKey: "new-key", MasterSecret: "new-secret"})
	if !client.Enabled() {
		t.Error("client should be enabled after Reconfigure with valid config")
	}

	// Reconfigure to disable
	client.Reconfigure(JPushConfig{Enabled: false, AppKey: "new-key", MasterSecret: "new-secret"})
	if client.Enabled() {
		t.Error("client should be disabled when Enabled=false")
	}

	// Reconfigure with missing appKey
	client.Reconfigure(JPushConfig{Enabled: true, AppKey: "", MasterSecret: "secret"})
	if client.Enabled() {
		t.Error("client should be disabled when AppKey is empty")
	}

	// Reconfigure with missing masterSecret
	client.Reconfigure(JPushConfig{Enabled: true, AppKey: "key", MasterSecret: ""})
	if client.Enabled() {
		t.Error("client should be disabled when MasterSecret is empty")
	}
}

func TestJPushClient_Enabled(t *testing.T) {
	// All three conditions must be true: enabled=true, appKey!="", masterSecret!=""
	client := NewJPushClient(JPushConfig{Enabled: true, AppKey: "key", MasterSecret: "secret"})
	if !client.Enabled() {
		t.Error("fully configured client should be enabled")
	}

	// Missing appKey
	client2 := NewJPushClient(JPushConfig{Enabled: true, AppKey: "", MasterSecret: "secret"})
	if client2.Enabled() {
		t.Error("client with empty AppKey should not be enabled")
	}

	// Missing masterSecret
	client3 := NewJPushClient(JPushConfig{Enabled: true, AppKey: "key", MasterSecret: ""})
	if client3.Enabled() {
		t.Error("client with empty MasterSecret should not be enabled")
	}

	// Disabled
	client4 := NewJPushClient(JPushConfig{Enabled: false, AppKey: "key", MasterSecret: "secret"})
	if client4.Enabled() {
		t.Error("disabled client should not be enabled")
	}
}

func TestJPushClient_Reconfigure_ConcurrentAccess(t *testing.T) {
	client := NewJPushClient(JPushConfig{Enabled: true, AppKey: "key", MasterSecret: "secret"})
	done := make(chan struct{})

	// Concurrent Reconfigure goroutine
	go func() {
		defer func() { done <- struct{}{} }()
		for range 100 {
			client.Reconfigure(JPushConfig{Enabled: true, AppKey: "key", MasterSecret: "secret"})
			client.Reconfigure(JPushConfig{Enabled: false, AppKey: "", MasterSecret: ""})
		}
	}()

	// Concurrent Enabled() reader goroutine
	go func() {
		defer func() { done <- struct{}{} }()
		for range 100 {
			_ = client.Enabled()
		}
	}()

	// Wait for both goroutines (no data race = test passes under -race)
	<-done
	<-done
}
