package speech

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

func newTestAlibabaProvider(baseURL string) *AlibabaTTSProvider {
	p := NewAlibabaTTSProvider()
	p.APIKey = "sk-test"
	p.BaseURL = baseURL
	p.Model = "cosyvoice-v3-flash"
	p.Voice = "longanyang"
	return p
}

func TestResolveAlibabaTTSSynthesisURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://dashscope.aliyuncs.com", "https://dashscope.aliyuncs.com/api/v1/services/audio/tts/SpeechSynthesizer"},
		{"https://dashscope.aliyuncs.com/", "https://dashscope.aliyuncs.com/api/v1/services/audio/tts/SpeechSynthesizer"},
		{"https://dashscope.aliyuncs.com/api/v1", "https://dashscope.aliyuncs.com/api/v1/services/audio/tts/SpeechSynthesizer"},
		{"https://x.cn-beijing.maas.aliyuncs.com/api/v1/services/audio/tts/SpeechSynthesizer", "https://x.cn-beijing.maas.aliyuncs.com/api/v1/services/audio/tts/SpeechSynthesizer"},
		{"", AlibabaTTSBaseURL + "/api/v1/services/audio/tts/SpeechSynthesizer"},
	}
	for _, c := range cases {
		if got := ResolveAlibabaTTSSynthesisURL(c.in); got != c.want {
			t.Errorf("ResolveAlibabaTTSSynthesisURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAlibabaTTS_DownloadsURL covers the normal non-streaming shape: the API
// answers with a URL and the audio is fetched from it.
func TestAlibabaTTS_DownloadsURL(t *testing.T) {
	audio := mp3(strings.Repeat("a", 64))

	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/audio":
			_, _ = w.Write(audio)
		default:
			body, _ := io.ReadAll(r.Body)
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			gotBody = string(body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 200,
				"output": map[string]any{
					"audio": map[string]any{"url": "http://" + r.Host + "/audio"},
				},
			})
		}
	}))
	defer srv.Close()

	p := newTestAlibabaProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := p.Synthesize(context.Background(), "你好", out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	if gotPath != "/api/v1/services/audio/tts/SpeechSynthesizer" {
		t.Errorf("request path = %q", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	var req struct {
		Model string `json:"model"`
		Input struct {
			Text   string `json:"text"`
			Voice  string `json:"voice"`
			Format string `json:"format"`
		} `json:"input"`
	}
	if err := json.Unmarshal([]byte(gotBody), &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.Model != "cosyvoice-v3-flash" || req.Input.Voice != "longanyang" {
		t.Errorf("unexpected model/voice: %+v", req)
	}
	if req.Input.Format != "mp3" {
		t.Errorf("input.format = %q, want mp3 (the pipeline assumes mp3)", req.Input.Format)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, audio) {
		t.Errorf("output audio = %d bytes, want %d", len(got), len(audio))
	}
}

// TestAlibabaTTS_InlineBase64 covers the inline payload shape.
func TestAlibabaTTS_InlineBase64(t *testing.T) {
	audio := mp3(strings.Repeat("b", 64))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": 200,
			"output": map[string]any{
				"audio": map[string]any{"data": base64.StdEncoding.EncodeToString(audio)},
			},
		})
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := newTestAlibabaProvider(srv.URL).Synthesize(context.Background(), "你好", out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, audio) {
		t.Errorf("output audio mismatch: %d bytes", len(got))
	}
}

// TestAlibabaTTS_APIError covers a 200 response carrying an error code — the
// shape DashScope uses for bad keys / unknown voices.
func TestAlibabaTTS_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": 400,
			"code":        "InvalidParameter",
			"message":     "voice not found",
			"request_id":  "req-1",
		})
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.mp3")
	err := newTestAlibabaProvider(srv.URL).Synthesize(context.Background(), "你好", out, "zh")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "InvalidParameter") || !strings.Contains(err.Error(), "voice not found") {
		t.Errorf("error should name the code and message, got: %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("failed synthesis must not leave an output file")
	}
}

// TestAlibabaTTS_RejectsNonMP3 guards the format assumption: a server that
// ignores input.format must fail loudly instead of caching WAV as .mp3.
func TestAlibabaTTS_RejectsNonMP3(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": 200,
			"output": map[string]any{
				"audio": map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("RIFF....WAVEfmt "))},
			},
		})
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.mp3")
	err := newTestAlibabaProvider(srv.URL).Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "not mp3") {
		t.Fatalf("expected a not-mp3 error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "WAV") {
		t.Errorf("error should name the detected format, got: %v", err)
	}
}

