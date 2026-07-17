package summarize

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- languageName ---

func TestLanguageName_CommonCodes(t *testing.T) {
	tests := []struct {
		code     string
		expected string
	}{
		{"zh", "Chinese"},
		{"en", "English"},
		{"ja", "Japanese"},
		{"ko", "Korean"},
		{"fr", "French"},
		{"de", "German"},
		{"es", "Spanish"},
		{"pt", "Portuguese"},
		{"ru", "Russian"},
		{"ar", "Arabic"},
		{"it", "Italian"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, languageName(tc.code))
	}
}

func TestLanguageName_Aliases(t *testing.T) {
	tests := []struct {
		code     string
		expected string
	}{
		{"cmn", "Chinese"},
		{"chinese", "Chinese"},
		{"eng", "English"},
		{"english", "English"},
		{"jpn", "Japanese"},
		{"japanese", "Japanese"},
		{"kor", "Korean"},
		{"korean", "Korean"},
		{"fra", "French"},
		{"deu", "German"},
		{"spa", "Spanish"},
		{"por", "Portuguese"},
		{"rus", "Russian"},
		{"ara", "Arabic"},
		{"ita", "Italian"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, languageName(tc.code))
	}
}

func TestLanguageName_CaseInsensitive(t *testing.T) {
	assert.Equal(t, "Chinese", languageName("ZH"))
	assert.Equal(t, "Chinese", languageName("Zh"))
	assert.Equal(t, "English", languageName("EN"))
	assert.Equal(t, "English", languageName("En"))
}

func TestLanguageName_UnknownCode(t *testing.T) {
	assert.Equal(t, "xx", languageName("xx"))
	assert.Equal(t, "th", languageName("th"))
	assert.Equal(t, "vi", languageName("vi"))
}

func TestLanguageName_Empty(t *testing.T) {
	assert.Equal(t, "", languageName(""))
}

// --- summarizePipeline.Summarize with language ---

func TestSummarizePipeline_ShortText_SkipsLLM(t *testing.T) {
	var passCalled bool
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		passCalled = true
		return "", nil
	}

	s := summarizePipeline{
		passFn:     passFn,
		basePrompt: "base prompt",
	}

	shortText := "短文本不需要总结"
	result, err := s.Summarize(context.Background(), shortText, "zh")
	assert.NoError(t, err)
	assert.Contains(t, result, "短文本")
	assert.False(t, passCalled)
}

func TestSummarizePipeline_LongText_ConstructsLanguageAwarePrompt(t *testing.T) {
	var capturedPrompt string
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		capturedPrompt = systemPrompt
		return "summarized result", nil
	}

	s := summarizePipeline{
		passFn:     passFn,
		basePrompt: "Base prompt for summarization",
	}

	longText := strings.Repeat("这是一段较长的AI回复内容，包含了详细的技术分析。", 20)

	// Test with Chinese
	_, err := s.Summarize(context.Background(), longText, "zh")
	assert.NoError(t, err)
	assert.Contains(t, capturedPrompt, "Base prompt for summarization")
	assert.Contains(t, capturedPrompt, "Output in Chinese.")
	assert.Contains(t, capturedPrompt, "Translate any non-Chinese content first")
}

func TestSummarizePipeline_LanguageDirective_VariesByLanguage(t *testing.T) {
	var capturedPrompt string
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		capturedPrompt = systemPrompt
		return "result", nil
	}

	s := summarizePipeline{
		passFn:     passFn,
		basePrompt: "Base",
	}

	longText := strings.Repeat("This is a long AI response with detailed analysis and conclusions. ", 20)

	// Chinese
	_, _ = s.Summarize(context.Background(), longText, "zh")
	assert.Contains(t, capturedPrompt, "Output in Chinese.")

	// English
	_, _ = s.Summarize(context.Background(), longText, "en")
	assert.Contains(t, capturedPrompt, "Output in English.")

	// Japanese
	_, _ = s.Summarize(context.Background(), longText, "ja")
	assert.Contains(t, capturedPrompt, "Output in Japanese.")
}

func TestSummarizePipeline_ReSummarization_UsesSamePrompt(t *testing.T) {
	callCount := 0
	var prompts []string
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		callCount++
		prompts = append(prompts, systemPrompt)
		// Return a long result on first pass to trigger re-summarization
		if pass == 1 {
			return strings.Repeat("a", reSummarizeThreshold+1), nil
		}
		return "condensed", nil
	}

	s := summarizePipeline{
		passFn:     passFn,
		basePrompt: "Base",
	}

	longText := strings.Repeat("Long text that needs summarization. ", 30)
	result, err := s.Summarize(context.Background(), longText, "en")
	assert.NoError(t, err)
	assert.Equal(t, "condensed", result)
	assert.Equal(t, 2, callCount)
	// Both passes should use the same prompt
	assert.Equal(t, prompts[0], prompts[1])
}

