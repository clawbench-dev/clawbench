package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// elicitationFormSchema builds the shape OpenCode sends for its question tool:
// one property per question, a companion "<key>_custom" free-text field, and a
// multiselect expressed as an array whose items carry the enum.
func elicitationFormSchema() acp.ElicitationSchema {
	// Decoded from the wire shape rather than built from structs: the typed
	// union only keeps the members the schema names, so going through JSON is
	// what the client actually sees (including `anyOf` on array items).
	const raw = `{
		"type": "object",
		"properties": {
			"q0": {
				"type": "string",
				"title": "部署方式",
				"description": "你想怎么部署？",
				"oneOf": [
					{"const": "docker", "title": "Docker", "description": "容器部署"},
					{"const": "binary", "title": "二进制"}
				]
			},
			"q0_custom": {"type": "string", "title": "部署方式 (other)"},
			"q1": {
				"type": "array",
				"title": "要开哪些",
				"description": "可以多选",
				"items": {
					"anyOf": [
						{"const": "logs", "title": "日志"},
						{"const": "metrics", "title": "指标"}
					]
				}
			}
		}
	}`
	var schema acp.ElicitationSchema
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		panic(err)
	}
	return schema
}

// newElicitationFormRequest builds the request OpenCode sends for its question
// tool: form mode with the question schema.
func newElicitationFormRequest(schema acp.ElicitationSchema) acp.CreateElicitationRequest {
	return acp.CreateElicitationRequest{Form: &acp.CreateElicitationForm{
		Message:         "Please answer",
		Mode:            "form",
		RequestedSchema: schema,
	}}
}

// mustElicitationSchema decodes a JSON-shaped schema into the typed form, the
// way the client receives it on the wire.
func mustElicitationSchema(t *testing.T, v any) acp.ElicitationSchema {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var schema acp.ElicitationSchema
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	return schema
}

func TestBuildElicitationQuestions_OrderAndCustomFolding(t *testing.T) {
	// q10 must sort after q9, not between q1 and q2 — the agent keys answers by
	// property name, so a shuffled card would hand the wrong answer to a
	// question.
	props := map[string]any{}
	for _, k := range []string{"q2", "q10", "q1"} {
		props[k] = map[string]any{
			"type":  "string",
			"title": k,
			"oneOf": []any{map[string]any{"const": "a", "title": "A"}},
		}
	}
	got := buildElicitationQuestions(mustElicitationSchema(t, map[string]any{
		"type":       "object",
		"properties": props,
	}))
	order := make([]string, 0, len(got))
	for _, q := range got {
		order = append(order, q.Key)
	}
	want := []string{"q1", "q2", "q10"}
	if len(order) != len(want) {
		t.Fatalf("questions = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("questions = %v, want %v", order, want)
		}
	}

	// The companion field is folded into its question instead of becoming a
	// second, answerless card.
	qs := buildElicitationQuestions(elicitationFormSchema())
	if len(qs) != 2 {
		t.Fatalf("questions = %d, want 2 (q0_custom must be folded)", len(qs))
	}
	if qs[0].CustomKey != "q0_custom" {
		t.Fatalf("q0 custom key = %q, want q0_custom", qs[0].CustomKey)
	}
	if !qs[1].MultiSelect {
		t.Fatal("q1 must be multi-select")
	}
	if len(qs[1].Options) != 2 || qs[1].Options[1].Value != "metrics" {
		t.Fatalf("q1 options = %+v", qs[1].Options)
	}
	if qs[0].Options[0].Label != "Docker" || qs[0].Options[0].Value != "docker" {
		t.Fatalf("q0 option = %+v, want label Docker / value docker", qs[0].Options[0])
	}
}

func TestBuildElicitationQuestions_SkipsUnrepresentableFields(t *testing.T) {
	// A number field has no control on the card. Dropping it leaves the answer
	// "unanswered" for that property; inventing a value would be worse.
	got := buildElicitationQuestions(mustElicitationSchema(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"count": map[string]any{"type": "integer", "title": "多少个"},
		},
	}))
	if len(got) != 0 {
		t.Fatalf("questions = %+v, want none", got)
	}
}

