package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCompactBackend is a stand-in for the ACP subprocess: it emits a scripted
// event list and closes the channel, exactly as the producer contract requires.
type fakeCompactBackend struct {
	events []ai.StreamEvent
	err    error
	// started is closed once ExecuteStream is entered, so a test can prove the
	// request was built before the backend was asked to run.
	gotReq ai.ChatRequest
}

func (f *fakeCompactBackend) Name() string { return "fake-compact" }

func (f *fakeCompactBackend) ExecuteStream(_ context.Context, req ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	f.gotReq = req
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan ai.StreamEvent, len(f.events)+1)
	for _, ev := range f.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

// withFakeCompactBackend swaps the backend seam for the duration of a test.
func withFakeCompactBackend(t *testing.T, be ai.AIBackend, err error) {
	t.Helper()
	orig := newBackendForAgent
	newBackendForAgent = func(_, _, _ string) (ai.AIBackend, error) { return be, err }
	t.Cleanup(func() { newBackendForAgent = orig })
}

// setupCompactableGroup creates a group whose host member runs over ACP, which
// is the only transport compaction supports.
func setupCompactableGroup(t *testing.T, project string) (groupID, memberID string) {
	t.Helper()
	setupGroupDB(t)
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, memberID, err := CreateGroup(project, "c", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	// The member row's transport override is what CompactGroupMember reads.
	require.NoError(t, UpdateSessionTransport(memberID, "acp-stdio"))
	return groupID, memberID
}

// TestCompactGroupMember_HappyPath drives the whole compaction seam with a fake
// backend: guards pass, the request is built for the member, state events land
// on the MEMBER row (never the group), the compacted flag is set, and the
// in-flight slot is released.
func TestCompactGroupMember_HappyPath(t *testing.T) {
	groupID, memberID := setupCompactableGroup(t, "/tmp/grouptest-compact-ok")

	// Only CONVERSATIONAL rows matter here: membership changes and the
	// compaction markers themselves are role='system' (rendered as a centered
	// line, never as speech), and group creation writes those too.
	var beforeMsgs int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role != 'system'", groupID,
	).Scan(&beforeMsgs))

	be := &fakeCompactBackend{events: []ai.StreamEvent{
		{Type: "usage_update", Usage: &ai.UsageState{Used: 1000, Size: 200000}},
		{Type: "session_capture", Content: "external-1"},
		{Type: "compact_detected"},
		{Type: "done"},
	}}
	withFakeCompactBackend(t, be, nil)

	usage, err := CompactGroupMember(context.Background(), groupID, memberID)
	require.NoError(t, err)
	require.NotNil(t, usage, "the post-compaction usage must be returned")
	assert.Equal(t, 1000, usage.Used)

	// The request must be the ACP slash command, addressed to the member.
	assert.Equal(t, "/compact", be.gotReq.Prompt, "compaction must send the slash command as the prompt")

	// State events are persisted onto the member row…
	assert.Equal(t, "external-1", GetExternalSessionID(memberID))
	// …and the compacted flag is set unconditionally (several agents never
	// announce compaction on the wire).
	var compacted int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT compacted FROM chat_sessions WHERE id = ?", memberID,
	).Scan(&compacted))
	assert.Equal(t, 1, compacted)

	// Nothing may be written to the GROUP timeline: a "/compact" bubble in the
	// discussion would be read by every member as ordinary speech.
	var afterMsgs int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role != 'system'", groupID,
	).Scan(&afterMsgs))
	assert.Equal(t, beforeMsgs, afterMsgs,
		"compaction must not write a user or assistant bubble to the group timeline")

	// The slot is released, so a second compaction is allowed.
	assert.False(t, IsGroupMemberCompacting(memberID))
}

