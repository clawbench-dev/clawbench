package summarize

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewAnthropic_Defaults(t *testing.T) {
	s := NewAnthropic("https://api.anthropic.com/v1/messages", "sk-ant-test", "")
	assert.Equal(t, "https://api.anthropic.com/v1/messages", s.BaseURL)
	assert.Equal(t, "claude-3-5-haiku-latest", s.Model)
	assert.Equal(t, "sk-ant-test", s.Key)
	assert.NotNil(t, s.HTTPClient)
}

func TestNewAnthropic_CustomConfig(t *testing.T) {
	s := NewAnthropic("https://custom.api.com/v1/messages/", "key-abc", "claude-3-opus-latest")
	assert.Equal(t, "https://custom.api.com/v1/messages", s.BaseURL) // trailing slash trimmed
	assert.Equal(t, "claude-3-opus-latest", s.Model)
	assert.Equal(t, "key-abc", s.Key)
}

func TestAnthropicSummarizer_ShortText_StillCallsLLM(t *testing.T) {
	var receivedReq anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := json.NewDecoder(r.Body).Decode(&receivedReq)
		assert.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "润色后的内容。"},
			},
		})
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "key", "")
	result, err := s.Summarize(context.Background(), "短文本", "zh")
	assert.NoError(t, err)
	assert.Equal(t, "润色后的内容。", result)
}

func TestAnthropicSummarizer_APICall(t *testing.T) {
	var receivedReq anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "sk-ant-test", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		err := json.NewDecoder(r.Body).Decode(&receivedReq)
		assert.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "这是总结后的内容。"},
			},
		})
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "sk-ant-test", "claude-3-5-haiku-latest")
	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	result, err := s.Summarize(context.Background(), longText, "zh")

	assert.NoError(t, err)
	assert.Equal(t, "这是总结后的内容。", result)
	assert.Equal(t, "claude-3-5-haiku-latest", receivedReq.Model)
	assert.Equal(t, "user", receivedReq.Messages[0].Role)
	assert.NotEmpty(t, receivedReq.System) // system prompt at top level
	assert.Equal(t, 1024, receivedReq.MaxTokens)
}

func TestAnthropicSummarizer_MultiPass(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var text string
		if callCount == 1 {
			text = strings.Repeat("这是一段很长的总结结果，", 300) // >4000 bytes
		} else {
			text = "精简后的总结。"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: text},
			},
		})
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "key", "claude-3-5-haiku-latest")
	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	result, err := s.Summarize(context.Background(), longText, "zh")

	assert.NoError(t, err)
	assert.Equal(t, "精简后的总结。", result)
	assert.Equal(t, 2, callCount)
}

func TestAnthropicSummarizer_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, "invalid api key")
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "bad-key", "claude-3-5-haiku-latest")
	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	_, err := s.Summarize(context.Background(), longText, "zh")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
	assert.Contains(t, err.Error(), "invalid api key")
}

func TestAnthropicSummarizer_ConnectionRefused(t *testing.T) {
	s := NewAnthropic("http://127.0.0.1:1", "key", "claude-3-5-haiku-latest")
	s.HTTPClient.Timeout = 2 * time.Second

	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	_, err := s.Summarize(context.Background(), longText, "zh")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "anthropic request")
}

func TestAnthropicSummarizer_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "  "},
			},
		})
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "key", "claude-3-5-haiku-latest")
	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	_, err := s.Summarize(context.Background(), longText, "zh")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty output")
}

func TestAnthropicSummarizer_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "too late"},
			},
		})
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "key", "claude-3-5-haiku-latest")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	_, err := s.Summarize(ctx, longText, "zh")

	assert.Error(t, err)
}

func TestAnthropicSummarizer_NoKey(t *testing.T) {
	// When key is empty, x-api-key header should not be set
	var apiKeyHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKeyHeader = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "ok"},
			},
		})
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "", "claude-3-5-haiku-latest")
	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	_, err := s.Summarize(context.Background(), longText, "zh")

	assert.NoError(t, err)
	assert.Empty(t, apiKeyHeader)
}

func TestAnthropicSummarizer_MultipleContentBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "第一部分。"},
				{Type: "text", Text: "第二部分。"},
			},
		})
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "key", "claude-3-5-haiku-latest")
	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	result, err := s.Summarize(context.Background(), longText, "zh")

	assert.NoError(t, err)
	assert.Equal(t, "第一部分。第二部分。", result)
}

func TestAnthropicDoRecommendPass_CacheableStable(t *testing.T) {
	var received anthropicRecommendRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, "rec-key", r.Header.Get("x-api-key"))
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{{Type: "text", Text: "推荐的操作。"}},
		})
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "rec-key", "claude-3-5-haiku-latest")
	stable := strings.Repeat("a", minCacheableRunes)
	result, err := s.DoRecommendPass(context.Background(), "sys", stable, "rolling")
	assert.NoError(t, err)
	assert.Equal(t, "推荐的操作。", result)
	assert.Equal(t, "claude-3-5-haiku-latest", received.Model)
	assert.Equal(t, "sys", received.System)
	assert.Len(t, received.Messages, 1)
	assert.Len(t, received.Messages[0].Content, 2)
	assert.NotNil(t, received.Messages[0].Content[0].CacheControl)
	assert.Equal(t, "ephemeral", received.Messages[0].Content[0].CacheControl.Type)
}

func TestAnthropicDoRecommendPass_SmallStable(t *testing.T) {
	var received anthropicRecommendRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{{Type: "text", Text: "ok"}},
		})
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "key", "claude-3-5-haiku-latest")
	_, err := s.DoRecommendPass(context.Background(), "sys", "short stable", "rolling")
	assert.NoError(t, err)
	assert.Len(t, received.Messages[0].Content, 2)
	assert.Nil(t, received.Messages[0].Content[0].CacheControl)
}

func TestAnthropicDoRecommendPass_EmptyStable(t *testing.T) {
	var received anthropicRecommendRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{{Type: "text", Text: "ok"}},
		})
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "key", "claude-3-5-haiku-latest")
	_, err := s.DoRecommendPass(context.Background(), "sys", "", "rolling")
	assert.NoError(t, err)
	assert.Len(t, received.Messages[0].Content, 1)
}

func TestAnthropicDoRecommendPass_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, "bad key")
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "bad-key", "claude-3-5-haiku-latest")
	_, err := s.DoRecommendPass(context.Background(), "sys", "stable", "rolling")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
}

func TestAnthropicDoRecommendPass_DecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, "not-json")
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "key", "claude-3-5-haiku-latest")
	_, err := s.DoRecommendPass(context.Background(), "sys", "stable", "rolling")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestAnthropicDoRecommendPass_EmptyOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{{Type: "text", Text: "   "}},
		})
	}))
	defer server.Close()

	s := NewAnthropic(server.URL, "key", "claude-3-5-haiku-latest")
	_, err := s.DoRecommendPass(context.Background(), "sys", "stable", "rolling")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty recommendation output")
}

func TestAnthropicDoRecommendPass_RequestCreateError(t *testing.T) {
	s := NewAnthropic("http://exa mple.com", "key", "claude-3-5-haiku-latest")
	_, err := s.DoRecommendPass(context.Background(), "sys", "stable", "rolling")
	assert.Error(t, err)
}

func TestAnthropicDoRecommendPass_ConnectionRefused(t *testing.T) {
	s := NewAnthropic("http://127.0.0.1:1", "key", "claude-3-5-haiku-latest")
	s.HTTPClient.Timeout = 2 * time.Second
	_, err := s.DoRecommendPass(context.Background(), "sys", "stable", "rolling")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "anthropic recommend request")
}
