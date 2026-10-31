package summarize

import (
	"context"
	"net/url"
	"strings"
)

// IsAnthropicURL detects whether a URL is an Anthropic API endpoint based on
// the domain or path patterns. This is used by both the Summarizer construction
// and the connectivity test handler to auto-detect the API format.
func IsAnthropicURL(u string) bool {
	return strings.Contains(u, "anthropic.com") ||
		strings.HasSuffix(strings.TrimRight(u, "/"), "/v1/messages")
}

// Standard endpoint paths. Exported so the connectivity test handler completes
// URLs with exactly the paths the summarizers call — the two must never
// disagree about which URL a configuration resolves to.
const (
	// OpenAIChatCompletionsPath is the standard OpenAI-compatible completion path.
	OpenAIChatCompletionsPath = "/v1/chat/completions"
	// AnthropicMessagesPath is the standard Anthropic Messages API path.
	AnthropicMessagesPath = "/v1/messages"
)

// BuildEndpointURL resolves a configured base URL to the full endpoint URL for
// defaultPath. It accepts every shape a user might reasonably configure:
//
//   - "https://api.openai.com"                     → "https://api.openai.com/v1/chat/completions"
//   - "https://api.openai.com/v1"                  → "https://api.openai.com/v1/chat/completions"
//   - "https://openrouter.ai/api/v1"               → "https://openrouter.ai/api/v1/chat/completions"
//   - "https://api.openai.com/v1/chat/completions" → unchanged
//   - "https://api.minimax.cn/anthropic"           → "https://api.minimax.cn/anthropic/v1/messages"
//   - "https://host/v1/text/chatcompletion_v2"     → unchanged (custom endpoint)
//
// The rule: an empty path is a bare base; a path ending with defaultPath is
// already complete; a path ending with a leading run of defaultPath's segments
// (e.g. "/v1", "/api/v1") is a partial base and gets the remainder appended; a
// path ending with a bare version segment other than defaultPath's own
// ("/api/v3") is a base for the same endpoint shape under a different API
// version and gets the part after the version prefix appended; a path ending
// with defaultPath's last segment (Azure's
// "/openai/deployments/<id>/chat/completions") is an endpoint without the
// version prefix and is left alone; a path with no version segment at all is a
// namespace the API is mounted under and gets defaultPath appended; anything
// else is the user's explicit custom endpoint and is used verbatim — blindly
// appending would turn a provider-specific path like MiniMax's
// "/v1/text/chatcompletion_v2" into a 404.
//
// This is the single implementation shared by the summarizer constructors and
// the connectivity test. They used to disagree: the test completed a bare host
// while the real request posted to it verbatim — so a bare host passed "test
// connection" and then 404'd on the actual call.
func BuildEndpointURL(baseURL, defaultPath string) string {
	u := strings.TrimRight(baseURL, "/")
	if u == "" {
		return ""
	}
	dp := ensureLeadingSlash(defaultPath)
	parsed, err := url.Parse(u)
	if err != nil {
		// Let http.NewRequest surface the malformed URL rather than guessing.
		return u
	}
	path := parsed.Path

	// A bare host (no path) is a base to complete.
	if path == "" || path == "/" {
		return u + dp
	}
	// Already the target endpoint.
	if strings.HasSuffix(path, dp) {
		return u
	}
	// A partial base ending in a leading run of defaultPath's segments, e.g.
	// "/v1" or "/api/v1" for "/v1/chat/completions". Longest run wins so the
	// remainder appended is the shortest.
	segments := strings.Split(strings.TrimLeft(dp, "/"), "/")
	for i := len(segments) - 1; i >= 1; i-- {
		prefix := "/" + strings.Join(segments[:i], "/")
		if strings.HasSuffix(path, prefix) {
			return u + strings.TrimPrefix(dp, prefix)
		}
	}
	// A path ending in a bare version segment is a base, not an endpoint: the
	// version is an address namespace, so the endpoint shape is defaultPath
	// minus its own version prefix. Volcengine Ark publishes "/api/v3" and
	// serves "/api/v3/chat/completions"; returning "/api/v3" verbatim 404'd
	// every summary, recommendation, auto-title and /btw call.
	if _, rest, ok := splitVersionPrefix(dp); ok {
		if _, ok := trailingVersionSegment(path); ok {
			return u + rest
		}
	}
	// An endpoint that keeps the protocol's action segment but drops the version
	// prefix is still complete, e.g. Azure OpenAI's
	// "/openai/deployments/<deployment>/chat/completions".
	if last := segments[len(segments)-1]; last != "" && strings.HasSuffix(path, "/"+last) {
		return u
	}
	// A versionless path is a namespace the API is mounted under, not an
	// endpoint: every wire protocol here ends with a versioned path. MiniMax's
	// Anthropic gateway publishes "/anthropic" as its base_url and serves
	// "/anthropic/v1/messages"; posting to the namespace itself 404s.
	if !hasVersionSegment(path) {
		return u + dp
	}
	// A provider-specific endpoint the user typed in full.
	return u
}

