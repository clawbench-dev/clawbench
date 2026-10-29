package common

import (
	"fmt"
	"strings"
	"testing"

	"clawbench/internal/model"
)

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
		// Multi-line messages must be preserved in full, not truncated to the first line.
		{"@a1b2c3d4 line1\nline2", "a1b2c3d4", "line1\nline2", true},
		{"@a1b2c3d4 line1\nline2\nline3", "a1b2c3d4", "line1\nline2\nline3", true},
		{"@a1b2c3d4\nline1\nline2", "a1b2c3d4", "line1\nline2", true}, // newline right after ID
		{"@a1b2c3d4 line1\r\nline2", "a1b2c3d4", "line1\r\nline2", true},
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

// TestResolveTarget covers the branch that picks between an explicit
// "@{shortID}" and the user's sticky session. The sticky path is what makes a
// plain message (or a bare file) land in the right place, so each of its
// failure modes must produce a distinct, user-actionable error rather than a
// silent send into nowhere.
func TestResolveTarget(t *testing.T) {
	sessions := []SessionInfo{
		{ID: "a1b2c3d4e5f60001", Title: "Running Session"},
		{ID: "b1b2c3d4e5f60002", Title: "Sticky Session"},
	}

	t.Run("explicit short id wins over sticky", func(t *testing.T) {
		m := &mockMessenger{sessions: sessions, running: map[string]bool{"a1b2c3d4e5f60001": true}}
		id, title, err := ResolveTarget(m, Route{Kind: RouteToSession, ShortID: "a1b2c3d4"}, "b1b2c3d4e5f60002")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "a1b2c3d4e5f60001" || title != "Running Session" {
			t.Errorf("got (%q, %q), want the explicitly named session", id, title)
		}
	})

	t.Run("sticky session is used when no short id", func(t *testing.T) {
		m := &mockMessenger{sessions: sessions, running: map[string]bool{}}
		id, title, err := ResolveTarget(m, Route{Kind: RouteToSession}, "b1b2c3d4e5f60002")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "b1b2c3d4e5f60002" || title != "Sticky Session" {
			t.Errorf("got (%q, %q), want the sticky session", id, title)
		}
	})

	t.Run("no sticky target is an error", func(t *testing.T) {
		m := &mockMessenger{sessions: sessions, running: map[string]bool{}}
		_, _, err := ResolveTarget(m, Route{Kind: RouteToSession}, "")
		if err == nil {
			t.Fatal("expected an error when there is no target at all")
		}
	})

	t.Run("nil messenger with a sticky target is an error", func(t *testing.T) {
		_, _, err := ResolveTarget(nil, Route{Kind: RouteToSession}, "b1b2c3d4e5f60002")
		if err == nil {
			t.Fatal("expected an error when the session messenger is unavailable")
		}
	})

	// A sticky ID pointing at a deleted/archived session must fail loudly: the
	// user has to be told to pick again, not have the message vanish.
	t.Run("sticky session no longer exists", func(t *testing.T) {
		m := &mockMessenger{sessions: sessions, running: map[string]bool{}}
		_, _, err := ResolveTarget(m, Route{Kind: RouteToSession}, "deadbeef00000000")
		if err == nil {
			t.Fatal("expected an error for an unavailable sticky session")
		}
		if !strings.Contains(err.Error(), "/ls") {
			t.Errorf("error should point the user at /ls, got %v", err)
		}
	})
}

