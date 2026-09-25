package tunnel

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDecodeControl_RoundTripsEveryType(t *testing.T) {
	cases := []ControlMessage{
		{Type: MsgBind, Port: 8080},
		{Type: MsgBind}, // port 0 = OS-assigned, omitted by omitempty
		{Type: MsgBound, Port: 34567},
		{Type: MsgBindErr, Port: 20000, Code: BindErrReservedOrTaken, Msg: "reserved"},
		{Type: MsgIncoming, Port: 8080, Token: "abc123"},
		{Type: MsgUnbind, Port: 8080},
		{Type: MsgUnbound, Port: 8080},
		{Type: MsgPing},
		{Type: MsgPong},
	}
	for _, want := range cases {
		t.Run(want.Type, func(t *testing.T) {
			line := EncodeControl(want)
			if len(line) == 0 || line[len(line)-1] != '\n' {
				t.Fatalf("EncodeControl must terminate the line with a newline, got %q", line)
			}
			got, err := DecodeControl(line)
			if err != nil {
				t.Fatalf("DecodeControl(%q) error: %v", line, err)
			}
			if got != want {
				t.Fatalf("round trip mismatch: got %+v, want %+v", got, want)
			}
		})
	}
}

func TestEncodeControl_OmitsZeroValuedOptionalFields(t *testing.T) {
	// A `ping` must not carry port/code/msg/token: the client parses these
	// structurally and a stray "port":0 would look like a bind request.
	line := string(EncodeControl(ControlMessage{Type: MsgPing}))
	if strings.TrimSpace(line) != `{"type":"ping"}` {
		t.Fatalf("ping must serialize to a bare type, got %q", line)
	}
}

func TestDecodeControl_AcceptsLineWithoutTrailingNewline(t *testing.T) {
	got, err := DecodeControl([]byte(`{"type":"ping"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Type != MsgPing {
		t.Fatalf("got type %q, want %q", got.Type, MsgPing)
	}
}

func TestDecodeControl_AcceptsCRLF(t *testing.T) {
	got, err := DecodeControl([]byte("{\"type\":\"pong\"}\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Type != MsgPong {
		t.Fatalf("got type %q, want %q", got.Type, MsgPong)
	}
}

func TestDecodeControl_MalformedLinesAreErrorsNotPanics(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"truncated json", `{"type":"bind"`},
		{"not json", "hello world"},
		{"json but not object", `["bind"]`},
		{"missing type", `{"port":8080}`},
		{"unknown type", `{"type":"explode","port":8080}`},
		{"wrong field type", `{"type":"bind","port":"8080"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeControl([]byte(tc.line))
			if err == nil {
				t.Fatalf("DecodeControl(%q) must fail", tc.line)
			}
			if !errors.Is(err, ErrMalformedControl) {
				t.Fatalf("error must wrap ErrMalformedControl, got %v", err)
			}
		})
	}
}

// TestDecodeControl_UnknownFieldsAreIgnored pins forward compatibility: a newer
// client adding a field must not break an older server.
func TestDecodeControl_UnknownFieldsAreIgnored(t *testing.T) {
	got, err := DecodeControl([]byte(`{"type":"bind","port":8080,"future":true}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Port != 8080 {
		t.Fatalf("got port %d, want 8080", got.Port)
	}
}

// TestControlMessage_JSONFieldNames pins the wire contract (design doc §4.4):
// the client and server must agree on these exact keys.
func TestControlMessage_JSONFieldNames(t *testing.T) {
	raw, err := json.Marshal(ControlMessage{Type: MsgIncoming, Port: 9000, Token: "t"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"type", "port", "token"} {
		if _, ok := m[key]; !ok {
			t.Errorf("incoming message must carry key %q, got %v", key, m)
		}
	}
}
