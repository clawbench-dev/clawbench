package summarize

import (
	"context"
	"strings"

	"clawbench/internal/endpointurl"
	"clawbench/internal/model"
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
// defaultPath (e.g. "/v1/messages").
//
// It delegates to endpointurl.Resolve, which is the single implementation
// shared by the summarizer constructors, the connectivity test and the builtin
// agent's model client. They used to carry separate copies of the rules and
// disagree: a base URL passed "test connection" and then 404'd on the actual
// call. See that package for the accepted shapes and the resolution rules.
func BuildEndpointURL(baseURL, defaultPath string) string {
	return endpointurl.Resolve(baseURL, defaultPath)
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

	// MaxVerbatimReadRunes is the length above which a text is too long to read
	// aloud as-is, so the LLM summarizer is asked to condense it (see
	// AutoSummarizer). It is a ROUTING threshold only: the non-AI cleaner never
	// truncates, because "could not condense" must mean "read all of it", not
	// "read part of it".
	MaxVerbatimReadRunes = 1000

	// CacheKeyHexLen is the number of hex characters used for the cache filename.
	CacheKeyHexLen = 16

	// reSummarizeThreshold — if the first summarization result exceeds this
	// many bytes, a second pass is requested to further condense the text.
	reSummarizeThreshold = 4000

	// maxSummarizePasses is the maximum number of summarization attempts
	// (first pass + optional re-summarization).
	maxSummarizePasses = 2
)

// summarizeBudgetCeiling is the output budget a summarization pass may use when
// the model's own limit is unknown or larger.
//
// The budget must cover the model's REASONING as well as the answer: a
// reasoning model spends the same budget thinking first, and when it runs out
// the API returns an empty `content` with finish_reason=length. The old
// hardcoded 1024 was measured to do exactly that on a 2600-rune message (1024 →
// empty, 2048 → empty, 4096 → content), so every long TTS summary silently fell
// back to the non-AI cleaner.
//
// 16384 covers the worst case this pipeline can be handed: inputs are capped at
// MaxSummarizeRunes (10000 runes), reasoning costs roughly 1.2 tokens per input
// rune (measured), and the answer itself is a few hundred tokens.
const summarizeBudgetCeiling = model.MaxOutputTokenBudget

// summarizePassBudget returns the max_tokens to request for one summarization
// pass of modelID (auto: the model's own limit, capped at the ceiling).
func summarizePassBudget(modelID string) int {
	return OutputBudgetForModel(modelID, 0)
}

// OutputBudgetForModel returns the max_tokens to request for one model call on
// modelID.
//
// requested <= 0 means "auto": as much as the model allows, capped at the
// ceiling above. A positive requested is a caller's own cap (a user setting) and
// is still clamped to the model's published limit — asking for more than a model
// supports is a hard API error, not a longer answer.
//
// The limit is read from the model catalog rather than hardcoded: models differ
// by orders of magnitude (the catalog publishes 8192 for some and 384000 for
// others), and an unknown model (a local proxy, a model the catalog has never
// seen) gets the ceiling, which is only an upper bound — a model that does not
// need it simply stops earlier.
func OutputBudgetForModel(modelID string, requested int) int {
	if requested <= 0 {
		return model.OutputTokenBudget(modelID)
	}
	if limit := model.CatalogOutputLimitForModel(modelID); limit > 0 && limit < requested {
		return limit
	}
	return requested
}

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
