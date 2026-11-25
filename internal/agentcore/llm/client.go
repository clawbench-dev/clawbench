// Package llm is the model client used by the built-in agent.
//
// It is deliberately small: one streaming call per model round, tool-call
// support, and the two wire formats ClawBench's provider table can express
// (OpenAI chat completions and Anthropic messages). Everything else — the tool
// loop, budgets, path confinement — lives one level up in agentcore.
//
// It does NOT reuse internal/summarize. That package is a one-shot text
// summarizer: no streaming, no tool calls, and its request shape is fixed. The
// overlap is a few struct tags, not enough to justify teaching a summarizer to
// drive an agent loop.
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Protocol names the wire format of an endpoint. The values match
// model.ProviderProtocol* so a provider row maps onto a client without a
// translation table that could drift.
const (
	ProtocolOpenAI    = "openai"
	ProtocolAnthropic = "anthropic"
)

// Role is a message author.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Image is one image handed to the model.
//
// MediaType must be a type the target endpoint accepts (image/png,
// image/jpeg, image/gif, image/webp). Data is the raw bytes; base64 encoding
// happens inside each client because the two wire formats disagree on where
// the encoding lives (a data URL vs a separate field).
//
// URL is set instead of Data when the picture already lives at an address the
// endpoint can fetch (the user attached a link). Passing the address costs no
// bytes on our side, and both protocols accept it natively.
type Image struct {
	MediaType string
	Data      []byte
	// URL is the picture's own address. When set, Data is empty.
	URL string
}

// Thinking is one reasoning block the assistant produced.
//
// It is kept on the message (not only streamed to the UI) because Anthropic
// requires the block to be replayed verbatim — signature included — when the
// same assistant turn also requested a tool. Dropping it makes every round
// after the first fail with a 400.
type Thinking struct {
	Text string
	// Signature is the provider's opaque proof that it produced this block.
	// Empty for providers that do not issue one.
	Signature string
}

// Part is one piece of a message's content.
//
// A message is a list of parts because a tool result may carry text AND
// images, and an assistant turn may carry text AND tool calls. Flattening
// either into a string is what forces the OpenAI-specific hack of appending a
// fake user message — see the openai client's tool-result handling.
type Part struct {
	// Text is set for a text part.
	Text string
	// Image is set for an image part.
	Image *Image
	// ToolCall is set for a tool call the ASSISTANT requested.
	ToolCall *ToolCall
	// ToolResult is set for the result of a tool call the CLIENT is returning.
	ToolResult *ToolResult
	// Thinking is set for a reasoning block the ASSISTANT produced. Clients
	// that cannot express it ignore it; Anthropic replays it.
	Thinking *Thinking
}

// ToolCall is one tool invocation requested by the model.
type ToolCall struct {
	// ID is the provider-assigned id used to match the result back to the call.
	// Not every provider supplies one; a client that receives an empty id must
	// mint a stable one, because the loop matches results by id.
	ID string
	// Name is the tool name, exactly as declared in ToolSpec.Name.
	Name string
	// Arguments is the raw JSON object the model produced. It is NOT validated
	// here: the tool layer parses it, so a malformed argument produces a tool
	// error the model can read and correct instead of a turn-ending failure.
	Arguments string
	// Index is the PROVIDER's slot number for this call, set on the streaming
	// events only (nil on a call read back from a conversation). It exists
	// because parallel calls stream INTERLEAVED — chunk for call 0, chunk for
	// call 1, more for call 0 — so a consumer cannot assume that an argument
	// fragment belongs to the most recently started call. Without it the two
	// calls' JSON gets spliced together into one blob that is not valid JSON.
	//
	// It is deliberately distinct from "the Nth call this round": providers
	// number their slots in their own order, and a consumer that conflated the
	// two would append fragments to the wrong call whenever the numbering is not
	// a dense 0..n-1 sequence.
	Index *int
}

// ToolResult is the outcome of one tool call, returned to the model.
type ToolResult struct {
	CallID string
	// Parts carries the output. Images are legal here (a read of a PNG), which
	// is the whole reason a tool result is a part list rather than a string.
	Parts []Part
	// IsError marks a tool failure. It is advisory: the loop already reports
	// the failure in the text, but some providers render errors differently.
	IsError bool
}

// ToolSpec declares one tool to the model.
type ToolSpec struct {
	Name        string
	Description string
	// Parameters is a JSON Schema object describing the arguments.
	Parameters map[string]any
}

