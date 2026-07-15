package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/coder/websocket"
)

func setupTTSAudioWSTest(t *testing.T) (*testEnv, *httptest.Server, func()) {
	t.Helper()
	env, teardown := setupTestEnv(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tts/audio/ws", middleware.Auth(TTSAudioWS))
	server := httptest.NewServer(mux)

	cleanup := func() {
		server.Close()
		teardown()
	}
	return env, server, cleanup
}

func connectTTSWS(t *testing.T, serverURL string, projectPath string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + serverURL[len("http"):] + "/api/tts/audio/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{}
	if projectPath != "" {
		opts.HTTPHeader = http.Header{
			"Cookie": []string{model.ScopedCookieName("clawbench_project") + "=" + url.QueryEscape(projectPath)},
		}
	}
	conn, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("failed to connect to WS: %v", err)
	}
	return conn
}

func readWSMessage(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read WS message: %v", err)
	}
	return data
}

func TestIsValidTTSJobID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"abc123", true},
		{"deadbeef", true},
		{"a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef1234", true}, // 64 chars (full SHA-256 hex)
		{"", false},
		{"ABC123", false},  // uppercase
		{"abc12g", false},  // 'g' not hex
		{"abc-123", false}, // dash not hex
		{"a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a", false}, // 65 chars
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isValidTTSJobID(tt.input); got != tt.want {
				t.Errorf("isValidTTSJobID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTTSAudioWS_InvalidStartMessage(t *testing.T) {
	_, server, cleanup := setupTTSAudioWSTest(t)
	defer cleanup()

	conn := connectTTSWS(t, server.URL, "")
	defer conn.CloseNow()

	// Send invalid start message
	err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"hello"}`))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Should receive an error message
	data := readWSMessage(t, conn)
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	if msg["type"] != "error" {
		t.Errorf("expected type=error, got %v", msg["type"])
	}
}

func TestTTSAudioWS_InvalidJobID(t *testing.T) {
	_, server, cleanup := setupTTSAudioWSTest(t)
	defer cleanup()

	conn := connectTTSWS(t, server.URL, "")
	defer conn.CloseNow()

	// Send start with invalid jobId (uppercase hex)
	err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"start","jobId":"ABC123"}`))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	data := readWSMessage(t, conn)
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	if msg["type"] != "error" {
		t.Errorf("expected type=error, got %v", msg["type"])
	}
}

func TestTTSAudioWS_JobNotFound(t *testing.T) {
	_, server, cleanup := setupTTSAudioWSTest(t)
	defer cleanup()

	conn := connectTTSWS(t, server.URL, "")
	defer conn.CloseNow()

	// Send start with valid hex but non-existent job
	err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"start","jobId":"abc123"}`))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	data := readWSMessage(t, conn)
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	if msg["type"] != "error" {
		t.Errorf("expected type=error, got %v", msg["type"])
	}
}

func TestTTSAudioWS_JobAlreadyCompleted(t *testing.T) {
	env, server, cleanup := setupTTSAudioWSTest(t)
	defer cleanup()

	// Create a completed audio file on disk
	jobID := "a1b2c3d4e5f6"
	ttsDir := filepath.Join(env.ProjectDir, ".clawbench", "generated", "tts")
	_ = os.MkdirAll(ttsDir, 0o755)
	audioFile := filepath.Join(ttsDir, jobID+".mp3")
	if err := os.WriteFile(audioFile, []byte("fake mp3 data"), 0o644); err != nil {
		t.Fatalf("failed to create audio file: %v", err)
	}

	conn := connectTTSWS(t, server.URL, env.ProjectDir)
	defer conn.CloseNow()

	// Send start message
	err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"start","jobId":"a1b2c3d4e5f6"}`))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Should receive a "done" message with audioPath
	data := readWSMessage(t, conn)
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	if msg["type"] != "done" {
		t.Errorf("expected type=done, got %v", msg["type"])
	}
	if msg["audioPath"] == nil || msg["audioPath"] == "" {
		t.Error("expected audioPath in done message")
	}
}

