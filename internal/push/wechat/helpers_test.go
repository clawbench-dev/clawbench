package wechat

import (
	"errors"
	"net/http"
	"sync"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/push/common"

	ilink "github.com/openilink/openilink-sdk-go"
)

// newTestClient builds an iLink client pointed at baseURL.
//
// It uses the same error-translating doer as production, so a test that feeds a
// `{"ret":-1}` body sees a real error rather than a silent success.
func newTestClient(baseURL string) *ilink.Client {
	return ilink.NewClient(
		"tok",
		ilink.WithBaseURL(baseURL),
		ilink.WithHTTPClient(apiErrorDoer{inner: &http.Client{}}),
	)
}

// mockClientChecker implements common.ConnectedClientChecker for testing.
type mockClientChecker struct {
	hasConnected bool
}

func (m *mockClientChecker) HasConnectedClients() bool { return m.hasConnected }

// mockDB implements the package's DB interface for testing.
type mockDB struct {
	subscribers   []common.SubscriberInfo
	lastSessionID string
	// mu guards tokens: the monitor goroutine writes it while the test reads.
	mu          sync.Mutex
	tokens      map[string]string
	getErr      error
	setTokenErr error
}

func (m *mockDB) MergeConfigSubscribers(_ []string) {}
func (m *mockDB) GetSubscribers() ([]common.SubscriberInfo, error) {
	return m.subscribers, m.getErr
}
func (m *mockDB) UpsertSubscriber(_, _, _, _ string) error { return nil }
func (m *mockDB) DeleteSubscriber(_ string) error          { return nil }
func (m *mockDB) GetLastSessionID(_ string) (string, error) {
	return m.lastSessionID, nil
}

func (m *mockDB) SetLastSessionID(_, sessionID string) error {
	m.lastSessionID = sessionID
	return nil
}

func (m *mockDB) SetContextToken(userID, token string) error {
	if m.setTokenErr != nil {
		return m.setTokenErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tokens == nil {
		m.tokens = make(map[string]string)
	}
	m.tokens[userID] = token
	return nil
}

// tokenFor reads a recorded token under the lock.
func (m *mockDB) tokenFor(userID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tokens[userID]
}

// tokenCount reports how many tokens were recorded.
func (m *mockDB) tokenCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tokens)
}

// mockSessionMessenger implements common.SessionMessenger for testing.
type mockSessionMessenger struct {
	sessions      []common.SessionInfo
	running       map[string]bool
	sendErr       error
	sentSessionID string
	sentMessage   string
	sentFiles     []model.FileEntry

	// /show
	lastReply    string
	lastReplyOK  bool
	lastReplyErr error
	// /stop
	stopRunning bool
	stopCleared int
	stopErr     error
	// /new
	projects    []common.ProjectRef
	projectsErr error
	createdID   string
	createErr   error
	createPath  string
}

func (m *mockSessionMessenger) FindSessionsByPrefix(prefix string, runningOnly bool) ([]common.SessionInfo, error) {
	var out []common.SessionInfo
	for _, s := range m.sessions {
		if len(s.ID) >= len(prefix) && s.ID[:len(prefix)] == prefix {
			if runningOnly && !m.running[s.ID] {
				continue
			}
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *mockSessionMessenger) ListRecentSessions(limit int) ([]common.SessionInfo, error) {
	if limit > len(m.sessions) {
		limit = len(m.sessions)
	}
	return m.sessions[:limit], nil
}

func (m *mockSessionMessenger) IsSessionRunning(sessionID string) bool {
	return m.running[sessionID]
}

func (m *mockSessionMessenger) GetSessionInfo(sessionID string) (common.SessionInfo, error) {
	for _, s := range m.sessions {
		if s.ID == sessionID {
			return s, nil
		}
	}
	return common.SessionInfo{}, errors.New("session not found")
}

func (m *mockSessionMessenger) SendMessageToSession(sessionID, message string, files []model.FileEntry) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sentSessionID = sessionID
	m.sentMessage = message
	m.sentFiles = files
	return nil
}

func (m *mockSessionMessenger) LastAssistantReply(string) (string, bool, error) {
	return m.lastReply, m.lastReplyOK, m.lastReplyErr
}

func (m *mockSessionMessenger) StopAndClearSession(string) (bool, int, error) {
	return m.stopRunning, m.stopCleared, m.stopErr
}

func (m *mockSessionMessenger) ListProjects() ([]common.ProjectRef, error) {
	return m.projects, m.projectsErr
}

func (m *mockSessionMessenger) CreateSessionInProject(path string) (string, string, error) {
	m.createPath = path
	return m.createdID, "T", m.createErr
}

// setupPushMode sets push_mode for one test and restores it afterwards.
func setupPushMode(mode string) func() {
	orig := model.ConfigInstance.PushMode
	model.ConfigInstance.PushMode = mode
	return func() { model.ConfigInstance.PushMode = orig }
}

// startedManager installs a Manager that reports started without touching the
// network, plus a mock DB, and restores both on cleanup.
func startedManager(t *testing.T, subscribers ...common.SubscriberInfo) *Manager {
	t.Helper()

	origDB := db
	t.Cleanup(func() { db = origDB })
	db = &mockDB{subscribers: subscribers}

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true
	SetManager(mgr)
	t.Cleanup(func() { SetManager(nil) })

	return mgr
}