func TestSummarizePipeline_SecondPassFailure_FallsBackToFirstPass(t *testing.T) {
	callCount := 0
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		callCount++
		if pass == 1 {
			return strings.Repeat("first pass result ", reSummarizeThreshold/10), nil
		}
		return "", context.DeadlineExceeded
	}

	s := summarizePipeline{
		passFn:     passFn,
		basePrompt: "Base",
	}

	longText := strings.Repeat("Long text that needs summarization. ", 30)
	result, err := s.Summarize(context.Background(), longText, "zh")
	assert.NoError(t, err)
	assert.Contains(t, result, "first pass result")
	assert.Equal(t, 2, callCount)
}

func TestSummarizePipeline_PassFnError(t *testing.T) {
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		return "", context.DeadlineExceeded
	}

	s := summarizePipeline{
		passFn:     passFn,
		basePrompt: "Base",
	}

	longText := strings.Repeat("这是一段较长的AI回复内容。", 30)
	_, err := s.Summarize(context.Background(), longText, "zh")
	assert.Error(t, err)
}

// --- summarizePipeline with PreserveMarkdown ---

func TestSummarizePipeline_PreserveMarkdown_ShortText(t *testing.T) {
	s := summarizePipeline{
		passFn:     func(ctx context.Context, text, systemPrompt string, pass int) (string, error) { return "", nil },
		basePrompt: "Base",
		opts:       SummarizeOption{PreserveMarkdown: true},
	}

	shortText := "**短文本**"
	result, err := s.Summarize(context.Background(), shortText, "zh")
	assert.NoError(t, err)
	// With PreserveMarkdown, short text should be returned as-is (no stripping)
	assert.Equal(t, "**短文本**", result)
}

func TestSummarizePipeline_PreserveMarkdown_LongText_NoStripOnOutput(t *testing.T) {
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		return "**总结结果** 包含markdown格式", nil
	}

	s := summarizePipeline{
		passFn:     passFn,
		basePrompt: "Base",
		opts:       SummarizeOption{PreserveMarkdown: true},
	}

	longText := strings.Repeat("这是一段较长的AI回复内容。", 30)
	result, err := s.Summarize(context.Background(), longText, "zh")
	assert.NoError(t, err)
	// With PreserveMarkdown, output should NOT be stripped
	assert.Contains(t, result, "**总结结果**")
}

func TestSummarizePipeline_NoPreserveMarkdown_StripsOnOutput(t *testing.T) {
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		return "**总结结果** 包含markdown格式", nil
	}

	s := summarizePipeline{
		passFn:     passFn,
		basePrompt: "Base",
		opts:       SummarizeOption{PreserveMarkdown: false},
	}

	longText := strings.Repeat("这是一段较长的AI回复内容。", 30)
	result, err := s.Summarize(context.Background(), longText, "zh")
	assert.NoError(t, err)
	// Without PreserveMarkdown, output should be stripped
	assert.NotContains(t, result, "**")
}

// --- prepareTextForSummarization ---

func TestPrepareTextForSummarization_ShortText(t *testing.T) {
	text := "短文本"
	cleaned, needs := prepareTextForSummarization(text, false)
	assert.Equal(t, text, cleaned)
	assert.False(t, needs)
}

func TestPrepareTextForSummarization_LongText(t *testing.T) {
	text := strings.Repeat("这是一段较长的文本内容。", 50)
	cleaned, needs := prepareTextForSummarization(text, false)
	assert.True(t, needs)
	assert.Equal(t, text, cleaned) // no truncation needed
}

func TestPrepareTextForSummarization_Truncation(t *testing.T) {
	origMax := MaxSummarizeRunes
	MaxSummarizeRunes = 100
	defer func() { MaxSummarizeRunes = origMax }()

	text := strings.Repeat("长文本", 200) // 600 runes
	cleaned, needs := prepareTextForSummarization(text, false)
	assert.True(t, needs)
	assert.Equal(t, 100, len([]rune(cleaned)))
}

