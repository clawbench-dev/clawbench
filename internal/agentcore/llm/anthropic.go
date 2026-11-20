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
	Model string `json:"model"`
	// System is an array of text blocks (not a bare string) so the last block
	// can carry a cache breakpoint. A string cannot: this API only caches a
	// prefix at an explicit marker.
	System      []anBlock   `json:"system,omitempty"`
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

// Content-block types and the user role, as they appear on the wire. They are
// written by the request builder and read back by the stream parser, so a typo
// in either direction is a 400 or an event the caller never sees.
const (
	anBlockText       = "text"
	anBlockImage      = "image"
	anBlockToolUse    = "tool_use"
	anBlockToolResult = "tool_result"
	anBlockThinking   = "thinking"

	// anDeltaText is the streaming variant of anBlockText.
	anDeltaText = "text_delta"

	anRoleUser      = "user"
	anRoleAssistant = "assistant"
)

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
	// CacheControl marks the end of a cacheable prefix. Only the request
	// builder sets it; responses never carry it.
	CacheControl *anCacheControl `json:"cache_control,omitempty"`
}

// anCacheControl is the "cache everything up to here" marker. Ephemeral is the
// only type this API defines (the entry lives ~5 minutes).
type anCacheControl struct {
	Type string `json:"type"`
}

type anImageSource struct {
	Type      string `json:"type"` // always "base64"
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  map[string]any  `json:"input_schema"`
	CacheControl *anCacheControl `json:"cache_control,omitempty"`
}

type anStreamEvent struct {
	Type string `json:"type"`
	// message_start
	Message *struct {
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
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
		Messages: msgs,
		Stream:   true,
		// max_tokens is REQUIRED by this API; there is no server default.
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Thinking:    thinking,
	}
	// Prompt caching is explicit on this protocol: without a breakpoint the
	// whole prompt is billed as fresh input on every turn. Three breakpoints
	// cover the stable prefix (system + tools) and the conversation so far; the
	// API allows at most four.
	if req.System != "" {
		body.System = []anBlock{{
			Type:         anBlockText,
			Text:         req.System,
			CacheControl: ephemeralCache,
		}}
	}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, anTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}
	if n := len(body.Tools); n > 0 {
		body.Tools[n-1].CacheControl = ephemeralCache
	}
	if n := len(body.Messages); n > 0 {
		if m := &body.Messages[n-1]; len(m.Content) > 0 {
			m.Content[len(m.Content)-1].CacheControl = ephemeralCache
		}
	}
	return json.Marshal(body)
}

// ephemeralCache is the one cache type this API defines.
var ephemeralCache = &anCacheControl{Type: "ephemeral"}

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
	b := &anMessageBuilder{}
	for _, m := range req.Messages {
		if err := b.add(c, m); err != nil {
			return nil, err
		}
	}
	return b.out, nil
}

// anMessageBuilder accumulates messages while enforcing the alternating-role
// rule this API validates.
type anMessageBuilder struct {
	out []anMessage
}

// appendBlocks adds blocks under role, merging into the previous message when
// the role matches: the API requires alternating roles, and two assistant turns
// in a row (text, then a tool_use turn) are the common case.
func (b *anMessageBuilder) appendBlocks(role string, blocks []anBlock) {
	if len(blocks) == 0 {
		return
	}
	if n := len(b.out); n > 0 && b.out[n-1].Role == role {
		b.out[n-1].Content = append(b.out[n-1].Content, blocks...)
		return
	}
	b.out = append(b.out, anMessage{Role: role, Content: blocks})
}

// appendToolResults is the tool-result variant of appendBlocks. This API
// requires tool_result blocks to come FIRST in the user turn that answers them;
// appending after a text block is a 400 the user cannot read.
func (b *anMessageBuilder) appendToolResults(blocks []anBlock) {
	if len(blocks) == 0 {
		return
	}
	if n := len(b.out); n > 0 && b.out[n-1].Role == anRoleUser {
		b.out[n-1].Content = append(append([]anBlock{}, blocks...), b.out[n-1].Content...)
		return
	}
	b.out = append(b.out, anMessage{Role: anRoleUser, Content: blocks})
}

