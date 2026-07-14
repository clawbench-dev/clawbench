# TTS Technical Debt Cleanup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Eliminate TTS streaming/non-streaming code duplication, add streaming path test coverage, and remove unnecessary nolint directives in `internal/handler/tts.go`.

**Architecture:** Extract a `ttsRunJob` helper that parameterizes the streaming vs non-streaming difference via a `synthesizeFunc` closure. This collapses the two 60-line goroutine branches into one unified path. The `audioExtForProvider` helper replaces the 3 shadowed-ok type assertions. Both changes reduce cyclomatic complexity enough to remove the `gocyclo`/`gocognit` nolints on `TTSGenerate`. A `mockStreamingSpeechProvider` is added for test coverage of the streaming path.

**Tech Stack:** Go 1.24, testify/assert

---

### Task 1: Add `mockStreamingSpeechProvider` test double

**Files:**
- Modify: `internal/handler/tts_test.go`

**Step 1: Write the test double**

Add `mockStreamingSpeechProvider` that implements both `speech.SpeechProvider` and `speech.StreamingSpeechProvider`:

```go
// mockStreamingSpeechProvider is a test double for speech.StreamingSpeechProvider.
type mockStreamingSpeechProvider struct {
	mockSpeechProvider               // embed non-streaming mock
	streamCalled       bool
	lastStreamText     string
	lastStreamLang     string
	streamErr          error
	// streamBlock, if non-nil, is closed before SynthesizeStream returns.
	// Same pattern as mockSpeechProvider.synthesizeBlock.
	streamBlock chan struct{}
}

func (m *mockStreamingSpeechProvider) SynthesizeStream(ctx context.Context, text string, outputPath string, language string, chunkCh chan<- []byte) error {
	m.streamCalled = true
	m.lastStreamText = text
	m.lastStreamLang = language
	if m.streamErr != nil {
		return m.streamErr
	}
	// Create a dummy audio file at outputPath
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, []byte("fake streaming audio data"), 0o644); err != nil {
		return err
	}
	// Send a test chunk to chunkCh
	select {
	case chunkCh <- []byte("fake chunk"):
	case <-ctx.Done():
		return ctx.Err()
	}
	// Block until the test releases us (if configured)
	if m.streamBlock != nil {
		select {
		case <-m.streamBlock:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Compile-time assertion
var _ speech.StreamingSpeechProvider = (*mockStreamingSpeechProvider)(nil)
```

**Step 2: Run existing tests to verify no breakage**

Run: `go test ./internal/handler/ -run TestTTS -count=1`
Expected: All existing tests PASS (new type is not yet used)

**Step 3: Commit**

```bash
git add internal/handler/tts_test.go
git commit -m "test(tts): add mockStreamingSpeechProvider for streaming path tests"
```

---

### Task 2: Add streaming path test using existing code (before refactor)

**Files:**
- Modify: `internal/handler/tts_test.go`

This test proves the streaming path works with the current code, and will continue working after the refactor in Task 4.

**Step 1: Write the test**

```go
func TestTTSGenerate_StreamingSuccess(t *testing.T) {
	mockProvider := &mockStreamingSpeechProvider{
		mockSpeechProvider: mockSpeechProvider{},
	}
	mockSum := &mockSummarizer{result: "这是流式核心结论"}
	env, teardown := setupTTSTest(t, &mockProvider.mockSpeechProvider, mockSum)
	defer teardown()

	// Replace the global provider with the streaming mock
	SetSpeechProvider(mockProvider)
	defer func() { SetSpeechProvider(&mockProvider.mockSpeechProvider) }()

	text := "这是一段较长的AI回复内容，需要被流式总结为语音。包含了详细的分析和代码示例。"
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": text})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Contains(t, resp, "jobId")
	assert.Equal(t, true, resp["streaming"], "should indicate streaming mode")

	// Wait for the background goroutine to complete
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	job, ok := service.GetTTSJob(cacheKey)
	if ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("streaming TTS job did not complete in time")
		}
	}

	assert.True(t, mockSum.called)
	assert.True(t, mockProvider.streamCalled)
}
```

**Step 2: Run the new test to verify it passes**

Run: `go test ./internal/handler/ -run TestTTSGenerate_StreamingSuccess -count=1 -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/handler/tts_test.go
git commit -m "test(tts): add streaming path success test"
```

