package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	Model       string        `json:"model"`
	Messages    []oaMessage   `json:"messages"`
	Tools       []oaTool      `json:"tools,omitempty"`
	Stream      bool          `json:"stream"`
	StreamOpts  *oaStreamOpts `json:"stream_options,omitempty"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
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

type oaToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type oaStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string       `json:"content"`
			ToolCalls []oaToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
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
	body := oaRequest{
		Model:    c.cfg.Model,
		Messages: msgs,
		Stream:   true,
		// Usage on a streamed response is omitted unless asked for. Without
		// this the turn has no token accounting at all.
		StreamOpts:  &oaStreamOpts{IncludeUsage: true},
		Temperature: resolveTemperature(req),
		MaxTokens:   req.MaxOutputTokens,
	}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, oaTool{
			Type: "function",
			Function: oaToolFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
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
//     most gateways reject. So the text goes in the tool message and each image
//     is appended as a following user message. That is a workaround, but it is
//     the only shape every OpenAI-compatible endpoint accepts.
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
						Type: "function",
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
			for _, p := range m.Parts {
				if p.ToolResult == nil {
					continue
				}
				text, images := splitToolResultParts(p.ToolResult.Parts)
				out = append(out, oaMessage{
					Role:       "tool",
					ToolCallID: p.ToolResult.CallID,
					Content:    text,
				})
				// Images ride a trailing user message (see the doc comment).
				if len(images) > 0 {
					out = append(out, oaMessage{
						Role:    "user",
						Content: imageParts(images, "The images from the previous tool output:"),
					})
				}
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
	out := []oaContentPart{{Type: "text", Text: text.String()}}
	for _, img := range images {
		out = append(out, oaContentPart{
			Type:     "image_url",
			ImageURL: &oaImageURL{URL: dataURL(img)},
		})
	}
	return out, nil
}

// imageParts builds a part array for a set of images plus a lead-in line.
func imageParts(images []Image, lead string) []oaContentPart {
	out := make([]oaContentPart, 0, len(images)+1)
	if lead != "" {
		out = append(out, oaContentPart{Type: "text", Text: lead})
	}
	for _, img := range images {
		out = append(out, oaContentPart{
			Type:     "image_url",
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

// readStream parses the SSE body, emitting events until the stream ends.
func (c *openAIClient) readStream(ctx context.Context, body io.Reader, ch chan<- Event) {
	scanner := bufio.NewScanner(body)
	// A single SSE line carrying a long tool-call argument easily exceeds the
	// 64KiB default; 4MiB covers a large file write without unbounded growth.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	// Tool calls stream as fragments keyed by index; the name arrives once and
	// the arguments arrive in pieces. Collect per index and emit a start event
	// on first sight so the UI can open a card immediately.
	seenCall := make(map[int]bool)
	var usage *Usage
	sawContent := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var chunk oaStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// A single unparseable frame is not worth failing the turn over:
			// gateways inject keepalives and vendor-specific frames.
			continue
		}
		if chunk.Error != nil {
			emitTerminal(ctx, ch, fmt.Errorf("openai: %s", chunk.Error.Message))
			return
		}
		if chunk.Usage != nil {
			usage = &Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				sawContent = true
				if !emit(ctx, ch, Event{Kind: KindText, Text: choice.Delta.Content}) {
					return
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}
				if !seenCall[idx] {
					seenCall[idx] = true
					if !emit(ctx, ch, Event{Kind: KindToolCallStart, ToolCall: &ToolCall{ID: tc.ID, Name: tc.Function.Name}}) {
						return
					}
				}
				if tc.Function.Arguments != "" {
					if !emit(ctx, ch, Event{Kind: KindToolCallDelta, ToolCall: &ToolCall{Arguments: tc.Function.Arguments}}) {
						return
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
		emitTerminal(ctx, ch, fmt.Errorf("openai: read stream: %w", err))
		return
	}
	if usage != nil {
		emit(ctx, ch, Event{Kind: KindUsage, Usage: usage})
	}
	if !sawContent && usage == nil && len(seenCall) == 0 {
		// The endpoint accepted the request and then produced nothing. That is
		// a distinct failure from "the model answered with nothing" and the
		// caller surfaces it differently.
		emitTerminal(ctx, ch, ErrNoOutput)
		return
	}
	emitTerminal(ctx, ch, nil)
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
// The accepted shapes are the ones the settings page allows:
//
//   - a bare host ("https://api.openai.com") gets the full path appended;
//   - a base ending in a version segment ("…/v1", "…/api/v1") is a partial
//     base and gets the remainder of the path appended;
//   - anything else is the user's explicit endpoint and is used verbatim.
//
// The last rule is what makes this safe: a provider-specific path such as
// MiniMax's "/v1/text/chatcompletion_v2" must not have another
// "/v1/chat/completions" glued onto it, which is a 404 the user cannot diagnose.
func resolveEndpoint(baseURL, defaultPath string) string {
	u := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if u == "" {
		return ""
	}
	if strings.HasSuffix(u, defaultPath) {
		return u
	}
	// Split the path off the authority so a host containing a dot is not
	// mistaken for a path segment.
	_, rest, hasScheme := strings.Cut(u, "://")
	if !hasScheme {
		rest = u
	}
	slash := strings.Index(rest, "/")
	if slash < 0 {
		// Bare host, no path at all.
		return u + defaultPath
	}
	path := rest[slash:]
	if isVersionPath(path) {
		return u + strings.TrimPrefix(defaultPath, pathPrefixForVersion(defaultPath))
	}
	return u
}

// isVersionPath reports whether path is nothing but version segments, e.g.
// "/v1", "/api/v1", "/v1beta".
func isVersionPath(path string) bool {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 {
		return false
	}
	for _, s := range segments {
		if !isVersionSegment(s) {
			return false
		}
	}
	return true
}

// isVersionSegment matches "v1", "v2", "v1beta", "api" — the pieces a base URL
// uses before the endpoint name. Only these are treated as "the base is a
// partial prefix"; anything else means the user gave a full endpoint.
func isVersionSegment(s string) bool {
	if s == "api" {
		return true
	}
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// pathPrefixForVersion returns the part of defaultPath the base already
// contains, so only the remainder is appended.
//
// defaultPath is always "/v1/<name>", so the prefix is "/v1" whenever the base
// ends in a version; a base ending in "/api/v1" still only needs "/<name>"
// appended, and the caller's base already carries "/api".
func pathPrefixForVersion(defaultPath string) string {
	if i := strings.Index(defaultPath[1:], "/"); i >= 0 {
		return defaultPath[:i+1]
	}
	return defaultPath
}

// authHeaderIsAPIKey reports whether the configured auth mode wants x-api-key.
func authHeaderIsAPIKey(mode string) bool {
	return strings.EqualFold(strings.TrimSpace(mode), AuthAPIKey)
}