// TestCompactGroupMember_BackendErrorSurfaces pins the two failure classes:
// a setup error is returned as-is, while a stream error is the user-facing
// runErr. Both must release the in-flight slot.
func TestCompactGroupMember_BackendErrorSurfaces(t *testing.T) {
	groupID, memberID := setupCompactableGroup(t, "/tmp/grouptest-compact-err")

	withFakeCompactBackend(t, nil, errors.New("cannot spawn"))
	_, err := CompactGroupMember(context.Background(), groupID, memberID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create backend")
	assert.False(t, IsGroupMemberCompacting(memberID), "a failed start must release the slot")

	withFakeCompactBackend(t, &fakeCompactBackend{events: []ai.StreamEvent{
		{Type: eventTypeError, Error: "agent exploded"},
	}}, nil)
	_, err = CompactGroupMember(context.Background(), groupID, memberID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent exploded")
	assert.False(t, IsGroupMemberCompacting(memberID))
}

// TestConsumeCompactEvent covers every branch of the event reducer. It is the
// only place that decides what a compaction stream means, so each case is
// asserted directly rather than through a full run.
func TestConsumeCompactEvent(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-compact-events"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, memberID, err := CreateGroup(project, "c", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	_ = groupID

	t.Run("usage_update persists onto the member row", func(t *testing.T) {
		require.NoError(t, consumeCompactEvent(memberID, ai.StreamEvent{
			Type:  "usage_update",
			Usage: &ai.UsageState{Used: 4242, Size: 200000},
		}))
		state := GetContextState(memberID)
		require.NotNil(t, state)
		require.NotNil(t, state.Usage)
		assert.Equal(t, 4242, state.Usage.Used)
	})

	t.Run("session_capture stores the external id once", func(t *testing.T) {
		require.NoError(t, UpdateExternalSessionID(memberID, ""))
		require.NoError(t, consumeCompactEvent(memberID, ai.StreamEvent{
			Type: "session_capture", Content: "ext-A",
		}))
		assert.Equal(t, "ext-A", GetExternalSessionID(memberID))

		// A second capture must NOT overwrite: the id identifies the agent
		// session the compaction ran on.
		require.NoError(t, consumeCompactEvent(memberID, ai.StreamEvent{
			Type: "session_capture", Content: "ext-B",
		}))
		assert.Equal(t, "ext-A", GetExternalSessionID(memberID))
	})

	t.Run("empty session_capture is ignored", func(t *testing.T) {
		require.NoError(t, UpdateExternalSessionID(memberID, ""))
		require.NoError(t, consumeCompactEvent(memberID, ai.StreamEvent{Type: "session_capture"}))
		assert.Empty(t, GetExternalSessionID(memberID))
	})

	t.Run("compact_detected marks the session compacted", func(t *testing.T) {
		_, err := store.WriteExec("UPDATE chat_sessions SET compacted = 0 WHERE id = ?", memberID)
		require.NoError(t, err)
		require.NoError(t, consumeCompactEvent(memberID, ai.StreamEvent{Type: "compact_detected"}))
		var compacted int
		require.NoError(t, store.ReadDB().QueryRow(
			"SELECT compacted FROM chat_sessions WHERE id = ?", memberID,
		).Scan(&compacted))
		assert.Equal(t, 1, compacted)
	})

	t.Run("error event becomes a user-facing error", func(t *testing.T) {
		err := consumeCompactEvent(memberID, ai.StreamEvent{Type: eventTypeError, Error: "boom"})
		require.Error(t, err)
		assert.Equal(t, "boom", err.Error())
	})

	t.Run("refusal warning becomes an error", func(t *testing.T) {
		// A refusal arrives as a warning with no content; reporting success
		// would tell the user their context was compacted when it was not.
		err := consumeCompactEvent(memberID, ai.StreamEvent{
			Type: blockTypeWarning, Reason: ai.ReasonRefused, Content: "model unavailable",
		})
		require.Error(t, err)
		assert.Equal(t, "model unavailable", err.Error())
	})

	// An empty answer is a legitimate /compact outcome (the agent may report
	// nothing); the usage numbers in the completion row show what changed.
	t.Run("an empty warning is not a failure", func(t *testing.T) {
		assert.NoError(t, consumeCompactEvent(memberID, ai.StreamEvent{
			Type: blockTypeWarning, Reason: ai.ReasonEmpty, Content: "empty",
		}))
	})

	// Every OTHER warning reason means the context was not rewritten. Treating
	// them as success wrote "compacted (32% → 32%)" into the timeline and marked
	// the member compacted, so the user believed a compaction happened that never
	// did — and could not explain the member's later context overflow.
	for _, reason := range []string{
		ai.ReasonRequestFailed, ai.ReasonAgentNoRun, ai.ReasonBackendExit,
		ai.ReasonAuthRequired, ai.ReasonTimeout, ai.ReasonDisconnect,
	} {
		t.Run("failure warning is an error: "+reason, func(t *testing.T) {
			err := consumeCompactEvent(memberID, ai.StreamEvent{
				Type: blockTypeWarning, Reason: reason, Content: "upstream failed",
			})
			require.Error(t, err)
			assert.Equal(t, "upstream failed", err.Error())
		})
	}

	// A warning with no text still has to fail loudly, naming the reason rather
	// than returning an empty error message.
	t.Run("failure warning without text names its reason", func(t *testing.T) {
		err := consumeCompactEvent(memberID, ai.StreamEvent{
			Type: blockTypeWarning, Reason: ai.ReasonRequestFailed,
		})
		require.Error(t, err)
		assert.Equal(t, ai.ReasonRequestFailed, err.Error())
	})

	t.Run("unrelated events are ignored", func(t *testing.T) {
		assert.NoError(t, consumeCompactEvent(memberID, ai.StreamEvent{Type: "content", Content: "hi"}))
		assert.NoError(t, consumeCompactEvent(memberID, ai.StreamEvent{Type: "done"}))
	})
}

// TestCurrentMemberUsage covers both sources and the persist-on-read behavior.
func TestCurrentMemberUsage(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-usage"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	_, memberID, err := CreateGroup(project, "c", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// Nothing reported yet.
	assert.Nil(t, currentMemberUsage(memberID))

	// Persisted context_state is the fallback.
	PatchContextStateMerge(memberID, map[string]string{
		"usage": `{"used":777,"size":1000}`,
	})
	got := currentMemberUsage(memberID)
	require.NotNil(t, got)
	assert.Equal(t, 777, got.Used)
}

// TestCompactGroupMember_RefusalIsNotSuccess pins the user-visible contract:
// a refused compaction must NOT mark the session compacted (that flag drives
// system-prompt re-injection on the next turn, so a false positive silently
// re-injects a prompt into an uncompacted context).
func TestCompactGroupMember_RefusalIsNotSuccess(t *testing.T) {
	groupID, memberID := setupCompactableGroup(t, "/tmp/grouptest-compact-refused")

	withFakeCompactBackend(t, &fakeCompactBackend{events: []ai.StreamEvent{
		{Type: blockTypeWarning, Reason: ai.ReasonRefused, Content: "nope"},
	}}, nil)

	_, err := CompactGroupMember(context.Background(), groupID, memberID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope")

	var compacted int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT compacted FROM chat_sessions WHERE id = ?", memberID,
	).Scan(&compacted))
	assert.Equal(t, 0, compacted, "a refused compaction must not set the compacted flag")
}

// TestCompactGroupMember_UnknownAgentIsNotCompacted pins the resolve step: the
// member row's agent_id is authoritative (ResolveAgentID would fall back to the
// DEFAULT agent for an empty id and compact the wrong context).
func TestCompactGroupMember_UnknownAgentIsNotCompacted(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-compact-noagent"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, memberID, err := CreateGroup(project, "c", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	require.NoError(t, UpdateSessionTransport(memberID, "acp-stdio"))
	// Blank the agent binding: the row can no longer name a real agent.
	_, werr := store.WriteExec("UPDATE chat_sessions SET agent_id = '' WHERE id = ?", memberID)
	require.NoError(t, werr)

	_, err = CompactGroupMember(context.Background(), groupID, memberID)
	assert.ErrorIs(t, err, ErrGroupMemberNotFound)
}

// TestGroupHasCompactingMember_EmptyGroup pins the short-circuit: an empty
// group id must not scan the map (it would match a compaction whose group id
// happens to be "", which is not a real group).
func TestGroupHasCompactingMember_EmptyGroup(t *testing.T) {
	assert.False(t, GroupHasCompactingMember(""))
}

// TestActiveMember_AgentIDRequired pins the "no agent binding" rejection.
func TestActiveMember_AgentIDRequired(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-active-member"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, memberID, err := CreateGroup(project, "c", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	_, werr := store.WriteExec("UPDATE chat_sessions SET agent_id = '' WHERE id = ?", memberID)
	require.NoError(t, werr)

	_, err = memberRowInGroup(groupID, memberID)
	assert.ErrorIs(t, err, ErrGroupMemberNotFound)
}

// TestActiveMember_ListErrorPropagates pins that a DB failure surfaces rather
// than being reported as "member not found" (the handler maps the two to
// different statuses).
func TestActiveMember_ListErrorPropagates(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-active-member-err"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "c", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	restore := store.SnapshotDBForTest()
	store.Close()
	_, err = memberRowInGroup(groupID, "whatever")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrGroupMemberNotFound),
		"a DB failure must not be reported as a missing member")
	restore()
}