// add converts one part-based message into this protocol's shape.
func (b *anMessageBuilder) add(c *anthropicClient, m Message) error {
	switch m.Role {
	case RoleSystem:
		b.appendSystem(m.Parts)

	case RoleAssistant:
		blocks, err := assistantBlocks(m.Parts)
		if err != nil {
			return err
		}
		b.appendBlocks(anRoleAssistant, blocks)

	case RoleTool:
		b.appendToolResults(toolResultBlocks(c, m.Parts))

	default: // user
		blocks, err := c.userBlocks(m.Parts)
		if err != nil {
			return err
		}
		b.appendBlocks(anRoleUser, blocks)
	}
	return nil
}

// appendSystem folds a system message into a user turn: the protocol has no
// mid-conversation system role, and dropping it silently would lose the
// instruction.
func (b *anMessageBuilder) appendSystem(parts []Part) {
	var text strings.Builder
	for _, p := range parts {
		text.WriteString(p.Text)
	}
	if text.Len() > 0 {
		b.appendBlocks(anRoleUser, []anBlock{{Type: anBlockText, Text: text.String()}})
	}
}

// assistantBlocks renders an assistant turn: its thinking, its tool calls and
// its text, in the order they were produced.
func assistantBlocks(parts []Part) ([]anBlock, error) {
	var blocks []anBlock
	for _, p := range parts {
		switch {
		case p.Thinking != nil:
			// Replayed verbatim, signature included: this API rejects a
			// tool-use turn whose thinking block was dropped.
			blocks = append(blocks, anBlock{
				Type:      anBlockThinking,
				Thinking:  p.Thinking.Text,
				Signature: p.Thinking.Signature,
			})
		case p.ToolCall != nil:
			input := json.RawMessage(p.ToolCall.Arguments)
			if len(input) == 0 || !json.Valid(input) {
				// An empty or malformed argument object is sent as {} so the
				// provider does not reject the whole request. The tool layer
				// will report the missing arguments.
				input = json.RawMessage("{}")
			}
			blocks = append(blocks, anBlock{
				Type:  anBlockToolUse,
				ID:    p.ToolCall.ID,
				Name:  p.ToolCall.Name,
				Input: input,
			})
		case p.Image != nil:
			return nil, errors.New("anthropic: assistant messages cannot carry images")
		default:
			if p.Text != "" {
				blocks = append(blocks, anBlock{Type: anBlockText, Text: p.Text})
			}
		}
	}
	return blocks, nil
}

// toolResultBlocks renders the tool_result blocks of one tool message.
func toolResultBlocks(c *anthropicClient, parts []Part) []anBlock {
	var blocks []anBlock
	for _, p := range parts {
		if p.ToolResult == nil {
			continue
		}
		blocks = append(blocks, anBlock{
			Type:      anBlockToolResult,
			ToolUseID: p.ToolResult.CallID,
			Content:   c.toolResultContent(p.ToolResult),
			IsError:   p.ToolResult.IsError,
		})
	}
	return blocks
}

