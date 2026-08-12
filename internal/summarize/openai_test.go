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

func TestNewOpenAI_Defaults(t *testing.T) {
	s := NewOpenAI("https://api.openai.com/v1/chat/completions", "sk-test", "")
	assert.Equal(t, "https://api.openai.com/v1/chat/completions", s.BaseURL)
	assert.Equal(t, "gpt-4o-mini", s.Model)
	assert.Equal(t, "sk-test", s.Key)
	assert.NotNil(t, s.HTTPClient)
}

func TestNewOpenAI_CustomConfig(t *testing.T) {
	s := NewOpenAI("https://api.deepseek.com/v1/chat/completions/", "sk-abc", "deepseek-chat")
	assert.Equal(t, "https://api.deepseek.com/v1/chat/completions", s.BaseURL) // trailing slash trimmed
	assert.Equal(t, "deepseek-chat", s.Model)
	assert.Equal(t, "sk-abc", s.Key)
}

func TestOpenAISummarizer_ShortText_StillCallsLLM(t *testing.T) {
	var receivedReq openaiChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := json.NewDecoder(r.Body).Decode(&receivedReq)
		assert.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Choices: []openaiChoice{
				{Message: openaiChatMessage{Role: "assistant", Content: "润色后的内容。"}},
			},
		})
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "key", "")
	result, err := s.Summarize(context.Background(), "短文本", "zh")
	assert.NoError(t, err)
	assert.Equal(t, "润色后的内容。", result)
	assert.Equal(t, "短文本", receivedReq.Messages[1].Content)
}

func TestOpenAISummarizer_APICall(t *testing.T) {
	var receivedReq openaiChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))

		err := json.NewDecoder(r.Body).Decode(&receivedReq)
		assert.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Choices: []openaiChoice{
				{Message: openaiChatMessage{Role: "assistant", Content: "这是总结后的内容。"}},
			},
		})
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "sk-test", "gpt-4o-mini")
	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	result, err := s.Summarize(context.Background(), longText, "zh")

	assert.NoError(t, err)
	assert.Equal(t, "这是总结后的内容。", result)
	assert.Equal(t, "gpt-4o-mini", receivedReq.Model)
	assert.Equal(t, "system", receivedReq.Messages[0].Role)
	assert.Equal(t, "user", receivedReq.Messages[1].Role)
	assert.Equal(t, 1024, receivedReq.MaxTokens)
}

func TestOpenAISummarizer_MultiPass(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var content string
		if callCount == 1 {
			content = strings.Repeat("这是一段很长的总结结果，", 300) // >4000 bytes
		} else {
			content = "精简后的总结。"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Choices: []openaiChoice{
				{Message: openaiChatMessage{Role: "assistant", Content: content}},
			},
		})
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "sk-test", "gpt-4o-mini")
	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	result, err := s.Summarize(context.Background(), longText, "zh")

	assert.NoError(t, err)
	assert.Equal(t, "精简后的总结。", result)
	assert.Equal(t, 2, callCount)
}

func TestOpenAISummarizer_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "model not found")
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "sk-test", "bad-model")
	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	_, err := s.Summarize(context.Background(), longText, "zh")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
	assert.Contains(t, err.Error(), "model not found")
}

func TestOpenAISummarizer_ConnectionRefused(t *testing.T) {
	s := NewOpenAI("http://127.0.0.1:1", "key", "gpt-4o-mini")
	s.HTTPClient.Timeout = 2 * time.Second

	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	_, err := s.Summarize(context.Background(), longText, "zh")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "openai request")
}

func TestOpenAISummarizer_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Choices: []openaiChoice{
				{Message: openaiChatMessage{Role: "assistant", Content: "  "}},
			},
		})
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "key", "gpt-4o-mini")
	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	_, err := s.Summarize(context.Background(), longText, "zh")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty output")
}

