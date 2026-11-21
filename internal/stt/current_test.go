package stt

import (
	"context"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentProvider(t *testing.T) {
	orig := Current()
	t.Cleanup(func() { SetCurrent(orig) })

	require.NotNil(t, Current(), "there is a default provider")

	p := &fakeProvider{}
	SetCurrent(p)
	assert.Same(t, p, Current())
}

type fakeProvider struct{}

func (fakeProvider) Transcribe(_ context.Context, _ io.Reader, _ string) (string, error) {
	return "", nil
}

func TestPeakRMS(t *testing.T) {
	assert.Zero(t, PeakRMS(nil))
	assert.Zero(t, PeakRMS([]byte{1}), "a single byte is not a sample")

	// 16384 of 32768 = half scale.
	pcm := []byte{0x00, 0x40}
	assert.InDelta(t, 0.5, PeakRMS(pcm), 0.001)

	// Negative peaks are folded to their magnitude.
	neg := []byte{0x00, 0xC0} // -16384
	assert.InDelta(t, 0.5, PeakRMS(neg), 0.001)
}

func TestWAVFromPCM(t *testing.T) {
	pcm := []byte{1, 2, 3, 4}
	out := WAVFromPCM(pcm, 16000)

	require.Len(t, out, 44+len(pcm))
	assert.Equal(t, "RIFF", string(out[0:4]))
	assert.Equal(t, "WAVE", string(out[8:12]))
	assert.Equal(t, "fmt ", string(out[12:16]))
	assert.Equal(t, "data", string(out[36:40]))
	assert.Equal(t, uint32(16000), binary.LittleEndian.Uint32(out[24:28]), "sample rate")
	assert.Equal(t, uint32(len(pcm)), binary.LittleEndian.Uint32(out[40:44]), "data size")
	assert.Equal(t, pcm, out[44:])

	// An out-of-range rate falls back to 16 kHz instead of truncating silently.
	fallback := WAVFromPCM(pcm, 0)
	assert.Equal(t, uint32(16000), binary.LittleEndian.Uint32(fallback[24:28]))
	fallback = WAVFromPCM(pcm, 999_999)
	assert.Equal(t, uint32(16000), binary.LittleEndian.Uint32(fallback[24:28]))
}
