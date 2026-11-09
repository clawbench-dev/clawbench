package stt

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// mustCompileTimeAssert ensures AlibabaProvider can stream.
var _ StreamingSTTProvider = (*AlibabaProvider)(nil)

// wavProbe is the smallest byte string detectAudioFormat accepts as WAV.
func wavProbe() []byte {
	return append([]byte("RIFF\x00\x00\x00\x00WAVE"), make([]byte, 16)...)
}

func TestAlibabaTranscribe(t *testing.T) {
	var gotPath, gotAuth string
	var gotReq alibabaChatRequest
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"你好世界"}}]}`))
	}))
	defer srv.Close()

	p := NewAlibabaProvider("sk-test")
	p.BaseURL = srv.URL
	p.HTTPClient = srv.Client()

	text, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), "")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "你好世界" {
		t.Fatalf("text = %q, want 你好世界", text)
	}
	<-done

	if gotPath != alibabaChatPath {
		t.Errorf("path = %q, want %q", gotPath, alibabaChatPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", gotAuth)
	}
	if gotReq.Model != alibabaFileModel {
		t.Errorf("model = %q, want %q", gotReq.Model, alibabaFileModel)
	}
	if len(gotReq.Messages) != 1 || len(gotReq.Messages[0].Content) != 1 {
		t.Fatalf("messages = %+v, want one user message with one content part", gotReq.Messages)
	}
	part := gotReq.Messages[0].Content[0]
	if part.Type != "input_audio" {
		t.Errorf("content type = %q, want input_audio", part.Type)
	}
	if !strings.HasPrefix(part.InputAudio.Data, "data:audio/wav;base64,") {
		t.Errorf("audio data = %q, want a WAV data URI", part.InputAudio.Data)
	}
	if gotReq.ASROptions == nil || gotReq.ASROptions.Language != "zh" || !gotReq.ASROptions.EnableITN {
		t.Errorf("asr_options = %+v, want language zh and enable_itn true", gotReq.ASROptions)
	}
}

func TestAlibabaTranscribeUsesCallerLanguage(t *testing.T) {
	var gotReq alibabaChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	p := NewAlibabaProvider("k")
	p.BaseURL = srv.URL
	p.HTTPClient = srv.Client()
	p.Language = "zh"

	if _, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), "en"); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotReq.ASROptions == nil || gotReq.ASROptions.Language != "en" {
		t.Fatalf("asr_options = %+v, want language en", gotReq.ASROptions)
	}
}

func TestAlibabaTranscribeAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"InvalidApiKey","message":"bad key"}}`))
	}))
	defer srv.Close()

	p := NewAlibabaProvider("k")
	p.BaseURL = srv.URL
	p.HTTPClient = srv.Client()

	_, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", apiErr.Status)
	}
	if !strings.Contains(apiErr.Body, "InvalidApiKey") {
		t.Errorf("body = %q, want the engine's error code", apiErr.Body)
	}
}

func TestAlibabaTranscribeRejectsBadInput(t *testing.T) {
	p := NewAlibabaProvider("k")

	if _, err := p.Transcribe(context.Background(), strings.NewReader(""), ""); err == nil || !strings.Contains(err.Error(), "empty audio") {
		t.Errorf("empty audio: err = %v, want empty-audio error", err)
	}
	if _, err := p.Transcribe(context.Background(), strings.NewReader("not audio"), ""); err == nil || !strings.Contains(err.Error(), "unsupported audio format") {
		t.Errorf("garbage audio: err = %v, want unsupported-format error", err)
	}

	big := make([]byte, alibabaMaxAudioBytes+1)
	copy(big, wavProbe())
	if _, err := p.Transcribe(context.Background(), strings.NewReader(string(big)), ""); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("oversized audio: err = %v, want a size error", err)
	}
}

