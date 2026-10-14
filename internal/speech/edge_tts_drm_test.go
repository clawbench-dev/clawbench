package speech

import (
	"strings"
	"testing"
)

// TestGenerateSecMsGecV2_ReferenceVectors pins the v2 token algorithm to
// outputs from the reference implementation (Python edge-tts 7.3.1,
// `edge_tts.drm.generate_sec_ms_gec_v2`).
//
// Microsoft switched the Edge TTS service from the v1 token (plain SHA-256) to
// this v2 token, rejecting v1 with HTTP 403 at the WebSocket handshake. The v2
// hash is deliberately obfuscated (custom SHA-256 initial state, tweaked
// schedule, plus a CS64 mixing step), so a subtle porting error would produce a
// token that *looks* valid but is rejected by the service — a failure mode that
// only surfaces at runtime. These fixed vectors catch that at build time.
//
// If this test starts failing after an upstream change, re-port from the latest
// edge-tts release; do not "fix" the vectors.
func TestGenerateSecMsGecV2_ReferenceVectors(t *testing.T) {
	cases := []struct {
		unix int64
		want string
	}{
		{1700000000, "2CD23A2CA0B2595A-F3C846C370E05F9CC83ACC5F995C5F5D87A4D0F14CE8936A0F902E64099E38A2"},
		{1760400000, "F3E7AA1D0E82AD85-0BB8E511444B3FA2EB3371B2B4255C4F9F702AB439E6F4F4055E708AB81FF422"},
		// Same 5-minute bucket as 1760400000 — must produce an identical token.
		{1760400180, "F3E7AA1D0E82AD85-0BB8E511444B3FA2EB3371B2B4255C4F9F702AB439E6F4F4055E708AB81FF422"},
		// First second of the next bucket — must differ.
		{1760400300, "565B2DB384001D54-6BE7EA3F6DC939B111D60DE0C2543CF4D3F3A2A5E316F6820ECAAE56169B9F86"},
		{1760432100, "39878E4ACE0AA17C-175AACC5BB405C1DC098D3CB971B8515CCDF619CDABE5AC12279AADD938D1349"},
		{1900000000, "35CB2860F259268E-7C6AC7AAFAF9ECFDEF5C38802D91759C088D45E6E8BDBC2EC1C78C22D51F1DF6"},
	}
	for _, tc := range cases {
		got := generateSecMsGecV2(tc.unix)
		if got != tc.want {
			t.Errorf("generateSecMsGecV2(%d) = %q, want %q", tc.unix, got, tc.want)
		}
	}
}

// TestGenerateSecMsGecV2_Format verifies the token shape the service expects:
// "<16 uppercase hex>-<64 uppercase hex>".
func TestGenerateSecMsGecV2_Format(t *testing.T) {
	got := generateSecMsGecV2(1760400000)

	parts := strings.Split(got, "-")
	if len(parts) != 2 {
		t.Fatalf("token %q: want exactly one '-' separator", got)
	}
	if len(parts[0]) != 16 {
		t.Errorf("CS64 part %q: want 16 hex chars, got %d", parts[0], len(parts[0]))
	}
	if len(parts[1]) != 64 {
		t.Errorf("digest part %q: want 64 hex chars, got %d", parts[1], len(parts[1]))
	}
	for _, part := range parts {
		for _, c := range part {
			if !strings.ContainsRune("0123456789ABCDEF", c) {
				t.Errorf("token %q contains non-uppercase-hex rune %q", got, c)
			}
		}
	}
}

// TestGenerateSecMsGecV2_FiveMinuteBucketing confirms the token only changes on
// 5-minute boundaries (it is derived from the bucket, not the exact second).
func TestGenerateSecMsGecV2_FiveMinuteBucketing(t *testing.T) {
	const base int64 = 1760400000 // exact bucket boundary
	if generateSecMsGecV2(base) != generateSecMsGecV2(base+299) {
		t.Error("token changed within a 5-minute bucket")
	}
	if generateSecMsGecV2(base) == generateSecMsGecV2(base+300) {
		t.Error("token did not change across a 5-minute boundary")
	}
}
