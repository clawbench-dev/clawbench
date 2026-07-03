package ws

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestGenerateEventID(t *testing.T) {
	// IDs should start with "evt_"
	id := GenerateEventID()
	if !strings.HasPrefix(id, "evt_") {
		t.Errorf("expected ID to start with 'evt_', got %q", id)
	}

	// IDs should contain the serverInstanceID
	expectedPrefix := fmt.Sprintf("evt_%d_", serverInstanceID)
	if !strings.HasPrefix(id, expectedPrefix) {
		t.Errorf("expected ID to contain serverInstanceID prefix %q, got %q", expectedPrefix, id)
	}

	// 1000 generated IDs should all be unique
	seen := make(map[string]struct{}, 1000)
	seen[id] = struct{}{}
	for i := 1; i < 1000; i++ {
		id := GenerateEventID()
		if _, ok := seen[id]; ok {
			t.Errorf("duplicate event ID: %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestServerMessageJSON(t *testing.T) {
	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_123_456",
		Event: "session_update",
		Data: &SessionUpdateData{
			SessionID:      "sess-1",
			Status:         "completed",
			HasNewMessages: true,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ServerMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != "event" {
		t.Errorf("expected type 'event', got %q", decoded.Type)
	}
	if decoded.Event != "session_update" {
		t.Errorf("expected event 'session_update', got %q", decoded.Event)
	}
	if decoded.ID != "evt_123_456" {
		t.Errorf("expected id 'evt_123_456', got %q", decoded.ID)
	}
}

func TestClientMessageJSON(t *testing.T) {
	tests := []struct {
		name string
		msg  ClientMessage
		want string
	}{
		{
			name: "ack",
			msg:  ClientMessage{Type: "ack", ID: "evt_123"},
			want: `{"type":"ack","id":"evt_123"}`,
		},
		{
			name: "pong",
			msg:  ClientMessage{Type: "pong"},
			want: `{"type":"pong"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded ClientMessage
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if decoded.Type != tt.msg.Type {
				t.Errorf("expected type %q, got %q", tt.msg.Type, decoded.Type)
			}
		})
	}
}

func TestDataTypesJSON(t *testing.T) {
	t.Run("SessionUpdateData", func(t *testing.T) {
		d := SessionUpdateData{SessionID: "s1", Status: "running", HasNewMessages: true, ProjectPath: "/home/user/project"}
		data, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var decoded SessionUpdateData
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if decoded.SessionID != "s1" || decoded.Status != "running" || !decoded.HasNewMessages || decoded.ProjectPath != "/home/user/project" {
			t.Errorf("roundtrip failed: %+v", decoded)
		}
	})

	t.Run("SessionUpdateData_empty_project_path", func(t *testing.T) {
		d := SessionUpdateData{SessionID: "s1", Status: "running", HasNewMessages: true}
		data, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(data), "project_path") {
			t.Errorf("expected project_path to be omitted when empty, got %s", data)
		}
	})

	t.Run("TaskUpdateData", func(t *testing.T) {
		d := TaskUpdateData{TaskID: "t1", Status: "completed", ExecutionID: "e1", SessionID: "s1", ProjectPath: "/home/user/project"}
		data, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var decoded TaskUpdateData
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if decoded.TaskID != "t1" || decoded.Status != "completed" || decoded.ExecutionID != "e1" || decoded.SessionID != "s1" || decoded.ProjectPath != "/home/user/project" {
			t.Errorf("roundtrip failed: %+v", decoded)
		}
	})

	t.Run("QueueUpdateData", func(t *testing.T) {
		d := QueueUpdateData{SessionID: "s1", Count: 5}
		data, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var decoded QueueUpdateData
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if decoded.SessionID != "s1" || decoded.Count != 5 {
			t.Errorf("roundtrip failed: %+v", decoded)
		}
	})

	t.Run("TaskUpdateData_empty_optional_fields", func(t *testing.T) {
		d := TaskUpdateData{TaskID: "t2", Status: "failed"}
		data, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// ExecutionID, SessionID, ProjectPath should be omitted when empty
		for _, field := range []string{"execution_id", "session_id", "project_path"} {
			if strings.Contains(string(data), field) {
				t.Errorf("expected %s to be omitted, got %s", field, data)
			}
		}
	})
}

func TestServerMessagePing(t *testing.T) {
	msg := ServerMessage{Type: "ping"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"type":"ping"`) {
		t.Errorf("expected ping message, got %s", data)
	}
	// event and id should be omitted
	if strings.Contains(string(data), "event") {
		t.Errorf("expected event to be omitted in ping, got %s", data)
	}
}

func TestServerMessageDataWithMap(t *testing.T) {
	// Data is `any`, so it should accept arbitrary JSON objects
	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "custom_event",
		Data:  map[string]any{"key": "value", "num": 42},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"key":"value"`) {
		t.Errorf("expected custom data in message, got %s", data)
	}
}