---

### Task 3: Add streaming fallback test (summarize failure in streaming mode)

**Files:**
- Modify: `internal/handler/tts_test.go`

**Step 1: Write the test**

```go
func TestTTSGenerate_StreamingSummarizeFailure(t *testing.T) {
	mockProvider := &mockStreamingSpeechProvider{
		mockSpeechProvider: mockSpeechProvider{},
	}
	mockSum := &mockSummarizer{err: context.DeadlineExceeded}
	env, teardown := setupTTSTest(t, &mockProvider.mockSpeechProvider, mockSum)
	defer teardown()

	SetSpeechProvider(mockProvider)
	defer func() { SetSpeechProvider(&mockProvider.mockSpeechProvider) }()

	text := "这是一段需要流式总结的长文本内容，由于摘要失败会触发fallback。内容足够长以触发摘要流程。"
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": text})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Wait for the background goroutine to complete
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	job, ok := service.GetTTSJob(cacheKey)
	if ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("streaming TTS job did not complete in time")
		}
	}

	// Summarizer failed, fallback SimpleSummarizer used — synthesize should still be called
	assert.True(t, mockSum.called)
	assert.True(t, mockProvider.streamCalled, "stream synthesize should be called with fallback summary")
}
```

**Step 2: Run the test**

Run: `go test ./internal/handler/ -run TestTTSGenerate_StreamingSummarizeFailure -count=1 -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/handler/tts_test.go
git commit -m "test(tts): add streaming summarize failure fallback test"
```

---

### Task 4: Extract `ttsRunJob` to unify streaming/non-streaming goroutines

**Files:**
- Modify: `internal/handler/tts.go`

This is the core refactor. The two goroutine branches (lines 198-260 streaming, lines 261-317 non-streaming) are unified into a single `ttsRunJob` function.

**Step 1: Write the `ttsRunJob` helper**

Add this function before `TTSGenerate`:

```go
// ttsSynthesizeFunc is a closure that performs the synthesize step and returns any error.
type ttsSynthesizeFunc func(ctx context.Context, summary, absAudioPath, language string) error

// ttsRunJob runs the TTS job in a background goroutine: summarize → synthesize → result.
// The caller provides a synthesizeFunc closure that handles the streaming vs non-streaming difference.
// audioCh is non-nil for streaming jobs (closed after synthesize); nil for non-streaming jobs.
func ttsRunJob(
	ctx context.Context,
	cacheKey string,
	projectPath, relAudioPath, absAudioPath string,
	req ttsGenerateRequest,
	curSummarizer summarize.Summarizer,
	errSummarizeFailed, errSynthesizeFailed string,
	isStreaming bool,
	synthesizeFunc ttsSynthesizeFunc,
	audioCh chan []byte,
) {
	var job *service.TTSJob
	cancel := func() {} // placeholder, overwritten below

	ctx, jobCancel := context.WithCancel(ctx)
	cancel = jobCancel

	if isStreaming {
		job = service.RegisterStreamingTTSJob(cacheKey, cancel)
	} else {
		job = service.RegisterTTSJob(cacheKey, cancel)
	}

	go func() {
		defer service.UnregisterTTSJob(cacheKey)
		defer service.CloseTTSJobDone(cacheKey)
		defer cancel()

		// Phase 1: Summarize
		summary, ok := ttsSummarize(ctx, curSummarizer, cacheKey, req.Text, req.Language, req.MessageID, errSummarizeFailed, isStreaming)
		if !ok {
			return
		}

		// Phase 2: Synthesize
		service.SendTTSEvent(cacheKey, service.TTSEvent{Type: "phase", Phase: "synthesizing", Streaming: isStreaming})

		synthesizeCtx, synthesizeCancel := context.WithTimeout(ctx, ttsSynthesizeTimeout)
		err := synthesizeFunc(synthesizeCtx, summary, absAudioPath, req.Language)
		synthesizeCancel()

		// Close AudioCh BEFORE sending result event (streaming only).
		// This signals the WS handler that no more audio chunks are coming.
		if audioCh != nil {
			close(audioCh)
		}

		if err != nil {
			logMsg := "tts synthesize failed"
			if isStreaming {
				logMsg = "tts stream synthesize failed"
			}
			slog.Error(
				logMsg,
				slog.String("error", err.Error()),
				slog.String("cache_key", cacheKey),
			)
			service.SendTTSEvent(cacheKey, service.TTSEvent{
				Type:             "result",
				SynthesizeFailed: true,
				SynthesizeError:  errSynthesizeFailed,
				Summary:          summary,
				Streaming:        isStreaming,
			})
			return
		}

		logMsg := "tts generate completed"
		if isStreaming {
			logMsg = "tts stream generate completed"
		}
		slog.Info(
			logMsg,
			slog.String("cache_key", cacheKey),
			slog.String("path", relAudioPath),
		)

		service.EvictTTSCache(projectPath, model.TTSMaxCacheFiles)

		service.SendTTSEvent(cacheKey, service.TTSEvent{
			Type:      "result",
			AudioPath: relAudioPath,
			Summary:   summary,
			Streaming: isStreaming,
		})
	}()
}
```

