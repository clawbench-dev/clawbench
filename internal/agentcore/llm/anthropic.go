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

// anthropicClient speaks the Anthropic Messages wire format.
//
// The two protocols disagree on three things that matter to a tool loop:
//
//  1. Tool results are USER messages carrying tool_result blocks, not a
//     dedicated role. This is why the Anthropic path can return an image
//     inside a tool result while OpenAI needs the fake-user-message workaround.
//  2. The system prompt is a top-level field, not a message.
//  3. Tool calls are content blocks (tool_use) of the assistant message, and
//     their arguments are a JSON OBJECT, not a string.
type anthropicClient struct {
	cfg  Config
	url  string
	http *http.Client
}

func newAnthropicClient(cfg Config) *anthropicClient {
	return &anthropicClient{
		cfg:  cfg,
		url:  resolveEndpoint(cfg.BaseURL, "/v1/messages"),
		http: &http.Client{Timeout: 0},
	}
}

func (c *anthropicClient) Model() string        { return c.cfg.Model }
func (c *anthropicClient) SupportsVision() bool { return c.cfg.SupportsVision }

type anRequest struct {
	Model       string      `json:"model"`
	System      string      `json:"system,omitempty"`
	Messages    []anMessage `json:"messages"`
	Tools       []anTool    `json:"tools,omitempty"`
	MaxTokens   int         `json:"max_tokens"`
	Stream      bool        `json:"stream"`
	Temperature float64     `json:"temperature,omitempty"`
	// Thinking turns on extended reasoning. Omitted for "auto".
	Thinking *anThinking `json:"thinking,omitempty"`
}

// anThinking is the Anthropic extended-thinking switch.
//
// Two constraints come with it, both enforced in buildBody: max_tokens must be
// strictly greater than budget_tokens, and temperature must be 1.
type anThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

// thinkingBudgets maps a user-facing level to a token budget. High stays below
// the largest models' ceilings on purpose: an older model then rejects the
// request with a message the user can act on (pick a lower level), instead of
// a number no model accepts.
var thinkingBudgets = map[string]int{
	EffortLow:    1024,
	EffortMedium: 4096,
	EffortHigh:   8192,
}

type anMessage struct {
	Role    string    `json:"role"`
	Content []anBlock `json:"content"`
}