func TestCreateElicitation_EmitsCardAndReturnsAnswer(t *testing.T) {
	client := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 8)
	client.RegisterSession("acp-sess-1", ch)

	type result struct {
		resp acp.CreateElicitationResponse
		err  error
	}
	done := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		resp, err := client.CreateElicitation(ctx, newElicitationFormRequest(elicitationFormSchema()))
		done <- result{resp: resp, err: err}
	}()

	var card StreamEvent
	select {
	case card = <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no tool_use event emitted for the elicitation")
	}
	if card.Type != "tool_use" || card.Tool == nil {
		t.Fatalf("event = %+v, want tool_use", card)
	}
	if card.Tool.Name != "AskUserQuestion" {
		t.Fatalf("tool name = %q, want AskUserQuestion (the card renderer keys off it)", card.Tool.Name)
	}
	if card.Tool.Done {
		t.Fatal("card must start pending")
	}

	var payload struct {
		Questions     []elicitationQuestion `json:"questions"`
		ElicitationID string                `json:"elicitationId"`
	}
	if err := json.Unmarshal([]byte(card.Tool.Input), &payload); err != nil {
		t.Fatalf("card input is not JSON: %v", err)
	}
	if payload.ElicitationID == "" || payload.ElicitationID != card.Tool.ID {
		t.Fatalf("elicitation id %q must match block id %q", payload.ElicitationID, card.Tool.ID)
	}
	if len(payload.Questions) != 2 {
		t.Fatalf("questions = %+v, want 2", payload.Questions)
	}

	if !client.HasPendingElicitation(card.Tool.ID) {
		t.Fatal("request must be registered before the card is shown")
	}
	if !client.RespondElicitation(card.Tool.ID, map[string]any{"q0": "binary", "q1": []string{"logs"}}) {
		t.Fatal("RespondElicitation reported no pending request")
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("CreateElicitation: %v", r.err)
		}
		if r.resp.Accept == nil || r.resp.Accept.Action != "accept" {
			t.Fatalf("response = %+v, want accept", r.resp)
		}
		if got := r.resp.Accept.Content["q0"].String; got == nil || string(*got) != "binary" {
			t.Fatalf("q0 answer = %v, want binary", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("elicitation did not unblock after the answer")
	}

	// The card must be closed out, otherwise the UI keeps spinning forever.
	select {
	case ev := <-ch:
		if ev.Type != "tool_result" || ev.Tool == nil || !ev.Tool.Done || ev.Tool.ID != card.Tool.ID {
			t.Fatalf("event = %+v, want a done tool_result for %s", ev, card.Tool.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no tool_result emitted after the answer")
	}
}

func TestCreateElicitation_AnswersUnansweredWhenNothingRenderable(t *testing.T) {
	client := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 4)
	client.RegisterSession("acp-sess-1", ch)

	resp, err := client.CreateElicitation(context.Background(), newElicitationFormRequest(
		mustElicitationSchema(t, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"count": map[string]any{"type": "integer", "title": "多少个"},
			},
		}),
	))
	if err != nil {
		t.Fatalf("CreateElicitation: %v", err)
	}
	// Accepting with empty content makes the agent report "unanswered" and keep
	// going. Declining/cancelling would end the whole turn instead.
	if resp.Accept == nil {
		t.Fatalf("response = %+v, want accept", resp)
	}
	if len(resp.Accept.Content) != 0 {
		t.Fatalf("content = %v, want empty", resp.Accept.Content)
	}
	select {
	case ev := <-ch:
		t.Fatalf("no card should be emitted, got %+v", ev)
	default:
	}
}

