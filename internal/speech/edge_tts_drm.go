package speech

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

// This file implements Microsoft Edge TTS's v2 Sec-MS-GEC token.
//
// Background: until 2025-10 the service accepted a v1 token — a plain
// SHA-256 hex digest of "<windowsFileTime><trustedClientToken>" (see git
// history). Microsoft then switched the service to a v2 token that appends a
// CS64 digest and derives its SHA-256 from a modified initial state, so the
// v1 token is now rejected with HTTP 403 at the WebSocket handshake.
//
// The algorithm below is a faithful port of edge-tts 7.3.1's drm.py
// (`generate_sec_ms_gec_v2` / `_token_hash` / `_sha256_digest` /
// `_cs64_digest`). The reference implementation deliberately obfuscates the
// hash by seeding a non-standard SHA-256 state and tweaking the message
// schedule, so the constants here must match byte-for-byte. The token format
// is "<16 hex>-<64 hex>" and the accompanying Sec-MS-GEC-Version is "2-<ver>".
//
// Because Microsoft rotates this scheme, treat a 403 handshake as a signal to
// re-port from the latest edge-tts release rather than as a transient error.

// sha256Initial is the standard FIPS 180-4 SHA-256 initial hash value. The v2
// token XORs the custom working state with this at the end of _sha256_init.
var sha256Initial = [8]uint32{
	0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
	0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19,
}

// sha256State is the custom starting working state for the first (partial)
// block, replacing the standard initial value.
var sha256State = [8]uint32{
	0xFD9FBEA2, 0x91243CE7, 0xBE499094, 0xCD2E1E0A,
	0x6C006F49, 0x04BD35AB, 0xFEB0A7E4, 0x03EFFBD2,
}

// sha256Schedule seeds the first 14 words of the modified message schedule.
var sha256Schedule = [14]uint32{
	0xEB24BF9E, 0x1558691F, 0xA0C09EE2, 0x0CFA32BA,
	0xA622EE8C, 0xD1DD9491, 0xC0DD25A0, 0xF9E70AB6,
	0xF0C31573, 0x7058CE4E, 0x7188E606, 0x00E5AB33,
	0xD258B060, 0x72766564,
}

// sha256RoundConstants is the standard SHA-256 round constant table.
var sha256RoundConstants = [64]uint32{
	0x428A2F98, 0x71374491, 0xB5C0FBCF, 0xE9B5DBA5, 0x3956C25B, 0x59F111F1, 0x923F82A4, 0xAB1C5ED5,
	0xD807AA98, 0x12835B01, 0x243185BE, 0x550C7DC3, 0x72BE5D74, 0x80DEB1FE, 0x9BDC06A7, 0xC19BF174,
	0xE49B69C1, 0xEFBE4786, 0x0FC19DC6, 0x240CA1CC, 0x2DE92C6F, 0x4A7484AA, 0x5CB0A9DC, 0x76F988DA,
	0x983E5152, 0xA831C66D, 0xB00327C8, 0xBF597FC7, 0xC6E00BF3, 0xD5A79147, 0x06CA6351, 0x14292967,
	0x27B70A85, 0x2E1B2138, 0x4D2C6DFC, 0x53380D13, 0x650A7354, 0x766A0ABB, 0x81C2C92E, 0x92722C85,
	0xA2BFE8A1, 0xA81A664B, 0xC24B8B70, 0xC76C51A3, 0xD192E819, 0xD6990624, 0xF40E3585, 0x106AA070,
	0x19A4C116, 0x1E376C08, 0x2748774C, 0x34B0BCB5, 0x391C0CB3, 0x4ED8AA4A, 0x5B9CCA4F, 0x682E6FF3,
	0x748F82EE, 0x78A5636F, 0x84C87814, 0x8CC70208, 0x90BEFFFA, 0xA4506CEB, 0xBEF9A3F7, 0xC67178F2,
}

// Adjustment constants that distinguish the v2 hash from standard SHA-256.
const (
	sha256Adjustment       = 0xC9DC06A7
	sha256Sigma0Adjustment = 0x059C0B80
	sha256Sigma1Adjustment = 0x000B92C0
)