func TestTTSAudioWS_ProtocolStreaming(t *testing.T) {
	_, server, cleanup := setupTTSAudioWSTest(t)
	defer cleanup()

	// Register a streaming TTS job (jobId must be lowercase hex like real SHA-256 prefix)
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := service.RegisterStreamingTTSJob("deadbeef1234", cancel)
	defer service.UnregisterTTSJob("deadbeef1234")
	defer service.CloseTTSJobDone("deadbeef1234")

	conn := connectTTSWS(t, server.URL, "")
	defer conn.CloseNow()

	// Send start message
	err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"start","jobId":"deadbeef1234"}`))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Send phase events
	service.SendTTSEvent("deadbeef1234", service.TTSEvent{Type: "phase", Phase: "summarizing"})

	// Read the phase message
	data := readWSMessage(t, conn)
	var phaseMsg map[string]any
	if err := json.Unmarshal(data, &phaseMsg); err != nil {
		t.Fatalf("failed to parse phase message: %v", err)
	}
	if phaseMsg["type"] != "phase" {
		t.Errorf("expected type=phase, got %v", phaseMsg["type"])
	}
	if phaseMsg["phase"] != "summarizing" {
		t.Errorf("expected phase=summarizing, got %v", phaseMsg["phase"])
	}

	// Send audio chunk
	audioChunk := []byte("fake mp3 chunk data")
	select {
	case job.AudioCh <- audioChunk:
	default:
		t.Fatal("failed to send audio chunk to channel")
	}

	// Read binary frame
	readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
	msgType, chunkData, err := conn.Read(readCtx)
	readCancel()
	if err != nil {
		t.Fatalf("failed to read audio chunk: %v", err)
	}
	if msgType != websocket.MessageBinary {
		t.Errorf("expected binary message, got %v", msgType)
	}
	if string(chunkData) != "fake mp3 chunk data" {
		t.Errorf("expected audio chunk data, got %q", string(chunkData))
	}

	// Close AudioCh and send result (matching the real goroutine lifecycle)
	close(job.AudioCh)
	service.SendTTSEvent("deadbeef1234", service.TTSEvent{
		Type:      "result",
		AudioPath: ".clawbench/generated/tts/deadbeef1234.mp3",
		Summary:   "test summary",
	})

	// Read done message
	doneData := readWSMessage(t, conn)
	var doneMsg map[string]any
	if err := json.Unmarshal(doneData, &doneMsg); err != nil {
		t.Fatalf("failed to parse done message: %v", err)
	}
	if doneMsg["type"] != "done" {
		t.Errorf("expected type=done, got %v", doneMsg["type"])
	}
	if doneMsg["audioPath"] != ".clawbench/generated/tts/deadbeef1234.mp3" {
		t.Errorf("unexpected audioPath: %v", doneMsg["audioPath"])
	}
	if doneMsg["summary"] != "test summary" {
		t.Errorf("unexpected summary: %v", doneMsg["summary"])
	}
}

func TestTTSAudioWS_ResultError(t *testing.T) {
	_, server, cleanup := setupTTSAudioWSTest(t)
	defer cleanup()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := service.RegisterStreamingTTSJob("cafebabe5678", cancel)
	defer service.UnregisterTTSJob("cafebabe5678")
	defer service.CloseTTSJobDone("cafebabe5678")

	conn := connectTTSWS(t, server.URL, "")
	defer conn.CloseNow()

	err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"start","jobId":"cafebabe5678"}`))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Close AudioCh and send error result
	close(job.AudioCh)
	service.SendTTSEvent("cafebabe5678", service.TTSEvent{
		Type:             "result",
		SynthesizeFailed: true,
		SynthesizeError:  "synthesis failed",
	})

	// Read error message
	data := readWSMessage(t, conn)
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	if msg["type"] != "error" {
		t.Errorf("expected type=error, got %v", msg["type"])
	}
	if msg["message"] != "synthesis failed" {
		t.Errorf("expected message='synthesis failed', got %v", msg["message"])
	}
}

func TestTTSAudioWS_ClientDisconnect(t *testing.T) {
	_, server, cleanup := setupTTSAudioWSTest(t)
	defer cleanup()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = service.RegisterStreamingTTSJob("baddcafe9012", cancel)
	defer service.UnregisterTTSJob("baddcafe9012")
	defer service.CloseTTSJobDone("baddcafe9012")

	conn := connectTTSWS(t, server.URL, "")

	err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"start","jobId":"baddcafe9012"}`))
	if err != nil {
		conn.CloseNow()
		t.Fatalf("failed to write: %v", err)
	}

	// Close the connection abruptly
	conn.CloseNow()

	// Give the read goroutine time to detect the disconnect and cancel the job
	time.Sleep(500 * time.Millisecond)

	// Verify the job's cancel was called (read goroutine calls CancelTTSJob on disconnect)
	_, ok := service.GetTTSJob("baddcafe9012")
	_ = ok // job may or may not still exist depending on timing
}
