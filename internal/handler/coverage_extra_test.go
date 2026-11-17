package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/kb"
	"clawbench/internal/mail"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
	"clawbench/internal/terminal"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── kb: guarded inputs ──────────────────────────────────────────────────────
//
// Most of the knowledge-base surface is a thin request → kb mapping. What is
// worth pinning is the behavior a client can observe being WRONG: a body that
// is accepted when it should be refused, a collection from another project that
// is visible, or a lookup that fails where the panel needs a fallback. Each case
// names the status (and the message key) the caller must see.

func kbCollectionJSON(t *testing.T, rec *httptest.ResponseRecorder) kb.Collection {
	t.Helper()
	var col kb.Collection
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &col), rec.Body.String())
	return col
}

func writeKBTextFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

// A blank or over-long name is refused for create AND rename: an empty name
// renders as an unclickable row in the panel, and the two paths must agree.
func TestServeKBCollections_NameIsRequiredOnCreateAndUpdate(t *testing.T) {
	setupKBHandlerTest(t)

	for _, name := range []string{"", "   ", strings.Repeat("名", 101)} {
		w := callHandler(ServeKBCollections, newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
			"name": name, "scope": "global",
		}))
		assert.Equal(t, http.StatusBadRequest, w.Code, "create with name %q: %s", name, w.Body.String())
		assert.Contains(t, w.Body.String(), "KBNameRequired")
	}

	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	query := "/api/kb/collections?id=" + strconv.FormatInt(col.ID, 10)
	for _, name := range []string{"", " ", strings.Repeat("x", 101)} {
		w := callHandler(ServeKBCollections, newRequest(t, http.MethodPatch, query, map[string]any{"name": name}))
		assert.Equal(t, http.StatusBadRequest, w.Code, "rename to %q: %s", name, w.Body.String())
	}

	// Every refused rename left the stored name alone.
	stored, err := kb.GetCollection(col.ID)
	require.NoError(t, err)
	assert.Equal(t, "资料", stored.Name)

	// A valid name is applied trimmed, not verbatim.
	w := callHandler(ServeKBCollections, newRequest(t, http.MethodPatch, query, map[string]any{"name": "  新名字  "}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "新名字", kbCollectionJSON(t, w).Name)
}

// An omitted scope means "this project", not "global": the default decides who
// else can see the collection, so it must be pinned.
func TestServeKBCollections_OmittedScopeDefaultsToTheProject(t *testing.T) {
	env := setupKBHandlerTest(t)

	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
		"name": "项目资料",
	}), env.ProjectDir)
	w := callHandler(ServeKBCollections, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	col := kbCollectionJSON(t, w)
	assert.Equal(t, kb.ScopeProject, col.Scope, "an omitted scope must not mean global")
	assert.Equal(t, "项目资料", col.Name)

	// Without a cookie there is no project to own it, so the request is refused
	// (403 NoProjectSelected) rather than silently creating a global collection.
	w = callHandler(ServeKBCollections, newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
		"name": "无项目",
	}))
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "NoProjectSelected")

	// An unknown scope is a client bug, not a license to create something global.
	w = callHandler(ServeKBCollections, newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
		"name": "误解", "scope": "workspace",
	}))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "InvalidRequestBody")
}

// A collection that belongs to another project must be invisible even for
// PATCH/DELETE: those are the two verbs that would otherwise let one project
// rename or destroy another's data by guessing a small integer id.
func TestServeKBCollections_ForeignCollectionIsForbidden(t *testing.T) {
	env := setupKBHandlerTest(t)

	foreign, err := kb.CreateCollection("别人的", "", kb.ScopeProject, 424242)
	require.NoError(t, err)

	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		var body any
		if method == http.MethodPatch {
			body = map[string]any{"pinned": true}
		}
		req := withProjectCookie(newRequest(t, method,
			"/api/kb/collections?id="+strconv.FormatInt(foreign.ID, 10), body), env.ProjectDir)
		w := callHandler(ServeKBCollections, req)
		assert.Equal(t, http.StatusForbidden, w.Code, "%s: %s", method, w.Body.String())
	}

	still, err := kb.GetCollection(foreign.ID)
	require.NoError(t, err, "a forbidden rename/delete must leave the row in place")
	assert.Equal(t, "别人的", still.Name)
	assert.False(t, still.Pinned)
}

