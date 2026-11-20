package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"clawbench/internal/endpointurl"
)

// openAIClient speaks the OpenAI chat-completions wire format, which nearly
// every gateway also implements.
type openAIClient struct {
	cfg  Config
	url  string
	http *http.Client
}

func newOpenAIClient(cfg Config) *openAIClient {
	return &openAIClient{
		cfg: cfg,
		// The chat-completions path is appended unless the configured URL
		// already names an endpoint. Reusing the summarizer's resolver keeps
		// the two from disagreeing about which URL a base resolves to, which
		// is the bug that once made "test connection" pass while the real call
		// 404'd.
		url:  resolveEndpoint(cfg.BaseURL, "/v1/chat/completions"),
		http: &http.Client{Timeout: 0}, // streaming: no overall deadline, ctx governs
	}
}

func (c *openAIClient) Model() string        { return c.cfg.Model }
func (c *openAIClient) SupportsVision() bool { return c.cfg.SupportsVision }

// OpenAI wire types. Only the fields ClawBench sets are declared; unknown
// fields in responses are ignored by encoding/json.

type oaRequest struct {
	Model      string        `json:"model"`
	Messages   []oaMessage   `json:"messages"`
	Tools      []oaTool      `json:"tools,omitempty"`
	Stream     bool          `json:"stream"`
	StreamOpts *oaStreamOpts `json:"stream_options,omitempty"`
	// Temperature is omitted when zero. Reasoning models (o-series, GPT-5)
	// reject any explicit temperature, so the reasoning path clears it instead
	// of sending a value the model refuses.
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
	// ReasoningEffort is the standard OpenAI field for o-series / GPT-5 class
	// models. Omitted entirely for "auto" so a plain chat model never receives
	// a parameter it does not know.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// PromptCacheKey groups requests of one conversation onto the same prompt
	// cache. It is only sent to OpenAI's own endpoint: other compatible
	// gateways either cache by prefix automatically (DeepSeek) or reject an
	// unknown field, and a 400 there would break every turn.
	PromptCacheKey string `json:"prompt_cache_key,omitempty"`
}

type oaStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type oaMessage struct {
	Role string `json:"role"`
	// Content is a string OR an array of parts. The field is `any` because
	// encoding/json would otherwise need two struct shapes for one key.
	Content any `json:"content"`
	// ToolCalls rides an assistant message that requested tools.
	ToolCalls []oaToolCall `json:"tool_calls,omitempty"`
	// ToolCallID identifies the call a role="tool" message answers.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type oaContentPart struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL *oaImageURL `json:"image_url,omitempty"`
}

type oaImageURL struct {
	URL string `json:"url"`
}

type oaToolCall struct {
	Index    *int       `json:"index,omitempty"`
	ID       string     `json:"id,omitempty"`
	Type     string     `json:"type,omitempty"`
	Function oaFunction `json:"function"`
}

type oaFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type oaTool struct {
	Type     string     `json:"type"`
	Function oaToolFunc `json:"function"`
}

// Content-part type names on the wire. `text` is the only one a request body
// ever repeats (image_url is appended once per image).
const (
	oaPartText     = "text"
	oaPartImageURL = "image_url"
)

// oaCallTypeFunction is the only tool type this API accepts.
const oaCallTypeFunction = "function"

// oaToolFunc is the request-side shape of a declared tool.
//
// It is an explicit struct rather than a cast of ToolSpec because ToolSpec
// carries no JSON tags (it is an internal type): casting emitted
// `{"Name":…,"Description":…,"Parameters":…}`. Strict gateways (DeepSeek among
// them) reject that with `tools[0].function: missing field name`, so the keys
// must be spelled out here.
type oaToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// oaDelta is one choice's incremental content.
type oaDelta struct {
	Content string `json:"content"`
	// ReasoningContent is the vendor field DeepSeek/GLM/Qwen gateways use for
	// the thinking stream; OpenRouter calls the same thing "reasoning". Both
	// are read so a reasoning level produces a visible thinking block instead
	// of silently doing nothing.
	ReasoningContent string       `json:"reasoning_content"`
	Reasoning        string       `json:"reasoning"`
	ToolCalls        []oaToolCall `json:"tool_calls"`
}