**Step 2: Replace the duplicated goroutine code in `TTSGenerate`**

Replace lines 198-317 (the entire `if isStreaming { ... } else { ... }` block) with:

```go
	if isStreaming {
		ttsRunJob(
			context.Background(), cacheKey, projectPath, relAudioPath, absAudioPath,
			req, curSummarizer, errSummarizeFailed, errSynthesizeFailed,
			true,
			func(ctx context.Context, summary, outPath, lang string) error {
				return streamingProvider.SynthesizeStream(ctx, summary, outPath, lang, job.AudioCh)
			},
			job.AudioCh,
		)
		// job was created inside ttsRunJob; fetch it to access AudioCh is not needed
		// because the synthesizeFunc closure captures streamingProvider directly.
		// However we need the job reference for the response.
		// Actually, ttsRunJob creates the job internally, so we need to get it after.
		// Let me reconsider...
	} else {
		ttsRunJob(
			context.Background(), cacheKey, projectPath, relAudioPath, absAudioPath,
			req, curSummarizer, errSummarizeFailed, errSynthesizeFailed,
			false,
			func(ctx context.Context, summary, outPath, lang string) error {
				return curProvider.Synthesize(ctx, summary, outPath, lang)
			},
			nil,
		)
	}
```

Wait — there's a problem: `ttsRunJob` creates the job internally, but we need the `job.AudioCh` reference in the streaming synthesizeFunc. We can't reference `job.AudioCh` before `ttsRunJob` creates the job.

**Revised approach:** Split job creation out of `ttsRunJob`. The caller creates the job and passes it in.

Actually, a cleaner approach: `ttsRunJob` returns the `*TTSJob` so the caller can use it. But the streaming path needs `job.AudioCh` in the synthesizeFunc closure, which means we need the job before creating the closure.

**Best approach:** Have `ttsRunJob` accept a `job *TTSJob` parameter instead of creating it internally. The caller creates the job, then passes it along with the synthesizeFunc that may reference `job.AudioCh`.

Let me revise:

```go
func ttsRunJob(
	job *service.TTSJob,
	cacheKey string,
	projectPath, relAudioPath, absAudioPath string,
	req ttsGenerateRequest,
	curSummarizer summarize.Summarizer,
	errSummarizeFailed, errSynthesizeFailed string,
	isStreaming bool,
	synthesizeFunc ttsSynthesizeFunc,
) {
	go func() {
		defer service.UnregisterTTSJob(cacheKey)
		defer service.CloseTTSJobDone(cacheKey)
		defer job.Cancel()

		// Phase 1: Summarize
		summary, ok := ttsSummarize(context.Background(), curSummarizer, cacheKey, req.Text, req.Language, req.MessageID, errSummarizeFailed, isStreaming)
		if !ok {
			return
		}

		// Phase 2: Synthesize
		service.SendTTSEvent(cacheKey, service.TTSEvent{Type: "phase", Phase: "synthesizing", Streaming: isStreaming})

		synthesizeCtx, synthesizeCancel := context.WithTimeout(context.Background(), ttsSynthesizeTimeout)
		err := synthesizeFunc(synthesizeCtx, summary, absAudioPath, req.Language)
		synthesizeCancel()

		// Close AudioCh BEFORE sending result event (streaming only).
		if job.AudioCh != nil {
			close(job.AudioCh)
		}

		if err != nil {
			logMsg := "tts synthesize failed"
			if isStreaming {
				logMsg = "tts stream synthesize failed"
			}
			slog.Error(logMsg, slog.String("error", err.Error()), slog.String("cache_key", cacheKey))
			service.SendTTSEvent(cacheKey, service.TTSEvent{
				Type: "result", SynthesizeFailed: true, SynthesizeError: errSynthesizeFailed, Summary: summary, Streaming: isStreaming,
			})
			return
		}

		logMsg := "tts generate completed"
		if isStreaming {
			logMsg = "tts stream generate completed"
		}
		slog.Info(logMsg, slog.String("cache_key", cacheKey), slog.String("path", relAudioPath))
		service.EvictTTSCache(projectPath, model.TTSMaxCacheFiles)
		service.SendTTSEvent(cacheKey, service.TTSEvent{
			Type: "result", AudioPath: relAudioPath, Summary: summary, Streaming: isStreaming,
		})
	}()
}
```