// TestAlibabaTTS_SplitsLongText verifies a long reply is cut into multiple
// requests instead of being rejected by the API's input limit.
func TestAlibabaTTS_SplitsLongText(t *testing.T) {
	audio := mp3(strings.Repeat("c", 64))
	var requests int
	var longest int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var req struct {
			Input struct {
				Text string `json:"text"`
			} `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if n := len([]rune(req.Input.Text)); n > longest {
			longest = n
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": 200,
			"output": map[string]any{
				"audio": map[string]any{"data": base64.StdEncoding.EncodeToString(audio)},
			},
		})
	}))
	defer srv.Close()

	text := strings.Repeat("这是一句用来测试切分的长文本。", 60) // ~960 runes
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := newTestAlibabaProvider(srv.URL).Synthesize(context.Background(), text, out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if requests < 2 {
		t.Errorf("expected multiple requests, got %d", requests)
	}
	if longest > alibabaTTSSegmentRunes {
		t.Errorf("a segment was %d runes, limit is %d", longest, alibabaTTSSegmentRunes)
	}
	got, _ := os.ReadFile(out)
	if want := requests * len(audio); len(got) != want {
		t.Errorf("output = %d bytes, want %d (all segments concatenated)", len(got), want)
	}
}

func TestAlibabaTTS_RequiresAPIKey(t *testing.T) {
	p := NewAlibabaTTSProvider()
	p.APIKey = ""
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "tts.alibaba.api_key") {
		t.Fatalf("expected a config error naming the field, got: %v", err)
	}
}

func TestAlibabaTTSCacheIdentity(t *testing.T) {
	p := newTestAlibabaProvider("https://dashscope.aliyuncs.com")
	p.Speed = 1.25
	id := p.CacheIdentity()
	for _, want := range []string{"alibaba", "cosyvoice-v3-flash", "longanyang", "1.25"} {
		if !strings.Contains(id, want) {
			t.Errorf("CacheIdentity %q missing %q", id, want)
		}
	}
	if strings.Contains(id, "sk-test") {
		t.Errorf("CacheIdentity must not contain the API key: %q", id)
	}
}

func TestResolveAlibabaTTSWSURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://dashscope.aliyuncs.com", "wss://dashscope.aliyuncs.com/api-ws/v1/inference"},
		{"https://dashscope.aliyuncs.com/", "wss://dashscope.aliyuncs.com/api-ws/v1/inference"},
		{"https://dashscope.aliyuncs.com/api/v1", "wss://dashscope.aliyuncs.com/api-ws/v1/inference"},
		{"https://dashscope.aliyuncs.com/api/v1/services/audio/tts/SpeechSynthesizer", "wss://dashscope.aliyuncs.com/api-ws/v1/inference"},
		{"wss://x.cn-beijing.maas.aliyuncs.com/api-ws/v1/inference", "wss://x.cn-beijing.maas.aliyuncs.com/api-ws/v1/inference"},
		{"http://127.0.0.1:1234", "ws://127.0.0.1:1234/api-ws/v1/inference"},
		{"", "wss://dashscope.aliyuncs.com/api-ws/v1/inference"},
	}
	for _, c := range cases {
		if got := ResolveAlibabaTTSWSURL(c.in); got != c.want {
			t.Errorf("ResolveAlibabaTTSWSURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAlibabaTTS_SambertUsesWebSocket covers the second protocol: the model is
// WebSocket-only, the whole text must ride in run-task (a continue-task is
// rejected by the engine), and the audio arrives as binary frames between
// task-started and task-finished.
func TestAlibabaTTS_SambertUsesWebSocket(t *testing.T) {
	audio := mp3(strings.Repeat("b", 48))

	var gotPath string
	var gotReq alibabaSambertRequest
	var finish alibabaSambertFinish
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := context.Background()

		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if err := json.Unmarshal(data, &gotReq); err != nil {
			t.Errorf("decode run-task: %v", err)
		}
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"header":{"event":"task-started"}}`))

		_, data, err = conn.Read(ctx)
		if err != nil {
			return
		}
		_ = json.Unmarshal(data, &finish)

		_ = conn.Write(ctx, websocket.MessageBinary, audio)
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"header":{"event":"task-finished"}}`))
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()

	p := newTestAlibabaProvider(srv.URL)
	p.Model = "sambert-zhide-v1"
	p.Voice = "" // sambert takes the voice from the model id
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := p.Synthesize(context.Background(), "你好世界", out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, audio) {
		t.Fatalf("output = %q, want the streamed audio bytes", got)
	}

	if gotPath != alibabaTTSWSPath {
		t.Errorf("path = %q, want %q", gotPath, alibabaTTSWSPath)
	}
	if gotReq.Header.Action != "run-task" {
		t.Errorf("action = %q, want run-task", gotReq.Header.Action)
	}
	if gotReq.Payload.Model != "sambert-zhide-v1" {
		t.Errorf("model = %q, want sambert-zhide-v1", gotReq.Payload.Model)
	}
	if gotReq.Payload.Input.Text != "你好世界" {
		t.Errorf("input.text = %q, want the whole segment", gotReq.Payload.Input.Text)
	}
	if gotReq.Payload.Parameters.Format != "mp3" {
		t.Errorf("parameters.format = %q, want mp3", gotReq.Payload.Parameters.Format)
	}
	if gotReq.Payload.Parameters.TextType != "PlainText" {
		t.Errorf("parameters.text_type = %q, want PlainText", gotReq.Payload.Parameters.TextType)
	}
	if finish.Header.Action != "finish-task" {
		t.Errorf("second instruction = %q, want finish-task", finish.Header.Action)
	}
	if finish.Header.TaskID != gotReq.Header.TaskID {
		t.Errorf("finish task_id = %q, want %q", finish.Header.TaskID, gotReq.Header.TaskID)
	}
}

func TestAlibabaTTS_SambertTaskFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := context.Background()
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
		_ = conn.Write(ctx, websocket.MessageText,
			[]byte(`{"header":{"event":"task-failed","error_code":"InvalidParameter","error_message":"text is invalid"}}`))
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()

	p := newTestAlibabaProvider(srv.URL)
	p.Model = "sambert-zhide-v1"
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "InvalidParameter") || !strings.Contains(err.Error(), "text is invalid") {
		t.Fatalf("err = %v, want the engine's code and message", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("a failed synthesis must not leave an output file behind")
	}
}

func TestAlibabaUsesSambert(t *testing.T) {
	cases := map[string]bool{
		"sambert-zhide-v1":         true,
		" SAMBERT-eva-v1 ":         true,
		"cosyvoice-v3-flash":       false,
		"qwen-audio-3.1-tts-flash": false,
		"":                         false,
	}
	for model, want := range cases {
		if got := alibabaUsesSambert(model); got != want {
			t.Errorf("alibabaUsesSambert(%q) = %v, want %v", model, got, want)
		}
	}
}