func TestCreateElicitation_CancelsWithTurn(t *testing.T) {
	client := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 4)
	client.RegisterSession("acp-sess-1", ch)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan acp.CreateElicitationResponse, 1)
	go func() {
		resp, _ := client.CreateElicitation(ctx, newElicitationFormRequest(elicitationFormSchema()))
		done <- resp
	}()

	select {
	case <-ch: // the card
	case <-time.After(2 * time.Second):
		t.Fatal("no card emitted")
	}
	cancel()

	select {
	case resp := <-done:
		if resp.Cancel == nil {
			t.Fatalf("response = %+v, want cancel", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("elicitation stayed blocked after the turn was cancelled")
	}
}

func TestRespondElicitation_UnknownIDIsFalse(t *testing.T) {
	client := NewClawBenchACPClient()
	if client.RespondElicitation("elic_missing", map[string]any{"q0": "x"}) {
		t.Fatal("RespondElicitation must report false for an unknown id")
	}
}

// A question waiting on the user produces no model progress, so without this
// the stall watchdog would kill the connection while the user is reading it.
func TestIsStalled_TreatsPendingElicitationAsActivity(t *testing.T) {
	conn := &ACPConn{}
	conn.lastModelProgress.Store(time.Now().Add(-time.Hour).UnixNano())

	conn.pendingElicitations.Add(1)
	if conn.isStalled(time.Minute) {
		t.Fatal("a connection waiting on a user answer must not be considered stalled")
	}
	conn.pendingElicitations.Add(-1)
	if !conn.isStalled(time.Minute) {
		t.Fatal("without the pending request the hour of silence must count as stalled")
	}
}

// TestSDKDispatchesElicitationCreateToClient drives the REAL SDK connection
// with a fake agent. The unit test above calls the method directly, so it would
// stay green even if the method name/signature stopped matching the interface
// the SDK type-asserts on (which silently degrades to JSON-RPC -32601
// "method not found" and puts the agent back on its fallback text).
func TestSDKDispatchesElicitationCreateToClient(t *testing.T) {
	agentToClientR, agentToClientW := io.Pipe()
	clientToAgentR, clientToAgentW := io.Pipe()

	client := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 8)
	client.RegisterSession("acp-sess-1", ch)

	conn := acp.NewClientSideConnection(client, clientToAgentW, agentToClientR)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	request := `{"jsonrpc":"2.0","id":7,"method":"elicitation/create","params":{` +
		`"sessionId":"acp-sess-1","mode":"form","message":"Questions","requestedSchema":{` +
		`"type":"object","properties":{"q0":{"type":"string","title":"部署方式",` +
		`"description":"你想怎么部署？","oneOf":[{"const":"docker","title":"Docker"},` +
		`{"const":"binary","title":"二进制"}]}}}}}`
	go func() {
		_, _ = io.WriteString(agentToClientW, request+"\n")
	}()

	var card StreamEvent
	select {
	case card = <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("SDK never delivered the elicitation to the client (method not dispatched)")
	}
	if card.Tool == nil || card.Tool.Name != "AskUserQuestion" {
		t.Fatalf("event = %+v, want the ask card", card)
	}

	var payload struct {
		ElicitationID string `json:"elicitationId"`
	}
	if err := json.Unmarshal([]byte(card.Tool.Input), &payload); err != nil {
		t.Fatalf("card input is not JSON: %v", err)
	}
	if !client.RespondElicitation(payload.ElicitationID, map[string]any{"q0": "binary"}) {
		t.Fatal("no pending elicitation for the card id")
	}

	respCh := make(chan string, 1)
	go func() {
		line, err := bufio.NewReader(clientToAgentR).ReadString('\n')
		if err == nil {
			respCh <- line
		}
	}()

	select {
	case line := <-respCh:
		var resp struct {
			ID     int `json:"id"`
			Result struct {
				Action  string         `json:"action"`
				Content map[string]any `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("response is not JSON: %v (%s)", err, line)
		}
		if resp.ID != 7 {
			t.Fatalf("response id = %d, want 7 (%s)", resp.ID, line)
		}
		if resp.Result.Action != "accept" || resp.Result.Content["q0"] != "binary" {
			t.Fatalf("response = %s, want accept with q0=binary", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the client never answered the elicitation request")
	}
}

func TestBuildElicitationQuestions_FreeTextOnlyWhenAlone(t *testing.T) {
	freeText := map[string]any{"type": "string", "title": "你的名字", "description": "请输入"}
	// Alone: the card's single note box is the answer.
	got := buildElicitationQuestions(mustElicitationSchema(t, map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": freeText},
	}))
	if len(got) != 1 || got[0].Key != "name" {
		t.Fatalf("questions = %+v, want the sole free-text question kept", got)
	}
	// Alongside a question with options: the one note box cannot be attributed,
	// so the free-text question must not render an unanswerable box.
	got = buildElicitationQuestions(mustElicitationSchema(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": freeText,
			"mode": map[string]any{
				"type":  "string",
				"title": "方式",
				"oneOf": []any{map[string]any{"const": "a", "title": "A"}},
			},
		},
	}))
	if len(got) != 1 || got[0].Key != "mode" {
		t.Fatalf("questions = %+v, want only the option-backed question", got)
	}
}

// A turn that ends (or a session that is unregistered) while a question is up
// must not leave the handler blocked: that would keep pendingElicitations > 0
// and silently disable the stall watchdog for the connection's whole life.
func TestUnregisterSession_CancelsPendingElicitation(t *testing.T) {
	client := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 8)
	client.RegisterSession("acp-sess-1", ch)

	done := make(chan acp.CreateElicitationResponse, 1)
	go func() {
		resp, _ := client.CreateElicitation(context.Background(), newElicitationFormRequest(elicitationFormSchema()))
		done <- resp
	}()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no card emitted")
	}
	client.UnregisterSession("acp-sess-1")

	select {
	case resp := <-done:
		if resp.Cancel == nil {
			t.Fatalf("response = %+v, want cancel", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("UnregisterSession left the elicitation blocked")
	}
	if len(client.pendingElicitations) != 0 {
		t.Fatalf("pendingElicitations = %d, want 0", len(client.pendingElicitations))
	}
}

// TestElicitationContent_JSONDecodedMultiSelect pins the wire shape the HTTP
// handler actually produces: encoding/json decodes a multi-select answer as
// []any, not []string. Before the v0.20 typed response, the untyped map passed
// it straight through; the typed conversion must accept []any or the field is
// silently dropped and the agent reports the question as unanswered.
func TestElicitationContent_JSONDecodedMultiSelect(t *testing.T) {
	var content map[string]any
	if err := json.Unmarshal([]byte(`{"q0":["logs","metrics"],"q1":"binary"}`), &content); err != nil {
		t.Fatal(err)
	}

	got := elicitationContent(content)
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"q0":["logs","metrics"],"q1":"binary"}`
	if string(b) != want {
		t.Fatalf("elicitationContent = %s, want %s", b, want)
	}
}

// A multi-select answer whose elements are not strings cannot be expressed by
// the protocol's string-array value; the non-string elements are dropped rather
// than turning into an empty value that fails to encode.
func TestElicitationContent_NonStringArrayElements(t *testing.T) {
	got := elicitationContent(map[string]any{"q0": []any{"logs", 1, true}})
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"q0":["logs"]}`
	if string(b) != want {
		t.Fatalf("elicitationContent = %s, want %s", b, want)
	}
}