// cs64Coefficients seeds the CS64 digest mixing step.
var cs64Coefficients = [14][2]uint32{
	{0xE82E91B1, 0xB32700D0},
	{0x44DE24C6, 0x88DFF51D},
	{0x3BEB65BC, 0x29514038},
	{0xC89C5933, 0xB328CBDF},
	{0x8D1EC211, 0xD405B4D5},
	{0x314EF5D5, 0x26E55F3C},
	{0xE92491B1, 0x607801C6},
	{0xF0A5B9D5, 0x82B2C1FB},
	{0x06064CF3, 0xFA51A3DA},
	{0xDA79FA60, 0x207CC464},
	{0xBED20560, 0x3C3C74F3},
	{0x89F9C9F8, 0xD2D14B61},
	{0x35FA38C6, 0xBB04A2C1},
	{0xCB49E3C6, 0xC5626167},
}

const (
	cs64Factor      = 0x82893933
	cs64Adjustment0 = 0xF2F90B1A
	cs64Adjustment1 = 0x0B1A0000
)

// sha256BlockConstants is sha256RoundConstants with round 14 replaced by the
// v2 adjustment, used when resuming the first (partial) block mid-round.
var sha256BlockConstants = func() [64]uint32 {
	c := sha256RoundConstants
	c[14] = sha256Adjustment
	return c
}()

func rol16(v uint32) uint32 { return v<<16 | v>>16 }

func sha256Sigma0(v uint32) uint32 { return v>>7 ^ v<<25 ^ v>>18 ^ v<<14 ^ v>>3 }
func sha256Sigma1(v uint32) uint32 { return v>>17 ^ v<<15 ^ v>>19 ^ v<<13 ^ v>>10 }

// expandSchedule extends the message schedule from `first` to 64 words.
func expandSchedule(words []uint32, first int) []uint32 {
	for i := first; i < 64; i++ {
		x, y := words[i-2], words[i-15]
		s1 := sha256Sigma1(x)
		s0 := sha256Sigma0(y)
		words = append(words, s1+words[i-7]+s0+words[i-16])
	}
	return words
}

// sha256Rounds runs rounds [firstRound, 64) of SHA-256 compression.
func sha256Rounds(state [8]uint32, words []uint32, firstRound int) [8]uint32 {
	a, b, c, d, e, f, g, h := state[0], state[1], state[2], state[3], state[4], state[5], state[6], state[7]
	constants := &sha256RoundConstants
	if firstRound == 14 {
		constants = &sha256BlockConstants
	}
	for i := firstRound; i < 64; i++ {
		bigE := e>>6 ^ e<<26 ^ e>>11 ^ e<<21 ^ e>>25 ^ e<<7
		bigA := a>>2 ^ a<<30 ^ a>>13 ^ a<<19 ^ a>>22 ^ a<<10
		t1 := h + bigE + (g ^ (e & (f ^ g))) + constants[i] + words[i]
		t2 := bigA + ((a & b) | (c & (a | b)))
		h, g, f, e, d, c, b, a = g, f, e, d+t1, c, b, a, t1+t2
	}
	return [8]uint32{a, b, c, d, e, f, g, h}
}

// sha256Init computes the v2 token's custom initial state from the first seven
// bytes of the token data.
func sha256Init(data7 []byte) [8]uint32 {
	words := make([]uint32, 14, 64)
	words = append(words,
		uint32(data7[0])<<16|uint32(data7[1])<<8|uint32(data7[2]),
		binary.BigEndian.Uint32(data7[3:7]),
	)
	for index := 16; index < 30; index++ {
		value := sha256Schedule[index-16]
		if index == 16 {
			value += sha256Sigma1(words[index-2]) ^ sha256Sigma1Adjustment
		} else {
			value += sha256Sigma1(words[index-2])
		}
		if index-7 >= 14 {
			value += words[index-7]
		}
		if index-15 >= 14 {
			if index == 29 {
				value += sha256Sigma0(words[index-15]) ^ sha256Sigma0Adjustment
			} else {
				value += sha256Sigma0(words[index-15])
			}
		}
		words = append(words, value)
	}
	words = append(words, sha256Sigma1(words[28])+words[23]+sha256Sigma0(words[15])+words[14]+
		sha256Adjustment-sha256RoundConstants[14])
	words = expandSchedule(words, 31)
	working := sha256Rounds(sha256State, words, 14)
	var out [8]uint32
	for i := range out {
		out[i] = sha256Initial[i] + working[i]
	}
	return out
}

