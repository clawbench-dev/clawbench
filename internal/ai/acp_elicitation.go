package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// ACP form elicitation (agent → client).
//
// OpenCode asks the user questions through `elicitation/create` with a JSON
// Schema form instead of a permission request. The agent BLOCKS on that RPC
// until the client answers, so the only way to keep the user's choice is to
// render the form as the ordinary AskUserQuestion card and hand the selection
// back as the RPC result. When the capability is not advertised at all the
// agent short-circuits to its own "the question couldn't be shown" fallback and
// the model invents an answer — that is the reported issue #633.
//
// Scope: only the `form` mode is advertised (see buildInitializeRequest); URL
// elicitations are declined. Fields the card cannot express (numbers, booleans,
// free text in a multi-question form) are left out of the answer, which the
// agent reports back to the model as "unanswered" — a graceful degradation, not
// an error.

// pendingElicitation tracks a blocking elicitation/create waiting for the user.
type pendingElicitation struct {
	ID        string
	SessionID string
	Ch        chan elicitationAnswer
}

type elicitationAnswer struct {
	Content map[string]any
	// Cancelled marks an answer that nobody gave: the turn was cancelled or
	// unregistered while the card was still up. The agent is told "cancel"
	// rather than "accepted, empty".
	Cancelled bool
}

// elicitationAnswerTimeout bounds how long an unanswered question may block a
// turn. The card is a request for a human, and some turns have no human behind
// them (a scheduled task, a session the user walked away from). Without a
// backstop those turns would hang forever — the stall watchdog deliberately
// ignores a pending elicitation, since waiting on a person is legitimate.
const elicitationAnswerTimeout = 30 * time.Minute

// elicitationSeq keeps ids unique within a process. The timestamp is part of
// the id so two connections created in the same nanosecond still differ.
var elicitationSeq atomic.Int64

// elicitationQuestion is one rendered question in the AskUserQuestion card.
// Field names match the shape the frontend already parses (web/src/utils/
// renderToolDetail.ts renderAskUserQuestion); `key`/`customKey`/`value` are the
// additions that let the card address a single form property on submit.
type elicitationQuestion struct {
	Key         string              `json:"key"`
	Header      string              `json:"header,omitempty"`
	Question    string              `json:"question,omitempty"`
	MultiSelect bool                `json:"multiSelect,omitempty"`
	Options     []elicitationOption `json:"options,omitempty"`
	CustomKey   string              `json:"customKey,omitempty"`
}