// anBlock is one content block. Only the fields for its Type are set.
type anBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// image
	Source *anImageSource `json:"source,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string    `json:"tool_use_id,omitempty"`
	Content   []anBlock `json:"content,omitempty"`
	IsError   bool      `json:"is_error,omitempty"`
	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
}

type anImageSource struct {
	Type      string `json:"type"` // always "base64"
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anStreamEvent struct {
	Type string `json:"type"`
	// message_start
	Message *struct {
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	// content_block_start
	Index        int      `json:"index"`
	ContentBlock *anBlock `json:"content_block"`
	// content_block_delta / message_delta. StopReason only appears on the
	// latter ("end_turn", "max_tokens", ...); the same struct is reused because
	// both events carry a "delta" object.
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	// message_delta
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *anthropicClient) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	body, err := c.buildBody(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if c.cfg.APIKey != "" {
		if authHeaderIsAPIKey(c.cfg.AuthMode) || c.cfg.AuthMode == "" || c.cfg.AuthMode == AuthAuto {
			httpReq.Header.Set("x-api-key", c.cfg.APIKey)
		} else {
			httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: request: %w", err)
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

func (c *anthropicClient) buildBody(req Request) ([]byte, error) {
	msgs, err := c.buildMessages(req)
	if err != nil {
		return nil, err
	}
	maxTokens := resolveMaxTokens(req)
	temperature := resolveTemperature(req)
	var thinking *anThinking
	if budget, ok := thinkingBudgets[normalizeEffort(req.ReasoningEffort)]; ok {
		// The API rejects max_tokens <= budget_tokens, so grow the cap when the
		// caller's default is too small to hold the thinking plus an answer.
		if maxTokens <= budget {
			maxTokens = budget + 4096
		}
		// Extended thinking requires temperature 1; any other value is a 400.
		temperature = 1
		thinking = &anThinking{Type: "enabled", BudgetTokens: budget}
	}
	body := anRequest{
		Model:    c.cfg.Model,
		System:   req.System,
		Messages: msgs,
		Stream:   true,
		// max_tokens is REQUIRED by this API; there is no server default.
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Thinking:    thinking,
	}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, anTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}
	return json.Marshal(body)
}

// resolveMaxTokens applies a default because the Anthropic API rejects a
// request without one.
func resolveMaxTokens(req Request) int {
	if req.MaxOutputTokens > 0 {
		return req.MaxOutputTokens
	}
	return 8192
}

// buildMessages converts the part-based conversation into Anthropic's shape.
//
// The structural difference from OpenAI: tool results are content blocks of a
// USER message. Consecutive tool results therefore merge into ONE user message
// — sending them as separate messages violates the alternating-role rule the
// API enforces.
func (c *anthropicClient) buildMessages(req Request) ([]anMessage, error) {
	var out []anMessage

	appendBlocks := func(role string, blocks []anBlock) {
		if len(blocks) == 0 {
			return
		}
		// Merge into the previous message when the role matches: the API
		// requires alternating roles, and a tool result followed by another
		// tool result is the common case.
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, blocks...)
			return
		}
		out = append(out, anMessage{Role: role, Content: blocks})
	}

	// appendToolResults is the tool-result variant of appendBlocks. This API
	// requires tool_result blocks to come FIRST in the user turn that answers
	// them; appending after a text block is a 400 the user cannot read.
	appendToolResults := func(blocks []anBlock) {
		if len(blocks) == 0 {
			return
		}
		if n := len(out); n > 0 && out[n-1].Role == "user" {
			out[n-1].Content = append(append([]anBlock{}, blocks...), out[n-1].Content...)
			return
		}
		out = append(out, anMessage{Role: "user", Content: blocks})
	}

	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem:
			// A system message mid-conversation cannot be expressed; fold it
			// into the user turn rather than dropping it silently.
			var text strings.Builder
			for _, p := range m.Parts {
				text.WriteString(p.Text)
			}
			if text.Len() > 0 {
				appendBlocks("user", []anBlock{{Type: "text", Text: text.String()}})
			}

		case RoleAssistant:
			var blocks []anBlock
			for _, p := range m.Parts {
				switch {
				case p.Thinking != nil:
					// Replayed verbatim, signature included: this API rejects a
					// tool-use turn whose thinking block was dropped.
					blocks = append(blocks, anBlock{
						Type:      "thinking",
						Thinking:  p.Thinking.Text,
						Signature: p.Thinking.Signature,
					})
				case p.ToolCall != nil:
					input := json.RawMessage(p.ToolCall.Arguments)
					if len(input) == 0 || !json.Valid(input) {
						// An empty or malformed argument object is sent as {}
						// so the provider does not reject the whole request.
						// The tool layer will report the missing arguments.
						input = json.RawMessage("{}")
					}
					blocks = append(blocks, anBlock{
						Type:  "tool_use",
						ID:    p.ToolCall.ID,
						Name:  p.ToolCall.Name,
						Input: input,
					})
				case p.Image != nil:
					return nil, errors.New("anthropic: assistant messages cannot carry images")
				default:
					if p.Text != "" {
						blocks = append(blocks, anBlock{Type: "text", Text: p.Text})
					}
				}
			}
			appendBlocks("assistant", blocks)

		case RoleTool:
			var blocks []anBlock
			for _, p := range m.Parts {
				if p.ToolResult == nil {
					continue
				}
				blocks = append(blocks, anBlock{
					Type:      "tool_result",
					ToolUseID: p.ToolResult.CallID,
					Content:   c.toolResultContent(p.ToolResult),
					IsError:   p.ToolResult.IsError,
				})
			}
			appendToolResults(blocks)

		default: // user
			blocks, err := c.userBlocks(m.Parts)
			if err != nil {
				return nil, err
			}
			appendBlocks("user", blocks)
		}
	}
	return out, nil
}

// toolResultContent renders a tool result. Images ride INSIDE the tool result
// here — the one place this protocol is strictly easier than OpenAI's.
func (c *anthropicClient) toolResultContent(tr *ToolResult) []anBlock {
	var blocks []anBlock
	for _, p := range tr.Parts {
		switch {
		case p.Image != nil:
			if !c.SupportsVision() {
				blocks = append(blocks, anBlock{Type: "text", Text: "[image omitted: the current model cannot read images]"})
				continue
			}
			blocks = append(blocks, anBlock{
				Type: "image",
				Source: &anImageSource{
					Type:      "base64",
					MediaType: p.Image.MediaType,
					Data:      base64.StdEncoding.EncodeToString(p.Image.Data),
				},
			})
		default:
			if p.Text != "" {
				blocks = append(blocks, anBlock{Type: "text", Text: p.Text})
			}
		}
	}
	if len(blocks) == 0 {
		// The API rejects an empty content array.
		blocks = append(blocks, anBlock{Type: "text", Text: "(no output)"})
	}
	return blocks
}

func (c *anthropicClient) userBlocks(parts []Part) ([]anBlock, error) {
	var blocks []anBlock
	for _, p := range parts {
		switch {
		case p.Image != nil:
			if !c.SupportsVision() {
				return nil, ErrVisionUnsupported
			}
			blocks = append(blocks, anBlock{
				Type: "image",
				Source: &anImageSource{
					Type:      "base64",
					MediaType: p.Image.MediaType,
					Data:      base64.StdEncoding.EncodeToString(p.Image.Data),
				},
			})
		case p.ToolResult != nil:
			blocks = append(blocks, anBlock{
				Type:      "tool_result",
				ToolUseID: p.ToolResult.CallID,
				Content:   c.toolResultContent(p.ToolResult),
				IsError:   p.ToolResult.IsError,
			})
		default:
			if p.Text != "" {
				blocks = append(blocks, anBlock{Type: "text", Text: p.Text})
			}
		}
	}
	if len(blocks) == 0 {
		blocks = append(blocks, anBlock{Type: "text", Text: "(empty)"})
	}
	return blocks, nil
}

func (c *anthropicClient) readStream(ctx context.Context, body io.Reader, ch chan<- Event) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		inputTokens int
		// Per-index accumulation of tool_use argument JSON. The API streams
		// partial_json fragments that must be concatenated.
		callID   = make(map[int]string)
		callName = make(map[int]string)
		sawAny   bool
		// stopReason arrives on message_delta, one event before message_stop.
		stopReason string
	)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

		var ev anStreamEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "message_start":
			if ev.Message != nil && ev.Message.Usage != nil {
				inputTokens = ev.Message.Usage.InputTokens
			}
			sawAny = true

		case "content_block_start":
			sawAny = true
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				callID[ev.Index] = ev.ContentBlock.ID
				callName[ev.Index] = ev.ContentBlock.Name
				if !emit(ctx, ch, Event{Kind: KindToolCallStart, ToolCall: &ToolCall{
					ID:   ev.ContentBlock.ID,
					Name: ev.ContentBlock.Name,
				}}) {
					return
				}
			}

		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					sawAny = true
					if !emit(ctx, ch, Event{Kind: KindText, Text: ev.Delta.Text}) {
						return
					}
				}
			case "thinking_delta":
				if ev.Delta.Thinking != "" {
					sawAny = true
					if !emit(ctx, ch, Event{Kind: KindReasoning, Reasoning: ev.Delta.Thinking}) {
						return
					}
				}
			case "signature_delta":
				// The signature closes the thinking block. It must travel with
				// the block when the turn also calls a tool, or the next round
				// is rejected.
				if ev.Delta.Signature != "" {
					if !emit(ctx, ch, Event{Kind: KindReasoningSignature, Signature: ev.Delta.Signature}) {
						return
					}
				}
			case "input_json_delta":
				if ev.Delta.PartialJSON != "" {
					if !emit(ctx, ch, Event{Kind: KindToolCallDelta, ToolCall: &ToolCall{Arguments: ev.Delta.PartialJSON}}) {
						return
					}
				}
			}

		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
			if ev.Usage != nil {
				emit(ctx, ch, Event{Kind: KindUsage, Usage: &Usage{
					InputTokens:  inputTokens,
					OutputTokens: ev.Usage.OutputTokens,
				}})
			}

		case "message_stop":
			if stopReason == "max_tokens" {
				// The reply was cut off at the output cap. Reported before the
				// empty-stream check for the same reason as the OpenAI client:
				// a reply cut off during thinking has no content at all.
				emitTruncated(ctx, ch)
				return
			}
			if !sawAny {
				emitTerminal(ctx, ch, ErrNoOutput)
				return
			}
			emitTerminal(ctx, ch, nil)
			return

		case "error":
			msg := "stream error"
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			}
			emitTerminal(ctx, ch, fmt.Errorf("anthropic: %s", msg))
			return
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
		emitTerminal(ctx, ch, fmt.Errorf("anthropic: read stream: %w", err))
		return
	}
	// The body ended without message_stop. Treat it as success if anything
	// arrived — some gateways close the connection instead of sending the
	// terminator — and as an empty reply otherwise.
	if !sawAny {
		emitTerminal(ctx, ch, ErrNoOutput)
		return
	}
	emitTerminal(ctx, ch, nil)
}