// toolResultContent renders a tool result. Images ride INSIDE the tool result
// here — the one place this protocol is strictly easier than OpenAI's.
func (c *anthropicClient) toolResultContent(tr *ToolResult) []anBlock {
	var blocks []anBlock
	for _, p := range tr.Parts {
		switch {
		case p.Image != nil:
			if !c.SupportsVision() {
				blocks = append(blocks, anBlock{Type: anBlockText, Text: "[image omitted: the current model cannot read images]"})
				continue
			}
			blocks = append(blocks, anBlock{
				Type: anBlockImage,
				Source: &anImageSource{
					Type:      "base64",
					MediaType: p.Image.MediaType,
					Data:      base64.StdEncoding.EncodeToString(p.Image.Data),
				},
			})
		default:
			if p.Text != "" {
				blocks = append(blocks, anBlock{Type: anBlockText, Text: p.Text})
			}
		}
	}
	if len(blocks) == 0 {
		// The API rejects an empty content array.
		blocks = append(blocks, anBlock{Type: anBlockText, Text: "(no output)"})
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
				Type: anBlockImage,
				Source: &anImageSource{
					Type:      "base64",
					MediaType: p.Image.MediaType,
					Data:      base64.StdEncoding.EncodeToString(p.Image.Data),
				},
			})
		case p.ToolResult != nil:
			blocks = append(blocks, anBlock{
				Type:      anBlockToolResult,
				ToolUseID: p.ToolResult.CallID,
				Content:   c.toolResultContent(p.ToolResult),
				IsError:   p.ToolResult.IsError,
			})
		default:
			if p.Text != "" {
				blocks = append(blocks, anBlock{Type: anBlockText, Text: p.Text})
			}
		}
	}
	if len(blocks) == 0 {
		blocks = append(blocks, anBlock{Type: anBlockText, Text: "(empty)"})
	}
	return blocks, nil
}

// anStreamState is one stream's running totals.
type anStreamState struct {
	// inputTokens arrives on message_start and is repeated on the usage event,
	// because the API reports output tokens separately on message_delta.
	inputTokens int
	// cacheReadTokens / cacheWriteTokens are the prompt-cache split reported on
	// message_start. This protocol excludes them from input_tokens, so they are
	// carried separately rather than folded in.
	cacheReadTokens  int
	cacheWriteTokens int
	// Per-index accumulation of tool_use argument JSON. The API streams
	// partial_json fragments that must be concatenated.
	callID   map[int]string
	callName map[int]string
	// openToolBlock is the content-block index of the tool_use block whose
	// arguments are currently streaming, or -1 when none is open. An
	// input_json_delta carries no id of its own — it belongs to the
	// content_block_start that opened it — so it has to be tracked here to
	// tell parallel calls apart.
	openToolBlock int
	sawAny        bool
	// stopReason arrives on message_delta, one event before message_stop.
	stopReason string
}

func newAnStreamState() *anStreamState {
	return &anStreamState{
		callID:        make(map[int]string),
		callName:      make(map[int]string),
		openToolBlock: -1,
	}
}