// Message is one turn of the conversation sent to the model.
type Message struct {
	Role  Role
	Parts []Part
}

// TextMessage builds a single-text-part message.
func TextMessage(role Role, text string) Message {
	return Message{Role: role, Parts: []Part{{Text: text}}}
}

// Request is one model call.
type Request struct {
	// System is the system prompt. Empty means none.
	System string
	// Messages is the conversation so far, oldest first.
	Messages []Message
	// Tools declares the tools the model may call. Empty means the model must
	// answer in text only — which is what a policy with no capabilities
	// produces, so "no tools" is a normal state, not an error.
	Tools []ToolSpec
	// MaxOutputTokens caps the reply. Zero means the client's default.
	MaxOutputTokens int
	// Temperature is passed through where the protocol supports it. Zero is a
	// valid value (deterministic), so callers that do not care should leave it
	// at DefaultTemperature.
	Temperature float64
	// ReasoningEffort asks the model to think harder ("low", "medium", "high").
	// Empty, "auto" and "default" all mean "let the model decide" and send no
	// reasoning parameter at all.
	//
	// The two protocols express this differently (OpenAI: reasoning_effort,
	// Anthropic: an extended-thinking budget), so each client translates it.
	// Neither is emulated: a model that cannot reason must fail loudly rather
	// than quietly produce a normal answer under a level the user chose.
	ReasoningEffort string
	// SessionID is the conversation this call belongs to. OpenAI-compatible
	// endpoints use it as the prompt-cache key so one conversation keeps one
	// cache route; empty means no key. It is never sent as a user-visible field.
	SessionID string
}

// Reasoning effort levels. Auto is the absence of a request, not a value sent
// to the provider.
const (
	EffortAuto   = "auto"
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
)

// normalizeEffort maps "no preference" to the empty string so the request
// builders can treat it uniformly. Unknown values pass through: a provider may
// accept levels this build has never heard of, and dropping them silently would
// make the setting look broken.
func normalizeEffort(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", EffortAuto, "default":
		return ""
	default:
		return strings.ToLower(strings.TrimSpace(level))
	}
}

// DefaultTemperature is used when a caller leaves Temperature at zero and has
// not opted into determinism via Request.Deterministic.
const DefaultTemperature = 0.2

// Event is one incremental update from a streaming call.
//
// Exactly one field group is meaningful per event type; see Kind.
type Event struct {
	Kind EventKind

	// Text is the incremental assistant text (KindText).
	Text string
	// Reasoning is the incremental reasoning/thinking text (KindReasoning).
	Reasoning string
	// Signature is set on KindReasoningSignature: the provider's opaque proof
	// for the reasoning block that just streamed, needed to replay it.
	Signature string
	// ToolCall is set on KindToolCallStart (ID+Name, Arguments empty) and on
	// KindToolCallDelta (Arguments holds the increment). It is nil otherwise.
	// Its Index identifies WHICH call the event belongs to, so interleaved
	// parallel calls can be reassembled.
	ToolCall *ToolCall
	// Usage is set on KindUsage, usually the last event of a round.
	Usage *Usage
	// Truncated is set on KindDone: the endpoint stopped at its output-token
	// cap (OpenAI finish_reason=length, Anthropic stop_reason=max_tokens), so
	// what arrived is a PREFIX of the reply, not the whole answer.
	//
	// It is reported separately from KindError because nothing failed: the
	// call succeeded, the answer is just incomplete. Treating it as a normal
	// finish is what makes a long reply stop mid-sentence and look done.
	Truncated bool
	// Err is set on KindError, which is always the last event.
	Err error
}

// EventKind discriminates an Event.
type EventKind string

const (
	// KindText is an incremental piece of the assistant's answer.
	KindText EventKind = "text"
	// KindReasoning is an incremental piece of the model's reasoning, when the
	// endpoint exposes it. Never mixed into the answer.
	KindReasoning EventKind = "reasoning"
	// KindReasoningSignature carries the signature that closes the reasoning
	// block. It must be replayed with the block when the turn also calls tools.
	KindReasoningSignature EventKind = "reasoning_signature"
	// KindToolCallStart opens a tool call (ID and Name are known).
	KindToolCallStart EventKind = "tool_call_start"
	// KindToolCallDelta appends to the current tool call's argument JSON.
	KindToolCallDelta EventKind = "tool_call_delta"
	// KindUsage reports token usage for the round.
	KindUsage EventKind = "usage"
	// KindDone is the terminal success event. The channel closes right after.
	KindDone EventKind = "done"
	// KindError is the terminal failure event. The channel closes right after.
	KindError EventKind = "error"
)