Wait — the original code creates `ctx, cancel := context.WithCancel(context.Background())` and passes `cancel` to `RegisterTTSJob`. The `job.Cancel` is that cancel func. The goroutine also `defer cancel()`. But in the original, the context passed to `ttsSummarize` and the synthesize timeout is derived from that `ctx`. In my refactor, I'm not passing the job's context around.

Let me re-examine the original more carefully:

```go
// Streaming path:
ctx, cancel := context.WithCancel(context.Background())
job := service.RegisterStreamingTTSJob(cacheKey, cancel)
go func() {
    defer service.UnregisterTTSJob(cacheKey)
    defer service.CloseTTSJobDone(cacheKey)
    defer cancel()
    // uses ctx for summarize and synthesize
    summary, ok := ttsSummarize(ctx, ...)
    synthesizeCtx, synthesizeCancel := context.WithTimeout(ctx, ttsSynthesizeTimeout)
    ...
}()
```

So the `ctx` from `context.WithCancel` is used as the parent for all operations. The `cancel` is stored in `job.Cancel` so external callers can cancel it. The goroutine also defers `cancel()`.

In my refactor, I need to pass this context through. Let me add it to the function signature:

```go
func ttsRunJob(
    ctx context.Context,
    job *service.TTSJob,
    cacheKey string,
    projectPath, relAudioPath, absAudioPath string,
    req ttsGenerateRequest,
    curSummarizer summarize.Summarizer,
    errSummarizeFailed, errSynthesizeFailed string,
    isStreaming bool,
    synthesizeFunc ttsSynthesizeFunc,
) {
    go func() {
        defer service.UnregisterTTSJob(cacheKey)
        defer service.CloseTTSJobDone(cacheKey)
        defer job.Cancel()

        summary, ok := ttsSummarize(ctx, curSummarizer, cacheKey, req.Text, req.Language, req.MessageID, errSummarizeFailed, isStreaming)
        if !ok {
            return
        }

        service.SendTTSEvent(cacheKey, service.TTSEvent{Type: "phase", Phase: "synthesizing", Streaming: isStreaming})

        synthesizeCtx, synthesizeCancel := context.WithTimeout(ctx, ttsSynthesizeTimeout)
        err := synthesizeFunc(synthesizeCtx, summary, absAudioPath, req.Language)
        synthesizeCancel()

        if job.AudioCh != nil {
            close(job.AudioCh)
        }

        if err != nil {
            slog.Error(
                ternary(isStreaming, "tts stream synthesize failed", "tts synthesize failed"),
                slog.String("error", err.Error()),
                slog.String("cache_key", cacheKey),
            )
            service.SendTTSEvent(cacheKey, service.TTSEvent{
                Type: "result", SynthesizeFailed: true, SynthesizeError: errSynthesizeFailed, Summary: summary, Streaming: isStreaming,
            })
            return
        }

        slog.Info(
            ternary(isStreaming, "tts stream generate completed", "tts generate completed"),
            slog.String("cache_key", cacheKey),
            slog.String("path", relAudioPath),
        )
        service.EvictTTSCache(projectPath, model.TTSMaxCacheFiles)
        service.SendTTSEvent(cacheKey, service.TTSEvent{
            Type: "result", AudioPath: relAudioPath, Summary: summary, Streaming: isStreaming,
        })
    }()
}
```

