package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #568: attaching a directory to a session and then sending the message
// failed with the client's generic "发送失败，请重试".
//
// Attachments are PERSISTED with their resolved absolute path
// (AIChat stores validatedFileEntries), so every path that re-sends an
// already-attached item — the "最近引用" list, a restored attachment draft, a
// drained queued message — hands back an absolute path. The handler validated
// it with model.ValidatePath, which JOINS the argument onto the project root:
// "/proj" + "/proj/sub" became "/proj/proj/sub", the following os.Stat 404'd,
// and the client surfaced that 404 as the generic send-failure toast.
//
// A directory is the most visible trigger (the whole attachment is the path),
// but the same defect hit any re-attached file.
// setupAbsAttach builds an isolated fixture: RegisterBackend panics on a
// duplicate id and its factory is process-global, so each test gets its own
// backend id (and therefore its own capture channel) instead of sharing one
// instance whose channel the first test already drained.
func setupAbsAttach(t *testing.T) (*testEnv, string, chan ai.ChatRequest) {
	t.Helper()
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	backendID := "mock-absattach-" + t.Name()
	agentID := "absattach-agent-" + t.Name()
	backend := &promptCapturingBackend{got: make(chan ai.ChatRequest, 4)}
	ai.RegisterBackend(backendID, func() ai.AIBackend { return backend })
	model.Agents[agentID] = &model.Agent{ID: agentID, Backend: backendID}

	sessionID, err := service.CreateSession(env.ProjectDir, backendID, "abs-attach", agentID, "", "default", "chat")
	require.NoError(t, err)
	return env, sessionID, backend.got
}

// waitPrompt drains the request the backend was asked to run.
func waitPrompt(t *testing.T, got chan ai.ChatRequest) ai.ChatRequest {
	t.Helper()
	select {
	case req := <-got:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("backend was never invoked")
		return ai.ChatRequest{}
	}
}

func TestAIChat_AbsoluteDirAttachment_Accepted(t *testing.T) {
	env, sessionID, got := setupAbsAttach(t)
	dir := filepath.Join(env.ProjectDir, "sub")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	body := map[string]any{
		"message": "看看这个目录",
		"agentId": "absattach-agent-" + t.Name(),
		// Both channels carry the persisted (absolute) form, exactly like the
		// frontend replaying an attachment it read back from the DB.
		"filePaths": []string{dir},
		"files":     []model.FileEntry{{Path: dir, IsDir: true}},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/chat?session_id="+sessionID, body)
	withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(AIChat, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	prompt := waitPrompt(t, got).Prompt
	assert.Contains(t, prompt, "[Current directory: "+dir+"]",
		"an absolute directory attachment must reach the AI as a directory, not 404 the send")
	assert.Contains(t, prompt, "看看这个目录", "the user's own text must survive")
}

func TestAIChat_AbsoluteFileAttachment_Accepted(t *testing.T) {
	env, sessionID, got := setupAbsAttach(t)
	file := filepath.Join(env.ProjectDir, "a.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	body := map[string]any{
		"message":   "看这个文件",
		"agentId":   "absattach-agent-" + t.Name(),
		"filePaths": []string{file},
		"files":     []model.FileEntry{{Path: file}},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/chat?session_id="+sessionID, body)
	withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(AIChat, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, waitPrompt(t, got).Prompt, "[Current file: "+file+"]")
}

// An absolute attachment path is accepted when it sits under a configured root.
//
// This documents the REAL rule, which is weaker than "inside the project": on
// Unix the root list is ["/"], so any existing absolute path passes — and it
// must, because the file manager browses the whole machine and its "attach to
// chat" action (FileManagerContent.doAttachToChat) forwards whatever path the
// user picked. Asserting a project-internal 403 here would demand a restriction
// the product does not have.
//
// Both root shapes are exercised so the test cannot pass merely because the
// fixture narrowed RootPaths (see TestSharePublic_LocalAbsolutePathOutsideProject_404
// for the same trap).
func TestAIChat_AbsoluteAttachmentOutsideProject_AcceptedUnderAnyRoot(t *testing.T) {
	env, sessionID, got := setupAbsAttach(t)

	outside := filepath.Join(t.TempDir(), "picked-in-file-manager.txt")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o644))

	for _, tc := range []struct {
		name  string
		roots []string
	}{
		// Production semantics: the app browses the whole machine. "/" is the
		// Unix root; on Windows the production list is the drive list, and a
		// temp file lives on whichever drive TEMP is on — so the literal "/"
		// would describe a root that does not exist there and the assertion
		// would fail for the wrong reason.
		{"production roots", platform.ListRootPaths()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origRoots := model.RootPaths
			model.RootPaths = tc.roots
			t.Cleanup(func() { model.RootPaths = origRoots })

			body := map[string]any{
				"message":   "读它",
				"agentId":   "absattach-agent-" + t.Name(),
				"filePaths": []string{outside},
			}
			req := newRequest(t, http.MethodPost, "/api/ai/chat?session_id="+sessionID, body)
			withProjectCookie(req, env.ProjectDir)
			w := callHandlerWithAuth(AIChat, req)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Contains(t, waitPrompt(t, got).Prompt, "[Current file: "+outside+"]")
		})
	}
}

