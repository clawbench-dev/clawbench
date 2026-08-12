package summarize

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/model"
)

func TestNewAISummarizer_EmptyBaseURL(t *testing.T) {
	s := NewAISummarizer(model.AISummaryConfig{})
	if s != nil {
		t.Fatalf("expected nil summarizer for empty base_url, got %T", s)
	}
}

func TestNewAISummarizer_FormatOpenAI(t *testing.T) {
	s := NewAISummarizer(model.AISummaryConfig{
		Format: "openai",
		Model:  "gpt-4o-mini",
		API:    model.APIConfig{BaseURL: "https://api.openai.com/v1/chat/completions", Key: "k"},
	})
	if _, ok := s.(*OpenAISummarizer); !ok {
		t.Fatalf("expected OpenAISummarizer for format=openai, got %T", s)
	}
}

func TestNewAISummarizer_FormatAnthropic(t *testing.T) {
	s := NewAISummarizer(model.AISummaryConfig{
		Format: "anthropic",
		Model:  "claude-3-haiku",
		API:    model.APIConfig{BaseURL: "https://api.anthropic.com/v1/messages", Key: "k"},
	})
	if _, ok := s.(*AnthropicSummarizer); !ok {
		t.Fatalf("expected AnthropicSummarizer for format=anthropic, got %T", s)
	}
}

func TestNewAISummarizer_AutoDetectFromURL(t *testing.T) {
	// Empty format + anthropic URL → AnthropicSummarizer
	s := NewAISummarizer(model.AISummaryConfig{
		API: model.APIConfig{BaseURL: "https://api.anthropic.com/v1/messages"},
	})
	if _, ok := s.(*AnthropicSummarizer); !ok {
		t.Fatalf("expected AnthropicSummarizer from URL auto-detect, got %T", s)
	}
	// Empty format + openai URL → OpenAISummarizer
	s2 := NewAISummarizer(model.AISummaryConfig{
		API: model.APIConfig{BaseURL: "https://api.openai.com/v1/chat/completions"},
	})
	if _, ok := s2.(*OpenAISummarizer); !ok {
		t.Fatalf("expected OpenAISummarizer from URL auto-detect, got %T", s2)
	}
}

func TestRecommendNextStep_UnsupportedBackend(t *testing.T) {
	// The "simple" summarizer does not expose DoSummarizePass → error.
	_, err := RecommendNextStep(context.Background(), NewSimple(), nil, nil, "conclusion", "zh")
	if err == nil {
		t.Fatal("expected error for simple summarizer, got nil")
	}
	if !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildRecommendInput_WithHistory(t *testing.T) {
	out := buildRecommendInput([]string{"user msg 1", "user msg 2"}, []string{"生成测试: run tests", "写文档: write doc"}, "the conclusion")
	if !strings.Contains(out, "Recent conversation") {
		t.Fatalf("expected conversation section, got: %q", out)
	}
	if !strings.Contains(out, "1. user msg 1") || !strings.Contains(out, "2. user msg 2") {
		t.Fatalf("expected chronological conversation, got: %q", out)
	}
	// Quick commands are framed as the user's frequently-used commands to be
	// referenced when appropriate — not as callable tools.
	if !strings.Contains(out, "以下是我的常用指令") {
		t.Fatalf("expected quick commands framed as user's frequently-used commands, got: %q", out)
	}
	if !strings.Contains(out, "生成测试: run tests") {
		t.Fatalf("expected quick commands section, got: %q", out)
	}
	if !strings.Contains(out, "Assistant's latest conclusion:") || !strings.Contains(out, "the conclusion") {
		t.Fatalf("expected conclusion section, got: %q", out)
	}
}

func TestBuildRecommendInput_NoHistory(t *testing.T) {
	out := buildRecommendInput(nil, nil, "conclusion only")
	if strings.Contains(out, "Recent conversation") || strings.Contains(out, "以下是我的常用指令") {
		t.Fatalf("empty sections should be omitted, got: %q", out)
	}
	if !strings.Contains(out, "conclusion only") {
		t.Fatalf("conclusion missing, got: %q", out)
	}
}

// TestRecommendNextStepPrompt_NoClarifyFallback guards the removal of the old
// "no reasonable next step" fallback requirement. The recommendation must always
// propose a concrete next step and never defer to a clarifying question.
func TestRecommendNextStepPrompt_NoClarifyFallback(t *testing.T) {
	if strings.Contains(recommendNextStepPrompt, "no reasonable next step") {
		t.Fatalf("prompt must not contain the clarify-fallback requirement, got: %q", recommendNextStepPrompt)
	}
	if strings.Contains(recommendNextStepPrompt, "inviting the user to clarify") {
		t.Fatalf("prompt must not contain the clarify-fallback requirement, got: %q", recommendNextStepPrompt)
	}
	if !strings.Contains(recommendNextStepPrompt, "exactly ONE concise next step") {
		t.Fatalf("prompt must still require exactly one next step, got: %q", recommendNextStepPrompt)
	}
}

// TestRecommendNextStepPrompt_NoUserQuestion guards that the recommendation is
// phrased as the user's next message to the AI, not a question back to the user.
func TestRecommendNextStepPrompt_NoUserQuestion(t *testing.T) {
	if !strings.Contains(recommendNextStepPrompt, "never ask the user a question") {
		t.Fatalf("prompt must instruct not to ask the user a question, got: %q", recommendNextStepPrompt)
	}
}

func TestRecommendNextStep_OpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Continue by running the tests."}}]}`))
	}))
	defer srv.Close()

	s := NewOpenAI(srv.URL, "key", "gpt-4o-mini")
	out, err := RecommendNextStep(context.Background(), s, []string{"please fix tests"}, nil, "The build passed.", "zh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "Continue by running the tests." {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRecommendNextStep_Anthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" {
			t.Errorf("unexpected auth header: %q", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Ask a clarifying question."}]}`))
	}))
	defer srv.Close()

	s := NewAnthropic(srv.URL, "key", "claude-3-haiku")
	out, err := RecommendNextStep(context.Background(), s, []string{"summarize this"}, nil, "Done.", "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "Ask a clarifying question." {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRecommendNextStep_APIFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	s := NewOpenAI(srv.URL, "bad", "gpt-4o-mini")
	_, err := RecommendNextStep(context.Background(), s, nil, nil, "text", "zh")
	if err == nil {
		t.Fatal("expected error on API failure, got nil")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected deadline error: %v", err)
	}
}