Hmm, there's no `ternary` in Go. Let me just use the if/else for the log message — it's a minor duplication of 2 strings, not the structural kind we're fixing. Or simply always use "tts generate completed" / "tts synthesize failed" since the `Streaming` field in the event already disambiguates. Let me check if the log messages are consumed by anything... They're just slog.Info/Error, so the distinction is cosmetic. I'll keep the if/else for now to maintain behavioral parity.

**The replacement code in `TTSGenerate`** (replacing the `if isStreaming { ... } else { ... }` block, lines 198-317):

```go
    if isStreaming {
        ctx, cancel := context.WithCancel(context.Background())
        job := service.RegisterStreamingTTSJob(cacheKey, cancel)
        ttsRunJob(ctx, job, cacheKey, projectPath, relAudioPath, absAudioPath,
            req, curSummarizer, errSummarizeFailed, errSynthesizeFailed, true,
            func(sCtx context.Context, summary, outPath, lang string) error {
                return streamingProvider.SynthesizeStream(sCtx, summary, outPath, lang, job.AudioCh)
            },
        )
        writeJSON(w, http.StatusOK, map[string]any{"jobId": cacheKey, "streaming": true})
    } else {
        ctx, cancel := context.WithCancel(context.Background())
        job := service.RegisterTTSJob(cacheKey, cancel)
        ttsRunJob(ctx, job, cacheKey, projectPath, relAudioPath, absAudioPath,
            req, curSummarizer, errSummarizeFailed, errSynthesizeFailed, false,
            func(sCtx context.Context, summary, outPath, lang string) error {
                return curProvider.Synthesize(sCtx, summary, outPath, lang)
            },
        )
        writeJSON(w, http.StatusOK, map[string]any{"jobId": cacheKey, "streaming": false})
    }
```

**Step 3: Run all TTS tests**

Run: `go test ./internal/handler/ -run TestTTS -count=1 -v`
Expected: All tests PASS (both streaming and non-streaming)

**Step 4: Commit**

```bash
git add internal/handler/tts.go
git commit -m "refactor(tts): extract ttsRunJob to unify streaming/non-streaming goroutines"
```

---

### Task 5: Extract `audioExtForProvider` to remove `nolint:govet` directives

**Files:**
- Modify: `internal/handler/tts.go`

**Step 1: Write the helper function**

Add before `TTSGenerate`:

```go
// audioExtForProvider returns the audio file extension for the given provider.
// Piper, Kokoro, and MOSS-Nano produce WAV; all others produce MP3.
func audioExtForProvider(p speech.SpeechProvider) string {
	switch p.(type) {
	case *speech.PiperProvider, *speech.KokoroProvider, *speech.MossNanoProvider:
		return ".wav"
	default:
		return ".mp3"
	}
}
```

**Step 2: Replace the 3 type assertions in `TTSGenerate`**

Replace lines 149-159:

```go
	// Determine audio file extension based on TTS engine
	audioExt := audioExtForProvider(curProvider)
```

This removes the 3 `//nolint:govet` lines.

**Step 3: Run tests**

