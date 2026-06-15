package terminal

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------- ClientMessage JSON round-trip ----------

func TestClientMessage_Input(t *testing.T) {
	msg := ClientMessage{
		Type: "input",
		Data: "ls -la\n",
	}
	data, err := json.Marshal(msg)
	assert.NoError(t, err)

	var decoded ClientMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "input", decoded.Type)
	assert.Equal(t, "ls -la\n", decoded.Data)
}

func TestClientMessage_Resize(t *testing.T) {
	msg := ClientMessage{
		Type: "resize",
		Cols: 120,
		Rows: 40,
	}
	data, err := json.Marshal(msg)
	assert.NoError(t, err)

	var decoded ClientMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "resize", decoded.Type)
	assert.Equal(t, uint16(120), decoded.Cols)
	assert.Equal(t, uint16(40), decoded.Rows)
}

func TestClientMessage_Close(t *testing.T) {
	msg := ClientMessage{Type: "close"}
	data, err := json.Marshal(msg)
	assert.NoError(t, err)

	var decoded ClientMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "close", decoded.Type)
}

// ---------- ServerMessage JSON round-trip ----------

func TestServerMessage_Output(t *testing.T) {
	msg := ServerMessage{
		Type:      "output",
		SessionID: "abc123",
		Data:      "\x1b[32mhello\x1b[0m",
	}
	data, err := json.Marshal(msg)
	assert.NoError(t, err)

	var decoded ServerMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "output", decoded.Type)
	assert.Equal(t, "abc123", decoded.SessionID)
	assert.Equal(t, "\x1b[32mhello\x1b[0m", decoded.Data)
}

func TestServerMessage_Error(t *testing.T) {
	msg := ServerMessage{
		Type:    "error",
		ErrCode: ErrCodeSessionLimit,
		Message: "max sessions (10) reached",
	}
	data, err := json.Marshal(msg)
	assert.NoError(t, err)

	var decoded ServerMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "error", decoded.Type)
	assert.Equal(t, ErrCodeSessionLimit, decoded.ErrCode)
	assert.Equal(t, "max sessions (10) reached", decoded.Message)
}

func TestServerMessage_Status(t *testing.T) {
	msg := ServerMessage{
		Type:      "status",
		SessionID: "xyz",
		Cwd:       "/home/user/project",
		Running:   true,
	}
	data, err := json.Marshal(msg)
	assert.NoError(t, err)

	var decoded ServerMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "status", decoded.Type)
	assert.Equal(t, "xyz", decoded.SessionID)
	assert.Equal(t, "/home/user/project", decoded.Cwd)
	assert.True(t, decoded.Running)
}

func TestServerMessage_Exit(t *testing.T) {
	msg := ServerMessage{
		Type: "exit",
		Code: 0,
	}
	data, err := json.Marshal(msg)
	assert.NoError(t, err)

	var decoded ServerMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "exit", decoded.Type)
	assert.Equal(t, 0, decoded.Code)
}

// ---------- Error code constants ----------

func TestErrorCodeConstants(t *testing.T) {
	assert.Equal(t, "shell_start_failed", ErrCodeShellFailed)
	assert.Equal(t, "session_limit", ErrCodeSessionLimit)
	assert.Equal(t, "platform_unsupported", ErrCodePlatformUnsupported)
	assert.Equal(t, 4001, StatusReplaced)
}

// ---------- OmitEmpty behavior ----------

func TestClientMessage_OmitEmpty(t *testing.T) {
	msg := ClientMessage{Type: "input", Data: "x"}
	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	// Cols and Rows should be omitted when zero
	assert.NotContains(t, string(data), "cols")
	assert.NotContains(t, string(data), "rows")
}

func TestServerMessage_OmitEmpty(t *testing.T) {
	msg := ServerMessage{Type: "output", Data: "hello"}
	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	// Optional fields should be omitted
	assert.NotContains(t, string(data), "sessionId")
	assert.NotContains(t, string(data), "errcode")
	assert.NotContains(t, string(data), "code")
}
