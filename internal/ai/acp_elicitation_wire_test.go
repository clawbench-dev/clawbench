package ai

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

func elicitationLine(properties string) []byte {
	return []byte(`{"jsonrpc":"2.0","id":1,"method":"elicitation/create","params":{` +
		`"sessionId":"s1","mode":"form","message":"m","requestedSchema":{` +
		`"type":"object","properties":` + properties + `,"required":["q0"]}}}`)
}

func decodeNormalizedElicitation(t *testing.T, line []byte) acp.CreateElicitationRequest {
	t.Helper()
	normalized := normalizeElicitationSchema(line)
	var envelope struct {
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(normalized, &envelope); err != nil {
		t.Fatalf("normalized envelope does not decode: %v", err)
	}
	var req acp.CreateElicitationRequest
	if err := json.Unmarshal(envelope.Params, &req); err != nil {
		t.Fatalf("normalized params do not decode: %v", err)
	}
	if req.Form == nil {
		t.Fatal("normalized request has no form")
	}
	return req
}

func TestNormalizeElicitationSchema_InfersMissingType(t *testing.T) {
	cases := []struct {
		name          string
		properties    string
		wantNoOptions bool
		assert        func(*testing.T, acp.ElicitationPropertySchema)
	}{
		{
			name:       "enum implies string",
			properties: `{"q0":{"enum":["a","b"],"title":"Pick"}}`,
			assert: func(t *testing.T, p acp.ElicitationPropertySchema) {
				if p.String == nil || len(p.String.Enum) != 2 {
					t.Fatalf("property = %+v, want string with 2 enum values", p)
				}
			},
		},
		{
			name:       "oneOf implies string",
			properties: `{"q0":{"oneOf":[{"const":"a","title":"A"}],"title":"Pick"}}`,
			assert: func(t *testing.T, p acp.ElicitationPropertySchema) {
				if p.String == nil || len(p.String.OneOf) != 1 {
					t.Fatalf("property = %+v, want string with one oneOf option", p)
				}
			},
		},
		{
			name:       "items implies array",
			properties: `{"q0":{"items":{"enum":["a","b"]},"title":"Pick"}}`,
			assert: func(t *testing.T, p acp.ElicitationPropertySchema) {
				if p.Array == nil || p.Array.Items.String == nil ||
					len(p.Array.Items.String.Enum) != 2 {
					t.Fatalf("property = %+v, want array of string enum", p)
				}
			},
		},
		{
			name:       "array items without type",
			properties: `{"q0":{"type":"array","items":{"enum":["a","b"]}}}`,
			assert: func(t *testing.T, p acp.ElicitationPropertySchema) {
				if p.Array == nil || p.Array.Items.String == nil ||
					len(p.Array.Items.String.Enum) != 2 {
					t.Fatalf("property = %+v, want array of string enum", p)
				}
			},
		},
		{
			name:       "array items oneOf becomes anyOf",
			properties: `{"q0":{"type":"array","items":{"oneOf":[{"const":"a","title":"A"}]}}}`,
			assert: func(t *testing.T, p acp.ElicitationPropertySchema) {
				if p.Array == nil || p.Array.Items.Titled == nil ||
					len(p.Array.Items.Titled.AnyOf) != 1 {
					t.Fatalf("property = %+v, want titled array items", p)
				}
			},
		},
		{
			name:       "anyOf implies string and moves to oneOf",
			properties: `{"q0":{"anyOf":[{"const":"a","title":"A"}]}}`,
			assert: func(t *testing.T, p acp.ElicitationPropertySchema) {
				if p.String == nil || len(p.String.OneOf) != 1 {
					t.Fatalf("property = %+v, want string with one oneOf option", p)
				}
			},
		},
		{
			name:       "typed string with anyOf moves to oneOf",
			properties: `{"q0":{"type":"string","anyOf":[{"const":"a","title":"A"}]}}`,
			assert: func(t *testing.T, p acp.ElicitationPropertySchema) {
				if p.String == nil || len(p.String.OneOf) != 1 {
					t.Fatalf("property = %+v, want string with one oneOf option", p)
				}
			},
		},
		{
			name:       "typed array items oneOf becomes anyOf",
			properties: `{"q0":{"type":"array","items":{"type":"string","oneOf":[{"const":"a","title":"A"}]}}}`,
			assert: func(t *testing.T, p acp.ElicitationPropertySchema) {
				if p.Array == nil || p.Array.Items.Titled == nil ||
					len(p.Array.Items.Titled.AnyOf) != 1 {
					t.Fatalf("property = %+v, want titled array items", p)
				}
			},
		},
		{
			name:       "array items anyOf",
			properties: `{"q0":{"type":"array","items":{"anyOf":[{"const":"a","title":"A"}]}}}`,
			assert: func(t *testing.T, p acp.ElicitationPropertySchema) {
				if p.Array == nil || p.Array.Items.Titled == nil ||
					len(p.Array.Items.Titled.AnyOf) != 1 {
					t.Fatalf("property = %+v, want titled array items", p)
				}
			},
		},
		{
			name:          "default implies string",
			properties:    `{"q0":{"default":"x","title":"Pick"}}`,
			wantNoOptions: true,
			assert: func(t *testing.T, p acp.ElicitationPropertySchema) {
				if p.String == nil {
					t.Fatalf("property = %+v, want string", p)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := decodeNormalizedElicitation(t, elicitationLine(tc.properties))
			prop, ok := req.Form.RequestedSchema.Properties["q0"]
			if !ok {
				t.Fatal("q0 was dropped")
			}
			tc.assert(t, prop)

			// The card builder must still see the options after the typed
			// round trip; a decode that loses them would show an empty card.
			questions := buildElicitationQuestions(req.Form.RequestedSchema)
			if len(questions) != 1 {
				t.Fatalf("questions = %+v, want one question", questions)
			}
			if !tc.wantNoOptions && len(questions[0].Options) == 0 {
				t.Fatalf("questions = %+v, want options", questions)
			}
		})
	}
}

func TestNormalizeElicitationSchema_DropsUnrenderableProperties(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":1,"method":"elicitation/create","params":{` +
		`"sessionId":"s1","mode":"form","message":"m","requestedSchema":{` +
		`"type":"object","properties":{"q0":null,` +
		`"q1":{"oneOf":[{"const":1,"title":"number"}]},` +
		`"q2":{"type":"string","title":"ok"},` +
		`"q3":{"type":"array","items":{"type":"number"}},` +
		`"q4":{"type":"array"}},"required":["q0","q1","q2","q3","q4"]}}}`)
	req := decodeNormalizedElicitation(t, line)

	if _, ok := req.Form.RequestedSchema.Properties["q0"]; ok {
		t.Fatal("null property must be dropped")
	}
	if _, ok := req.Form.RequestedSchema.Properties["q1"]; ok {
		t.Fatal("numeric oneOf must be dropped")
	}
	if _, ok := req.Form.RequestedSchema.Properties["q2"]; !ok {
		t.Fatal("valid property must be kept")
	}
	for _, name := range []string{"q3", "q4"} {
		if _, ok := req.Form.RequestedSchema.Properties[name]; ok {
			t.Fatalf("%s must be dropped (unrenderable array)", name)
		}
	}
	if len(req.Form.RequestedSchema.Required) != 1 || req.Form.RequestedSchema.Required[0] != "q2" {
		t.Fatalf("required = %v, want [q2]", req.Form.RequestedSchema.Required)
	}
}

func TestNormalizeElicitationSchema_LeavesOtherLinesAlone(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`),
		[]byte(`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"s"}}`),
		[]byte(`{"jsonrpc":"2.0","id":3,"method":"elicitation/create","params":{"sessionId":"s"}}`),
	}
	for _, line := range lines {
		if got := normalizeElicitationSchema(line); !bytes.Equal(got, line) {
			t.Fatalf("line changed:\n got %s\nwant %s", got, line)
		}
	}
}

func TestNormalizeElicitationSchema_LeavesTypedStringPropertyAlone(t *testing.T) {
	line := elicitationLine(`{"q0":{"type":"string","title":"Pick","oneOf":[{"const":"a","title":"A"}]}}`)
	if got := normalizeElicitationSchema(line); !bytes.Equal(got, line) {
		t.Fatalf("already-valid line changed:\n got %s\nwant %s", got, line)
	}
}

// The filter is the only thing standing between a lenient agent schema and the
// SDK's strict union. Drive the real pipe end to end: without the rewrite the
// request is answered -32602 and the card never appears.
func TestElicitationSchemaWithoutTypeReachesClient(t *testing.T) {
	agentToClientR, agentToClientW := io.Pipe()
	_, clientToAgentW := io.Pipe()

	client := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 8)
	client.RegisterSession("acp-sess-1", ch)
	defer client.UnregisterSession("acp-sess-1")

	filter := newACPStdoutFilter(agentToClientR)
	defer filter.Close()
	conn := acp.NewClientSideConnection(client, clientToAgentW, filter)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	request := `{"jsonrpc":"2.0","id":7,"method":"elicitation/create","params":{` +
		`"sessionId":"acp-sess-1","mode":"form","message":"Questions","requestedSchema":{` +
		`"type":"object","properties":{"q0":{"enum":["a","b"],"title":"Pick"}}}}}`
	go func() {
		_, _ = io.WriteString(agentToClientW, request+"\n")
	}()

	var card StreamEvent
	select {
	case card = <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no card: the filter did not normalize the schema")
	}
	if card.Tool == nil || card.Tool.Name != "AskUserQuestion" {
		t.Fatalf("event = %+v, want the ask card", card)
	}
	var payload struct {
		Questions []struct {
			Options []struct {
				Value string `json:"value"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal([]byte(card.Tool.Input), &payload); err != nil {
		t.Fatalf("card input is not JSON: %v", err)
	}
	if len(payload.Questions) != 1 || len(payload.Questions[0].Options) != 2 {
		t.Fatalf("questions = %+v, want one question with two options", payload.Questions)
	}
}
