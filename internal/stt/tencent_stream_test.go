package stt

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// wsBase turns an httptest HTTP URL into the ws:// base the provider dials.
func wsBase(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// camStub answers the APPID lookup so unit tests never reach Tencent's account
// API. Every provider that dials the realtime host needs one: the AppID comes
// from the key pair, not from the struct.
func camStub(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"Response":{"AppId":1250000000}}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestTencentOpenStream(t *testing.T) {
	var gotQuery url.Values
	var gotPath string
	var audioBytes int
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := context.Background()

		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"code":0,"message":"success","voice_id":"v1"}`))
		for i := range 2 {
			mt, data, err := conn.Read(ctx)
			if err != nil || mt != websocket.MessageBinary {
				t.Errorf("frame %d: type=%v err=%v, want binary", i, mt, err)
				return
			}
			audioBytes += len(data)
		}
		mt, data, err := conn.Read(ctx)
		if err != nil || mt != websocket.MessageText || !strings.Contains(string(data), `"end"`) {
			t.Errorf("end frame: type=%v data=%s err=%v", mt, data, err)
			return
		}

		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"code":0,"result":{"slice_type":0,"index":0,"voice_text_str":""}}`))
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"code":0,"result":{"slice_type":1,"index":0,"voice_text_str":"你好"}}`))
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"code":0,"result":{"slice_type":2,"index":0,"voice_text_str":"你好，世界。"}}`))
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"code":0,"final":1}`))
	}))
	defer srv.Close()

	p := NewTencentProvider("AKIDtest", "secretkey", "1250000000")
	p.WSBaseURL = wsBase(srv)
	p.CAMBaseURL = camStub(t)
	p.EndWindowSize = 900

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

	if gotPath != tencentStreamAudioPath+"1250000000" {
		t.Errorf("path = %q, want %q", gotPath, tencentStreamAudioPath+"1250000000")
	}
	if audioBytes != 640 {
		t.Errorf("audio bytes = %d, want 640", audioBytes)
	}
	// Handshake parameters.
	if gotQuery.Get("secretid") != "AKIDtest" {
		t.Errorf("secretid = %q", gotQuery.Get("secretid"))
	}
	if gotQuery.Get("engine_model_type") != TencentDefaultModel {
		t.Errorf("engine_model_type = %q, want %q", gotQuery.Get("engine_model_type"), TencentDefaultModel)
	}
	if gotQuery.Get("voice_format") != "1" {
		t.Errorf("voice_format = %q, want 1 (PCM)", gotQuery.Get("voice_format"))
	}
	if gotQuery.Get("needvad") != "1" {
		t.Errorf("needvad = %q, want 1", gotQuery.Get("needvad"))
	}
	if gotQuery.Get("vad_silence_time") != "900" {
		t.Errorf("vad_silence_time = %q, want 900", gotQuery.Get("vad_silence_time"))
	}
	if gotQuery.Get("voice_id") == "" {
		t.Error("voice_id is empty")
	}
	if gotQuery.Get("convert_num_mode") != "1" {
		t.Errorf("convert_num_mode = %q, want 1", gotQuery.Get("convert_num_mode"))
	}

	// Results: empty slice 0 is not a change, interim, final sentence, final marker.
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
}

// TestTencentStreamSignature recomputes the handshake signature from the query
// the server actually received. Recomputing (rather than reusing the provider's
// helper) is what proves the signed string matches the wire form.
func TestTencentStreamSignature(t *testing.T) {
	var gotQuery url.Values
	var gotHostPath string
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		gotQuery = r.URL.Query()
		gotHostPath = r.Host + r.URL.Path
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		// No audio is needed: the signature is fully determined by the query.
		_ = conn.Write(context.Background(), websocket.MessageText, []byte(`{"code":0,"message":"success"}`))
	}))
	defer srv.Close()

	p := NewTencentProvider("AKIDtest", "secretkey", "1250000000")
	p.WSBaseURL = wsBase(srv)
	p.CAMBaseURL = camStub(t)

	session, err := p.OpenStream(context.Background(), StreamOptions{SampleRate: 16000, Channels: 1, Bits: 16})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer session.Close()
	<-done

	params := url.Values{}
	for k, v := range gotQuery {
		if k == "signature" {
			continue
		}
		params[k] = v
	}
	mac := hmac.New(sha1.New, []byte("secretkey"))
	_, _ = mac.Write([]byte(gotHostPath + "?" + params.Encode()))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if got := gotQuery.Get("signature"); got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestTencentOpenStreamHandshakeFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		_ = conn.Write(context.Background(), websocket.MessageText,
			[]byte(`{"code":4002,"message":"鉴权失败"}`))
	}))
	defer srv.Close()

	p := NewTencentProvider("bad", "bad", "1")
	p.WSBaseURL = wsBase(srv)
	p.CAMBaseURL = camStub(t)

	_, err := p.OpenStream(context.Background(), StreamOptions{})
	if err == nil {
		t.Fatal("OpenStream succeeded, want a handshake error")
	}
	if !strings.Contains(err.Error(), "4002") || !strings.Contains(err.Error(), "鉴权失败") {
		t.Fatalf("err = %v, want the engine's code and message", err)
	}
	// The UIN sits next to the APPID in the console and passes every other
	// check, so the one auth failure users hit has to name the field.
	if !strings.Contains(err.Error(), "not the UIN") {
		t.Fatalf("err = %v, want the AppID-vs-UIN hint", err)
	}
}

func TestTencentOpenStreamRejectsBadOptions(t *testing.T) {
	cases := []struct {
		name string
		p    *TencentProvider
		opts StreamOptions
		want string
	}{
		{"no credentials", NewTencentProvider("", "", "1"), StreamOptions{}, "SecretId"},
		{"stereo", NewTencentProvider("id", "key", "1"), StreamOptions{Channels: 2}, "mono"},
		{"8-bit", NewTencentProvider("id", "key", "1"), StreamOptions{Bits: 8}, "16-bit"},
		{"8k model on 16k audio", func() *TencentProvider {
			p := NewTencentProvider("id", "key", "1")
			p.Model = "8k_zh"
			return p
		}(), StreamOptions{SampleRate: 16000}, "8 kHz"},
		{"16k model on 8k audio", NewTencentProvider("id", "key", "1"), StreamOptions{SampleRate: 8000}, "16 kHz"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.p.OpenStream(context.Background(), c.opts)
			if err == nil {
				t.Fatal("OpenStream succeeded, want an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestTencentStreamErrorFrame(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := context.Background()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"code":0,"message":"success"}`))
		_, _, _ = conn.Read(ctx)
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"code":4000,"message":"音频发送过多"}`))
	}))
	defer srv.Close()

	p := NewTencentProvider("id", "key", "1")
	p.WSBaseURL = wsBase(srv)
	p.CAMBaseURL = camStub(t)
	session, err := p.OpenStream(context.Background(), StreamOptions{})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer session.Close()
	if err := session.Write(make([]byte, 320)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	select {
	case res, ok := <-session.Results():
		if !ok {
			t.Fatal("results closed without an error")
		}
		if res.Err == nil || !strings.Contains(res.Err.Error(), "4000") {
			t.Fatalf("result = %+v, want an error carrying code 4000", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the error result")
	}
	<-done
}

// TestTencentStreamIgnoresRepeatedFinalIndex guards the accumulator against the
// engine resending a finalized sentence: a duplicate index must not append its
// text twice.
func TestTencentStreamIgnoresRepeatedFinalIndex(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := context.Background()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"code":0,"message":"success"}`))
		_, _, _ = conn.Read(ctx)
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"code":0,"result":{"slice_type":2,"index":0,"voice_text_str":"你好。"}}`))
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"code":0,"result":{"slice_type":2,"index":0,"voice_text_str":"你好。"}}`))
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"code":0,"final":1}`))
	}))
	defer srv.Close()

	p := NewTencentProvider("id", "key", "1")
	p.WSBaseURL = wsBase(srv)
	p.CAMBaseURL = camStub(t)
	session, err := p.OpenStream(context.Background(), StreamOptions{})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer session.Close()
	if err := session.Write(make([]byte, 320)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var results []StreamResult
	for res := range session.Results() {
		results = append(results, res)
	}
	<-done

	last := results[len(results)-1]
	if last.Text != "你好。" {
		t.Fatalf("final text = %q, want 你好。 (duplicate index must not append)", last.Text)
	}
	for _, r := range results {
		if strings.Count(r.Text, "你好") > 1 {
			t.Fatalf("result %+v repeated the sentence", r)
		}
	}
}

func TestTencentStreamURLDefaults(t *testing.T) {
	p := NewTencentProvider("id", "key", "1250000000")
	u, err := p.streamURL("16k_zh", "1250000000")
	if err != nil {
		t.Fatalf("streamURL: %v", err)
	}
	if !strings.HasPrefix(u, "wss://asr.cloud.tencent.com/asr/v2/1250000000?") {
		t.Fatalf("url = %q, want the production realtime endpoint", u)
	}
	if !strings.Contains(u, "signature=") {
		t.Fatal("url has no signature")
	}

	// A provider whose base is an HTTP URL must still dial wss.
	p.WSBaseURL = "https://asr.cloud.tencent.com"
	u, err = p.streamURL("16k_zh", "1250000000")
	if err != nil {
		t.Fatalf("streamURL: %v", err)
	}
	if !strings.HasPrefix(u, "wss://") {
		t.Fatalf("url = %q, want an upgraded wss URL", u)
	}
}

func TestTencentStreamResponseDecodes(t *testing.T) {
	// A frame without a result (the handshake ack and the final marker) must
	// decode without a result, otherwise the handshake would look like speech.
	var resp tencentStreamResponse
	if err := json.Unmarshal([]byte(`{"code":0,"message":"success","voice_id":"v"}`), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Result != nil {
		t.Fatalf("result = %+v, want nil", resp.Result)
	}
}
