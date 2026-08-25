package common

import "testing"

func TestShortSessionID(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"a1b2c3d4e5f6", "a1b2c3d4"},
		{"short", "short"},
		{"", ""},
		{"12345678", "12345678"},
	}
	for _, tt := range tests {
		got := ShortSessionID(tt.id)
		if got != tt.want {
			t.Errorf("ShortSessionID(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestFormatSessionLabel(t *testing.T) {
	tests := []struct {
		id    string
		title string
		want  string
	}{
		{"a1b2c3d4e5f6", "My Session", "My Session"},
		{"a1b2c3d4e5f6", "", "会话 a1b2c3d4"},
		{"short", "", "会话 short"},
	}
	for _, tt := range tests {
		got := FormatSessionLabel(tt.id, tt.title)
		if got != tt.want {
			t.Errorf("FormatSessionLabel(%q, %q) = %q, want %q", tt.id, tt.title, got, tt.want)
		}
	}
}

func TestParseSessionCommand(t *testing.T) {
	tests := []struct {
		text      string
		wantID    string
		wantMsg   string
		wantMatch bool
	}{
		{"@a1b2c3d4 hello world", "a1b2c3d4", "hello world", true},
		{"@a1b2c3d4", "a1b2c3d4", "", true},
		{"@a1b2c3d4e5f6 some msg", "a1b2c3d4e5f6", "some msg", true},
		{"hello", "", "", false},
		{"@short no", "", "", false},                     // less than 8 hex chars
		{"@A1B2C3D4 hello", "A1B2C3D4", "hello", true},   // uppercase
		{" @a1b2c3d4 hello ", "a1b2c3d4", "hello", true}, // leading/trailing space
	}
	for _, tt := range tests {
		gotID, gotMsg, gotMatch := ParseSessionCommand(tt.text)
		if gotID != tt.wantID || gotMsg != tt.wantMsg || gotMatch != tt.wantMatch {
			t.Errorf("ParseSessionCommand(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.text, gotID, gotMsg, gotMatch, tt.wantID, tt.wantMsg, tt.wantMatch)
		}
	}
}

func TestResolveShortSessionID(t *testing.T) {
	mockSessions := []SessionInfo{
		{ID: "a1b2c3d4e5f60001", Title: "Running Session"},
		{ID: "a1b2c3d4e5f60002", Title: "Other Session"},
		{ID: "b1b2c3d4e5f60001", Title: "Different"},
	}

	t.Run("found running", func(t *testing.T) {
		m := &mockMessenger{sessions: mockSessions, running: map[string]bool{"a1b2c3d4e5f60001": true}}
		id, title, err := ResolveShortSessionID(m, "a1b2c3d4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "a1b2c3d4e5f60001" {
			t.Errorf("id = %q, want a1b2c3d4e5f60001", id)
		}
		if title != "Running Session" {
			t.Errorf("title = %q, want Running Session", title)
		}
	})

	t.Run("found not running", func(t *testing.T) {
		m := &mockMessenger{sessions: mockSessions, running: map[string]bool{}}
		id, _, err := ResolveShortSessionID(m, "b1b2c3d4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "b1b2c3d4e5f60001" {
			t.Errorf("id = %q, want b1b2c3d4e5f60001", id)
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		m := &mockMessenger{sessions: mockSessions, running: map[string]bool{}}
		_, _, err := ResolveShortSessionID(m, "a1b2c3d4")
		if err == nil {
			t.Fatal("expected error for ambiguous match")
		}
	})

	t.Run("not found", func(t *testing.T) {
		m := &mockMessenger{sessions: mockSessions, running: map[string]bool{}}
		_, _, err := ResolveShortSessionID(m, "zzzzzzzz")
		if err == nil {
			t.Fatal("expected error for not found")
		}
	})

	t.Run("nil messenger", func(t *testing.T) {
		_, _, err := ResolveShortSessionID(nil, "a1b2c3d4")
		if err == nil {
			t.Fatal("expected error for nil messenger")
		}
	})
}

// mockMessenger implements SessionMessenger for testing.
type mockMessenger struct {
	sessions []SessionInfo
	running  map[string]bool
}

func (m *mockMessenger) FindSessionsByPrefix(prefix string, runningOnly bool) ([]SessionInfo, error) {
	var result []SessionInfo
	for _, s := range m.sessions {
		if len(s.ID) >= len(prefix) && s.ID[:len(prefix)] == prefix {
			if runningOnly && !m.running[s.ID] {
				continue
			}
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockMessenger) ListRecentSessions(limit int) ([]SessionInfo, error) {
	if limit > len(m.sessions) {
		limit = len(m.sessions)
	}
	return m.sessions[:limit], nil
}

func (m *mockMessenger) IsSessionRunning(sessionID string) bool    { return m.running[sessionID] }
func (m *mockMessenger) SendMessageToSession(string, string) error { return nil }