// Usage is token accounting for one model round.
//
// InputTokens is the prompt size the provider billed as fresh input; the cache
// fields are the part of that prompt it served from (or wrote to) its prompt
// cache. Providers disagree on whether InputTokens INCLUDES the cached part
// (OpenAI/DeepSeek do, Anthropic does not), so the two are kept separate and
// never summed here.
type Usage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}

// Client is one model endpoint.
//
// Stream sends exactly one terminal event (KindDone or KindError) and then
// closes the channel. A caller that stops reading early MUST keep draining:
// the producer blocks on the channel, so abandoning it leaks a goroutine.
type Client interface {
	// Stream performs one model call, emitting events as they arrive.
	Stream(ctx context.Context, req Request) (<-chan Event, error)
	// Model returns the model id this client requests.
	Model() string
	// SupportsVision reports whether this endpoint can accept image parts.
	SupportsVision() bool
}

// Config describes one endpoint.
type Config struct {
	// Protocol is ProtocolOpenAI or ProtocolAnthropic.
	Protocol string
	// BaseURL is the configured base (a bare host, a partial base ending in
	// /v1, or the full endpoint). It is normalized per protocol.
	BaseURL string
	// APIKey is sent as a Bearer token (OpenAI) or x-api-key (Anthropic).
	APIKey string
	// AuthMode overrides how the key is presented. Empty means "whatever the
	// protocol does by default"; see the auth-mode constants.
	AuthMode string
	// Model is the model id to request.
	Model string
	// SupportsVision marks the model as accepting image input. It comes from
	// the model catalog, not from the protocol: an OpenAI-compatible gateway
	// may serve both vision and text-only models.
	SupportsVision bool
	// PresetID is the models.dev provider id this endpoint came from ("" for a
	// hand-written one). Some endpoints require request headers beyond auth,
	// and the id is how this client recognizes them; see providerhttp.
	PresetID string
	// SessionID is this conversation's stable id, sent as the provider's
	// routing header when the endpoint wants one. Empty means "one-off call":
	// the client generates one id and reuses it for its own lifetime.
	SessionID string
}

// Auth modes, mirroring model.ProviderAuth*.
//
// The explicit modes exist because many third-party Anthropic-compatible
// gateways accept only one of the two headers; "auto" picks the protocol's
// conventional one.
const (
	AuthAuto   = "auto"
	AuthAPIKey = "x-api-key"
	AuthBearer = "bearer"
)

// ErrUnsupportedProtocol is returned for a protocol this package cannot speak.
var ErrUnsupportedProtocol = errors.New("llm: unsupported protocol")

// New builds a client for cfg.
func New(cfg Config) (Client, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Protocol)) {
	case ProtocolOpenAI:
		return newOpenAIClient(cfg), nil
	case ProtocolAnthropic:
		return newAnthropicClient(cfg), nil
	case "":
		return nil, fmt.Errorf("%w: empty protocol", ErrUnsupportedProtocol)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedProtocol, cfg.Protocol)
	}
}

// emit sends an event without blocking a cancelled call.
//
// The producer holds the only send side, so a plain send is correct; the ctx
// check is what lets a cancelled turn stop the producer instead of parking it
// on a full channel while the consumer has already given up.
func emit(ctx context.Context, ch chan<- Event, ev Event) bool {
	select {
	case ch <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// emitTerminal sends the closing event and returns. Every stream path ends
// here so the "exactly one terminal event" contract holds by construction.
func emitTerminal(ctx context.Context, ch chan<- Event, err error) {
	if err != nil {
		emit(ctx, ch, Event{Kind: KindError, Err: err})
		return
	}
	emit(ctx, ch, Event{Kind: KindDone})
}

// emitTruncated sends the terminal event for a reply that hit the output cap.
//
// It is a KindDone, not a KindError: the call succeeded. The flag is what lets
// the agent loop continue the reply instead of accepting a prefix as the answer.
func emitTruncated(ctx context.Context, ch chan<- Event) {
	emit(ctx, ch, Event{Kind: KindDone, Truncated: true})
}
