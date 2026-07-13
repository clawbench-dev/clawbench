package dingtalk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"clawbench/internal/model"
)

func TestSendMarkdownMessage_Success(t *testing.T) {
	var receivedReq struct {
		RobotCode string   `json:"robotCode"`
		UserIDs   []string `json:"userIds"`
		MsgKey    string   `json:"msgKey"`
		MsgParam  string   `json:"msgParam"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-acs-dingtalk-access-token") != "test-access-token" {
			t.Errorf("expected access token header, got %q", r.Header.Get("x-acs-dingtalk-access-token"))
		}
		_ = json.NewDecoder(r.Body).Decode(&receivedReq)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	origSendURL := dingtalkRobotSendURL
	origTokenURL := dingtalkTokenURL
	dingtalkRobotSendURL = srv.URL
	dingtalkTokenURL = srv.URL
	defer func() {
		dingtalkRobotSendURL = origSendURL
		dingtalkTokenURL = origTokenURL
	}()

	tokenMu.Lock()
	cachedToken = "test-access-token"
	cachedExp = time.Now().Add(2 * time.Hour)
	tokenMu.Unlock()
	defer resetTokenCache()

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "test-app-key"}}
	err := mgr.SendMarkdownMessage(context.Background(), "staff123", "Test Title", "## Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedReq.RobotCode != "test-app-key" {
		t.Errorf("expected robotCode test-app-key, got %q", receivedReq.RobotCode)
	}
	if receivedReq.MsgKey != "sampleMarkdown" {
		t.Errorf("expected msgKey sampleMarkdown, got %q", receivedReq.MsgKey)
	}
	if len(receivedReq.UserIDs) != 1 || receivedReq.UserIDs[0] != "staff123" {
		t.Errorf("expected userIds [staff123], got %v", receivedReq.UserIDs)
	}
}

func TestSendMarkdownMessage_TokenExpired401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"Unauthorized"}`))
	}))
	defer srv.Close()

	origSendURL := dingtalkRobotSendURL
	origTokenURL := dingtalkTokenURL
	dingtalkRobotSendURL = srv.URL
	dingtalkTokenURL = srv.URL
	defer func() {
		dingtalkRobotSendURL = origSendURL
		dingtalkTokenURL = origTokenURL
	}()

	tokenMu.Lock()
	cachedToken = "expired-token"
	cachedExp = time.Now().Add(2 * time.Hour)
	tokenMu.Unlock()
	defer resetTokenCache()

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "test-key"}}
	err := mgr.SendMarkdownMessage(context.Background(), "staff123", "Title", "Text")
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}

	tokenMu.RLock()
	token := cachedToken
	tokenMu.RUnlock()
	if token != "" {
		t.Error("expected token to be invalidated after 401")
	}
}

func TestSendMarkdownMessage_InvalidAuthentication(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"InvalidAuthentication","message":"token is invalid"}`))
	}))
	defer srv.Close()

	origSendURL := dingtalkRobotSendURL
	origTokenURL := dingtalkTokenURL
	dingtalkRobotSendURL = srv.URL
	dingtalkTokenURL = srv.URL
	defer func() {
		dingtalkRobotSendURL = origSendURL
		dingtalkTokenURL = origTokenURL
	}()

	tokenMu.Lock()
	cachedToken = "bad-token"
	cachedExp = time.Now().Add(2 * time.Hour)
	tokenMu.Unlock()
	defer resetTokenCache()

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "test-key"}}
	err := mgr.SendMarkdownMessage(context.Background(), "staff123", "Title", "Text")
	if err == nil {
		t.Fatal("expected error for InvalidAuthentication, got nil")
	}

	tokenMu.RLock()
	token := cachedToken
	tokenMu.RUnlock()
	if token != "" {
		t.Error("expected token to be invalidated after InvalidAuthentication")
	}
}

func TestSendMarkdownMessage_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"BadRequest","message":"invalid parameter"}`))
	}))
	defer srv.Close()

	origSendURL := dingtalkRobotSendURL
	origTokenURL := dingtalkTokenURL
	dingtalkRobotSendURL = srv.URL
	dingtalkTokenURL = srv.URL
	defer func() {
		dingtalkRobotSendURL = origSendURL
		dingtalkTokenURL = origTokenURL
	}()

	tokenMu.Lock()
	cachedToken = "valid-token"
	cachedExp = time.Now().Add(2 * time.Hour)
	tokenMu.Unlock()
	defer resetTokenCache()

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "test-key"}}
	err := mgr.SendMarkdownMessage(context.Background(), "staff123", "Title", "Text")
	if err == nil {
		t.Fatal("expected error for API error response, got nil")
	}
}

func TestSendMarkdownMessage_InvalidJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	origSendURL := dingtalkRobotSendURL
	origTokenURL := dingtalkTokenURL
	dingtalkRobotSendURL = srv.URL
	dingtalkTokenURL = srv.URL
	defer func() {
		dingtalkRobotSendURL = origSendURL
		dingtalkTokenURL = origTokenURL
	}()

	tokenMu.Lock()
	cachedToken = "valid-token"
	cachedExp = time.Now().Add(2 * time.Hour)
	tokenMu.Unlock()
	defer resetTokenCache()

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "test-key"}}
	err := mgr.SendMarkdownMessage(context.Background(), "staff123", "Title", "Text")
	if err == nil {
		t.Fatal("expected error for invalid JSON response, got nil")
	}
}

func TestSendMarkdownMessage_TokenFetchError(t *testing.T) {
	InvalidateToken()
	defer resetTokenCache()

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "key", AppSecret: "secret"}}
	err := mgr.SendMarkdownMessage(context.Background(), "staff123", "Title", "Text")
	if err == nil {
		t.Fatal("expected error when token fetch fails, got nil")
	}
}