// hasVersionSegment reports whether a URL path carries an API version segment
// ("v1", "v2", …). A versioned path is an address the user typed deliberately;
// a versionless one is a mount point to complete.
func hasVersionSegment(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if isVersionSegment(seg) {
			return true
		}
	}
	return false
}

// trailingVersionSegment returns the final path segment when it is a bare API
// version ("v1", "v2", …) — i.e. the path is a base, not an endpoint.
func trailingVersionSegment(path string) (string, bool) {
	seg := path[strings.LastIndex(path, "/")+1:]
	if !isVersionSegment(seg) {
		return "", false
	}
	return seg, true
}

// splitVersionPrefix splits a default endpoint path into its leading version
// segment and the remainder that identifies the endpoint shape:
// "/v1/chat/completions" → ("v1", "/chat/completions").
func splitVersionPrefix(defaultPath string) (version, rest string, ok bool) {
	seg, remainder, found := strings.Cut(strings.TrimPrefix(defaultPath, "/"), "/")
	if !found || !isVersionSegment(seg) {
		return "", "", false
	}
	return seg, "/" + remainder, true
}

// isVersionSegment reports whether seg is a bare API version ("v1", "v2", …).
func isVersionSegment(seg string) bool {
	if len(seg) < 2 || seg[0] != 'v' {
		return false
	}
	return strings.Trim(seg[1:], "0123456789") == ""
}