// TestApplyMemberResumeOverrides_IsUsedByCompaction pins that the request handed
// to the backend carries the member's external session id: without it the
// command opens a BRAND NEW agent session and compacts an empty context.
func TestApplyMemberResumeOverrides_IsUsedByCompaction(t *testing.T) {
	groupID, memberID := setupCompactableGroup(t, "/tmp/grouptest-compact-resume")
	require.NoError(t, UpdateExternalSessionID(memberID, "ext-resume"))

	be := &fakeCompactBackend{events: []ai.StreamEvent{{Type: "done"}}}
	withFakeCompactBackend(t, be, nil)

	_, err := CompactGroupMember(context.Background(), groupID, memberID)
	require.NoError(t, err)
	// The ACP backend resolves the external session id itself (via the
	// getExternalSessionID getter keyed by the member row), so what must be
	// true on the request is the resume signal derived from it.
	assert.True(t, be.gotReq.Resume,
		"compaction must resume the member's existing agent session, not start a new one")
	assert.True(t, be.gotReq.HasConversationHistory,
		"the member's history must be advertised so a recovery does not fall back to a fresh session")
}

// TestRunMemberCompact_UsesMemberAgent pins the agent id passed to the backend
// factory: a member row's agent_id is the binding (not the backend name).
func TestRunMemberCompact_UsesMemberAgent(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-compact-agent"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, memberID, err := CreateGroup(project, "c", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	var gotAgent, gotTransport string
	orig := newBackendForAgent
	newBackendForAgent = func(_, agentID, transport string) (ai.AIBackend, error) {
		gotAgent, gotTransport = agentID, transport
		return &fakeCompactBackend{events: []ai.StreamEvent{{Type: "done"}}}, nil
	}
	t.Cleanup(func() { newBackendForAgent = orig })

	_, err = runMemberCompact(context.Background(), groupID, memberID, "agent-host", "acp-stdio")
	require.NoError(t, err)
	assert.Equal(t, "agent-host", gotAgent)
	assert.Equal(t, "acp-stdio", gotTransport)
}