// tokenSha256Digest is the v2 variant of SHA-256 used for the token. It differs
// from standard SHA-256 only in its initial state and schedule seeding.
func tokenSha256Digest(data []byte) []byte {
	length := 57 + len(data)
	padded := append([]byte{}, data[7:]...)
	padded = append(padded, 0x80)
	pad := (55 - length) % 64
	if pad < 0 {
		pad += 64
	}
	for range pad {
		padded = append(padded, 0)
	}
	var lengthBytes [8]byte
	binary.BigEndian.PutUint64(lengthBytes[:], uint64(length)*8)
	padded = append(padded, lengthBytes[:]...)

	state := sha256Init(data[:7])
	for offset := 0; offset < len(padded); offset += 64 {
		words := make([]uint32, 16, 64)
		for i := range 16 {
			words[i] = binary.BigEndian.Uint32(padded[offset+4*i : offset+4*i+4])
		}
		words = expandSchedule(words, 16)
		working := sha256Rounds(state, words, 0)
		for i := range state {
			state[i] += working[i]
		}
	}
	out := make([]byte, 32)
	for i := range state {
		binary.BigEndian.PutUint32(out[4*i:], state[i])
	}
	return out
}

// wordsLE4Even splits data into little-endian 32-bit words, zero-padded to an
// even count.
func wordsLE4Even(data []byte) []uint32 {
	padded := append([]byte{}, data...)
	for len(padded)%4 != 0 {
		padded = append(padded, 0)
	}
	words := make([]uint32, 0, len(padded)/4)
	for i := 0; i < len(padded); i += 4 {
		words = append(words, binary.LittleEndian.Uint32(padded[i:i+4]))
	}
	if len(words)%2 != 0 {
		words = append(words, 0)
	}
	return words
}

// cs64Digest computes the two CS64 digest words mixed from the token data and
// the SHA-256 digest.
func cs64Digest(data, digest []byte) (uint32, uint32) {
	keyA := binary.LittleEndian.Uint32(digest[0:4]) | 1
	keyB := binary.LittleEndian.Uint32(digest[4:8]) | 1
	factors := [2]uint32{keyA * cs64Factor, keyB * cs64Factor}

	terms := make([]uint32, 0, 64)
	offsets := make([]uint32, 0, 64)
	for i, c := range cs64Coefficients {
		terms = append(terms, c[0]*factors[i%2])
		offsets = append(offsets, c[1])
	}
	for i, w := range wordsLE4Even(append([]byte{0}, data...)) {
		key := keyB
		offsetFactor := uint32(0xC31713DB)
		if i%2 == 0 {
			key = keyA
			offsetFactor = 0xEF0569FB
		}
		term := w * key
		offset := rol16(w) * offsetFactor
		if i == 0 {
			term += cs64Adjustment0 * factors[0]
			offset += cs64Adjustment1
		}
		terms = append(terms, term)
		offsets = append(offsets, offset)
	}

	var swapAcc, swapLast, state, acc uint32
	for i := 0; i < len(terms); i += 2 {
		x, y := terms[i], terms[i+1]
		t := rol16(offsets[i] + x)
		q := t * 0x76F50F34
		swapAcc += rol16(q)*0xC3EFEA97 + q
		t = rol16(offsets[i+1] + y)
		q = t * 0x1881CE3C
		swapLast = rol16(q)*0x35BD1EC9 + q
		swapAcc += swapLast

		state = state*keyA + x
		state = rol16(state) * 0xCF98B111
		state = rol16(state) * 0x87085B9F
		state = rol16(state) * 0x12CEB96D
		state = rol16(state) * 0x257E1D83
		acc += state

		t = state*keyB + y
		t = rol16(t) * 0xA27416F5
		t = rol16(t) * 0xD38396FF
		state = rol16(rol16(t)*0x7C932B89) * 0xBFA49F69
		acc += state
	}
	return swapLast ^ state, swapAcc ^ acc
}

// generateSecMsGecV2 returns the v2 Sec-MS-GEC token for the given Unix
// timestamp. The result has the form "<16 hex>-<64 hex>".
func generateSecMsGecV2(unixTimestamp int64) string {
	// Switch to the Windows file-time epoch (1601-01-01), round down to the
	// nearest 5 minutes, then convert to 100-nanosecond intervals.
	ticks := float64(unixTimestamp)
	ticks += winEpochOffset
	ticks -= math.Mod(ticks, 300)
	ticks *= 1e9 / 100

	data := []byte(fmt.Sprintf("%.0f%s", ticks, edgeTrustedClientToken))
	digest := tokenSha256Digest(data)
	cs64A, cs64B := cs64Digest(data, digest)

	cs64 := make([]byte, 8)
	binary.LittleEndian.PutUint32(cs64[0:4], cs64A)
	binary.LittleEndian.PutUint32(cs64[4:8], cs64B)
	return strings.ToUpper(hex.EncodeToString(cs64)) + "-" + strings.ToUpper(hex.EncodeToString(digest))
}