type elicitationOption struct {
	Label       string `json:"label"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
}

// UnstableCreateElicitation implements the ACP client side of
// `elicitation/create` (form mode only). It emits the questions as an
// AskUserQuestion tool block and blocks until the user answers or the turn is
// cancelled.
func (c *ClawBenchACPClient) UnstableCreateElicitation(
	ctx context.Context,
	p acp.UnstableCreateElicitationRequest,
) (acp.UnstableCreateElicitationResponse, error) {
	if p.Form == nil {
		// Only form elicitation is advertised; a URL request means the agent
		// ignored the capability. Declining lets it fall back on its own.
		return elicitationDecline(), nil
	}

	questions := buildElicitationQuestions(p.Form.RequestedSchema)
	if len(questions) == 0 {
		// Nothing the card can express. Accept with empty content: the agent
		// then reports "unanswered" to the model and continues. Declining or
		// cancelling would end the whole turn instead.
		return elicitationAccept(nil), nil
	}

	c.mu.Lock()
	sessionID := c.activeSessionID
	ch, hasRoute := c.sessionRoutes[sessionID]
	c.mu.Unlock()
	if !hasRoute || ch == nil {
		// No live stream to show the card on (turn already finished, or a
		// session that is not being watched). Degrade the same way.
		slog.Warn("acp: elicitation with no active stream, answering unanswered",
			"session_id", sessionID, "questions", len(questions))
		return elicitationAccept(nil), nil
	}

	// Tell the stall watchdog this turn is legitimately waiting on a human.
	// Paired with the function's return (answered, cancelled or failed).
	if c.connRef != nil {
		c.connRef.pendingElicitations.Add(1)
		defer c.connRef.pendingElicitations.Add(-1)
	}

	id := fmt.Sprintf("elic_%d_%d", time.Now().UnixNano(), elicitationSeq.Add(1))
	payload := map[string]any{
		"questions":     questions,
		"elicitationId": id,
	}
	inputJSON, err := json.Marshal(payload)
	if err != nil {
		return elicitationAccept(nil), nil
	}

	pp := &pendingElicitation{
		ID:        id,
		SessionID: sessionID,
		Ch:        make(chan elicitationAnswer, 1),
	}
	c.mu.Lock()
	c.pendingElicitations[id] = pp
	c.mu.Unlock()

	forwardACPEvent(ch, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{
			Name:  "AskUserQuestion",
			ID:    id,
			Input: string(inputJSON),
			Done:  false,
		},
	})

	timeout := time.NewTimer(elicitationAnswerTimeout)
	defer timeout.Stop()

	select {
	case ans := <-pp.Ch:
		c.forgetElicitation(id)
		if ans.Cancelled {
			// The turn ended (or was unregistered) while the card was up; the
			// answer is worthless now, so tell the agent to stop waiting.
			return acp.UnstableCreateElicitationResponse{
				Cancel: &acp.UnstableCreateElicitationCancel{Action: "cancel"},
			}, nil
		}
		forwardACPEvent(ch, StreamEvent{
			Type: "tool_result",
			Tool: &ToolCall{
				ID:     id,
				Done:   true,
				Status: "success",
				Output: elicitationResultText(ans.Content),
			},
		})
		return elicitationAccept(ans.Content), nil
	case <-timeout.C:
		// Nobody answered. Accept with empty content: the agent reports
		// "unanswered" to the model and the turn continues. Cancelling would
		// end the whole turn, which is worse than a model assumption.
		c.forgetElicitation(id)
		slog.Warn("acp: elicitation timed out with no answer",
			"session_id", sessionID, "elicitation_id", id, "timeout", elicitationAnswerTimeout)
		forwardACPEvent(ch, StreamEvent{
			Type: "tool_result",
			Tool: &ToolCall{
				ID:     id,
				Done:   true,
				Status: "error",
				Output: "Timed out",
			},
		})
		return elicitationAccept(nil), nil
	case <-ctx.Done():
		c.forgetElicitation(id)
		// The connection is going away — mirror that to the agent rather than
		// leaving it blocked on a request nobody will answer.
		return acp.UnstableCreateElicitationResponse{
			Cancel: &acp.UnstableCreateElicitationCancel{Action: "cancel"},
		}, nil
	}
}

func (c *ClawBenchACPClient) forgetElicitation(id string) {
	c.mu.Lock()
	delete(c.pendingElicitations, id)
	c.mu.Unlock()
}

// cancelElicitationsLocked unblocks every pending elicitation of a session with
// a "cancelled" answer, so a turn that ended without an answer does not leave
// its handler (and the stall-watchdog exemption) alive for the connection's
// lifetime. Caller holds c.mu; the per-request channel is buffered, so the send
// never blocks.
func (c *ClawBenchACPClient) cancelElicitationsLocked(acpSessionID string) {
	for id, pp := range c.pendingElicitations {
		if pp.SessionID != acpSessionID {
			continue
		}
		delete(c.pendingElicitations, id)
		select {
		case pp.Ch <- elicitationAnswer{Cancelled: true}:
		default:
		}
	}
}

// RespondElicitation delivers the user's answer to a pending elicitation.
// Returns false when no pending request carries that id (already answered,
// timed out, or a stale card restored from history).
func (c *ClawBenchACPClient) RespondElicitation(id string, content map[string]any) bool {
	c.mu.Lock()
	pp, ok := c.pendingElicitations[id]
	if ok {
		delete(c.pendingElicitations, id)
	}
	c.mu.Unlock()
	if !ok {
		return false
	}
	if content == nil {
		content = map[string]any{}
	}
	pp.Ch <- elicitationAnswer{Content: content}
	return true
}

// HasPendingElicitation reports whether this connection owns the request. Used
// to find the member connection holding a group chat's elicitation.
func (c *ClawBenchACPClient) HasPendingElicitation(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.pendingElicitations[id]
	return ok
}

// RegisterPendingElicitationForTest injects a pending elicitation so the
// respond plumbing can be exercised without a live agent. Production code must
// not use this. The returned channel receives the answer.
func (c *ClawBenchACPClient) RegisterPendingElicitationForTest(id, acpSessionID string) chan elicitationAnswer {
	ch := make(chan elicitationAnswer, 1)
	c.mu.Lock()
	c.pendingElicitations[id] = &pendingElicitation{ID: id, SessionID: acpSessionID, Ch: ch}
	c.mu.Unlock()
	return ch
}

func elicitationAccept(content map[string]any) acp.UnstableCreateElicitationResponse {
	return acp.UnstableCreateElicitationResponse{
		Accept: &acp.UnstableCreateElicitationAccept{Action: "accept", Content: content},
	}
}

func elicitationDecline() acp.UnstableCreateElicitationResponse {
	return acp.UnstableCreateElicitationResponse{
		Decline: &acp.UnstableCreateElicitationDecline{Action: "decline"},
	}
}

// elicitationResultText is what the card's tool_result carries. Only used for
// display; the agent gets the structured content.
func elicitationResultText(content map[string]any) string {
	if len(content) == 0 {
		return "Unanswered"
	}
	return "Answered"
}

// buildElicitationQuestions turns a form schema into card questions.
//
// Property order matters: the schema arrives as a JSON object (map), and the
// agent expects answers keyed by property name, so the card must keep a stable
// q0, q1, q2 … order rather than Go's randomized map order.
func buildElicitationQuestions(schema acp.UnstableElicitationSchema) []elicitationQuestion {
	if len(schema.Properties) == 0 {
		return nil
	}

	keys := make([]string, 0, len(schema.Properties))
	for k := range schema.Properties {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return naturalLess(keys[i], keys[j]) })

	present := make(map[string]bool, len(keys))
	for _, k := range keys {
		present[k] = true
	}

	out := make([]elicitationQuestion, 0, len(keys))
	for _, key := range keys {
		if q, ok := elicitationQuestionFor(key, schema.Properties[key], present); ok {
			out = append(out, q)
		}
	}

	// A question with no options is a free-text field. The card has exactly one
	// note box, and it can only be attributed to the answer when that field is
	// the ONLY question (see submitElicitation). Keeping optionless questions in
	// a multi-question form would render a box the user cannot fill, and if that
	// were the only content the card could never produce an answer at all — the
	// submit would silently do nothing while the agent stayed blocked.
	if len(out) > 1 {
		withOptions := out[:0]
		for _, q := range out {
			if len(q.Options) > 0 {
				withOptions = append(withOptions, q)
			}
		}
		out = withOptions
	}
	return out
}

// elicitationQuestionFor builds the card question for one schema property.
// ok is false when the property should not appear as a card at all.
func elicitationQuestionFor(key string, raw any, present map[string]bool) (elicitationQuestion, bool) {
	// "<q>_custom" is the agent's "type your own answer" companion field for
	// question <q>. It is folded into that question instead of becoming a card
	// of its own (see customKeyFor).
	if base, isCustom := strings.CutSuffix(key, "_custom"); isCustom && present[base] {
		return elicitationQuestion{}, false
	}
	prop, isObj := raw.(map[string]any)
	if !isObj {
		return elicitationQuestion{}, false
	}
	q := elicitationQuestion{
		Key:      key,
		Header:   stringField(prop, "title"),
		Question: stringField(prop, "description"),
		CustomKey: func() string {
			if present[key+"_custom"] {
				return key + "_custom"
			}
			return ""
		}(),
	}
	switch typ := stringField(prop, "type"); typ {
	case "array":
		q.MultiSelect = true
		if items, isMap := prop["items"].(map[string]any); isMap {
			q.Options = elicitationOptions(items)
		}
	case "string":
		q.Options = elicitationOptions(prop)
	default:
		// number / integer / boolean: the card has no control for these.
		// Skipping leaves the field unanswered instead of mis-answering it.
		return elicitationQuestion{}, false
	}
	// A question with neither text nor options cannot be rendered (and an empty
	// card would show the "invalid format" notice).
	if q.Question == "" && q.Header == "" && len(q.Options) == 0 {
		return elicitationQuestion{}, false
	}
	return q, true
}

// elicitationOptions reads the enum carried by a property: `oneOf`/`anyOf`
// entries of {const,title,description} (OpenCode's shape) or a plain `enum`.
func elicitationOptions(prop map[string]any) []elicitationOption {
	var out []elicitationOption
	for _, field := range []string{"oneOf", "anyOf"} {
		entries, ok := prop[field].([]any)
		if !ok {
			continue
		}
		for _, e := range entries {
			obj, ok := e.(map[string]any)
			if !ok {
				continue
			}
			value := stringField(obj, "const")
			label := stringField(obj, "title")
			if label == "" {
				label = value
			}
			if value == "" && label == "" {
				continue
			}
			if value == "" {
				value = label
			}
			out = append(out, elicitationOption{
				Label:       label,
				Value:       value,
				Description: stringField(obj, "description"),
			})
		}
	}
	if len(out) > 0 {
		return out
	}
	if enum, ok := prop["enum"].([]any); ok {
		for _, e := range enum {
			s, ok := e.(string)
			if !ok {
				continue
			}
			out = append(out, elicitationOption{Label: s, Value: s})
		}
	}
	return out
}

func stringField(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// naturalLess orders q0, q1, q2, … q10 the way a human reads them. A plain
// string compare would put q10 between q1 and q2.
func naturalLess(a, b string) bool {
	ai, aRest, aNum := splitTrailingNumber(a)
	bi, bRest, bNum := splitTrailingNumber(b)
	if aRest != bRest {
		return aRest < bRest
	}
	if aNum && bNum && ai != bi {
		return ai < bi
	}
	return a < b
}

func splitTrailingNumber(s string) (int, string, bool) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) {
		return 0, s, false
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return 0, s, false
	}
	return n, s[:i], true
}