// mockMessenger implements SessionMessenger for testing.
type mockMessenger struct {
	sessions []SessionInfo
	running  map[string]bool

	lastReply       string
	lastReplyOK     bool
	wasRunning      bool
	cleared         int
	projects        []ProjectRef
	newSessionID    string
	newSessionTitle string
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

func (m *mockMessenger) IsSessionRunning(sessionID string) bool { return m.running[sessionID] }

func (m *mockMessenger) GetSessionInfo(sessionID string) (SessionInfo, error) {
	for _, s := range m.sessions {
		if s.ID == sessionID {
			return s, nil
		}
	}
	return SessionInfo{}, fmt.Errorf("session %s not found", sessionID)
}

func (m *mockMessenger) SendMessageToSession(string, string, []model.FileEntry) error { return nil }

func TestClassifyIncoming_NewCommands(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		sticky    string
		wantKind  RouteKind
		wantArg   string
		wantShort string
		wantMsg   string
	}{
		{
			name: "show without arg", text: "/show", sticky: "s1",
			wantKind: RouteShowLastReply,
		},
		{
			name: "show with id", text: "/show a1b2c3d4", sticky: "s1",
			wantKind: RouteShowLastReply, wantArg: "a1b2c3d4",
		},
		{
			name: "show with padded id", text: "  /show   a1b2c3d4  ", sticky: "",
			wantKind: RouteShowLastReply, wantArg: "a1b2c3d4",
		},
		{
			name: "stop without arg", text: "/stop", sticky: "s1",
			wantKind: RouteStopSession,
		},
		{
			name: "stop with id", text: "/stop a1b2c3d4", sticky: "",
			wantKind: RouteStopSession, wantArg: "a1b2c3d4",
		},
		{
			name: "new without arg lists projects", text: "/new", sticky: "s1",
			wantKind: RouteNewSession, wantArg: "",
		},
		{
			name: "new with index", text: "/new 2", sticky: "s1",
			wantKind: RouteNewSession, wantArg: "2",
		},
		{
			// A path may contain spaces; the arg must be the whole remainder,
			// not the first whitespace-delimited token.
			name: "new with path containing spaces", text: "/new /home/u/my project", sticky: "",
			wantKind: RouteNewSession, wantArg: "/home/u/my project",
		},
		{
			// "@id" wins over any command word: this is the escape hatch that
			// sends the literal text "/new" to a session.
			name: "explicit target beats command", text: "@a1b2c3d4 /new", sticky: "",
			wantKind: RouteToSession, wantShort: "a1b2c3d4", wantMsg: "/new",
		},
		{
			// A command word embedded mid-sentence is ordinary text.
			name: "command word embedded is plain text", text: "please /stop it", sticky: "s1",
			wantKind: RouteToSession, wantMsg: "please /stop it",
		},
		{
			// Commands are lowercase only.
			name: "uppercase is not a command", text: "/SHOW", sticky: "s1",
			wantKind: RouteToSession, wantMsg: "/SHOW",
		},
		{
			// Unknown slash commands fall through to the session, so a user can
			// still type them to the AI.
			name: "unknown slash command goes to session", text: "/help me", sticky: "s1",
			wantKind: RouteToSession, wantMsg: "/help me",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyIncoming(tt.text, tt.sticky)
			if got.Kind != tt.wantKind {
				t.Fatalf("Kind = %v, want %v", got.Kind, tt.wantKind)
			}
			if got.Arg != tt.wantArg {
				t.Errorf("Arg = %q, want %q", got.Arg, tt.wantArg)
			}
			if got.ShortID != tt.wantShort {
				t.Errorf("ShortID = %q, want %q", got.ShortID, tt.wantShort)
			}
			if got.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMsg)
			}
		})
	}
}

func (m *mockMessenger) LastAssistantReply(string) (string, bool, error) {
	return m.lastReply, m.lastReplyOK, nil
}

func (m *mockMessenger) StopAndClearSession(string) (bool, int, error) {
	return m.wasRunning, m.cleared, nil
}

func (m *mockMessenger) ListProjects() ([]ProjectRef, error) {
	return m.projects, nil
}

func (m *mockMessenger) CreateSessionInProject(string) (string, string, error) {
	return m.newSessionID, m.newSessionTitle, nil
}

func TestResolveCommandTarget(t *testing.T) {
	m := &mockMessenger{
		sessions: []SessionInfo{
			{ID: "a1b2c3d4-1111", Title: "T1"},
			{ID: "b2c3d4e5-2222", Title: "T2"},
		},
		running: map[string]bool{"b2c3d4e5-2222": true},
	}

	// No arg → the sticky session, read back for its title.
	id, title, err := ResolveCommandTarget(m, "", "a1b2c3d4-1111")
	if err != nil || id != "a1b2c3d4-1111" || title != "T1" {
		t.Fatalf("sticky resolve = %q %q %v", id, title, err)
	}

	// Explicit prefix wins over the sticky target.
	id, title, err = ResolveCommandTarget(m, "b2c3d4e5", "a1b2c3d4-1111")
	if err != nil || id != "b2c3d4e5-2222" || title != "T2" {
		t.Fatalf("prefix resolve = %q %q %v", id, title, err)
	}

	// No arg and no sticky target is an error, not a silent empty session.
	if _, _, err := ResolveCommandTarget(m, "", ""); err == nil {
		t.Fatal("expected error when neither arg nor sticky target is set")
	}

	// Unknown prefix propagates the not-found error.
	if _, _, err := ResolveCommandTarget(m, "ffffffff", "a1b2c3d4-1111"); err == nil {
		t.Fatal("expected error for unknown prefix")
	}
}

func TestClassifyIncoming_ShowStopWithoutTargetOrSticky(t *testing.T) {
	// No argument and no sticky target: the user has nothing to act on, so the
	// command must degrade to the standard no-target hint instead of surfacing
	// an internal "no target session" error.
	for _, cmd := range []string{"/show", "/stop"} {
		got := ClassifyIncoming(cmd, "")
		if got.Kind != RouteNoTarget {
			t.Errorf("%s with no sticky = %v, want RouteNoTarget", cmd, got.Kind)
		}
	}

	// An explicit argument still routes even without a sticky target.
	if got := ClassifyIncoming("/show a1b2c3d4", ""); got.Kind != RouteShowLastReply {
		t.Errorf("/show with id = %v, want RouteShowLastReply", got.Kind)
	}
	if got := ClassifyIncoming("/stop a1b2c3d4", ""); got.Kind != RouteStopSession {
		t.Errorf("/stop with id = %v, want RouteStopSession", got.Kind)
	}
}