Run: `go test ./internal/handler/ -run TestTTS -count=1`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/handler/tts.go
git commit -m "refactor(tts): extract audioExtForProvider, remove nolint:govet"
```

---

### Task 6: Remove `nolint:gocyclo,gocognit` from `TTSGenerate`

**Files:**
- Modify: `internal/handler/tts.go`

After the refactor in Tasks 4-5, the cyclomatic complexity of `TTSGenerate` should have dropped significantly. The streaming/non-streaming branching is now minimal (just the if/else for job creation and `writeJSON`).

**Step 1: Remove the nolint directive**

Change line 92 from:
```go
func TTSGenerate(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo,gocognit // multi-mode TTS generation
```
to:
```go
func TTSGenerate(w http.ResponseWriter, r *http.Request) {
```

**Step 2: Run golangci-lint on the file**

Run: `golangci-lint run ./internal/handler/tts.go`
Expected: No gocyclo/gocognit errors for `TTSGenerate`

If lint fails, re-add the directive and note it in the commit message. But the refactor should bring the complexity well under the thresholds.

**Step 3: Commit**

```bash
git add internal/handler/tts.go
git commit -m "refactor(tts): remove gocyclo/gocognit nolint after complexity reduction"
```

---

### Task 7: Remove `nolint:goconst` from file-level directive

**Files:**
- Modify: `internal/handler/tts.go`

The `goconst` nolint on line 1 suppresses warnings about repeated string literals like `"type"`, `"phase"`, `"result"`, etc. These are JSON field names — domain strings, not configuration constants. The nolint is justified and harmless, but let's verify it's still needed.

**Step 1: Check if goconst still fires**

Temporarily remove line 1 (`//nolint:goconst ...`), then run:
```bash
golangci-lint run ./internal/handler/tts.go
```

**Step 2: Decision**

If goconst fires on JSON field names → the nolint is justified, keep it but verify the comment is accurate.
If goconst doesn't fire (maybe the linter config excludes short strings) → remove the directive.

**Step 3: Commit (if changed)**

```bash
git add internal/handler/tts.go
git commit -m "refactor(tts): remove unnecessary goconst nolint"
```

---

### Task 8: Add streaming synthesize failure test

**Files:**
- Modify: `internal/handler/tts_test.go`

**Step 1: Write the test**

```go
func TestTTSGenerate_StreamingSynthesizeFailure(t *testing.T) {
	mockProvider := &mockStreamingSpeechProvider{
		mockSpeechProvider: mockSpeechProvider{},
		streamErr:          context.DeadlineExceeded,
	}
	mockSum := &mockSummarizer{result: "流式总结文本"}
	env, teardown := setupTTSTest(t, &mockProvider.mockSpeechProvider, mockSum)
	defer teardown()

	SetSpeechProvider(mockProvider)
	defer func() { SetSpeechProvider(&mockProvider.mockSpeechProvider) }()

	text := "测试流式语音合成失败的场景。"
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": text})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, true, resp["streaming"])

	// Wait for job to complete
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	job, ok := service.GetTTSJob(cacheKey)
	if ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("streaming TTS job did not complete in time")
		}
	}

	assert.True(t, mockSum.called)
	assert.True(t, mockProvider.streamCalled)
}
```

**Step 2: Run the test**

Run: `go test ./internal/handler/ -run TestTTSGenerate_StreamingSynthesizeFailure -count=1 -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/handler/tts_test.go
git commit -m "test(tts): add streaming synthesize failure test"
```

---

### Task 9: Verify `ttsExtractConclusion` nolint is still necessary

**Files:**
- Modify: `internal/handler/tts.go` (potentially)

The `nolint:gocyclo,gocognit` on `ttsExtractConclusion` (line 383) suppresses warnings from the multi-branch AskUserQuestion extraction. This function's complexity is inherent to the JSON structure traversal — it's not a duplication issue.

**Step 1: Try removing the nolint**

Temporarily remove the directive and run:
```bash
golangci-lint run ./internal/handler/tts.go
```

**Step 2: Decision**

If lint passes → remove the directive, commit.
If lint fails → the complexity is inherent, keep the directive. Consider if extracting an `extractAskUserQuestionText` helper from lines 402-447 would help. If it does, do it. If not, keep the nolint with a clearer comment.

**Step 3: Commit (if changed)**

```bash
git add internal/handler/tts.go
git commit -m "refactor(tts): extract AskUserQuestion text extraction, reduce complexity"
```

---

### Task 10: Verify `tts_audio_ws.go` compilation and clean up

**Files:**
- Read: `internal/handler/tts_audio_ws.go`

The exploration found that this file actually compiles fine — the original issue #6 about compilation errors appears to be stale. Verify this.

**Step 1: Build the package**

Run: `go build ./internal/handler/`
Expected: Success (no errors)

**Step 2: Run go vet**

Run: `go vet ./internal/handler/`
Expected: No issues

**Step 3: Run existing WS tests**

Run: `go test ./internal/handler/ -run TestTTSAudioWS -count=1 -v`
Expected: All PASS

If all pass, no action needed for this item — it was a false alarm.

---

### Task 11: Run full test suite and pre-push checks

**Files:**
- None (verification only)

**Step 1: Run Go tests with race detector**

Run: `go test -race ./internal/handler/ -count=1`
Expected: All PASS, no race conditions

**Step 2: Run golangci-lint**

Run: `golangci-lint run ./internal/handler/`
Expected: No new errors (the removed nolints should not cause failures)

**Step 3: Run broader Go tests**

Run: `go test ./...`
Expected: All PASS

**Step 4: Final commit (if any cleanup needed)**

If any issues found during verification, fix and commit.