// TestCompactGroupMember_DoesNotLeakIntoRunningSessions pins decision #73: a
// member row must never appear in the user-visible running-session list, even
// while its compaction is in flight.
func TestCompactGroupMember_DoesNotLeakIntoRunningSessions(t *testing.T) {
	groupID, memberID := setupCompactableGroup(t, "/tmp/grouptest-compact-running")

	started := make(chan struct{})
	release := make(chan struct{})
	orig := newBackendForAgent
	newBackendForAgent = func(_, _, _ string) (ai.AIBackend, error) {
		return &blockingCompactBackend{started: started, release: release}, nil
	}
	t.Cleanup(func() { newBackendForAgent = orig })

	done := make(chan error, 1)
	go func() {
		_, err := CompactGroupMember(context.Background(), groupID, memberID)
		done <- err
	}()

	<-started
	// While the compaction runs the member is gated…
	assert.True(t, IsGroupMemberCompacting(memberID))
	assert.True(t, GroupHasCompactingMember(groupID))
	// …but must NOT be advertised as a running session.
	assert.False(t, IsSessionRunning(memberID),
		"a compacting member must not show up in the running-session list")

	close(release)
	require.NoError(t, <-done)
	assert.False(t, IsGroupMemberCompacting(memberID))
}

// blockingCompactBackend blocks inside ExecuteStream until released, so a test
// can observe the in-flight state.
type blockingCompactBackend struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingCompactBackend) Name() string { return "blocking-compact" }

func (b *blockingCompactBackend) ExecuteStream(_ context.Context, _ ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	ch := make(chan ai.StreamEvent, 1)
	close(b.started)
	go func() {
		defer close(ch)
		<-b.release
		ch <- ai.StreamEvent{Type: "done"}
	}()
	return ch, nil
}

// Compile-time guard: the fake must satisfy the backend contract.
var (
	_ ai.AIBackend = (*fakeCompactBackend)(nil)
	_ ai.AIBackend = (*blockingCompactBackend)(nil)
)