type oaStreamChunk struct {
	Choices []struct {
		Delta        oaDelta `json:"delta"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage *oaUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// oaUsage is one streamed chunk's token accounting.
type oaUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// OpenAI reports the cached part under prompt_tokens_details.cached_tokens.
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	// DeepSeek and several gateways report it at the top level instead.
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
	CachedTokens         int `json:"cached_tokens"`
}

// openAIPromptCacheKey returns the cache key to send, or "" when the endpoint
// must not receive the field.
//
// Only OpenAI's own endpoint is known to accept prompt_cache_key. Other
// compatible gateways either cache by prefix automatically (DeepSeek) or reject
// unknown fields, and a 400 there would break every turn of a working agent.
func openAIPromptCacheKey(baseURL, sessionID string) string {
	if sessionID == "" || !strings.Contains(baseURL, "api.openai.com") {
		return ""
	}
	return sessionID
}

// cachedPromptTokens reads the provider's cache-hit count from whichever field
// it uses. Zero is "no cache reported", which is also the honest value for a
// provider that does not cache.
func cachedPromptTokens(u *oaUsage) int {
	if u == nil {
		return 0
	}
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		return u.PromptTokensDetails.CachedTokens
	}
	if u.PromptCacheHitTokens > 0 {
		return u.PromptCacheHitTokens
	}
	return u.CachedTokens
}

func (c *openAIClient) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	body, err := c.buildBody(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.cfg.APIKey != "" {
		if authHeaderIsAPIKey(c.cfg.AuthMode) {
			httpReq.Header.Set("x-api-key", c.cfg.APIKey)
		} else {
			httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, &HTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(detail))}
	}

	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		defer func() { _ = resp.Body.Close() }()
		c.readStream(ctx, resp.Body, ch)
	}()
	return ch, nil
}

// HTTPError is a non-2xx response. Status is kept separate from the message so
// the caller can decide whether it is retryable (429/5xx) or terminal (401).
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("endpoint returned status %d", e.Status)
	}
	return fmt.Sprintf("endpoint returned status %d: %s", e.Status, e.Body)
}

// Retryable reports whether retrying the same request could plausibly work.
func (e *HTTPError) Retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

func (c *openAIClient) buildBody(req Request) ([]byte, error) {
	msgs, err := c.buildMessages(req)
	if err != nil {
		return nil, err
	}
	effort := normalizeEffort(req.ReasoningEffort)
	temperature := resolveTemperature(req)
	if effort != "" {
		// Reasoning models only accept their own default temperature; sending
		// ours is a 400. Clearing it here (omitempty) is what lets the level
		// actually take effect instead of failing on a parameter the user never
		// chose.
		temperature = 0
	}
	body := oaRequest{
		Model:    c.cfg.Model,
		Messages: msgs,
		Stream:   true,
		// Usage on a streamed response is omitted unless asked for. Without
		// this the turn has no token accounting at all.
		StreamOpts:      &oaStreamOpts{IncludeUsage: true},
		Temperature:     temperature,
		MaxTokens:       req.MaxOutputTokens,
		ReasoningEffort: effort,
	}
	if key := openAIPromptCacheKey(c.cfg.BaseURL, req.SessionID); key != "" {
		body.PromptCacheKey = key
	}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, oaTool{
			Type:     oaCallTypeFunction,
			Function: oaToolFunc(t),
		})
	}
	return json.Marshal(body)
}

// buildMessages flattens the part-based conversation into OpenAI's shape.
//
// Two conversions matter:
//
//   - A tool result with images cannot be expressed: role="tool" content is a
//     STRING in the spec, and the array form is a non-standard extension that
//     most gateways reject. So the text goes in the tool message and the images
//     are appended to one user message after the round's last tool message.
//     That is a workaround, but it is the only shape every OpenAI-compatible
//     endpoint accepts.
//   - Tool calls ride the assistant message, so consecutive assistant parts
//     must be merged into ONE message. Emitting one message per part would
//     break the call/result pairing providers validate.
func (c *openAIClient) buildMessages(req Request) ([]oaMessage, error) {
	var out []oaMessage
	if req.System != "" {
		out = append(out, oaMessage{Role: "system", Content: req.System})
	}

	for _, m := range req.Messages {
		switch m.Role {
		case RoleAssistant:
			msg := oaMessage{Role: "assistant"}
			var text strings.Builder
			for _, p := range m.Parts {
				switch {
				case p.ToolCall != nil:
					msg.ToolCalls = append(msg.ToolCalls, oaToolCall{
						ID:   p.ToolCall.ID,
						Type: oaCallTypeFunction,
						Function: oaFunction{
							Name:      p.ToolCall.Name,
							Arguments: p.ToolCall.Arguments,
						},
					})
				case p.Image != nil:
					// An assistant turn never produces images in this loop;
					// silently dropping one would hide a bug in the caller.
					return nil, errors.New("openai: assistant messages cannot carry images")
				default:
					text.WriteString(p.Text)
				}
			}
			// Content must be null (not "") when the message is tool-calls-only,
			// otherwise some gateways reject the request.
			if text.Len() > 0 {
				msg.Content = text.String()
			}
			out = append(out, msg)

		case RoleTool:
			// Every tool message of this round must come before any other
			// role: providers pair them with the assistant turn's calls by
			// position, and a message wedged between two results reads as
			// "tool calls and tool results do not match". So the images are
			// collected first and sent once, after the last result.
			var images []Image
			for _, p := range m.Parts {
				if p.ToolResult == nil {
					continue
				}
				text, resultImages := splitToolResultParts(p.ToolResult.Parts)
				out = append(out, oaMessage{
					Role:       "tool",
					ToolCallID: p.ToolResult.CallID,
					Content:    text,
				})
				images = append(images, resultImages...)
			}
			// Images ride a trailing user message (see the doc comment).
			if len(images) > 0 {
				out = append(out, oaMessage{
					Role:    "user",
					Content: imageParts(images, "The images from the previous tool output:"),
				})
			}

		default: // system / user
			parts, err := c.buildUserParts(m.Parts)
			if err != nil {
				return nil, err
			}
			out = append(out, oaMessage{Role: string(m.Role), Content: parts})
		}
	}
	return out, nil
}

// buildUserParts renders user/system content: a plain string when there are no
// images (the form every endpoint accepts) and a part array when there are.
func (c *openAIClient) buildUserParts(parts []Part) (any, error) {
	var text strings.Builder
	var images []Image
	for _, p := range parts {
		switch {
		case p.Image != nil:
			images = append(images, *p.Image)
		case p.ToolResult != nil:
			t, imgs := splitToolResultParts(p.ToolResult.Parts)
			text.WriteString(t)
			images = append(images, imgs...)
		default:
			text.WriteString(p.Text)
		}
	}
	if len(images) == 0 {
		return text.String(), nil
	}
	if !c.SupportsVision() {
		// Refuse rather than send bytes the model will reject with an opaque
		// 400. The caller turns this into a tool error the model can read.
		return nil, ErrVisionUnsupported
	}
	out := []oaContentPart{{Type: oaPartText, Text: text.String()}}
	for _, img := range images {
		out = append(out, oaContentPart{
			Type:     oaPartImageURL,
			ImageURL: &oaImageURL{URL: dataURL(img)},
		})
	}
	return out, nil
}

// imageParts builds a part array for a set of images plus a lead-in line.
func imageParts(images []Image, lead string) []oaContentPart {
	out := make([]oaContentPart, 0, len(images)+1)
	if lead != "" {
		out = append(out, oaContentPart{Type: oaPartText, Text: lead})
	}
	for _, img := range images {
		out = append(out, oaContentPart{
			Type:     oaPartImageURL,
			ImageURL: &oaImageURL{URL: dataURL(img)},
		})
	}
	return out
}

// splitToolResultParts separates a tool result into its text and its images.
func splitToolResultParts(parts []Part) (string, []Image) {
	var text strings.Builder
	var images []Image
	for _, p := range parts {
		if p.Image != nil {
			images = append(images, *p.Image)
			continue
		}
		text.WriteString(p.Text)
	}
	return text.String(), images
}

func dataURL(img Image) string {
	return "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
}

// openAIStreamState is one stream's running totals.
type openAIStreamState struct {
	// seenCall records the provider slots already announced, so a start event
	// opens each UI card exactly once. Tool calls stream as fragments keyed by
	// index: the name arrives once and the arguments arrive in pieces.
	seenCall map[int]bool
	usage    *Usage
	// sawContent records that the model produced text; a stream that produced
	// nothing at all is a distinct failure (ErrNoOutput).
	sawContent bool
	// truncated records finish_reason=length: the model was cut off at the
	// output cap, so the reply is a prefix. Some gateways send the reason on a
	// chunk with no other content, which is why it is tracked outside the
	// per-choice body below.
	truncated bool
}

// readStream parses the SSE body, emitting events until the stream ends.
func (c *openAIClient) readStream(ctx context.Context, body io.Reader, ch chan<- Event) {
	st := &openAIStreamState{seenCall: make(map[int]bool)}
	scanner := newSSEScanner(body)
	for {
		payload, ok := nextSSEPayload(scanner)
		if !ok {
			break
		}
		if payload == "[DONE]" {
			break
		}
		chunk, parsed := decodeOAStreamChunk(payload)
		if !parsed {
			// A single unparseable frame is not worth failing the turn over:
			// gateways inject keepalives and vendor-specific frames.
			continue
		}
		if chunk.Error != nil {
			emitTerminal(ctx, ch, fmt.Errorf("openai: %s", chunk.Error.Message))
			return
		}
		if chunk.Usage != nil {
			st.usage = &Usage{
				InputTokens:     chunk.Usage.PromptTokens,
				OutputTokens:    chunk.Usage.CompletionTokens,
				CacheReadTokens: cachedPromptTokens(chunk.Usage),
			}
		}
		// A false result means the consumer is gone; the caller stops.
		if !st.consumeDelta(ctx, ch, chunk) {
			return
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
		emitTerminal(ctx, ch, fmt.Errorf("openai: read stream: %w", err))
		return
	}
	if st.usage != nil {
		emit(ctx, ch, Event{Kind: KindUsage, Usage: st.usage})
	}
	if st.truncated {
		// Checked before ErrNoOutput: a reply cut off during reasoning has no
		// content at all, and "the answer is incomplete" is the useful fact —
		// "the endpoint produced nothing" would send the caller down a retry
		// path that hits the same cap again.
		emitTruncated(ctx, ch)
		return
	}
	if !st.sawContent && st.usage == nil && len(st.seenCall) == 0 {
		// The endpoint accepted the request and then produced nothing. That is
		// a distinct failure from "the model answered with nothing" and the
		// caller surfaces it differently.
		emitTerminal(ctx, ch, ErrNoOutput)
		return
	}
	emitTerminal(ctx, ch, nil)
}

// consumeDelta applies one chunk's choices, emitting what the UI needs. It
// reports false when an event could not be delivered.
func (st *openAIStreamState) consumeDelta(ctx context.Context, ch chan<- Event, chunk oaStreamChunk) bool {
	for _, choice := range chunk.Choices {
		if choice.FinishReason == "length" {
			st.truncated = true
		}
		if !emitEach(ctx, ch, reasoningEvents(choice.Delta)) {
			return false
		}
		if choice.Delta.Content != "" {
			st.sawContent = true
			if !emit(ctx, ch, Event{Kind: KindText, Text: choice.Delta.Content}) {
				return false
			}
		}
		if !st.emitToolCalls(ctx, ch, choice.Delta.ToolCalls) {
			return false
		}
	}
	return true
}

// reasoningEvents renders a chunk's reasoning text.
//
// Two field names exist in the wild for the same thing
// (reasoning_content from DeepSeek-style endpoints, reasoning from the
// OpenRouter family); they are mutually exclusive per chunk.
func reasoningEvents(delta oaDelta) []Event {
	for _, reasoning := range []string{delta.ReasoningContent, delta.Reasoning} {
		if reasoning != "" {
			return []Event{{Kind: KindReasoning, Reasoning: reasoning}}
		}
	}
	return nil
}

// emitToolCalls announces each call on first sight and forwards its argument
// fragments.
func (st *openAIStreamState) emitToolCalls(ctx context.Context, ch chan<- Event, calls []oaToolCall) bool {
	for _, tc := range calls {
		idx := 0
		if tc.Index != nil {
			idx = *tc.Index
		}
		// The provider's slot number rides every event: parallel calls stream
		// interleaved, so the consumer needs to know which call a fragment
		// belongs to rather than assuming "the last one".
		index := idx
		if !st.seenCall[idx] {
			st.seenCall[idx] = true
			if !emit(ctx, ch, Event{Kind: KindToolCallStart, ToolCall: &ToolCall{ID: tc.ID, Name: tc.Function.Name, Index: &index}}) {
				return false
			}
		}
		if tc.Function.Arguments != "" {
			if !emit(ctx, ch, Event{Kind: KindToolCallDelta, ToolCall: &ToolCall{Arguments: tc.Function.Arguments, Index: &index}}) {
				return false
			}
		}
	}
	return true
}

// ErrNoOutput means the endpoint returned a well-formed empty stream.
var ErrNoOutput = errors.New("model produced no output")

// ErrVisionUnsupported means an image was sent to a model that cannot read one.
var ErrVisionUnsupported = errors.New("current model does not accept image input")

// resolveTemperature picks the request temperature, honoring an explicit zero
// only when the caller set Deterministic.
func resolveTemperature(req Request) float64 {
	if req.Temperature == 0 {
		return DefaultTemperature
	}
	return req.Temperature
}

// resolveEndpoint completes a configured base URL with a default path.
//
// It delegates to endpointurl.Resolve so the agent's model client agrees with
// the connectivity test and the summarizers about which URL a base resolves to.
// The local copy used to disagree: it treated a namespace base such as
// MiniMax's "/anthropic" as a complete endpoint and posted to it verbatim,
// which 404'd every turn even though "test connection" passed.
func resolveEndpoint(baseURL, defaultPath string) string {
	return endpointurl.Resolve(baseURL, defaultPath)
}

// authHeaderIsAPIKey reports whether the configured auth mode wants x-api-key.
func authHeaderIsAPIKey(mode string) bool {
	return strings.EqualFold(strings.TrimSpace(mode), AuthAPIKey)
}
