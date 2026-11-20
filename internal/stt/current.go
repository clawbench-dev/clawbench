package stt

import (
	"encoding/binary"
	"sync"
)

// 全局 provider 实例。原先定义在 internal/handler/stt.go，会议纪要（live）
// 要用它，而功能搬进 internal/plugin/<id> 之后不能再 import handler。
//
// 放在本包而不是新建一个包：默认值与接口都在这里，没有额外的依赖方向。

var (
	current   STTProvider = NewVLLMProvider("http://localhost:8000/v1", "openai/whisper-large-v3", "", "zh")
	currentMu sync.RWMutex
)

// SetCurrent replaces the global provider. Goroutine-safe.
func SetCurrent(p STTProvider) {
	currentMu.Lock()
	current = p
	currentMu.Unlock()
}

// Current returns the global provider. Goroutine-safe for concurrent reads.
func Current() STTProvider {
	currentMu.RLock()
	p := current
	currentMu.RUnlock()
	return p
}

// PeakRMS is the loudest 16-bit sample magnitude in one frame, as a 0..1
// fraction of full scale. Peak rather than average: a frame of speech with
// pauses would otherwise average down to nothing.
func PeakRMS(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	var peak float64
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8) //nolint:gosec // G115: the two bytes ARE a signed 16-bit sample; the wrap is the conversion
		f := float64(v) / 32768
		if f < 0 {
			f = -f
		}
		if f > peak {
			peak = f
		}
	}
	return peak
}

// WAVFromPCM wraps 16-bit mono PCM samples in a WAV container.
//
// It used to live in the meeting-notes stream file. Both the STT endpoint and
// the meeting relay need it, and the meeting plugin cannot import handler.
func WAVFromPCM(pcm []byte, rate int) []byte {
	const headerBytes = 44
	const defaultRate = 16000
	if rate <= 0 || rate > 192_000 {
		rate = defaultRate
	}
	out := make([]byte, headerBytes, headerBytes+len(pcm))
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+len(pcm))) //nolint:gosec // bounded by the frame cap
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(out[22:24], 1) // mono
	binary.LittleEndian.PutUint32(out[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(rate*2)) // byte rate
	binary.LittleEndian.PutUint16(out[32:34], 2)              // block align
	binary.LittleEndian.PutUint16(out[34:36], 16)             // bits
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(len(pcm))) //nolint:gosec // bounded by the frame cap
	return append(out, pcm...)
}