func (c *anthropicClient) readStream(ctx context.Context, body io.Reader, ch chan<- Event) {
	scanner := newSSEScanner(body)
	st := newAnStreamState()

	for {
		payload, ok := nextSSEPayload(scanner)
		if !ok {
			break
		}
		ev, parsed := decodeAnStreamEvent(payload)
		if !parsed {
			// One malformed frame is not worth failing an otherwise complete
			// answer over; gateways inject frames this parser does not model.
			continue
		}
		if terminate, ok := st.handle(ctx, ch, ev); terminate {
			return
		} else if !ok {
			// The consumer is gone.
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
	st.finish(ctx, ch)
}

// handle applies one event. It reports (terminate, delivered): terminate means a
// terminal event was emitted and the caller must stop reading, delivered=false
// means the consumer went away.
func (st *anStreamState) handle(ctx context.Context, ch chan<- Event, ev anStreamEvent) (terminate, delivered bool) {
	switch ev.Type {
	case "message_start":
		st.messageStart(ev)
		return false, true

	case "content_block_start":
		return false, st.contentBlockStart(ctx, ch, ev)

	case "content_block_delta":
		return false, st.contentBlockDelta(ctx, ch, ev)

	case "message_delta":
		st.messageDelta(ctx, ch, ev)
		return false, true

	case "message_stop":
		if st.stopReason == "max_tokens" {
			// The reply was cut off at the output cap. Reported before the
			// empty-stream check for the same reason as the OpenAI client:
			// a reply cut off during thinking has no content at all.
			emitTruncated(ctx, ch)
			return true, true
		}
		st.finish(ctx, ch)
		return true, true

	case "error":
		msg := "stream error"
		if ev.Error != nil && ev.Error.Message != "" {
			msg = ev.Error.Message
		}
		emitTerminal(ctx, ch, fmt.Errorf("anthropic: %s", msg))
		return true, true
	}
	return false, true
}

func (st *anStreamState) messageStart(ev anStreamEvent) {
	if ev.Message != nil && ev.Message.Usage != nil {
		st.inputTokens = ev.Message.Usage.InputTokens
		st.cacheReadTokens = ev.Message.Usage.CacheReadInputTokens
		st.cacheWriteTokens = ev.Message.Usage.CacheCreationInputTokens
	}
	st.sawAny = true
}

// contentBlockStart opens a content block. A tool_use block is announced
// immediately so the UI can open a card before its arguments finish streaming.
func (st *anStreamState) contentBlockStart(ctx context.Context, ch chan<- Event, ev anStreamEvent) bool {
	st.sawAny = true
	st.openToolBlock = -1
	if ev.ContentBlock == nil || ev.ContentBlock.Type != anBlockToolUse {
		return true
	}
	st.callID[ev.Index] = ev.ContentBlock.ID
	st.callName[ev.Index] = ev.ContentBlock.Name
	st.openToolBlock = ev.Index
	index := ev.Index
	return emit(ctx, ch, Event{Kind: KindToolCallStart, ToolCall: &ToolCall{
		ID:    ev.ContentBlock.ID,
		Name:  ev.ContentBlock.Name,
		Index: &index,
	}})
}

// contentBlockDelta forwards one delta of the open block.
func (st *anStreamState) contentBlockDelta(ctx context.Context, ch chan<- Event, ev anStreamEvent) bool {
	if ev.Delta == nil {
		return true
	}
	switch ev.Delta.Type {
	case anDeltaText:
		if ev.Delta.Text == "" {
			return true
		}
		st.sawAny = true
		return emit(ctx, ch, Event{Kind: KindText, Text: ev.Delta.Text})

	case "thinking_delta":
		if ev.Delta.Thinking == "" {
			return true
		}
		st.sawAny = true
		return emit(ctx, ch, Event{Kind: KindReasoning, Reasoning: ev.Delta.Thinking})

	case "signature_delta":
		// The signature closes the thinking block. It must travel with the
		// block when the turn also calls a tool, or the next round is rejected.
		if ev.Delta.Signature == "" {
			return true
		}
		return emit(ctx, ch, Event{Kind: KindReasoningSignature, Signature: ev.Delta.Signature})

	case "input_json_delta":
		if ev.Delta.PartialJSON == "" {
			return true
		}
		// No open tool_use block means this fragment belongs to a block this
		// client never saw start (a gateway quirk); emitting it without an
		// index would let the consumer append it to an unrelated call.
		if st.openToolBlock < 0 {
			return true
		}
		index := st.openToolBlock
		return emit(ctx, ch, Event{Kind: KindToolCallDelta, ToolCall: &ToolCall{Arguments: ev.Delta.PartialJSON, Index: &index}})
	}
	return true
}

func (st *anStreamState) messageDelta(ctx context.Context, ch chan<- Event, ev anStreamEvent) {
	if ev.Delta != nil && ev.Delta.StopReason != "" {
		st.stopReason = ev.Delta.StopReason
	}
	if ev.Usage != nil {
		emit(ctx, ch, Event{Kind: KindUsage, Usage: &Usage{
			InputTokens:      st.inputTokens,
			OutputTokens:     ev.Usage.OutputTokens,
			CacheReadTokens:  st.cacheReadTokens,
			CacheWriteTokens: st.cacheWriteTokens,
		}})
	}
}

// finish emits the terminal event for a stream that ended: success when
// anything arrived, ErrNoOutput otherwise.
func (st *anStreamState) finish(ctx context.Context, ch chan<- Event) {
	if !st.sawAny {
		emitTerminal(ctx, ch, ErrNoOutput)
		return
	}
	emitTerminal(ctx, ch, nil)
}
