package dingtalk

import (
	"fmt"
	"strings"
	"testing"
)

func TestResolveShortSessionID_NoMessenger(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()
	sessionMessenger = nil

	_, _, err := resolveShortSessionID("a1b2c3d4")
	if err == nil {
		t.Error("expected error when no session messenger")
	}
}

func TestResolveShortSessionID_RunningFirst(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Running Session"},
		},
		allSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Running Session"},
			{ID: "a1b2c3d4-2222-2222-2222-222222222222", Title: "Old Session"},
		},
	}

	id, _, err := resolveShortSessionID("a1b2c3d4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("expected running session, got %q", id)
	}
}

func TestResolveShortSessionID_ConflictInRunning(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111"},
			{ID: "a1b2c3d4-2222-2222-2222-222222222222"},
		},
	}

	_, _, err := resolveShortSessionID("a1b2c3d4")
	if err == nil {
		t.Error("expected error for conflicting short IDs in running sessions")
	}
}

func TestResolveShortSessionID_FallbackToAll(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{},
		allSessions: []SessionInfo{
			{ID: "a1b2c3d4-2222-2222-2222-222222222222", Title: "Old Session"},
		},
	}

	id, _, err := resolveShortSessionID("a1b2c3d4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "a1b2c3d4-2222-2222-2222-222222222222" {
		t.Errorf("expected all-sessions fallback, got %q", id)
	}
}

func TestResolveShortSessionID_NotFound(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{},
		allSessions:     []SessionInfo{},
	}

	_, _, err := resolveShortSessionID("deadbeef")
	if err == nil {
		t.Error("expected error for not found session")
	}
}

func TestResolveShortSessionID_CaseInsensitive(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{},
		allSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Session"},
		},
	}

	id, _, err := resolveShortSessionID("A1B2C3D4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("expected case-insensitive match, got %q", id)
	}
}

func TestParseSessionCommand(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantID    string
		wantMsg   string
		wantMatch bool
	}{
		{"standard", "@a1b2c3d4 继续修改", "a1b2c3d4", "继续修改", true},
		{"no message", "@a1b2c3d4", "a1b2c3d4", "", true},
		{"extra spaces", "@a1b2c3d4   hello world", "a1b2c3d4", "hello world", true},
		{"not a command", "hello world", "", "", false},
		{"at but wrong format", "@abc hello", "", "", false},
		{"exactly 8 hex", "@deadbeef test", "deadbeef", "test", true},
		{"uppercase hex", "@A1B2C3D4 test", "A1B2C3D4", "test", true},
		{"7 chars not match", "@abcdef1 test", "", "", false},
		{"9 chars now matches", "@a1b2c3d4e test", "a1b2c3d4e", "test", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, msg, ok := parseSessionCommand(tt.input)
			if ok != tt.wantMatch {
				t.Errorf("parseSessionCommand(%q) ok = %v, want %v", tt.input, ok, tt.wantMatch)
			}
			if ok {
				if id != tt.wantID {
					t.Errorf("id = %q, want %q", id, tt.wantID)
				}
				if msg != tt.wantMsg {
					t.Errorf("msg = %q, want %q", msg, tt.wantMsg)
				}
			}
		})
	}
}

func TestResolveShortSessionID_ConflictInAllSessions(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{},
		allSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111"},
			{ID: "a1b2c3d4-2222-2222-2222-222222222222"},
		},
	}

	_, _, err := resolveShortSessionID("a1b2c3d4")
	if err == nil {
		t.Error("expected error for conflicting short IDs in all sessions")
	}
}

func TestResolveShortSessionID_FindRunningError(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessengerWithErr{
		runningErr: fmt.Errorf("db error"),
	}

	_, _, err := resolveShortSessionID("a1b2c3d4")
	if err == nil {
		t.Error("expected error when FindSessionsByPrefix(running) fails")
	}
	if !strings.Contains(err.Error(), "find running sessions") {
		t.Errorf("expected 'find running sessions' in error, got %q", err.Error())
	}
}