// TestAlibabaOpenStream covers the wire protocol: the run-task body, the
// wait-for-started gate, per-sentence accumulation and the finish handshake.
func TestAlibabaOpenStream(t *testing.T) {
	var gotRun alibabaTaskRequest
	var gotFinish alibabaFinishRequest
	var audioBytes int
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("handshake Authorization = %q, want Bearer sk-test", got)
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := context.Background()

		mt, data, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("read run-task: %v", err)
			return
		}
		if mt != websocket.MessageText {
			t.Errorf("run-task frame type = %v, want text", mt)
			return
		}
		if err := json.Unmarshal(data, &gotRun); err != nil {
			t.Errorf("decode run-task: %v", err)
			return
		}
		if err := conn.Write(ctx, websocket.MessageText, []byte(`{"header":{"event":"task-started","attributes":{}},"payload":{}}`)); err != nil {
			t.Errorf("write task-started: %v", err)
			return
		}

		// Two audio frames, then the finish instruction.
		for i := range 2 {
			mt, data, err := conn.Read(ctx)
			if err != nil {
				t.Errorf("read audio %d: %v", i, err)
				return
			}
			if mt != websocket.MessageBinary {
				t.Errorf("audio frame %d type = %v, want binary", i, mt)
				return
			}
			audioBytes += len(data)
			if i == 0 {
				_ = conn.Write(ctx, websocket.MessageText, []byte(`{"header":{"event":"result-generated"},"payload":{"output":{"sentence":{"text":"你好","sentence_end":false,"heartbeat":false}}}}`))
			}
		}

		mt, data, err = conn.Read(ctx)
		if err != nil {
			t.Errorf("read finish-task: %v", err)
			return
		}
		if mt != websocket.MessageText {
			t.Errorf("finish-task frame type = %v, want text", mt)
			return
		}
		if err := json.Unmarshal(data, &gotFinish); err != nil {
			t.Errorf("decode finish-task: %v", err)
		}

		// Final sentence, a heartbeat packet that must be ignored, then the
		// terminal event.
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"header":{"event":"result-generated"},"payload":{"output":{"sentence":{"text":"你好，世界。","sentence_end":true,"heartbeat":false}}}}`))
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"header":{"event":"result-generated"},"payload":{"output":{"sentence":{"text":"","sentence_end":false,"heartbeat":true}}}}`))
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"header":{"event":"task-finished"},"payload":{"output":{},"usage":null}}`))
	}))
	defer srv.Close()

	p := NewAlibabaProvider("sk-test")
	p.BaseURL = srv.URL
	p.Language = "zh"
	p.EndWindowSize = 900
	p.HTTPClient = srv.Client()

	session, err := p.OpenStream(context.Background(), StreamOptions{SampleRate: 16000, Channels: 1, Bits: 16})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer session.Close()

	if err := session.Write(make([]byte, 320)); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if err := session.Write(make([]byte, 320)); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	if err := session.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	var results []StreamResult
	for res := range session.Results() {
		results = append(results, res)
	}
	<-done

	// 你好 (interim) → 你好，世界。 (cumulative) → final marker.
	if len(results) != 3 {
		t.Fatalf("results = %+v, want 3 updates", results)
	}
	if results[0].Text != "你好" || results[0].Final {
		t.Errorf("result[0] = %+v, want interim 你好", results[0])
	}
	if results[1].Text != "你好，世界。" || results[1].Final {
		t.Errorf("result[1] = %+v, want cumulative 你好，世界。", results[1])
	}
	if results[2].Text != "你好，世界。" || !results[2].Final {
		t.Errorf("result[2] = %+v, want final 你好，世界。", results[2])
	}

	if gotRun.Header.Action != "run-task" || gotRun.Header.Streaming != "duplex" || gotRun.Header.TaskID == "" {
		t.Errorf("run-task header = %+v", gotRun.Header)
	}
	if gotRun.Payload.TaskGroup != "audio" || gotRun.Payload.Task != "asr" || gotRun.Payload.Function != "recognition" {
		t.Errorf("run-task payload = %+v", gotRun.Payload)
	}
	if gotRun.Payload.Model != AlibabaDefaultStreamModel {
		t.Errorf("model = %q, want %q", gotRun.Payload.Model, AlibabaDefaultStreamModel)
	}
	params := gotRun.Payload.Parameters
	if params.Format != "pcm" || params.SampleRate != 16000 {
		t.Errorf("audio params = %+v, want pcm/16000", params)
	}
	if len(params.LanguageHints) != 1 || params.LanguageHints[0] != "zh" {
		t.Errorf("language_hints = %v, want [zh]", params.LanguageHints)
	}
	if !params.Heartbeat || !params.PunctuationPredictionEnabled || !params.InverseTextNormalizationEnabled {
		t.Errorf("switches = %+v, want heartbeat/punc/itn on", params)
	}
	if params.SemanticPunctuationEnabled {
		t.Errorf("semantic_punctuation_enabled = true, want false (VAD segmentation)")
	}
	if params.MaxSentenceSilence != 900 {
		t.Errorf("max_sentence_silence = %d, want 900", params.MaxSentenceSilence)
	}
	if audioBytes != 640 {
		t.Errorf("audio bytes = %d, want 640", audioBytes)
	}
	if gotFinish.Header.Action != "finish-task" || gotFinish.Header.TaskID != gotRun.Header.TaskID {
		t.Errorf("finish-task header = %+v, want action finish-task and task_id %q", gotFinish.Header, gotRun.Header.TaskID)
	}
}

func TestAlibabaOpenStreamFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		_, _, _ = conn.Read(context.Background())
		_ = conn.Write(context.Background(), websocket.MessageText,
			[]byte(`{"header":{"event":"task-failed","error_code":"CLIENT_ERROR","error_message":"invalid api key"},"payload":{}}`))
	}))
	defer srv.Close()

	p := NewAlibabaProvider("bad")
	p.BaseURL = srv.URL
	p.HTTPClient = srv.Client()

	_, err := p.OpenStream(context.Background(), StreamOptions{})
	if err == nil {
		t.Fatal("OpenStream succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "CLIENT_ERROR") || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("err = %v, want the engine's code and message", err)
	}
}

func TestAlibabaOpenStreamRequiresMono(t *testing.T) {
	p := NewAlibabaProvider("k")
	if _, err := p.OpenStream(context.Background(), StreamOptions{Channels: 2}); err == nil {
		t.Error("stereo OpenStream succeeded, want an error")
	}
	if _, err := p.OpenStream(context.Background(), StreamOptions{Bits: 8}); err == nil {
		t.Error("8-bit OpenStream succeeded, want an error")
	}
	if _, err := NewAlibabaProvider("").OpenStream(context.Background(), StreamOptions{}); err == nil {
		t.Error("OpenStream without an API key succeeded, want an error")
	}
}

func TestAlibabaURLs(t *testing.T) {
	cases := []struct {
		base string
		chat string
		ws   string
	}{
		{"", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", "wss://dashscope.aliyuncs.com/api-ws/v1/inference"},
		{"https://dashscope-intl.aliyuncs.com", "https://dashscope-intl.aliyuncs.com/compatible-mode/v1/chat/completions", "wss://dashscope-intl.aliyuncs.com/api-ws/v1/inference"},
		{"https://dashscope.aliyuncs.com/", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", "wss://dashscope.aliyuncs.com/api-ws/v1/inference"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", "wss://dashscope.aliyuncs.com/api-ws/v1/inference"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", "wss://dashscope.aliyuncs.com/api-ws/v1/inference"},
		{"wss://x.cn-beijing.maas.aliyuncs.com/api-ws/v1/inference", "https://x.cn-beijing.maas.aliyuncs.com/compatible-mode/v1/chat/completions", "wss://x.cn-beijing.maas.aliyuncs.com/api-ws/v1/inference"},
	}
	for _, c := range cases {
		if got := alibabaChatURL(c.base); got != c.chat {
			t.Errorf("alibabaChatURL(%q) = %q, want %q", c.base, got, c.chat)
		}
		if got := alibabaWSURL(c.base); got != c.ws {
			t.Errorf("alibabaWSURL(%q) = %q, want %q", c.base, got, c.ws)
		}
	}
}

func TestAlibabaSessionCloseUnblocksResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		_, _, _ = conn.Read(context.Background())
		_ = conn.Write(context.Background(), websocket.MessageText, []byte(`{"header":{"event":"task-started"},"payload":{}}`))
		// Stay silent so the client blocks in Results until Close; the read
		// returns as soon as the client closes, which lets the test finish
		// without httptest's 5s shutdown grace period.
		_, _, _ = conn.Read(context.Background())
	}))
	defer srv.Close()

	p := NewAlibabaProvider("k")
	p.BaseURL = srv.URL
	p.HTTPClient = srv.Client()

	session, err := p.OpenStream(context.Background(), StreamOptions{})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}

	closed := make(chan struct{})
	go func() {
		for range session.Results() {
		}
		close(closed)
	}()

	session.Close()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Results did not close after Close")
	}
}

func TestAlibabaLanguageHints(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"zh", []string{"zh"}},
		{"ZH", []string{"zh"}},
		{"en,zh", []string{"en"}},
		{"yue", []string{"yue"}},
		{"", nil},
		{"pt", nil}, // supported by the file endpoint, not by Paraformer
		{"klingon", nil},
	}
	for _, c := range cases {
		got := alibabaLanguageHints(c.in)
		if len(got) != len(c.want) {
			t.Errorf("alibabaLanguageHints(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("alibabaLanguageHints(%q) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}

func TestAlibabaLanguageCodeKeepsFileOnlyLanguages(t *testing.T) {
	// The file endpoint understands far more languages than Paraformer, so the
	// code must survive for Transcribe even when it cannot be a stream hint.
	if got := alibabaLanguageCode("pt"); got != "pt" {
		t.Errorf("alibabaLanguageCode(pt) = %q, want pt", got)
	}
	if got := alibabaLanguageCode("nope"); got != "" {
		t.Errorf("alibabaLanguageCode(nope) = %q, want empty", got)
	}
}