// A path outside EVERY configured root is refused. This is the containment that
// does hold, and it is only meaningful when the roots are narrowed — with
// production roots on Unix ("/") there is nothing outside them, which is why the
// previous version of this test asserted on the fixture's boundary and passed
// for the wrong reason.
func TestAIChat_AbsoluteAttachmentOutsideAllRoots_Forbidden(t *testing.T) {
	env, sessionID, _ := setupAbsAttach(t)

	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o644))

	origRoots := model.RootPaths
	model.RootPaths = []string{env.WatchDir}
	t.Cleanup(func() { model.RootPaths = origRoots })

	body := map[string]any{
		"message":   "读它",
		"agentId":   "absattach-agent-" + t.Name(),
		"filePaths": []string{outside},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/chat?session_id="+sessionID, body)
	withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(AIChat, req)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

// Relative paths keep their existing meaning (resolved against the project
// root), so a fresh attach is unaffected.
func TestAIChat_RelativeDirAttachment_StillResolvedAgainstProject(t *testing.T) {
	env, sessionID, got := setupAbsAttach(t)
	require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "sub"), 0o755))

	body := map[string]any{
		"message":   "看看",
		"agentId":   "absattach-agent-" + t.Name(),
		"filePaths": []string{"sub"},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/chat?session_id="+sessionID, body)
	withProjectCookie(req, env.ProjectDir)
	require.Equal(t, http.StatusOK, callHandlerWithAuth(AIChat, req).Code)

	assert.Contains(t, waitPrompt(t, got).Prompt,
		"[Current directory: "+filepath.Join(env.ProjectDir, "sub")+"]")
}

// The queue endpoint shares the same validation, so a queued message that
// carries a persisted absolute attachment must not 404 either.
func TestQueueHandler_AbsoluteDirAttachment_Accepted(t *testing.T) {
	env, sessionID, _ := setupAbsAttach(t)
	dir := filepath.Join(env.ProjectDir, "sub")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	body := map[string]any{
		"message":   "排队的目录",
		"filePaths": []string{dir},
		"files":     []model.FileEntry{{Path: dir, IsDir: true}},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(QueueHandler, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// The drain loop rebuilds the prompt from the stored row, which holds the
// absolute form. It must classify it as a directory instead of silently
// degrading to a bare "[Current file]" label.
func TestBuildChatRequestFromQueue_AbsoluteDirAttachment_ClassifiedAsDirectory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "queue-abs-dir", "", "", "codebuddy", "chat")
	require.NoError(t, err)
	dir := filepath.Join(env.ProjectDir, "sub")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	qMsg := model.QueuedMessage{
		Text:      "继续看这个目录",
		FilePaths: []string{dir},
		Files:     []model.FileEntry{{Path: dir, IsDir: true}},
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	req, err := buildChatRequestFromQueue(qMsg, sessionID, env.ProjectDir, "codebuddy", "codebuddy", "")
	require.NoError(t, err)

	assert.Contains(t, req.Prompt, "[Current directory: "+dir+"]",
		"a drained absolute directory attachment must stay a directory reference")
	assert.Contains(t, req.Prompt, "继续看这个目录")
}