func TestResolveShortSessionID_FindAllError(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessengerWithErr{
		allErr: fmt.Errorf("db error"),
	}

	_, _, err := resolveShortSessionID("a1b2c3d4")
	if err == nil {
		t.Error("expected error when FindSessionsByPrefix(all) fails")
	}
	if !strings.Contains(err.Error(), "find sessions") {
		t.Errorf("expected 'find sessions' in error, got %q", err.Error())
	}
}

func TestFormatSessionLabel_EmptyTitle(t *testing.T) {
	label := formatSessionLabel("a1b2c3d4-1111-1111-1111-111111111111", "")
	if label != "会话 a1b2c3d4" {
		t.Errorf("expected '会话 a1b2c3d4', got %q", label)
	}
}

func TestFormatSessionLabel_WithTitle(t *testing.T) {
	label := formatSessionLabel("a1b2c3d4-1111-1111-1111-111111111111", "My Session")
	if label != "My Session" {
		t.Errorf("expected 'My Session', got %q", label)
	}
}

// mockSessionMessengerWithErr supports error returns for FindSessionsByPrefix.
type mockSessionMessengerWithErr struct {
	runningSessions []SessionInfo
	allSessions     []SessionInfo
	runningErr      error
	allErr          error
}

func (m *mockSessionMessengerWithErr) FindSessionsByPrefix(prefix string, runningOnly bool) ([]SessionInfo, error) {
	if runningOnly && m.runningErr != nil {
		return nil, m.runningErr
	}
	if !runningOnly && m.allErr != nil {
		return nil, m.allErr
	}
	src := m.allSessions
	if runningOnly {
		src = m.runningSessions
	}
	lowerPrefix := strings.ToLower(prefix)
	var result []SessionInfo
	for _, s := range src {
		if len(s.ID) >= len(lowerPrefix) && strings.ToLower(s.ID[:len(lowerPrefix)]) == lowerPrefix {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockSessionMessengerWithErr) ListRecentSessions(limit int) ([]SessionInfo, error) {
	return m.allSessions, nil
}

func (m *mockSessionMessengerWithErr) IsSessionRunning(sessionID string) bool {
	for _, s := range m.runningSessions {
		if s.ID == sessionID {
			return true
		}
	}
	return false
}

func (m *mockSessionMessengerWithErr) EnqueueMessage(sessionID, message string) error { return nil }
func (m *mockSessionMessengerWithErr) ClearQueue(sessionID string)                    {}
func (m *mockSessionMessengerWithErr) SendMessageToSession(sessionID, message string) error {
	return nil
}

// mockSessionMessenger implements SessionMessenger for testing.
type mockSessionMessenger struct {
	runningSessions  []SessionInfo
	allSessions      []SessionInfo
	sendErr          error
	listErr          error
	EnqueueMessageFn func(sid, msg string) error
	SendMessageFn    func(sid, msg string) error
}

func (m *mockSessionMessenger) FindSessionsByPrefix(prefix string, runningOnly bool) ([]SessionInfo, error) {
	src := m.allSessions
	if runningOnly {
		src = m.runningSessions
	}
	lowerPrefix := strings.ToLower(prefix)
	var result []SessionInfo
	for _, s := range src {
		if len(s.ID) >= len(lowerPrefix) && strings.ToLower(s.ID[:len(lowerPrefix)]) == lowerPrefix {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockSessionMessenger) ListRecentSessions(limit int) ([]SessionInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.allSessions, nil
}

func (m *mockSessionMessenger) IsSessionRunning(sessionID string) bool {
	for _, s := range m.runningSessions {
		if s.ID == sessionID {
			return true
		}
	}
	return false
}

func (m *mockSessionMessenger) EnqueueMessage(sessionID, message string) error {
	if m.EnqueueMessageFn != nil {
		return m.EnqueueMessageFn(sessionID, message)
	}
	return m.sendErr
}

func (m *mockSessionMessenger) ClearQueue(sessionID string) {}

func (m *mockSessionMessenger) SendMessageToSession(sessionID, message string) error {
	if m.SendMessageFn != nil {
		return m.SendMessageFn(sessionID, message)
	}
	return m.sendErr
}
