# Remove AutoResumeBackend Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove AutoResumeBackend entirely — when CLI agents hit ExitPlanMode, the stream ends normally without auto-resume.

**Architecture:** Delete AutoResumeBackend struct and all wrapping logic. Remove NeedsAutoResume from registration. Remove resume_split event from the entire pipeline (backend → service → ws → frontend). Remove handleResumeSplit from SessionExecutor. Remove ChatRequest.Resume field usage by AutoResume (field itself stays — it's also used by ACP ResumeSession). Clean up all 12 backend registrations and their tests.

**Tech Stack:** Go (backend), TypeScript/Vue (frontend)

---

### Task 1: Delete AutoResumeBackend core and update factory

**Files:**
- Delete: `internal/ai/auto_resume.go`
- Modify: `internal/ai/factory.go`
- Delete: `internal/ai/auto_resume_test.go`

**Step 1: Delete auto_resume.go**

Remove the entire file. This deletes `AutoResumeBackend` struct, `mergeStreams`, and all ExitPlanMode detection logic.

**Step 2: Update factory.go — remove NeedsAutoResume and AutoResume wrapping**

In `internal/ai/factory.go`:
- Remove `NeedsAutoResume bool` field from `BackendFactoryEntry`
- Change `RegisterBackend` signature from `func RegisterBackend(id string, newBackend func() AIBackend, needsAutoResume bool)` to `func RegisterBackend(id string, newBackend func() AIBackend)`
- Remove `NeedsAutoResume: needsAutoResume` in `RegisterBackend` body
- In `NewBackend()`, remove the AutoResume wrapping block:
  ```go
  // DELETE these lines:
  if entry.NeedsAutoResume {
      backend = &AutoResumeBackend{inner: backend}
  }
  ```
- Update comments referencing AutoResume (lines 74, 110)

**Step 3: Delete auto_resume_test.go**

Remove the entire test file (~600 lines).

**Step 4: Verify build compiles**

Run: `go build ./internal/ai/`
Expected: compile errors from 12 backend files still passing `needsAutoResume` arg

**Step 5: Commit**

```bash
git add -A internal/ai/auto_resume.go internal/ai/auto_resume_test.go internal/ai/factory.go
git commit -m "refactor: remove AutoResumeBackend core and factory wrapping"
```

---

### Task 2: Update 12 backend registrations (remove needsAutoResume arg)

**Files:**
- Modify: `internal/ai/backends/claude/cli.go`
- Modify: `internal/ai/backends/codebuddy/cli.go`
- Modify: `internal/ai/backends/opencode/cli.go`
- Modify: `internal/ai/backends/codex/custom.go`
- Modify: `internal/ai/backends/qoder/cli.go`
- Modify: `internal/ai/backends/vecli/custom.go`
- Modify: `internal/ai/backends/deepseek/cli.go`
- Modify: `internal/ai/backends/pi/cli.go`
- Modify: `internal/ai/backends/cline/cli.go`
- Modify: `internal/ai/backends/kimi/cli.go`
- Modify: `internal/ai/backends/copilot/cli.go`
- Modify: `internal/ai/backends/mimo/cli.go`

**Step 1: Change all RegisterBackend calls**

Each file has `ai.RegisterBackend("xxx", newFn, needsAutoResume_bool)`. Change to `ai.RegisterBackend("xxx", newFn)` — remove the boolean argument:

| File | Old | New |
|------|-----|-----|
| claude/cli.go:13 | `ai.RegisterBackend("claude", newClaudeBackend, true)` | `ai.RegisterBackend("claude", newClaudeBackend)` |
| codebuddy/cli.go:13 | `ai.RegisterBackend("codebuddy", newCodebuddyBackend, true)` | `ai.RegisterBackend("codebuddy", newCodebuddyBackend)` |
| opencode/cli.go:38 | `ai.RegisterBackend("opencode", newOpenCodeBackend, false)` | `ai.RegisterBackend("opencode", newOpenCodeBackend)` |
| codex/custom.go:10 | `ai.RegisterBackend("codex", newCodexBackend, false)` | `ai.RegisterBackend("codex", newCodexBackend)` |
| qoder/cli.go:13 | `ai.RegisterBackend("qoder", newQoderBackend, true)` | `ai.RegisterBackend("qoder", newQoderBackend)` |
| vecli/custom.go:10 | `ai.RegisterBackend("vecli", newVeCLIBackend, false)` | `ai.RegisterBackend("vecli", newVeCLIBackend)` |
| deepseek/cli.go:20 | `ai.RegisterBackend("deepseek", newDeepSeekBackend, true)` | `ai.RegisterBackend("deepseek", newDeepSeekBackend)` |
| pi/cli.go:20 | `ai.RegisterBackend("pi", newPiBackend, true)` | `ai.RegisterBackend("pi", newPiBackend)` |
| cline/cli.go:13 | `ai.RegisterBackend("cline", newClineBackend, true)` | `ai.RegisterBackend("cline", newClineBackend)` |
| kimi/cli.go:41 | `ai.RegisterBackend("kimi", newKimiBackend, true)` | `ai.RegisterBackend("kimi", newKimiBackend)` |
| copilot/cli.go:13 | `ai.RegisterBackend("copilot", newCopilotBackend, true)` | `ai.RegisterBackend("copilot", newCopilotBackend)` |
| mimo/cli.go:13 | `ai.RegisterBackend("mimo", newMimoBackend, true)` | `ai.RegisterBackend("mimo", newMimoBackend)` |

Also remove AutoResume-related comments:
- codex/custom.go:26 `// AutoResume is not needed.` → remove this line
- vecli/custom.go:23 `// AutoResume is not needed.` → remove this line

**Step 2: Update BackendPlugin — remove NeedsAutoResume field**

In `internal/ai/backends/plugin.go`:
- Remove `NeedsAutoResume bool` field and its comment from `BackendPlugin` struct

**Step 3: Update all 12 BackendPlugin registrations — remove NeedsAutoResume**

Each backend's `backends.Register()` call has `NeedsAutoResume: true/false`. Remove this field from all 12 registrations.

**Step 4: Verify build compiles**

Run: `go build ./internal/ai/...`
Expected: PASS

**Step 5: Commit**

```bash
git add -A internal/ai/backends/
git commit -m "refactor: remove NeedsAutoResume from all backend registrations"
```

---

### Task 3: Remove resume_split from StreamEvent and interface

**Files:**
- Modify: `internal/ai/interface.go`
- Modify: `internal/ws/stream_hub.go`

**Step 1: Remove resume_split from StreamEvent type comment**

In `internal/ai/interface.go`, line 259 has the event type list. Remove `"resume_split"` from the comment string. The actual string values are runtime-determined, so removing from the comment is sufficient for documentation — but we need to ensure no code emits `resume_split` events anymore (which is covered by deleting auto_resume.go).

**Step 2: Remove resume_split case from StreamEventToPayload**

In `internal/ws/stream_hub.go`, line 163:
```go
case "resume_split":
```
Returns nil. Remove this case entirely. If `resume_split` events somehow still arrive, they'd hit the default case (which also returns nil), so this is safe.

**Step 3: Remove EmitResumeSplitEvent from StreamHub**

In `internal/ws/stream_hub.go`, remove the entire `EmitResumeSplitEvent` function (lines 393-428 approximately). This function sends `resume_split` events with a `message_id` payload. No code will call it after AutoResume removal.

**Step 4: Commit**

```bash
git add -A internal/ai/interface.go internal/ws/stream_hub.go
git commit -m "refactor: remove resume_split event type from interface and StreamHub"
```

---

### Task 4: Remove handleResumeSplit from SessionExecutor

**Files:**
- Modify: `internal/service/session_executor.go`
- Modify: `internal/service/tool_calls.go`

**Step 1: Remove resume_split event handling in RunWithChannel**

In `session_executor.go`, the event loop at ~line 171-173:
```go
// resume_split: finalize current message, start new one
if event.Type == "resume_split" {
    e.handleResumeSplit()
```
Remove this block. Also remove the comment at line 194 about resume_split hub emission deferral.

At line 203-204:
```go
// Emit to StreamHub for WS fan-out (except resume_split, handled separately)
if event.Type != "resume_split" {
```
Change this to just emit all events:
```go
ws.EmitToSession(e.cfg.SessionID, event)
```
Remove the conditional skip and the special-case comment.

**Step 2: Delete handleResumeSplit function**

Remove the entire `handleResumeSplit()` method from `SessionExecutor` (lines ~393-457). This method:
- Finalizes the current streaming message
- Creates a new streaming message placeholder
- Emits resume_split via StreamHub
- Resets blocks, metadata, rawOutput

All of this is AutoResume-specific and no longer needed.

**Step 3: Update comment about resume_split in tool_calls.go**

In `internal/service/tool_calls.go`, line 72 has a comment about AutoResumeBackend resume splits. Update or remove this comment since resume splits no longer exist.

**Step 4: Commit**

```bash
git add -A internal/service/session_executor.go internal/service/tool_calls.go
git commit -m "refactor: remove handleResumeSplit from SessionExecutor"
```

---

### Task 5: Remove resume_split from handler and ws tests

**Files:**
- Modify: `internal/handler/chat_history.go`
- Modify: `internal/ws/stream_hub_test.go`

**Step 1: Update handler comment**

In `internal/handler/chat_history.go`, line 214-215 has a comment about AutoResumeBackend resume splits creating multiple assistant messages. Remove or update this comment — the `sessionId` fallback logic in tool call lookup is still needed for ACP sessions, so keep the fallback but rephrase the comment to remove AutoResume reference.

**Step 2: Remove resume_split test cases from stream_hub_test.go**

Remove:
- Line 127-128: resume_split returns nil payload test
- `TestStreamEventToPayload_ResumeSplit` function (lines ~232-235)
- All 3 `TestStreamHub_EmitResumeSplitEvent_*` functions (lines ~641-675)

**Step 3: Commit**

```bash
git add -A internal/handler/chat_history.go internal/ws/stream_hub_test.go
git commit -m "refactor: remove resume_split references from handler and ws tests"
```

---

### Task 6: Update factory tests and backend plugin tests

**Files:**
- Modify: `internal/ai/factory_test.go`
- Modify: `internal/ai/backends/registry_test.go`
- Modify: All 12 `internal/ai/backends/*/cli_test.go` or `custom_test.go`

**Step 1: Update factory_test.go**

Remove all `NeedsAutoResume` assertions. The test currently checks whether backends are wrapped with `AutoResumeBackend`. After removal, all CLI backends should be the raw `CLIBackend` (no wrapping). Simplify assertions:

- Remove `NeedsAutoResume` field from `BackendFactoryEntry` test setup
- Remove assertions like `_, ok := backend.(*ai.AutoResumeBackend)` — replace with `_, ok := backend.(*ai.CLIBackend)` for CLI backends
- Remove comments about AutoResume wrapping
- Remove `TestNewBackendForAgent_ACPNoAutoResume` — ACP was never wrapped with AutoResume, this test is now trivially true
- Remove `TestNewBackendForAgentWithTransport_ACPFallbackToCLI` AutoResume assertions

**Step 2: Update registry_test.go**

Remove `NeedsAutoResume` assertions from `TestRegister` and `TestLookup`. Remove the field from test `BackendPlugin` setup.

**Step 3: Update all 12 backend NeedsAutoResume tests**

Each backend has a `TestXxxPlugin_NeedsAutoResume` test. **Delete all 12 of these tests** — the concept no longer exists:

| File | Test function to delete |
|------|------------------------|
| claude/cli_test.go | TestClaudePlugin_NeedsAutoResume |
| codebuddy/cli_test.go | TestCodebuddyPlugin_NeedsAutoResume |
| opencode/cli_test.go | TestOpenCodePlugin_NeedsAutoResume |
| qoder/cli_test.go | TestQoderPlugin_NeedsAutoResume |
| pi/cli_test.go | TestPiPlugin_NeedsAutoResume |
| deepseek/cli_test.go | TestDeepSeekPlugin_NeedsAutoResume |
| cline/cli_test.go | TestClinePlugin_NeedsAutoResume |
| kimi/cli_test.go | TestKimiPlugin_NeedsAutoResume |
| copilot/cli_test.go | TestCopilotPlugin_NeedsAutoResume |
| mimo/cli_test.go | TestMimoPlugin_NeedsAutoResume |
| codex/custom_test.go | TestCodexPlugin_NeedsAutoResume |
| vecli/custom_test.go | TestVeCLIPlugin_NeedsAutoResume |

**Step 4: Run Go tests to verify**

Run: `go test ./internal/ai/... ./internal/ai/backends/... ./internal/ws/... -count=1`
Expected: PASS (some integration tests may fail if they reference AutoResume — handle in Task 7)

**Step 5: Commit**

```bash
git add -A internal/ai/factory_test.go internal/ai/backends/*/cli_test.go internal/ai/backends/*/custom_test.go internal/ai/backends/registry_test.go
git commit -m "refactor: remove AutoResume assertions from factory and backend tests"
```

---

### Task 7: Update session_executor tests and integration tests

**Files:**
- Modify: `internal/service/session_executor_test.go`
- Modify: `internal/service/session_executor_extra_test.go`
- Modify: `internal/ai/integration_test.go`
- Modify: `internal/ai/acp_integration_test.go`
- Modify: `internal/handler/chat_history_session_test.go`

**Step 1: Remove resume_split test cases from session_executor_test.go**

Remove these test functions entirely:
- `TestSessionExecutor_HandleResumeSplit_WithRawOutput` (lines ~242-281)
- `TestSessionExecutor_HandleResumeSplit` (lines ~1036-1095)
- `TestSessionExecutor_HandleResumeSplit_AskQuestionConversion` (lines ~1099-1133)
- `TestSessionExecutor_RunWithChannel_ResumeSplit` (lines ~1646-1684)
- `TestSessionExecutor_HandleResumeSplit_Errors` (lines ~2049-2111)

Remove references to `handleResumeSplit` in other tests (line 908 comment about "Finalize and handleResumeSplit").

**Step 2: Remove resume_split tests from session_executor_extra_test.go**

Remove:
- `TestSessionExecutor_HandleResumeSplit_NoRawOutput` (lines ~596-621)
- `TestSessionExecutor_HandleResumeSplit_SetsStreamingMessageID` (lines ~875-904)
- The section header comment "handleResumeSplit additional coverage"

**Step 3: Update integration_test.go**

- Remove AutoResume-specific comments (lines 42, 59, 81, 100, 105, 118, 128, 133, 161, 196, 240, 284)
- Remove `TestIntegration_AutoResume_ExitPlanMode` test (lines ~1082-1122)
- Update comment at line 896 about AutoResumeBackend forwarding done events
- Change backend type assertions that check `*AutoResumeBackend` to check `*CLIBackend` instead

**Step 4: Update acp_integration_test.go**

- Rename `TestACPIntegration_ProcessCrash_AutoResume` to `TestACPIntegration_ProcessCrash` (remove AutoResume from name)
- Remove AutoResume reference in comments

**Step 5: Update chat_history_session_test.go**

Line 484: Remove the comment "Simulate AutoResumeBackend: two assistant messages" — the test scenario of multiple assistant messages still exists for ACP sessions, so keep the test logic but update the comment.

**Step 6: Commit**

```bash
git add -A internal/service/session_executor_test.go internal/service/session_executor_extra_test.go internal/ai/integration_test.go internal/ai/acp_integration_test.go internal/handler/chat_history_session_test.go
git commit -m "refactor: remove AutoResume test cases from service and integration tests"
```

---

### Task 8: Remove resume_split from frontend

**Files:**
- Modify: `web/src/composables/useChatStream.ts`
- Modify: `web/src/composables/useTaskExecStream.ts`
- Modify: `web/src/composables/useToolDetailDrawer.ts`
- Modify: `web/src/composables/__tests__/useChatStream.test.ts`
- Modify: `web/src/composables/__tests__/useTaskExecStream.test.ts`

**Step 1: Remove resume_split handler from useChatStream.ts**

Remove the entire `case 'resume_split':` block (lines ~209-228). This block:
- Finalizes Phase 1 streaming message
- Creates Phase 2 streaming message placeholder
- Resets thinkingBlockCounter
- Calls onRenderNeeded and debouncedRender

**Step 2: Remove resume_split handler from useTaskExecStream.ts**

Remove the `case 'resume_split':` block (lines ~90-101).

**Step 3: Update useToolDetailDrawer.ts comment**

Line 36-39 has a comment about AutoResumeBackend resume splits. Remove the AutoResume reference — keep the `sessionId` fallback logic but rephrase the comment to reference ACP sessions or multiple assistant messages generically.

**Step 4: Remove resume_split tests from useChatStream.test.ts**

Remove the entire `describe('WS event handling -- resume_split', ...)` block (lines ~1676-1797) and all its nested tests:
- `should finalize Phase 1 message and start Phase 2`
- `should set Phase 2 message id from resume_split message_id`
- `should call onRenderNeeded on resume_split`

**Step 5: Remove resume_split test from useTaskExecStream.test.ts**

Remove `it('handles resume_split event', ...)` (lines ~205-213).

**Step 6: Commit**

```bash
git add -A web/src/composables/ web/src/composables/__tests__/
git commit -m "refactor: remove resume_split handling from frontend composables"
```

---

### Task 9: Clean up remaining references and documentation

**Files:**
- Modify: `internal/ai/common_stream.go` — keep ExitPlanMode mapping (it's still a valid tool name)
- Modify: `internal/ai/acp_tool_names.go` — keep ExitPlanMode mapping (still valid for ACP)
- Modify: `internal/ai/acp_client.go` — update comment about ExitPlanMode (line 272)
- Modify: `AGENTS.md` — remove AutoResumeBackend from architecture table
- Modify: `docs/spec/core/ai-backend.md` — remove AutoResume section
- Modify: `docs/spec/core/session-lifecycle.md` — remove AutoResume references
- Modify: `docs/timer/spec-update.md` — update AutoResume items
- Modify: Memory file: `auto_resume_backend_rule.md` — delete

**Step 1: Keep ExitPlanMode tool name mappings**

ExitPlanMode is still a valid tool name used by ACP and CLI backends. The tool name normalization in `common_stream.go` (line 106-107) and `acp_tool_names.go` (line 96) should remain. The frontend renderer for ExitPlanMode (`renderToolDetail.ts` line 787/1490) and icon (`icons.ts` line 39) should also remain — they handle the tool call display, not the auto-resume logic.

Only update the comment in `acp_client.go` line 272 that references ExitPlanMode in an AutoResume context.

**Step 2: Update AGENTS.md**

In the architecture table, remove the row for AutoResumeBackend:
```
| `internal/ai/` + `backends/` | AI 后端抽象：`AIBackend` → `CLIBackend`（CLI+行解析）→ `AutoResumeBackend`（计划模式自动续行）→ `ACPBackend`...
```
Change to:
```
| `internal/ai/` + `backends/` | AI 后端抽象：`AIBackend` → `CLIBackend`（CLI+行解析）或 `ACPBackend`（JSON-RPC over stdio）。12 个后端子包通过 `ai.RegisterBackend()` 注册 |
```

**Step 3: Update spec docs**

- `docs/spec/core/ai-backend.md`: Remove the AutoResume section (lines 94-133), update the mermaid diagram (lines 19-20), remove the AutoResume participant from sequence diagram (lines 74-91)
- `docs/spec/core/session-lifecycle.md`: Remove AutoResume references (line 96)

**Step 4: Delete memory file**

Delete `/home/xulongzhe/.codebuddy/projects/home-xulongzhe-projects-clawbench/memory/auto_resume_backend_rule.md` and remove its entry from `MEMORY.md`.

**Step 5: Commit**

```bash
git add -A AGENTS.md docs/ internal/ai/acp_client.go
git commit -m "refactor: remove AutoResume references from docs and comments"
```

---

### Task 10: Run full test suite and verify

**Step 1: Run Go tests**

```bash
go test ./internal/ai/... -count=1 -timeout 60s
go test ./internal/ai/backends/... -count=1 -timeout 60s
go test ./internal/ws/... -count=1 -timeout 60s
go test ./internal/service/... -count=1 -timeout 120s
go test ./internal/handler/... -count=1 -timeout 120s
```

Expected: All PASS

**Step 2: Run frontend tests**

```bash
cd web && npx vitest run --reporter=verbose
```

Expected: All PASS (resume_split test cases removed, no references remain)

**Step 3: Run build**

```bash
./build.sh
```

Expected: Build succeeds (Go binary + Vue frontend)

**Step 4: Run pre-push checks**

```bash
./scripts/pre-push-checks.sh
```

Expected: PASS (lint + test + build + typecheck + coverage)

**Step 5: Final commit if any fixes needed**

If any test failures or issues arise during verification, fix them and commit.

---

### Task 11: Remove ChatRequest.Resume field (if safe)

**Note:** ChatRequest.Resume has two uses:
1. AutoResumeBackend sets `Resume=true` for the Phase 2 stream — this is the usage we're removing
2. ACP ResumeSession uses Resume indirectly through `req.Resume` in some backend args builders

**Investigation needed:** Check if any CLI `BuildArgsFn` references `req.Resume` for session resume flags (e.g., `--resume`). If CLI backends use `req.Resume` for legitimate session resume (user explicitly continues a session), keep the field. If it's only used by AutoResume, remove it.

**Step 1: Search for req.Resume usage in BuildArgsFn**

Search all backend BuildArgsFn implementations for `req.Resume`. Report findings.

**Step 2: Decide**

- If `req.Resume` is used by any BuildArgsFn for `--resume`/`--continue` flags: **Keep the field** — it serves legitimate session resume
- If only used by AutoResume: **Remove the field** and update `ShouldInjectSystemPrompt()` logic that references `r.Resume`

**Step 3: Update accordingly and commit**

This task may result in either keeping Resume (with AutoResume reference removed from its comment) or removing it entirely.

---

## Notes

- **ExitPlanMode tool name, renderer, and icon are kept** — they handle displaying the tool call in chat, not auto-resume logic
- **ChatRequest.Resume field investigation** — may need to keep for legitimate session resume
- **ACP ResumeSession** — completely separate from AutoResume, not affected
- **common_stream.go ExitPlanMode mapping** — kept for tool name normalization
- **acp_tool_names.go ExitPlanMode entry** — kept for ACP tool kind resolution
