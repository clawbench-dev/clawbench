# AI Backend Integration Test Report

Date: 2026-06-17

**Result: 96 PASS / 0 FAIL** | Total: 20m37s

---

## CLI Integration Tests (7 backends x 11 tests)

| Test | claude | codebuddy | opencode | codex | qoder | deepseek | vecli |
|---|---|---|---|---|---|---|---|
| NewSession | PASS 7.7s | PASS 6.8s | PASS 12.8s | PASS 8.1s | PASS 13.2s | PASS 1.4s | PASS 7.6s |
| StreamEvents | PASS 6.2s | PASS 6.9s | PASS 9.2s | PASS 11.4s | PASS 12.8s | PASS 4.0s | PASS 8.9s |
| ResumeSession | PASS 16.5s | PASS 17.8s | PASS 12.1s | PASS 16.2s | PASS 27.1s | PASS 3.6s | - |
| CancelMidStream | PASS 6.9s | PASS 7.7s | PASS 43.0s | PASS 25.4s | - | PASS 2.1s | PASS 10.2s |
| InvalidWorkDir | PASS 0.0s | PASS 0.0s | PASS 0.0s | - | - | - | PASS 0.0s |
| SystemPromptInjection | PASS 9.6s | PASS 7.5s | PASS 5.8s | PASS 11.4s | PASS 15.2s | PASS 2.2s | PASS 30.4s |
| MultiTurnResume | PASS 19.5s | PASS 30.3s | PASS 16.0s | PASS 20.9s | PASS 24.9s | PASS 5.1s | - |
| ResumeSessionIDConsistency | PASS 9.2s | PASS 14.9s | - | - | PASS 14.9s | - | - |
| ResumeAfterCancel | PASS 20.1s | PASS 33.0s | PASS 17.3s | PASS 52.2s | PASS 34.8s | PASS 5.5s | - |
| ResumeMetadataCapture | PASS 11.5s | PASS 17.4s | PASS 12.5s | PASS 38.0s | PASS 19.9s | PASS 3.2s | - |
| AutoResume_ExitPlanMode | PASS | - | - | - | - | - | - |

"-" means the backend does not support this feature or is not in the test scope.

### Test Descriptions

| Test | Description |
|---|---|
| NewSession | Create a new session, send a prompt, verify content + metadata + done events |
| StreamEvents | Verify stream completeness: content/thinking events, metadata, session ID mechanism, raw_output |
| ResumeSession | 2-turn: new session -> resume with same session ID, verify content continues |
| CancelMidStream | Cancel context after first content event, verify partial content received |
| InvalidWorkDir | Execute with nonexistent WorkDir, verify error/warning events |
| SystemPromptInjection | Inject system prompt, verify stream completes (best-effort marker check) |
| MultiTurnResume | 3-turn: new -> resume -> resume, verify all turns produce content + metadata + done |
| ResumeSessionIDConsistency | Verify session ID in metadata stays consistent across new -> resume |
| ResumeAfterCancel | Cancel mid-stream, then resume the session, verify content continues |
| ResumeMetadataCapture | Verify metadata fields (SessionID, Model, InputTokens) in new + resumed sessions |
| AutoResume_ExitPlanMode | Claude-specific: trigger plan mode exit, verify AutoResume split + resume flow |

---

## ACP Integration Tests (CodeBuddy ACP, 28 tests)

| Category | Test | Result | Time |
|---|---|---|---|
| Connection Management | NewSession_CreateAndCapture | PASS | 10.1s |
| | ConnReuse_SameSession | PASS | 11.8s |
| | MultipleSessions_IsolatedConns | PASS | 14.4s |
| | ExplicitClose_NewSessionCreated | PASS | 13.5s |
| Fault Tolerance | ProcessCrash_AutoResume | PASS | 16.7s |
| | PeerDisconnect_RetryPrompt | PASS | 23.9s |
| | IdleSweep_ConnectionRecycled | PASS | 13.9s |
| | SSEDisconnect_DrainAndContinue | PASS | 7.3s |
| | SSEReconnect_StateReemitted | PASS | 9.0s |
| Config Switching | ModeSwitch_CodeToPlan | PASS | 8.1s |
| | ModelSwitch_ChangeModel | PASS | 8.6s |
| | ThinkingEffortSwitch | PASS | 13.1s |
| | UnsupportedConfig_GracefulDegradation | PASS | 6.1s |
| | ConfigDedup_NoResend | PASS | 7.5s |
| | ConfigKilledConnection_RetrySkipsConfig | PASS | 5.6s |
| Post-Crash State | ResumeAfterCrash_ModePreserved | PASS | 15.7s |
| | ResumeAfterCrash_ModelPreserved | PASS | 17.3s |
| | ResumeAfterCrash_ThinkingPreserved | PASS | 14.1s |
| | ResumeAfterCrash_CommandsPreserved | PASS | 14.1s |
| | ResumeAfterCrash_ConfigDedupReset | PASS | 12.0s |
| | ResumeAfterCrash_PlanStateLost | PASS | 17.1s |
| Long Running | LongRunning_MultipleTurns | PASS | 17.6s |
| | LongRunning_ConfigStateConsistency | PASS | 8.7s |
| Cancel/Crash Resume | UserCancel_ResumeConversation | PASS | 42.0s |
| | ProcessCrash_ResumeConversation | PASS | 26.9s |
| | MultipleCancel_Resume | PASS | 24.4s |
| | MultipleCrash_Resume | PASS | 24.0s |
| | CancelAndCrash_ResumeConversation | PASS | 25.9s |

---

## Backend Capability Matrix

| Backend | HasModelInMeta | HasSessionIDInMeta | HasTokenUsageInMeta | EmitsSessionCapture | SupportsResume | SkipNewSessionID |
|---|---|---|---|---|---|---|
| claude | yes | yes | yes | no | yes | no |
| codebuddy | yes | yes | yes | no | yes | no |
| opencode | no | yes | no | yes | yes | yes |
| codex | no | yes | no | yes | yes | yes |
| qoder | yes | yes | no | no | yes | no |
| deepseek | yes | yes | no | yes | yes | yes |
| vecli | yes | no | no | no | no | yes |

- **HasModelInMeta**: Backend includes model name in metadata event
- **HasSessionIDInMeta**: Backend includes session ID in metadata event
- **HasTokenUsageInMeta**: Backend reports InputTokens in metadata event
- **EmitsSessionCapture**: Backend emits `session_capture` event for auto-captured session IDs
- **SupportsResume**: Backend supports `--resume` or equivalent for session continuation
- **SkipNewSessionID**: Backend generates own session IDs (cannot accept ClawBench UUID on new session)

---

## How to Run

```bash
# CLI integration tests (all backends)
go test -tags integration -run "TestIntegration_CLI" -v -timeout 1800s ./internal/ai/

# ACP integration tests (CodeBuddy ACP)
go test -tags integration -run "TestACPIntegration" -v -timeout 1800s ./internal/ai/

# Single backend
go test -tags integration -run "TestIntegration_CLI_NewSession/claude" -v -timeout 300s ./internal/ai/

# Full suite
go test -tags integration -run "TestIntegration_CLI|TestACPIntegration" -v -timeout 1800s ./internal/ai/
```