func TestOpenAISummarizer_NoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Choices: []openaiChoice{},
		})
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "key", "gpt-4o-mini")
	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	_, err := s.Summarize(context.Background(), longText, "zh")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no choices")
}

func TestOpenAISummarizer_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Choices: []openaiChoice{
				{Message: openaiChatMessage{Role: "assistant", Content: "too late"}},
			},
		})
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "key", "gpt-4o-mini")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	_, err := s.Summarize(ctx, longText, "zh")

	assert.Error(t, err)
}

func TestOpenAISummarizer_NoKey(t *testing.T) {
	// When key is empty, Authorization header should not be set
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Choices: []openaiChoice{
				{Message: openaiChatMessage{Role: "assistant", Content: "ok"}},
			},
		})
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "", "gpt-4o-mini")
	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	_, err := s.Summarize(context.Background(), longText, "zh")

	assert.NoError(t, err)
	assert.Empty(t, authHeader)
}

func TestOpenAIDoRecommendPass_Success(t *testing.T) {
	var received openaiChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer rec-key", r.Header.Get("Authorization"))
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Choices: []openaiChoice{{Message: openaiChatMessage{Role: "assistant", Content: "推荐的操作。"}}},
		})
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "rec-key", "gpt-4o-mini")
	result, err := s.DoRecommendPass(context.Background(), "sys", "stable prefix", "rolling")
	assert.NoError(t, err)
	assert.Equal(t, "推荐的操作。", result)
	assert.Equal(t, "gpt-4o-mini", received.Model)
	assert.Equal(t, 1024, received.MaxTokens)
	assert.Len(t, received.Messages, 3)
	assert.Equal(t, "system", received.Messages[0].Role)
	assert.Equal(t, "stable prefix", received.Messages[1].Content)
	assert.Equal(t, "rolling", received.Messages[2].Content)
}

func TestOpenAIDoRecommendPass_EmptyStable(t *testing.T) {
	var received openaiChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Choices: []openaiChoice{{Message: openaiChatMessage{Role: "assistant", Content: "ok"}}},
		})
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "key", "gpt-4o-mini")
	_, err := s.DoRecommendPass(context.Background(), "sys", "", "rolling")
	assert.NoError(t, err)
	assert.Len(t, received.Messages, 2)
}

func TestOpenAIDoRecommendPass_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "model down")
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "key", "gpt-4o-mini")
	_, err := s.DoRecommendPass(context.Background(), "sys", "stable", "rolling")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestOpenAIDoRecommendPass_DecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, "not-json")
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "key", "gpt-4o-mini")
	_, err := s.DoRecommendPass(context.Background(), "sys", "stable", "rolling")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestOpenAIDoRecommendPass_NoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openaiChatResponse{Choices: []openaiChoice{}})
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "key", "gpt-4o-mini")
	_, err := s.DoRecommendPass(context.Background(), "sys", "stable", "rolling")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no choices")
}

func TestOpenAIDoRecommendPass_EmptyOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Choices: []openaiChoice{{Message: openaiChatMessage{Role: "assistant", Content: "  "}}},
		})
	}))
	defer server.Close()

	s := NewOpenAI(server.URL, "key", "gpt-4o-mini")
	_, err := s.DoRecommendPass(context.Background(), "sys", "stable", "rolling")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty recommendation output")
}

func TestOpenAIDoRecommendPass_RequestCreateError(t *testing.T) {
	s := NewOpenAI("http://exa mple.com", "key", "gpt-4o-mini")
	_, err := s.DoRecommendPass(context.Background(), "sys", "stable", "rolling")
	assert.Error(t, err)
}

func TestOpenAIDoRecommendPass_ConnectionRefused(t *testing.T) {
	s := NewOpenAI("http://127.0.0.1:1", "key", "gpt-4o-mini")
	s.HTTPClient.Timeout = 2 * time.Second
	_, err := s.DoRecommendPass(context.Background(), "sys", "stable", "rolling")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "openai recommend request")
}