// A group message that arrives while a member is compacting must QUEUE, not
// start a group turn.
//
// Compaction drives that member's ACP connection directly and does not hold the
// group's session-runner claim, so EnqueueGroupMessage's TryClaimSessionRun
// would succeed and the orchestrator would send a SECOND prompt on the same ACP
// session: the agent answers "Session is busy", RegisterSession overwrites the
// compaction's stream channel (its usage events are dropped) and promptCancel
// is overwritten too. The chat handler has its own gate, but this is the other
// group entry point — cross-session "send to this group" and IM pushes come
// straight here.
func TestEnqueueGroupMessage_QueuesWhileMemberCompacting(t *testing.T) {
	project := "/tmp/grouptest-enqueue-compacting"
	groupID, memberID := setupCompactableGroup(t, project)

	// Take the compaction slot directly: this test is about the send path, not
	// about driving a real agent.
	require.True(t, beginCompaction(groupID, memberID))
	t.Cleanup(func() { endCompaction(memberID) })

	launched := make(chan string, 1)
	restore := SetRunGroupDrainLoopForTest(func(_ context.Context, gid, _ string, _ int64, _ string, _ []model.FileEntry) {
		select {
		case launched <- gid:
		default:
		}
	})
	t.Cleanup(func() { SetRunGroupDrainLoopForTest(restore) })

	started, err := EnqueueGroupMessage(groupID, "hello", nil, "")
	require.NoError(t, err)
	assert.False(t, started, "a compacting group must not start a turn")

	// The message must be waiting in the queue, not dropped: the compaction's
	// deferred wake consumes it.
	assert.Equal(t, 1, GetQueuedCount(groupID))

	select {
	case gid := <-launched:
		t.Fatalf("the drain loop must not start while a member compacts (got %q)", gid)
	case <-time.After(150 * time.Millisecond):
	}
}

// Once the compaction ends, the deferred wake must consume the queued message.
// This is the second half of the gate: queueing without a wake would leave the
// message stranded until the reaper's next sweep (~30s).
func TestWakeQueuedAfterCompaction_ConsumesQueuedMessage(t *testing.T) {
	project := "/tmp/grouptest-wake-compaction"
	groupID, memberID := setupCompactableGroup(t, project)

	launched := make(chan string, 1)
	restore := SetRunGroupDrainLoopForTest(func(_ context.Context, gid, _ string, _ int64, _ string, _ []model.FileEntry) {
		select {
		case launched <- gid:
		default:
		}
	})
	t.Cleanup(func() { SetRunGroupDrainLoopForTest(restore) })

	require.True(t, beginCompaction(groupID, memberID))
	started, err := EnqueueGroupMessage(groupID, "queued during compact", nil, "")
	require.NoError(t, err)
	require.False(t, started)
	require.Equal(t, 1, GetQueuedCount(groupID))

	endCompaction(memberID)
	wakeQueuedAfterCompaction(groupID)

	select {
	case gid := <-launched:
		assert.Equal(t, groupID, gid)
	case <-time.After(2 * time.Second):
		t.Fatal("the post-compaction wake must start the consumer")
	}
}

// The wake must NOT depend on the queue being non-empty at that instant. The
// send path inserts the row and only then releases the runner claim; a wake that
// snapshots GetQueuedCount can legitimately run between the two and see zero,
// leaving the message stranded. EnsureConsumer already no-ops on an empty queue,
// so the wake is unconditional and this test pins that.
func TestWakeQueuedAfterCompaction_EmptyQueueIsHarmless(t *testing.T) {
	project := "/tmp/grouptest-wake-empty"
	groupID, _ := setupCompactableGroup(t, project)

	launched := make(chan string, 1)
	restore := SetRunGroupDrainLoopForTest(func(_ context.Context, gid, _ string, _ int64, _ string, _ []model.FileEntry) {
		select {
		case launched <- gid:
		default:
		}
	})
	t.Cleanup(func() { SetRunGroupDrainLoopForTest(restore) })

	require.Equal(t, 0, GetQueuedCount(groupID))
	wakeQueuedAfterCompaction(groupID)

	select {
	case gid := <-launched:
		t.Fatalf("an empty queue must not start a drain loop (got %q)", gid)
	case <-time.After(150 * time.Millisecond):
	}
	// And the session must not be left claimed as "running".
	assert.False(t, IsSessionRunning(groupID), "the claim must be released when there is nothing to run")
}
