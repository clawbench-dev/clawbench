package summarize

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The reported envelope (issue #688): a gateway answered an empty reasoning
// completion by sending the whole chat.completion payload as the content. A
// title must not be cut out of it, and a suggestion must not be stored.
const issue688Paylod = `{"id":"gen-1794992937-abc","object":"chat.completion","created":1794992937,"model":"nvidia/nemotron-3.5-lightning:free","choices":[{"index":0,"finish_reason":"length","message":{"role":"assistant","content":null,"reasoning":"thinking and thinking"}}]}`

// newJSONServer serves one fixed body for every request and returns its URL.
func newJSONServer(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// quoteJSON makes s a JSON string literal, so it can be embedded as the
// "content" of a chat completion response.
func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestLooksLikeEnvelope(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"reported chat completion", "  " + issue688Paylod + "\n", true},
		{"openai error body", `{"error":{"message":"model overloaded","type":"server_error"}}`, true},
		{"anthropic error body", `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`, true},
		{"anthropic message", `{"type":"message","content":[{"type":"text","text":"hi"}]}`, true},
		{"unknown usage envelope", `{"model":"m","usage":{"prompt_tokens":3}}`, true},
		{"plain suggestion", "帮我跑一下单元测试", false},
		{"suggestion quoting a json literal", `把 {"name": "x"} 这个参数改成字符串`, false},
		{"json object that is just payload data", `{"title":"修复登录"}`, false},
		{"truncated json", `{"choices":[{"index":0,"mess`, false},
		{"markdown table", "| a | b |\n|---|---|\n| 1 | 2 |", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, looksLikeEnvelope(c.in))
		})
	}
}

func TestGenerateSessionTitle_RejectsEnvelopePayload(t *testing.T) {
	srv := newJSONServer(t, `{"choices":[{"message":{"content":`+quoteJSON(issue688Paylod)+`}}]}`)
	s := NewOpenAI(srv, "key", "m")

	title, err := GenerateSessionTitle(context.Background(), s, []string{"帮我看下这个报错"}, "zh")
	assert.ErrorIs(t, err, ErrOutputNotPlain)
	assert.Empty(t, title, "nothing usable may be stored as the title")
}

func TestRecommendNextStep_RejectsEnvelopePayload(t *testing.T) {
	srv := newJSONServer(t, `{"choices":[{"message":{"content":`+quoteJSON(issue688Paylod)+`}}]}`)
	s := NewOpenAI(srv, "key", "m")

	out, err := RecommendNextStep(context.Background(), s, nil, nil, nil, "结论", "zh")
	assert.ErrorIs(t, err, ErrOutputNotPlain)
	assert.Empty(t, out)
}

// A reasoning model that could still not answer (effort rejected, budget spent
// thinking) must fail visibly with the reason instead of an empty result.
func TestOpenAIDoAskPass_ReasoningOnlyReplyNamesTheCause(t *testing.T) {
	// DeepSeek / GLM / Qwen / Gemini's compatible layer report the thinking in
	// "reasoning_content"; OpenRouter uses "reasoning". Both must be attributed.
	cases := map[string]string{
		"reasoning_content": `{"choices":[{"finish_reason":"length","message":{"content":"","reasoning_content":"lots of thinking"}}]}`,
		"reasoning":         `{"choices":[{"finish_reason":"length","message":{"content":"","reasoning":"lots of thinking"}}]}`,
	}
	for key, body := range cases {
		t.Run(key, func(t *testing.T) {
			srv := newJSONServer(t, body)
			s := NewOpenAI(srv, "key", "m")

			_, err := s.DoRecommendPass(context.Background(), "sys", "", "rolling")
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "whole output budget for reasoning")
			assert.Contains(t, err.Error(), "finish_reason=\"length\"")
		})
	}
}

// The reasoning fields are response-only: they must never be sent back in the
// request body (some gateways reject unknown input fields).
func TestOpenAIRequest_OmitsReasoningFields(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		msgs, _ := body["messages"].([]any)
		got = msgs[0].(map[string]any)
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	s := NewOpenAI(srv.URL, "key", "m")
	_, _ = s.DoRecommendPass(context.Background(), "sys", "", "rolling")
	assert.NotContains(t, got, "reasoning")
	assert.NotContains(t, got, "reasoning_content")
}

// Anthropic reports a spent budget as thinking blocks plus stop_reason; an
// empty reply must say which, not just "empty output".
func TestAnthropicDoAskPass_ReasoningOnlyReplyNamesTheCause(t *testing.T) {
	srv := newJSONServer(t, `{"stop_reason":"max_tokens","content":[{"type":"thinking","thinking":"lots of thinking"},{"type":"text","text":""}]}`)
	s := NewAnthropic(srv, "key", "m")

	_, err := s.DoRecommendPass(context.Background(), "sys", "", "rolling")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "whole output budget for reasoning")
	assert.Contains(t, err.Error(), "finish_reason=\"max_tokens\"")
	assert.Contains(t, err.Error(), "16 chars of reasoning")
}

func TestReasoningBudget(t *testing.T) {
	// auto: at least the old 1024, and never below what the model allows.
	maxTokens, effort := reasoningBudget("gpt-4o-mini", 0)
	assert.GreaterOrEqual(t, maxTokens, recommendMaxTokens)
	assert.Equal(t, reasoningEffortNone, effort)

	// A caller's larger cap is kept.
	big := OutputBudgetForModel("", 0) + 500
	maxTokens, _ = reasoningBudget("gpt-4o-mini", big)
	assert.Equal(t, big, maxTokens)
}