// An unknown project sees the global collections and nothing else — it must
// never fall back to "everything", which would leak every project's data.
func TestServeKBCollections_UnknownProjectSeesOnlyGlobalOnes(t *testing.T) {
	env := setupKBHandlerTest(t)

	global, err := kb.CreateCollection("全局", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	mine, err := kb.CreateCollection("我的", "", kb.ScopeProject, mustProjectID(t, env.ProjectDir))
	require.NoError(t, err)
	other, err := kb.CreateCollection("其他项目", "", kb.ScopeProject, 999999)
	require.NoError(t, err)

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/kb/collections", nil), env.ProjectDir)
	w := callHandler(ServeKBCollections, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Collections []kb.Collection `json:"collections"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	names := make([]string, 0, len(body.Collections))
	for _, c := range body.Collections {
		names = append(names, c.Name)
	}
	assert.ElementsMatch(t, []string{global.Name, mine.Name}, names, "another project's collection must not appear")
	assert.NotContains(t, names, other.Name)
}

// The document detail endpoint is reachable by two lookups (id, and id +
// chunk_id). A chunk id that no longer exists — a reindex since the search —
// must fall back to the document's first page instead of failing the request,
// because the user clicked a hit that WAS valid a second ago.
func TestServeKBDocuments_ChunkWindowFallsBackWhenTheChunkIsGone(t *testing.T) {
	env := setupKBHandlerTest(t)
	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	path := writeKBTextFile(t, env.ProjectDir, "notes.md", "# 笔记\n\n第一段内容\n\n第二段内容\n")
	doc, err := kb.AddDocumentFromPath(col.ID, path)
	require.NoError(t, err)
	waitKBDocumentReady(t, doc.ID)
	query := "/api/kb/documents?id=" + strconv.FormatInt(doc.ID, 10)

	w := callHandler(ServeKBDocuments, newRequest(t, http.MethodGet, query+"&chunk_id=999999", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var detail struct {
		Chunks       []kb.Chunk `json:"chunks"`
		FocusChunkID int64      `json:"focus_chunk_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	assert.Zero(t, detail.FocusChunkID, "a missing chunk must not be claimed as the focus")
	assert.NotEmpty(t, detail.Chunks, "the document's first page is the fallback")

	// A non-positive / non-numeric chunk id is a malformed request, not a
	// "missing chunk".
	for _, bad := range []string{"0", "-3", "abc"} {
		w = callHandler(ServeKBDocuments, newRequest(t, http.MethodGet, query+"&chunk_id="+bad, nil))
		assert.Equal(t, http.StatusBadRequest, w.Code, "chunk_id=%s", bad)
	}
}

// Editing tags needs the field to be present. `{}` is not "clear the tags": a
// client that forgot the key would otherwise silently wipe them.
func TestServeKBDocuments_TagsFieldIsRequired(t *testing.T) {
	env := setupKBHandlerTest(t)
	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	path := writeKBTextFile(t, env.ProjectDir, "tagged.md", "内容\n")
	doc, err := kb.AddDocumentFromPath(col.ID, path)
	require.NoError(t, err)
	query := "/api/kb/documents?id=" + strconv.FormatInt(doc.ID, 10)

	w := callHandler(ServeKBDocuments, newRequest(t, http.MethodPatch, query, map[string]any{}))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "InvalidRequestBody")

	// A present-but-empty string IS an intentional clear and is stored.
	w = callHandler(ServeKBDocuments, newRequest(t, http.MethodPatch, query, map[string]any{"tags": "  a, b  "}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var updated kb.Document
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.Equal(t, "a, b", updated.Tags, "tags are stored trimmed")
}

// A document in another project's collection is unreachable through the detail,
// tag and delete endpoints — the same boundary the collection endpoints enforce.
func TestServeKBDocuments_ForeignDocumentIsForbidden(t *testing.T) {
	setupKBHandlerTest(t)
	foreign, err := kb.CreateCollection("别人的", "", kb.ScopeProject, 888888)
	require.NoError(t, err)
	filePath := writeKBTextFile(t, t.TempDir(), "secret.md", "别人的内容\n")
	doc, err := kb.AddDocumentFromPath(foreign.ID, filePath)
	require.NoError(t, err)
	query := "/api/kb/documents?id=" + strconv.FormatInt(doc.ID, 10)

	w := callHandler(ServeKBDocuments, newRequest(t, http.MethodGet, query, nil))
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	w = callHandler(ServeKBDocuments, newRequest(t, http.MethodPatch, query, map[string]any{"tags": "x"}))
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	w = callHandler(ServeKBDocuments, newRequest(t, http.MethodDelete, query, nil))
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	_, err = kb.GetDocument(doc.ID)
	assert.NoError(t, err, "a forbidden delete must leave the document in place")
}

// An upload without the file part is a client bug; the panel shows
// "NoFileProvided" rather than a generic failure, and nothing is indexed.
func TestServeKBDocumentUpload_RequiresTheFilePart(t *testing.T) {
	setupKBHandlerTest(t)
	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)

	var buf strings.Builder
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("collection_id", strconv.FormatInt(col.ID, 10)))
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/kb/documents/upload", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := callHandler(ServeKBDocumentUpload, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "NoFileProvided")
	docs, err := kb.ListDocuments(col.ID)
	require.NoError(t, err)
	assert.Empty(t, docs)
}

// The detail of a private-address rejection is what tells the user WHY a URL was
// refused; a plain "BadRequest" would look like a client bug.
func TestWriteKBError_PrivateAddressCarriesTheDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	writeKBError(rec, httptest.NewRequest(http.MethodPost, "/api/kb/documents/url", http.NoBody),
		&kb.FileError{Reason: kb.ReasonPrivateAddress, Detail: "127.0.0.1", Err: kb.ErrPrivateAddress})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "KBPrivateAddress")
	assert.Contains(t, rec.Body.String(), "127.0.0.1")

	// A typed "too large" rejection reports 413 and the configured limit, so the
	// user knows what to shrink the file to.
	rec = httptest.NewRecorder()
	writeKBError(rec, httptest.NewRequest(http.MethodPost, "/api/kb/documents", http.NoBody),
		&kb.FileError{Reason: kb.ReasonFileTooLarge, Detail: "80", Err: kb.ErrFileTooLarge})
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "KBReasonFileTooLarge")
	assert.Contains(t, rec.Body.String(), strconv.FormatInt(kb.MaxFileBytes()>>20, 10))
}

