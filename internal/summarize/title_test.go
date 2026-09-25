package summarize

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateSessionTitle_OpenAI(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"修复登录超时问题"}}]}`))
	}))
	defer srv.Close()

	s := NewOpenAI(srv.URL, "key", "gpt-4o-mini")
	title, err := GenerateSessionTitle(context.Background(), s, []string{"登录总是超时", "帮我看看"}, "zh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "修复登录超时问题" {
		t.Fatalf("unexpected title: %q", title)
	}
	// The title prompt is the system prompt; the user messages are the payload.
	if !strings.Contains(capturedBody, "session title generator") {
		t.Fatalf("expected title prompt in request, got: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, "登录总是超时") || !strings.Contains(capturedBody, "帮我看看") {
		t.Fatalf("expected all user messages in request, got: %s", capturedBody)
	}
}

func TestGenerateSessionTitle_Anthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" {
			t.Errorf("unexpected auth header: %q", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Fix login timeout"}]}`))
	}))
	defer srv.Close()

	s := NewAnthropic(srv.URL, "key", "claude-3-haiku")
	title, err := GenerateSessionTitle(context.Background(), s, []string{"login times out"}, "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Fix login timeout" {
		t.Fatalf("unexpected title: %q", title)
	}
}

// The "simple" summarizer is not an LLM backend — title generation must fail
// rather than silently return the raw text.
func TestGenerateSessionTitle_UnsupportedBackend(t *testing.T) {
	_, err := GenerateSessionTitle(context.Background(), NewSimple(), []string{"hello"}, "zh")
	if err == nil {
		t.Fatal("expected error for simple summarizer, got nil")
	}
	if !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// No usable user text means there is nothing to summarize; the caller relies on
// this error to report "no user messages" instead of sending an empty prompt.
func TestGenerateSessionTitle_NoMessages(t *testing.T) {
	s := NewOpenAI("http://127.0.0.1:0", "key", "")
	for _, msgs := range [][]string{nil, {}, {"", "   "}} {
		_, err := GenerateSessionTitle(context.Background(), s, msgs, "zh")
		if err == nil {
			t.Fatalf("expected error for empty messages %#v, got nil", msgs)
		}
	}
}

func TestGenerateSessionTitle_APIFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	s := NewOpenAI(srv.URL, "bad", "")
	_, err := GenerateSessionTitle(context.Background(), s, []string{"hello"}, "zh")
	if err == nil {
		t.Fatal("expected error on API failure, got nil")
	}
}

func TestGenerateSessionTitle_EmptyModelOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"   "}}]}`))
	}))
	defer srv.Close()

	s := NewOpenAI(srv.URL, "key", "")
	if _, err := GenerateSessionTitle(context.Background(), s, []string{"hello"}, "zh"); err == nil {
		t.Fatal("expected error for empty model output, got nil")
	}
}

func TestSanitizeSessionTitle(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"plain", "修复登录超时", "修复登录超时"},
		{"surrounding whitespace", "  修复登录超时  ", "修复登录超时"},
		{"double quotes", `"修复登录超时"`, "修复登录超时"},
		{"bold markdown", "**修复登录超时**", "修复登录超时"},
		{"backticks", "`修复登录超时`", "修复登录超时"},
		{"heading marker", "## 修复登录超时", "修复登录超时"},
		{"bullet marker", "- 修复登录超时", "修复登录超时"},
		{"title prefix", "Title: Fix login timeout", "Fix login timeout"},
		{"chinese title prefix", "标题：修复登录超时", "修复登录超时"},
		{"first non-empty line wins", "\n\n修复登录超时\n第二行不要", "修复登录超时"},
		{"only the first line is kept", "修复\n登录超时", "修复"},
		{"collapses internal spaces", "修复   登录超时", "修复 登录超时"},
		// A '#' without a following space is part of the title, not a heading.
		{"hash without space kept", "#1 优先修复", "#1 优先修复"},
		// Only matching wrappers are stripped — a lone trailing quote stays.
		{"unmatched quote kept", `修复"登录`, `修复"登录`},
		{"empty", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeSessionTitle(tc.raw); got != tc.want {
				t.Fatalf("sanitizeSessionTitle(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSanitizeSessionTitle_Truncates(t *testing.T) {
	long := strings.Repeat("字", 200)
	got := sanitizeSessionTitle(long)
	if n := len([]rune(got)); n != MaxSessionTitleRunes {
		t.Fatalf("expected %d runes, got %d", MaxSessionTitleRunes, n)
	}
}

// buildTitlePayload must cap the payload by runes (not bytes), so a CJK-heavy
// session cannot blow past the limit or split a multi-byte character.
func TestBuildTitlePayload_CapsByRunes(t *testing.T) {
	payload := buildTitlePayload([]string{strings.Repeat("字", maxTitlePayloadRunes+500)})
	if n := len([]rune(payload)); n != maxTitlePayloadRunes {
		t.Fatalf("expected %d runes, got %d", maxTitlePayloadRunes, n)
	}
}

func TestBuildTitlePayload_SkipsEmptyAndJoins(t *testing.T) {
	got := buildTitlePayload([]string{"first", "", "   ", "second"})
	if got != "first\nsecond" {
		t.Fatalf("unexpected payload: %q", got)
	}
}
