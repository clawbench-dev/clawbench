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

func TestAlibabaTTS_SambertIgnoresFramesItCannotRead(t *testing.T) {
	// A real stream can carry a text frame that is not our JSON (a keepalive, a
	// vendor notice) or an event we do not handle. Neither may abort a synthesis
	// that is otherwise producing audio — the audio is the payload; control
	// frames are advisory. This pins the "unknown frame → keep reading" contract
	// that the control-frame split introduced.
	audio := mp3(strings.Repeat("c", 32))
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
		// Not JSON at all.
		_ = conn.Write(ctx, websocket.MessageText, []byte(`not json`))
		// Valid JSON, but an event nobody handles.
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"header":{"event":"task-queued"}}`))
		_ = conn.Write(ctx, websocket.MessageBinary, audio)
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"header":{"event":"task-finished"}}`))
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()

	p := newTestAlibabaProvider(srv.URL)
	p.Model = "sambert-zhide-v1"
	p.Voice = ""
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := p.Synthesize(context.Background(), "你好世界", out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, audio) {
		t.Fatalf("output = %q, want the audio that followed the unreadable frames", got)
	}
}

func TestAlibabaTTS_SambertEmptyStreamIsAnError(t *testing.T) {
	// task-finished with zero audio bytes must fail rather than write a 0-byte
	// file: an empty mp3 plays as silence, which looks like a working voice
	// that produced nothing.
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
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"header":{"event":"task-finished"}}`))
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()

	p := newTestAlibabaProvider(srv.URL)
	p.Model = "sambert-zhide-v1"
	p.Voice = ""
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "no audio") {
		t.Fatalf("err = %v, want a 'returned no audio' error", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("a synthesis with no audio must not leave an output file behind")
	}
}

func TestSambertFailureReason(t *testing.T) {
	// The error surface is what a user sees when a voice is misconfigured, so
	// every shape must read sensibly — including the one where the vendor sends
	// neither a code nor a message.
	var ev alibabaSambertEvent
	ev.Header.ErrorCode = "InvalidParameter"
	ev.Header.ErrorMessage = "text is invalid"
	if got := sambertFailureReason(ev); got != "InvalidParameter: text is invalid" {
		t.Errorf("code+message = %q", got)
	}

	ev = alibabaSambertEvent{}
	ev.Header.ErrorMessage = "quota exceeded"
	if got := sambertFailureReason(ev); got != "quota exceeded" {
		t.Errorf("message only = %q", got)
	}

	ev = alibabaSambertEvent{}
	if got := sambertFailureReason(ev); got != "no reason given" {
		t.Errorf("empty = %q, want the placeholder", got)
	}
}

func TestAlibabaUsesSambert(t *testing.T) {
	// The padded key is deliberate: model ids arrive from user config, and the
	// family check has to trim before matching (otherwise a stray space silently
	// routes sambert models to the HTTP endpoint, which rejects them).
	cases := map[string]bool{
		"sambert-zhide-v1": true,
		// Padded on purpose: model ids come from user config, and the family
		// check must trim before matching (otherwise a stray space silently
		// routes sambert models to the HTTP endpoint, which rejects them).
		" SAMBERT-eva-v1 ":         true, //nolint:gocritic // whitespace is the case under test
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

func TestAlibabaTTS_ValidateCoversEveryRequiredField(t *testing.T) {
	cases := []struct {
		name string
		mut  func(p *AlibabaTTSProvider)
		want string
	}{
		{"api_key", func(p *AlibabaTTSProvider) { p.APIKey = "" }, "tts.alibaba.api_key"},
		{"api_key blank", func(p *AlibabaTTSProvider) { p.APIKey = "   " }, "tts.alibaba.api_key"},
		{"model", func(p *AlibabaTTSProvider) { p.Model = "" }, "tts.alibaba.model"},
		{"voice", func(p *AlibabaTTSProvider) { p.Voice = "" }, "tts.alibaba.voice"},
	}
	for _, tc := range cases {
		p := newTestAlibabaProvider("http://unused")
		tc.mut(p)
		if err := p.validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: validate err = %v, want it to name %s", tc.name, err, tc.want)
		}
	}
	if err := newTestAlibabaProvider("http://unused").validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}

	// sambert names the voice by model id, so an empty voice is NOT a
	// misconfiguration there — this is the branch that would otherwise send the
	// user to fill in a field the engine ignores.
	sambert := newTestAlibabaProvider("http://unused")
	sambert.Model = "sambert-zhide-v1"
	sambert.Voice = ""
	if err := sambert.validate(); err != nil {
		t.Errorf("sambert without a voice must validate: %v", err)
	}
}

func TestAlibabaTTS_SpeedIsSentOnlyWhenItDiffers(t *testing.T) {
	// 1.0x is the default, so the field is omitted; anything else is clamped to
	// the documented 0.5…2.0 range. Asserted on the wire body.
	cases := []struct {
		speed float64
		want  float64
	}{
		{0, 0}, // unset
		{1, 0}, // default → omitted
		{1.5, 1.5},
		{3, 2}, // clamped
		{0.1, 0.5},
	}
	for _, tc := range cases {
		var got struct {
			Input struct {
				Rate float64 `json:"rate"`
			} `json:"input"`
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &got)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 200,
				"output":      map[string]any{"audio": map[string]any{"data": base64.StdEncoding.EncodeToString(mp3(strings.Repeat("r", 32)))}},
			})
		}))

		p := newTestAlibabaProvider(srv.URL)
		p.Speed = tc.speed
		out := filepath.Join(t.TempDir(), "out.mp3")
		err := p.Synthesize(context.Background(), "你好", out, "zh")
		srv.Close()
		if err != nil {
			t.Fatalf("speed %v: Synthesize: %v", tc.speed, err)
		}
		if got.Input.Rate != tc.want {
			t.Errorf("speed %v → input.rate %v, want %v", tc.speed, got.Input.Rate, tc.want)
		}
	}
}

func TestAlibabaTTS_HTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("key disabled"))
	}))
	defer srv.Close()

	p := newTestAlibabaProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "key disabled") {
		t.Fatalf("err = %v, want the status and the body", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("a failed synthesis must not leave an output file behind")
	}
}

func TestAlibabaTTS_MalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>gateway</html>`))
	}))
	defer srv.Close()

	p := newTestAlibabaProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "malformed response") {
		t.Fatalf("err = %v, want a malformed-response error", err)
	}
}