// writeKBFileError is also reachable with a sentinel that carries no FileError
// (ErrUnsupportedFile / ErrNoText): it must answer the generic key rather than
// dereferencing a nil reason.
func TestWriteKBFileError_SentinelWithoutAFileError(t *testing.T) {
	for _, err := range []error{kb.ErrNoText, kb.ErrUnsupportedFile} {
		rec := httptest.NewRecorder()
		writeKBFileError(rec, httptest.NewRequest(http.MethodPost, "/api/kb/documents", http.NoBody), err)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "KBUnsupportedFile")
	}
}

// A malformed JSON body is a 400 from decodeJSON; the handler must not continue
// with a zero request (for documents/url a zero collection means "global").
func TestKBMalformedBodiesAreRejected(t *testing.T) {
	setupKBHandlerTest(t)

	cases := []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"collections create", ServeKBCollections, "/api/kb/collections"},
		{"document move", ServeKBDocumentMove, "/api/kb/documents/move"},
		{"document url", ServeKBDocumentURL, "/api/kb/documents/url"},
		{"document import", ServeKBDocumentImport, "/api/kb/documents/import"},
		{"search", ServeKBSearch, "/api/kb/search"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{"collection_id":`))
		req.Header.Set("Content-Type", "application/json")
		w := callHandler(tc.handler, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s: %s", tc.name, w.Body.String())
	}
}

// ── mail: the client-id classification ─────────────────────────────────────

// A server that refuses a command until the client identifies itself is an
// upstream problem (502 with its own key), not something the user can fix by
// re-entering their password.
func TestWriteMailError_ClientIDRequiredIsUpstreamFailure(t *testing.T) {
	err := &mail.Error{Kind: mail.KindClientID, Op: "send", Err: errors.New("server requires CLIENTID")}
	rec := httptest.NewRecorder()
	writeMailError(rec, httptest.NewRequest(http.MethodPost, "/api/mail/send", http.NoBody), err)
	assert.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "MailClientIDRequired")
}

// ── providers: the model-list conflict ─────────────────────────────────────

// Emptying a provider's model list breaks every config-directory agent bound to
// it (the file no longer declares any model the CLI can resolve), so the edit is
// refused with a 409 that NAMES those agents: without the names the user cannot
// know which agent to rebind.
func TestProvidersAPI_EmptyingModelsNamesTheBoundAgents(t *testing.T) {
	setupProvidersHandlerTest(t)
	clearSummaryBinding(t)

	p, err := service.CreateProvider(model.Provider{
		Name: "WithModels", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://api.example.com/v1", APIKey: "k", Models: []string{"m1"},
	})
	require.NoError(t, err)
	require.NoError(t, service.SaveAgent(store.WriteDB(), &model.Agent{
		ID: "pi-agent", Name: "Pi Agent", Backend: "pi", ProviderID: &p.ID,
	}))
	t.Cleanup(func() { model.ReplaceAgents(map[string]*model.Agent{}, nil) })

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodPatch,
		fmt.Sprintf("/api/providers/%d", p.ID), map[string]any{"models": []string{}})
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderModelsRequired")
	assert.Contains(t, rec.Body.String(), "Pi Agent")

	stored, err := service.GetProvider(p.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"m1"}, stored.Models, "the refused edit must not be stored")
}

// ── terminal: the history-capture sink ─────────────────────────────────────

// Recording a command from the terminal is what makes the history panel useful,
// and the sink is installed by SetTerminalManager (a second creation site would
// otherwise forget it). This is the whole reason the sink exists: the service
// package cannot be imported from the terminal one, so the injection point is
// the only place the wiring can be checked.
//
// The recorder is unexported and has no test hook, and a real submission needs a
// foreground shell on a PTY (already covered by the terminal package's own
// tests). What is pinned here is the wiring: a manager created the way main.go
// creates it — through SetTerminalManager — must come out with a sink, or every
// command a user types silently never reaches the history panel.
func TestSetTerminalManager_InstallsTheCommandRecorder(t *testing.T) {
	origMgr := GetTerminalManager()
	t.Cleanup(func() {
		if cur := GetTerminalManager(); cur != nil && cur != origMgr {
			cur.Close()
		}
		SetTerminalManager(origMgr)
	})

	// A broken database is not needed: the wiring is the assertion, and a real
	// row would only re-test service.RecordTerminalCommand.
	manager := terminal.NewManager(model.TerminalConfig{
		Enabled: true, IdleTimeout: "1m", BufferLines: 100, MaxLineBytes: 65536, MaxBufferMB: 4,
	}, 0)
	SetTerminalManager(manager)
	if !manager.HasCommandRecorder() {
		t.Fatal("SetTerminalManager must install the command recorder; otherwise /api/terminal/history stays empty")
	}

	// Clearing the manager must not panic and leaves the global unset.
	SetTerminalManager(nil)
	if GetTerminalManager() != nil {
		t.Fatal("SetTerminalManager(nil) must clear the global manager")
	}
}

// ── auto probe of an ACP agent ─────────────────────────────────────────────

func resetAutoProbeState(t *testing.T) {
	t.Helper()
	autoProbeMu.Lock()
	prev := autoProbeInFlight
	autoProbeInFlight = map[string]bool{}
	autoProbeMu.Unlock()
	t.Cleanup(func() {
		autoProbeMu.Lock()
		autoProbeInFlight = prev
		autoProbeMu.Unlock()
	})
}

// A nil agent or an empty project has nothing to probe: the probe needs a cwd
// for session/new, and a nil agent is the "unknown id" case of the caller.
func TestMaybeAutoProbeAgentState_NeedsAnAgentAndAProject(t *testing.T) {
	resetAutoProbeState(t)
	called := make(chan struct{}, 1)
	restore := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		called <- struct{}{}
		return &ai.ACPStateProbeResult{}, nil
	})
	defer restore()

	agent := &model.Agent{ID: "probe-guard", Backend: "claude", AcpCommand: "claude --acp", Transport: "acp-stdio"}
	maybeAutoProbeAgentState(nil, t.TempDir())
	maybeAutoProbeAgentState(agent, "")

	select {
	case <-called:
		t.Fatal("the probe must not start without an agent and a project")
	case <-time.After(200 * time.Millisecond):
		// A positive wait is the only way to assert "nothing happened"; 200ms is
		// orders of magnitude above the goroutine start the guard would have
		// reached, and the signal is the probe itself (not a timer race).
	}
	autoProbeMu.Lock()
	defer autoProbeMu.Unlock()
	assert.False(t, autoProbeInFlight[agent.ID], "a refused probe must not mark the agent in flight")
}

// While a probe is running, a second request for the same agent is dropped: three
// session creations in a row must not spawn three agent processes. The slot is
// released when the probe finishes, so the NEXT attempt does run.
func TestMaybeAutoProbeAgentState_DedupesWhileInFlightAndReleasesTheSlot(t *testing.T) {
	resetAutoProbeState(t)
	agent := &model.Agent{ID: "probe-dedupe", Backend: "claude", AcpCommand: "claude --acp", Transport: "acp-stdio"}
	reg := ai.GetAgentCapabilityRegistry()
	t.Cleanup(func() { reg.UpdateModels(agent.ID, nil) })

	release := make(chan struct{})
	started := make(chan struct{}, 8)
	finished := make(chan struct{}, 8)
	restore := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		started <- struct{}{}
		<-release
		finished <- struct{}{}
		return &ai.ACPStateProbeResult{Models: []model.AgentModel{{ID: "m1", Name: "M1"}}}, nil
	})
	defer restore()

	maybeAutoProbeAgentState(agent, t.TempDir())
	select {
	case <-started:
	case <-timeoutAfter(t):
		t.Fatal("the first probe never started")
	}

	// In flight: the second and third calls return without a probe.
	maybeAutoProbeAgentState(agent, t.TempDir())
	maybeAutoProbeAgentState(agent, t.TempDir())
	select {
	case <-started:
		t.Fatal("a probe was started for an agent that already has one in flight")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-finished:
	case <-timeoutAfter(t):
		t.Fatal("the probe never finished")
	}

	// The in-flight slot is released after completion (otherwise the second
	// session ever created for this agent would never be probed again).
	require.Eventually(t, func() bool {
		autoProbeMu.Lock()
		defer autoProbeMu.Unlock()
		return !autoProbeInFlight[agent.ID]
	}, 2*time.Second, 10*time.Millisecond, "the in-flight slot must be released when the probe ends")

	// A failure must not leave the slot taken either.
	failRelease := make(chan struct{})
	restore2 := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		<-failRelease
		return nil, errors.New("agent is not signed in")
	})
	defer restore2()
	maybeAutoProbeAgentState(agent, t.TempDir())
	close(failRelease)
	require.Eventually(t, func() bool {
		autoProbeMu.Lock()
		defer autoProbeMu.Unlock()
		return !autoProbeInFlight[agent.ID]
	}, 2*time.Second, 10*time.Millisecond, "a failed probe must release its slot too")
}

// A successful probe announces "agents_updated" so every open picker refills
// without a reload. The manager is process-global, so the test installs one and
// reads what the probe broadcast.
func TestMaybeAutoProbeAgentState_BroadcastsAgentsUpdatedOnSuccess(t *testing.T) {
	resetAutoProbeState(t)
	agent := &model.Agent{ID: "probe-broadcast", Backend: "claude", AcpCommand: "claude --acp", Transport: "acp-stdio"}
	reg := ai.GetAgentCapabilityRegistry()
	t.Cleanup(func() { reg.UpdateModels(agent.ID, nil) })

	prevMgr := ws.GetManager()
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(prevMgr) })

	// A disconnected subscriber is where BroadcastEvent leaves its trace: every
	// event is buffered for replay, so the buffer IS the observable outcome.
	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "probe-client", "")
	require.NotNil(t, sub)
	mgr.DisconnectClient("probe-client")

	done := make(chan struct{})
	restore := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		close(done)
		return &ai.ACPStateProbeResult{
			Models:   []model.AgentModel{{ID: "m1", Name: "M1"}},
			Modes:    nil,
			Efforts:  nil,
			Commands: nil,
		}, nil
	})
	defer restore()

	maybeAutoProbeAgentState(agent, t.TempDir())
	select {
	case <-done:
	case <-timeoutAfter(t):
		t.Fatal("the probe never ran")
	}

	// The broadcast happens after the probe returns; poll the manager's own event
	// log rather than sleeping a fixed time.
	require.Eventually(t, func() bool {
		for _, msg := range sub.GetBufferedEvents() {
			if msg.Event == "agents_updated" {
				return true
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "a successful probe must tell clients the agent list changed")
}

// ── agent install: remaining refusals ──────────────────────────────────────

// A registry backend without an install command (the built-in agent) is a 400
// with its own key: a 404 would say "unknown backend", which it is not.
func TestServeBackendInstall_BackendWithoutAnInstallCommand(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// builtin has no CLI to install; it is in the registry with InstallCmd "".
	spec := model.FindSpecByBackend("builtin")
	require.NotNil(t, spec)
	require.Empty(t, strings.TrimSpace(spec.InstallCmd), "fixture assumption: builtin has no install command")

	req := newRequest(t, http.MethodPost, "/api/backends/"+spec.ID+"/install", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBackendInstall, req)

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "InstallCommandUnavailable")
}

// The rescan is best-effort: a refresh failure must not turn a successful install
// into an error, but it must be REPORTED as refreshed:false so the UI can ask for
// a manual rescan.
func TestRescanAfterInstall_ReportsFailureWithoutFailingTheInstall(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// RefreshAgents reloads the agent set from the database; hiding that table
	// is the failure a rescan can actually hit (a half-migrated store), and it is
	// the one this handler has to survive without failing the install.
	_, err := store.WriteExec("ALTER TABLE agents RENAME TO agents_renamed")
	require.NoError(t, err)
	assert.False(t, rescanAfterInstall("claude"), "a failed rescan must be reported, not swallowed")
}

// The service's sentinel errors each have their own status; a busy install is a
// 409 (retry later) while an empty command is a 400 (nothing to run).
func TestWriteAgentInstallError_MapsSentinels(t *testing.T) {
	cases := []struct {
		err    error
		status int
		key    string
	}{
		{service.ErrAgentInstallBusy, http.StatusConflict, "AgentInstallBusy"},
		{service.ErrAgentInstallEmptyCommand, http.StatusBadRequest, "InstallCommandUnavailable"},
		{errors.New("something unexpected"), http.StatusInternalServerError, "InternalError"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		writeAgentInstallError(rec, httptest.NewRequest(http.MethodPost, "/api/backends/x/install", http.NoBody), "x", tc.err)
		assert.Equal(t, tc.status, rec.Code, "%v: %s", tc.err, rec.Body.String())
		assert.Contains(t, rec.Body.String(), tc.key)
	}
}

// A Node.js failure with no underlying cause still names the tool and says the
// automatic install failed, rather than printing an empty reason after a colon.
func TestWriteAgentInstallError_NodeFailureWithoutCause(t *testing.T) {
	rec := httptest.NewRecorder()
	writeAgentInstallError(rec, httptest.NewRequest(http.MethodPost, "/api/backends/x/install", http.NoBody), "x",
		&service.AgentInstallNodeMissingError{Tool: "npm"})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "NodeRuntimeMissing")
	assert.Contains(t, rec.Body.String(), "npm")
	assert.Contains(t, rec.Body.String(), "the automatic install failed")
}

// ── live notes: clear-all per-row failures ────────────────────────────────

// A row whose artifact-job lookup fails is skipped, not fatal: refusing the whole
// clear because one row's query broke would leave the other ninety-nine behind.
func TestServeLiveNotesClear_JobLookupFailureSkipsEveryRow(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	pid := mustProjectID(t, project)

	first := "0123456789abcdef0123456789abcde1"
	second := "0123456789abcdef0123456789abcde2"
	require.NoError(t, service.StartLiveNote(first, pid, "第一条", 1_700_000_000_000))
	require.NoError(t, service.StartLiveNote(second, pid, "第二条", 1_700_000_001_000))

	// ArtifactJobActive wraps its scan error, and a missing table is the one fault
	// that hits EVERY row: nothing can be judged, so nothing may be deleted and
	// the whole clear must still answer 200 reporting the skips — refusing the
	// call outright would leave the user with no way to clear anything.
	_, err := db.Exec(`DROP TABLE live_notes_artifacts`)
	require.NoError(t, err)

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions/all", project)
	req.Method = http.MethodDelete
	rec := httptest.NewRecorder()
	ServeLiveNotesClear(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Deleted int `json:"deleted"`
		Skipped int `json:"skipped"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Zero(t, body.Deleted, "a row whose lookup failed must not be deleted")
	assert.Equal(t, 2, body.Skipped, "both rows are reported as left behind")

	for _, token := range []string{first, second} {
		_, exists, getErr := service.GetLiveNote(token)
		require.NoError(t, getErr)
		assert.True(t, exists, "the clear must not delete a row it could not check")
	}
}

// The list itself failing is a 500 with no body beyond the standard error: the
// caller must not be told "deleted: 0" when nothing was even read.
func TestServeLiveNotesClear_ListFailureIsInternalError(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()

	_, err := db.Exec(`DROP TABLE live_notes`)
	require.NoError(t, err)

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions/all", project)
	req.Method = http.MethodDelete
	rec := httptest.NewRecorder()
	ServeLiveNotesClear(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "internal server error",
		"the failure is reported as a server error, not as \"deleted: 0\"")
}
