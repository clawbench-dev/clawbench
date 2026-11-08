package handler

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"time"

	"clawbench/internal/speech"

	"github.com/coder/websocket"
	"github.com/hajimehoshi/go-mp3"
)

// Speech frame constants shared by the voice-turn WebSocket. The frame
// vocabulary (type/text) reuses the stt.go constants.
const (
	speechTypePhase  = "phase"
	speechTypeAudio  = "audio"
	speechPhaseThink = "thinking"
)

// speakSummary streams the summary as PCM, reusing cached audio when present
// and falling back to a file-based synthesis for providers without streaming.
func speakSummary(ctx context.Context, conn *websocket.Conn, provider speech.SpeechProvider,
	spoken, mp3Path, lang string,
) {
	if info, statErr := os.Stat(mp3Path); statErr == nil && info.Size() > 0 {
		streamPCMFile(ctx, conn, mp3Path)
		return
	}

	if streaming, ok := provider.(speech.StreamingSpeechProvider); ok {
		streamPCMSynth(ctx, conn, streaming, spoken, mp3Path, lang)
		return
	}

	// Provider without streaming support: synthesize to the file, then decode.
	sctx, scancel := context.WithTimeout(ctx, ttsSynthesizeTimeout)
	err := provider.Synthesize(sctx, spoken, mp3Path, lang)
	scancel()
	if err != nil {
		slog.Error("speech stream: synthesize failed", slog.String("error", err.Error()))
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "synthesize failed"})
		return
	}
	streamPCMFile(ctx, conn, mp3Path)
}

func writeSpeechFrame(ctx context.Context, conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, wcancel := context.WithTimeout(ctx, ttsWSWriteTimeout)
	defer wcancel()
	return conn.Write(wctx, websocket.MessageText, data)
}

// streamPCMFile decodes an already synthesized MP3 file into PCM frames.
func streamPCMFile(ctx context.Context, conn *websocket.Conn, path string) {
	f, err := os.Open(path)
	if err != nil {
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "open audio failed"})
		return
	}
	defer func() { _ = f.Close() }()

	dec, err := mp3.NewDecoder(f)
	if err != nil {
		slog.Error("speech stream: decode failed", slog.String("error", err.Error()))
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "decode failed"})
		return
	}
	pumpPCM(ctx, conn, dec)
}

// streamPCMSynth synthesizes while decoding: the MP3 chunks go through a pipe
// into the decoder, so the first PCM frame leaves before synthesis finishes.
func streamPCMSynth(ctx context.Context, conn *websocket.Conn,
	provider speech.StreamingSpeechProvider, text, outPath, lang string,
) {
	pr, pw := io.Pipe()
	chunks := make(chan []byte, 16)
	synthDone := make(chan error, 1)

	go func() {
		defer close(chunks)
		synthDone <- provider.SynthesizeStream(ctx, text, outPath, lang, chunks)
	}()
	go func() {
		for b := range chunks {
			if _, werr := pw.Write(b); werr != nil {
				break // client gone or decoder stopped
			}
		}
		_ = pw.Close()
	}()

	// NewDecoder blocks until enough bytes for the MP3 header have arrived.
	dec, err := mp3.NewDecoder(pr)
	if err != nil {
		slog.Error("speech stream: stream decode failed", slog.String("error", err.Error()))
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "decode failed"})
		_ = pr.Close()
		return
	}
	pumpPCM(ctx, conn, dec)
	_ = pr.Close()
	<-synthDone
}

// downmixFrames averages each stereo frame into mono, in place.
//
// The samples are offset-binary: an unsigned value u represents the signed
// sample u-32768. Averaging two such values as unsigned and keeping the result
// unsigned therefore yields exactly the offset-binary average of the signed
// samples, so no widening conversions are needed at all.
func downmixFrames(stereo, mono []byte, frames int) {
	for i := range frames {
		l := int32(binary.LittleEndian.Uint16(stereo[i*4:]))
		r := int32(binary.LittleEndian.Uint16(stereo[i*4+2:]))
		//nolint:gosec // G115: the mean of two uint16 samples is itself in [0,65535]
		binary.LittleEndian.PutUint16(mono[i*2:], uint16((l+r)/2))
	}
}

// pumpPCM converts go-mp3's 16-bit stereo output to mono and writes it as
// binary frames. The format frame goes first so the client can configure its
// output before the first sample arrives.
func pumpPCM(ctx context.Context, conn *websocket.Conn, dec *mp3.Decoder) {
	rate := dec.SampleRate()
	if rate <= 0 {
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "invalid sample rate"})
		return
	}
	if err := writeSpeechFrame(ctx, conn, map[string]any{
		ttsWSType: speechTypeAudio, "sampleRate": rate, "channels": 1, "bits": 16,
	}); err != nil {
		return
	}

	buf := make([]byte, 8192)  // 立体声 16bit
	mono := make([]byte, 4096) // 一半大小
	sent := 0
	started := time.Now()
	firstFrame := true
	for {
		n, err := dec.Read(buf)
		if n > 0 {
			frames := n / 4 // 2 声道 × 2 字节
			downmixFrames(buf, mono, frames)
			if firstFrame {
				firstFrame = false
				slog.Info("speech stream: first audio frame",
					slog.Duration("after", time.Since(started)))
			}
			wctx, wcancel := context.WithTimeout(ctx, ttsWSWriteTimeout)
			werr := conn.Write(wctx, websocket.MessageBinary, mono[:frames*2])
			wcancel()
			if werr != nil {
				slog.Info("speech stream: client gone", slog.Int("sent_bytes", sent))
				return
			}
			sent += frames * 2
			// 按实时速度发:设备只有几十 KB 播放缓冲,发太快会把它压爆,
			// 它停止读 -> 服务器写超时 -> 连接被重置 -> 结尾整段丢失。
			if audioSec := float64(sent) / float64(rate*2); audioSec > time.Since(started).Seconds() {
				time.Sleep(time.Duration((audioSec - time.Since(started).Seconds()) * float64(time.Second)))
			}
		}
		if err != nil {
			break
		}
	}
	slog.Info(
		"speech stream: audio done",
		slog.Int("sent_bytes", sent),
		slog.Float64("seconds", float64(sent)/float64(rate*2)),
	)
	_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: ttsWSDone})
	// 给 TCP 一点时间把最后一帧真正发出去,再关连接。
	time.Sleep(300 * time.Millisecond)
}