func TestPrepareTextForSummarization_PreserveMarkdown(t *testing.T) {
	text := "**bold** and `code`"
	cleaned, needs := prepareTextForSummarization(text, true)
	assert.False(t, needs)
	// With PreserveMarkdown, markdown should be preserved
	assert.Equal(t, text, cleaned)
}

func TestPrepareTextForSummarization_NoPreserveMarkdown(t *testing.T) {
	text := "**bold** and `code`"
	cleaned, needs := prepareTextForSummarization(text, false)
	assert.False(t, needs)
	// Without PreserveMarkdown, markdown should be stripped
	assert.NotContains(t, cleaned, "**")
	assert.NotContains(t, cleaned, "`")
}

// --- needsReSummarization ---

func TestNeedsReSummarization(t *testing.T) {
	assert.True(t, needsReSummarization(strings.Repeat("a", reSummarizeThreshold+1), 1))
	assert.False(t, needsReSummarization("short", 1))
	assert.False(t, needsReSummarization(strings.Repeat("a", reSummarizeThreshold+1), 2)) // max pass reached
}

// --- NewSummarizePipeline ---

func TestNewSummarizePipeline(t *testing.T) {
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		return "", nil
	}

	s := NewSummarizePipeline(passFn)
	assert.Equal(t, defaultTTSPrompt, s.basePrompt)
	assert.False(t, s.opts.PreserveMarkdown)
}

// --- NewPipelineWithOpts ---

func TestNewPipelineWithOpts(t *testing.T) {
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		return "", nil
	}

	s := NewPipelineWithOpts(passFn, "custom prompt", SummarizeOption{PreserveMarkdown: true})
	assert.Equal(t, "custom prompt", s.basePrompt)
	assert.True(t, s.opts.PreserveMarkdown)
}

func TestNewPipelineWithOpts_DefaultPrompt(t *testing.T) {
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		return "", nil
	}

	s := NewPipelineWithOpts(passFn, "", SummarizeOption{PreserveMarkdown: false})
	assert.Equal(t, defaultTTSPrompt, s.basePrompt)
}

// --- IsAnthropicURL ---

func TestIsAnthropicURL_AnthropicDomain(t *testing.T) {
	assert.True(t, IsAnthropicURL("https://api.anthropic.com/v1/messages"))
	assert.True(t, IsAnthropicURL("https://api.anthropic.com/some/path"))
	assert.True(t, IsAnthropicURL("https://anthropic.com/"))
}

func TestIsAnthropicURL_V1MessagesSuffix(t *testing.T) {
	assert.True(t, IsAnthropicURL("https://some-proxy.example.com/v1/messages"))
	assert.True(t, IsAnthropicURL("https://custom-host/v1/messages/"))
}

func TestIsAnthropicURL_NonAnthropicURL(t *testing.T) {
	assert.False(t, IsAnthropicURL("https://api.openai.com/v1/chat/completions"))
	assert.False(t, IsAnthropicURL("https://api.example.com/v1/chat"))
	assert.False(t, IsAnthropicURL("https://some-host/v1/messages2"))
}

func TestIsAnthropicURL_EmptyString(t *testing.T) {
	assert.False(t, IsAnthropicURL(""))
}

func TestIsAnthropicURL_AnthropicDomainOnly(t *testing.T) {
	// Domain without /v1/messages path
	assert.True(t, IsAnthropicURL("https://api.anthropic.com"))
}

func TestIsAnthropicURL_AnthropicTrailingSlash(t *testing.T) {
	// TrimRight strips trailing / so /v1/messages/ matches /v1/messages
	assert.True(t, IsAnthropicURL("https://api.anthropic.com/v1/messages/"))
}

func TestIsAnthropicURL_CustomDomainV1Messages(t *testing.T) {
	// Non-Anthropic domain but /v1/messages path
	assert.True(t, IsAnthropicURL("https://custom.api.com/v1/messages"))
}

func TestIsAnthropicURL_OpenAIDomain(t *testing.T) {
	assert.False(t, IsAnthropicURL("https://api.openai.com/v1/chat/completions"))
}

// --- postProcess ---

func TestPostProcess_PreserveMarkdown_ReturnsAsIs(t *testing.T) {
	result := postProcess("# Hello **world**\n- item", true)
	assert.Equal(t, "# Hello **world**\n- item", result)
}

func TestPostProcess_NoPreserveMarkdown_StripsMarkdown(t *testing.T) {
	input := "# Hello **world**\n- item"
	result := postProcess(input, false)
	// After StripMarkdown, markdown syntax should be removed
	assert.NotContains(t, result, "#")
	assert.NotContains(t, result, "**")
}