// ensureLeadingSlash prefixes path with "/" unless it already has one.
func ensureLeadingSlash(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

const (
	// roleUser is the standard "user" chat role used by all recommendation
	// request builders (OpenAI + Anthropic compatible).
	roleUser = "user"
	// defaultTTSPrompt is the fallback prompt used when the external file is not found.
	// A language directive (e.g. "Output in Chinese.") is appended at load time.
	defaultTTSPrompt = `You are a text condenser for TTS voice playback. Your job is to faithfully condense the given text into natural spoken language that can be read aloud as-is.

Core principle: Preserve the original meaning and tone exactly. You are condensing, not responding — never add your own opinions, commentary, or conversational replies. The output should sound like the original author speaking concisely, not like someone chatting about the content.

Rules:
1. Preserve all key information, conclusions, and important details from the original text. Do not bias toward only the end — retain substance throughout.
2. Omit code, commands, file paths, config values, and technical syntax that cannot be read aloud naturally.
3. Omit intermediate step-by-step reasoning, verbose explanations, and redundant repetition — but keep the essential points they lead to.
4. Use natural spoken language. Plain text only — no markdown, no formatting.
5. No meta-phrases like "In summary", "The answer is", or "Here's what was said."
6. Ignore XML/HTML tags, schedule proposals, tool-call artifacts, and UI labels.
7. Drop any fragmented or incoherent text caused by truncation — output only fluent, complete content.
8. Maintain the original author's stance and intent. If the original is neutral/explanatory, stay neutral. If it makes a claim, preserve that claim faithfully.`

	// ShortTextThreshold — texts shorter than this are not summarized.
	// Configurable for testing purposes only (internal package).
	ShortTextThreshold = 300

	// DefaultMaxSummarizeRunes is the default maximum number of runes for summarization input.
	// Texts longer than this are truncated to the last N characters.
	// Configurable via config/config.yaml tts.max_summarize_runes.
	DefaultMaxSummarizeRunes = 10000

	// SimpleMaxSummarizeRunes is the maximum number of runes for the "simple" summarizer,
	// which does no AI-based summarization — just strips markdown and truncates.
	// Lower than the AI summarizer because without AI condensation, longer text
	// would be too verbose for TTS.
	SimpleMaxSummarizeRunes = 1000

	// CacheKeyHexLen is the number of hex characters used for the cache filename.
	CacheKeyHexLen = 16

	// reSummarizeThreshold — if the first summarization result exceeds this
	// many bytes, a second pass is requested to further condense the text.
	reSummarizeThreshold = 4000

	// maxSummarizePasses is the maximum number of summarization attempts
	// (first pass + optional re-summarization).
	maxSummarizePasses = 2
)

// MaxSummarizeRunes is the maximum number of runes for summarization input.
// Texts longer than this are truncated to the last N characters.
// Set by config; defaults to DefaultMaxSummarizeRunes.
var MaxSummarizeRunes = DefaultMaxSummarizeRunes

// MaxTextRunes is the maximum number of runes accepted for TTS input.
// Set to 0 for no hard limit — long texts are handled by the summarization step before synthesis.
var MaxTextRunes = 0

// SummarizeOption controls summarization behavior.
type SummarizeOption struct {
	// PreserveMarkdown, when true, skips StripMarkdown on both input preparation
	// and output post-processing. Used by TaskSummarizer to retain formatting.
	PreserveMarkdown bool
}

// Summarizer abstracts text summarization.
// Implementations can use different backends (AI backends, API, etc.)
type Summarizer interface {
	// Summarize condenses text.
	// For short text, it may return the text as-is after stripping markdown.
	// language is a language code (e.g. "zh", "en") used to direct output language.
	// The caller is responsible for setting a deadline on ctx.
	Summarize(ctx context.Context, text string, language string) (string, error)
}

// summarizePassFunc is the strategy function for a single summarization pass.
// Each backend (API, AI backend) provides its own implementation.
type summarizePassFunc func(ctx context.Context, text, systemPrompt string, pass int) (string, error)

// summarizePipeline implements the shared summarization logic:
//  1. prepareTextForSummarization (conditionally strip markdown, truncate)
//  2. Short text bypass
//  3. Multi-pass with re-summarization (language-aware prompt)
//  4. Post-processing (conditionally strip markdown on output)
//
// When PreserveMarkdown is false (TTS mode), markdown is stripped for voice output.
// When PreserveMarkdown is true (display mode), formatting is preserved for readability.
type summarizePipeline struct {
	passFn     summarizePassFunc
	basePrompt string // language-independent base prompt
	opts       SummarizeOption
}

// NewSummarizePipeline creates a summarizePipeline for TTS (strip markdown).
// The base prompt (without language) is loaded at creation time; language is
// resolved per-request in the Summarize call.
func NewSummarizePipeline(passFn summarizePassFunc) summarizePipeline {
	return summarizePipeline{
		passFn:     passFn,
		basePrompt: defaultTTSPrompt,
		opts:       SummarizeOption{PreserveMarkdown: false},
	}
}

// NewPipelineWithOpts creates a pipeline with the given pass function and options.
// When opts.PreserveMarkdown is true, StripMarkdown is skipped on both input
// and output — used by TaskSummarizer with API backends (OpenAI/Anthropic).
func NewPipelineWithOpts(passFn summarizePassFunc, basePrompt string, opts SummarizeOption) summarizePipeline {
	if basePrompt == "" {
		basePrompt = defaultTTSPrompt
	}
	return summarizePipeline{
		passFn:     passFn,
		basePrompt: basePrompt,
		opts:       opts,
	}
}

// Summarize implements the shared summarization pipeline.
// language is a language code (e.g. "zh", "en") used to direct output language.
func (g *summarizePipeline) Summarize(ctx context.Context, text string, language string) (string, error) {
	cleaned, needsSummarization := prepareTextForSummarization(text, g.opts.PreserveMarkdown)
	if !needsSummarization {
		return cleaned, nil
	}

	prompt := g.basePrompt + "\n\nOutput in " + languageName(language) + ". Translate any non-" + languageName(language) + " content first (keep proper nouns, variable names, and commands in their original form)."

	result, err := g.passFn(ctx, cleaned, prompt, 1)
	if err != nil {
		return "", err
	}

	// If the result is still too long, do a second pass with the same prompt
	if needsReSummarization(result, 1) {
		second, err := g.passFn(ctx, result, prompt, 2)
		if err != nil {
			// On second pass failure, use first pass result
			return postProcess(result, g.opts.PreserveMarkdown), nil //nolint:nilerr // intentional: fallback to first pass on second pass failure
		}
		result = second
	}

	return postProcess(result, g.opts.PreserveMarkdown), nil
}

// postProcess applies final processing to the summarization result.
// If PreserveMarkdown is false (TTS mode), StripMarkdown is applied.
func postProcess(result string, preserveMarkdown bool) string {
	if preserveMarkdown {
		return result
	}
	return StripMarkdown(result)
}

// languageName returns a human-readable language name for common language codes.
// Falls back to the code itself for unknown codes.
func languageName(code string) string {
	switch strings.ToLower(code) {
	case "zh", "cmn", "chinese":
		return "Chinese"
	case "en", "eng", "english":
		return "English"
	case "ja", "jpn", "japanese":
		return "Japanese"
	case "ko", "kor", "korean":
		return "Korean"
	case "fr", "fra", "french":
		return "French"
	case "de", "deu", "german":
		return "German"
	case "es", "spa", "spanish":
		return "Spanish"
	case "pt", "por", "portuguese":
		return "Portuguese"
	case "ru", "rus", "russian":
		return "Russian"
	case "ar", "ara", "arabic":
		return "Arabic"
	case "it", "ita", "italian":
		return "Italian"
	default:
		return code
	}
}

// prepareTextForSummarization cleans and truncates text before sending to a summarizer.
// Returns the cleaned text and true if summarization is needed,
// or the cleaned text and false if the text is empty (nothing to summarize).
func prepareTextForSummarization(text string, preserveMarkdown bool) (string, bool) {
	var cleaned string
	if preserveMarkdown {
		cleaned = text // preserve markdown formatting
	} else {
		cleaned = StripMarkdown(text)
	}

	// Empty (or whitespace-only) text has nothing to summarize.
	// Short text is still sent to the summarizer so it can polish format.
	if len([]rune(cleaned)) == 0 {
		return cleaned, false
	}

	// Truncate to last MaxSummarizeRunes if too long
	if runes := []rune(cleaned); len(runes) > MaxSummarizeRunes {
		cleaned = string(runes[len(runes)-MaxSummarizeRunes:])
	}

	return cleaned, true
}

// needsReSummarization returns true if the summarization result is still
// too long (in bytes) and a second pass would be beneficial.
func needsReSummarization(result string, pass int) bool {
	return pass < maxSummarizePasses && len(result) > reSummarizeThreshold
}
