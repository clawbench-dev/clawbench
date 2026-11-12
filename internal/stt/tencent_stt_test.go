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
)

// mustCompileTimeAssert ensures TencentProvider can stream.
var _ StreamingSTTProvider = (*TencentProvider)(nil)

func TestTencentTranscribe(t *testing.T) {
	var gotPath string
	var gotHeaders http.Header
	var gotReq tencentSentenceRequest
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		gotPath = r.URL.Path
		gotHeaders = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"Response":{"Result":"你好世界。","RequestId":"req-1"}}`))
	}))
	defer srv.Close()

	p := NewTencentProvider("AKIDtest", "secretkey", "1250000000")
	p.BaseURL = srv.URL
	p.HTTPClient = srv.Client()

	text, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), "")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "你好世界。" {
		t.Fatalf("text = %q, want 你好世界。", text)
	}
	<-done

	if gotPath != tencentSentencePath {
		t.Errorf("path = %q, want %q", gotPath, tencentSentencePath)
	}
	if gotHeaders.Get("X-TC-Action") != "SentenceRecognition" {
		t.Errorf("X-TC-Action = %q", gotHeaders.Get("X-TC-Action"))
	}
	if gotHeaders.Get("X-TC-Version") != TencentAPIVersion {
		t.Errorf("X-TC-Version = %q, want %q", gotHeaders.Get("X-TC-Version"), TencentAPIVersion)
	}
	auth := gotHeaders.Get("Authorization")
	if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 Credential=AKIDtest/") {
		t.Errorf("Authorization = %q, want a TC3 credential for AKIDtest", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=content-type;host;x-tc-action") {
		t.Errorf("Authorization = %q, want the three signed headers", auth)
	}
	if gotHeaders.Get("Content-Type") != tencentJSONContentType {
		t.Errorf("Content-Type = %q, want %q", gotHeaders.Get("Content-Type"), tencentJSONContentType)
	}

	if gotReq.EngSerViceType != TencentDefaultModel {
		t.Errorf("EngSerViceType = %q, want %q", gotReq.EngSerViceType, TencentDefaultModel)
	}
	if gotReq.SourceType != 1 {
		t.Errorf("SourceType = %d, want 1 (inline data)", gotReq.SourceType)
	}
	if gotReq.VoiceFormat != "wav" {
		t.Errorf("VoiceFormat = %q, want wav", gotReq.VoiceFormat)
	}
	if gotReq.DataLen != len(wavProbe()) {
		t.Errorf("DataLen = %d, want %d", gotReq.DataLen, len(wavProbe()))
	}
	if gotReq.ConvertNumMode != 1 {
		t.Errorf("ConvertNumMode = %d, want 1 (ITN on by default)", gotReq.ConvertNumMode)
	}
}

func TestTencentTranscribeRejectsWebM(t *testing.T) {
	p := NewTencentProvider("id", "key", "1")
	webm := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, make([]byte, 32)...)
	_, err := p.Transcribe(context.Background(), strings.NewReader(string(webm)), "")
	if err == nil {
		t.Fatal("Transcribe accepted webm, want an actionable error")
	}
	if !strings.Contains(err.Error(), "webm") || !strings.Contains(err.Error(), "wav") {
		t.Fatalf("err = %v, want it to name webm and the formats the API does accept", err)
	}
}

func TestTencentTranscribeSurfacesBodyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Tencent reports failures with HTTP 200 and an Error object.
		_, _ = w.Write([]byte(`{"Response":{"Error":{"Code":"AuthFailure.SignatureFailure","Message":"signature mismatch"},"RequestId":"req-2"}}`))
	}))
	defer srv.Close()

	p := NewTencentProvider("id", "bad", "1")
	p.BaseURL = srv.URL
	p.HTTPClient = srv.Client()

	_, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), "")
	if err == nil {
		t.Fatal("Transcribe succeeded, want the body error")
	}
	for _, want := range []string{"AuthFailure.SignatureFailure", "signature mismatch", "req-2"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want it to carry %q", err, want)
		}
	}
}

func TestTencentTranscribeRequiresCredentials(t *testing.T) {
	cases := []*TencentProvider{
		NewTencentProvider("", "key", "1"),
		NewTencentProvider("id", "  ", "1"),
	}
	for _, p := range cases {
		if _, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), ""); err == nil {
			t.Error("Transcribe without credentials succeeded, want an error")
		}
	}
}

func TestTencentTranscribeRejectsOversizedAudio(t *testing.T) {
	audio := append([]byte("RIFF\x00\x00\x00\x00WAVE"), make([]byte, tencentMaxAudioBytes)...)
	p := NewTencentProvider("id", "key", "1")
	_, err := p.Transcribe(context.Background(), strings.NewReader(string(audio)), "")
	if err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("err = %v, want a size-limit error", err)
	}
}

// TestTencentTC3Signature pins the canonical request. A golden value is the
// only way to catch a changed canonical string: the server answers a mismatch
// with an opaque SignatureFailure, so nothing else in the test suite would
// notice that signing broke.
func TestTencentTC3Signature(t *testing.T) {
	got := tencentTC3Authorization("AKIDtest", "secretkey", tencentSignService,
		"SentenceRecognition", "asr.tencentcloudapi.com", `{"a":1}`, time.Unix(1700000000, 0).UTC())
	const want = "TC3-HMAC-SHA256 Credential=AKIDtest/2023-11-14/asr/tc3_request, SignedHeaders=content-type;host;x-tc-action, Signature=38a6b9c28b8dc845608455dc87b353c862dfaa8b2f8736847a9c8c88bb815edb"
	if got != want {
		t.Fatalf("Authorization =\n%s\nwant\n%s", got, want)
	}
}

func TestTencentVoiceFormat(t *testing.T) {
	cases := map[string]string{"wav": "wav", "mp3": "mp3", "ogg": "ogg-opus"}
	for in, want := range cases {
		got, err := tencentVoiceFormat(in)
		if err != nil {
			t.Fatalf("tencentVoiceFormat(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("tencentVoiceFormat(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := tencentVoiceFormat("webm"); err == nil {
		t.Error("tencentVoiceFormat(webm) succeeded, want an error")
	}
}

// TestTencentTranscribeReadError proves a read failure is reported instead of
// uploading a truncated clip. errReader is shared with the Alibaba tests.
func TestTencentTranscribeReadError(t *testing.T) {
	p := NewTencentProvider("id", "key", "1")
	if _, err := p.Transcribe(context.Background(), errReader{err: errors.New("boom")}, ""); err == nil {
		t.Fatal("Transcribe succeeded, want the read error")
	}
}