func TestAlibabaTTS_NoAudioFieldAtAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": 200,
			"request_id":  "req-empty",
		})
	}))
	defer srv.Close()

	p := newTestAlibabaProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "neither audio.data nor audio.url") || !strings.Contains(err.Error(), "req-empty") {
		t.Fatalf("err = %v, want an error naming both shapes and the request", err)
	}
}

func TestAlibabaTTS_UndecodableInlineAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": 200,
			"output":      map[string]any{"audio": map[string]any{"data": "!!!!not-base64!!!!"}},
		})
	}))
	defer srv.Close()

	p := newTestAlibabaProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "decode inline audio") {
		t.Fatalf("err = %v, want an inline decode error", err)
	}
}

func TestAlibabaTTS_DownloadFailures(t *testing.T) {
	audio := mp3(strings.Repeat("a", 32))
	cases := []struct {
		name   string
		status int
		body   []byte
		want   string
	}{
		{"non-200", http.StatusNotFound, []byte("gone"), "status 404"},
		{"empty body", http.StatusOK, nil, "downloaded audio is empty"},
	}
	for _, tc := range cases {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/services/audio/tts/SpeechSynthesizer", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 200,
				"output":      map[string]any{"audio": map[string]any{"url": "http://" + r.Host + "/audio"}},
			})
		})
		mux.HandleFunc("/audio", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write(tc.body)
		})
		srv := httptest.NewServer(mux)

		p := newTestAlibabaProvider(srv.URL)
		out := filepath.Join(t.TempDir(), "out.mp3")
		err := p.Synthesize(context.Background(), "你好", out, "zh")
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	_ = audio
}

// TestAlibabaTTS_DownloadDoesNotSendTheAPIKey pins a security property: the
// audio URL is a pre-signed OSS link carrying its own credentials, so the
// provider's API key must not travel with it (a third-party host would
// otherwise receive it).
func TestAlibabaTTS_DownloadDoesNotSendTheAPIKey(t *testing.T) {
	audio := mp3(strings.Repeat("a", 32))
	var downloadAuth, downloadPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/services/audio/tts/SpeechSynthesizer", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": 200,
			"output":      map[string]any{"audio": map[string]any{"url": "http://" + r.Host + "/oss/audio.mp3"}},
		})
	})
	mux.HandleFunc("/oss/audio.mp3", func(w http.ResponseWriter, r *http.Request) {
		downloadAuth = r.Header.Get("Authorization")
		downloadPath = r.URL.Path
		_, _ = w.Write(audio)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := newTestAlibabaProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := p.Synthesize(context.Background(), "你好", out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if downloadPath != "/oss/audio.mp3" {
		t.Errorf("download path = %q", downloadPath)
	}
	if downloadAuth != "" {
		t.Errorf("Authorization = %q on the pre-signed download, want none", downloadAuth)
	}
}

func TestAlibabaTTS_NoAudioDataAtAll(t *testing.T) {
	// Every segment succeeds but produces zero bytes: a 0-byte file must not be
	// left behind (silence looks like a working voice).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": 200,
			"output":      map[string]any{"audio": map[string]any{"data": ""}},
		})
	}))
	defer srv.Close()

	p := newTestAlibabaProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "neither audio.data nor audio.url") {
		t.Fatalf("err = %v, want the no-audio-shape error", err)
	}
}

// TestAlibabaTTS_SambertDialFailure: a closed port must surface as a sambert
// dial error, not a hang.
func TestAlibabaTTS_SambertDialFailure(t *testing.T) {
	p := newTestAlibabaProvider("http://127.0.0.1:1")
	p.Model = "sambert-zhide-v1"
	p.Voice = ""
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "sambert dial") {
		t.Fatalf("err = %v, want a dial failure", err)
	}
}
