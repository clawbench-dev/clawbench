/**
 * Voice capture tuning shared by chat dictation, free-dialog mode and meeting
 * 听记.
 *
 * Leaf module on purpose: no Vue, no settings composable, no i18n. Callers pass
 * the server config record in and get plain values back, so the same parsing
 * runs in tests, in `liveNotes.ts` (which must stay importable without a Vue
 * app) and in `useVoiceInput`.
 *
 * Three distinct things live here, and they are deliberately separate:
 *
 *   1. Microphone constraints (echo cancellation / noise suppression / auto
 *      gain). These are handled by the browser and the OS, apply to the device
 *      itself, and therefore also change what a meeting audience hears.
 *   2. The band filter. This is part of the STT preprocessing pipeline only —
 *      routing it into the meeting mix would make every voice sound like a
 *      phone call for the people listening.
 *   3. The voice gate (silence threshold + how long a pause counts as the end
 *      of an utterance). Pure decision logic; it never touches audio.
 */

/** How loud the microphone has to be for a window to count as speech. */
export type VoiceSensitivity = 'conservative' | 'standard' | 'aggressive'

/** What happens to AI speech-to-text playback when the user starts talking. */
export type BargeInMode = 'off' | 'any' | 'keyword'

export interface VoiceTuning {
  echoCancellation: boolean
  noiseSuppression: boolean
  autoGainControl: boolean
  bandFilter: boolean
  /** Drop audio that carries no speech instead of streaming it to the engine. */
  voiceGate: boolean
  sensitivity: VoiceSensitivity
  /** Saying any of these sends the message immediately. */
  triggerWords: string[]
  /** A pause this long after the last recognised word also sends it. 0 disables. */
  silenceSendMs: number
  /**
   * Leave the mode after this long with no recognised words at all. 0 disables.
   *
   * A separate number from `silenceSendMs` on purpose: one ends an utterance
   * (send and keep listening), the other ends the session (the user walked
   * away and the microphone is still open).
   */
  idleExitMs: number
  bargeIn: BargeInMode
  /** Only used when `bargeIn === 'keyword'`. */
  bargeInWords: string[]
}

/**
 * RMS thresholds per sensitivity, measured on 100 ms windows of 16-bit PCM
 * normalised to [-1, 1].
 *
 * `standard` is exactly the value 听记 has used since it shipped, so making the
 * setting shared does not silently change existing meetings. Room tone and
 * codec noise sit below ~0.005; normal speech in a quiet room sits well above
 * 0.02; a soft-spoken user at arm's length lands around 0.012.
 */
export const SENSITIVITY_RMS: Record<VoiceSensitivity, number> = {
  // Lowered from 0.006 / 0.01 / 0.02 after measuring a real capture: at 0.01
  // roughly four out of five 100 ms windows of ordinary speech-with-pauses fell
  // below the line, and with noise suppression engaged the quiet half of a
  // sentence is even lower. A gate that drops the sentence is worse than no
  // gate at all, so these now sit just above true room tone (≈0.0005–0.0015)
  // and leave the engine's own VAD to do the fine judgement.
  conservative: 0.0015,
  standard: 0.003,
  aggressive: 0.008,
}

export const DEFAULT_TRIGGER_WORDS = ['发送', '好了', '就这样']
export const DEFAULT_BARGE_IN_WORDS = ['等等', '停一下']

/** Defaults mirror `internal/model/defaults.go`; see `defaultsForTuning`. */
export const VOICE_TUNING_DEFAULTS: VoiceTuning = {
  echoCancellation: true,
  noiseSuppression: true,
  autoGainControl: true,
  bandFilter: true,
  voiceGate: true,
  sensitivity: 'standard',
  triggerWords: [...DEFAULT_TRIGGER_WORDS],
  silenceSendMs: 3000,
  idleExitMs: 60000,
  bargeIn: 'any',
  bargeInWords: [...DEFAULT_BARGE_IN_WORDS],
}

/** Band filter corners. See `buildBandFilter` for why the low-pass is soft. */
export const BAND_HIGHPASS_HZ = 100
export const BAND_LOWPASS_HZ = 8000

function asBool(value: unknown, fallback: boolean): boolean {
  if (typeof value === 'boolean') return value
  if (value === 'true') return true
  if (value === 'false') return false
  return fallback
}

function asSensitivity(value: unknown): VoiceSensitivity {
  return value === 'conservative' || value === 'aggressive' ? value : 'standard'
}

function asBargeIn(value: unknown): BargeInMode {
  return value === 'off' || value === 'keyword' ? value : 'any'
}

/**
 * Splits a user-entered word list.
 *
 * Commas, semicolons and newlines all separate words: the settings field is a
 * plain text input, so users type whichever they think of first, and Chinese
 * input methods produce the full-width variants. Blank entries are dropped
 * rather than kept as empty strings, which would otherwise match every
 * transcript (`includes('')` is always true).
 */
export function parseWordList(value: unknown, fallback: string[]): string[] {
  // Absent (an older server that does not know the field) keeps the defaults.
  if (typeof value !== 'string') return [...fallback]
  // An explicitly empty string is a real choice — "no trigger words, use the
  // pause instead" — and must not be silently replaced by the defaults, or the
  // setting can never be cleared.
  return value
    .split(/[,，;；\n\r]+/)
    .map((w) => w.trim())
    .filter((w) => w.length > 0)
}

/** Serialises a word list back into the settings text field. */
export function formatWordList(words: string[]): string {
  return words.join('，')
}

/**
 * The `stt` section of `GET /api/config`.
 *
 * The response is NESTED (`{"stt": {"provider": ...}}`), not a flat map of
 * dotted keys — `useSettingsConfig.getServerValue` walks it by dot-path for
 * exactly that reason. Reading `config['stt.provider']` yields undefined for
 * every field, which silently leaves the defaults in force forever: the
 * settings page saves fine, the server stores fine, and nothing the user
 * changes has any effect.
 */
function sttSection(config: Record<string, unknown> | undefined | null): Record<string, unknown> {
  const section = (config ?? {}).stt
  return section && typeof section === 'object' ? (section as Record<string, unknown>) : {}
}

/** One field of the `stt` section, for callers outside this module. */
export function readSttField(config: Record<string, unknown> | undefined | null, key: string): unknown {
  return sttSection(config)[key]
}

/**
 * Reads the tuning out of the server config.
 *
 * Every field falls back to the shipped default, so a server that predates
 * these settings (or a page that has not loaded config yet) behaves exactly
 * like the old code instead of suddenly gating audio with a 0 threshold.
 */
export function readVoiceTuning(config: Record<string, unknown> | undefined | null): VoiceTuning {
  const cfg = sttSection(config)
  const d = VOICE_TUNING_DEFAULTS
  // Stored in seconds (what the settings page shows); the loop below works in
  // milliseconds because everything else here — the gate, the hangover, the
  // keepalive — is measured that way.
  const silenceRaw = Number(cfg.silence_send_seconds)
  const idleExitRaw = Number(cfg.idle_exit_seconds)
  return {
    echoCancellation: asBool(cfg.mic_echo_cancel, d.echoCancellation),
    noiseSuppression: asBool(cfg.mic_noise_suppress, d.noiseSuppression),
    autoGainControl: asBool(cfg.mic_auto_gain, d.autoGainControl),
    bandFilter: asBool(cfg.mic_band_filter, d.bandFilter),
    voiceGate: asBool(cfg.voice_gate, d.voiceGate),
    sensitivity: asSensitivity(cfg.voice_gate_sensitivity),
    triggerWords: parseWordList(cfg.trigger_words, d.triggerWords),
    silenceSendMs: Number.isFinite(silenceRaw) && silenceRaw >= 0 ? Math.round(silenceRaw * 1000) : d.silenceSendMs,
    idleExitMs: Number.isFinite(idleExitRaw) && idleExitRaw >= 0 ? Math.round(idleExitRaw * 1000) : d.idleExitMs,
    bargeIn: asBargeIn(cfg.barge_in),
    bargeInWords: parseWordList(cfg.barge_in_words, d.bargeInWords),
  }
}

/**
 * Microphone constraints derived from the tuning.
 *
 * A device id, when given, always wins; otherwise the OS default is used
 * (`audio: true` semantics — `{exact: ''}` fails on every machine).
 */
export function buildMicConstraints(tuning: VoiceTuning, deviceId = ''): MediaTrackConstraints {
  return {
    echoCancellation: tuning.echoCancellation,
    noiseSuppression: tuning.noiseSuppression,
    autoGainControl: tuning.autoGainControl,
    ...(deviceId ? { deviceId: { exact: deviceId } } : {}),
  }
}

/** The RMS threshold the tuning selects. */
export function rmsThreshold(tuning: VoiceTuning): number {
  return SENSITIVITY_RMS[tuning.sensitivity]
}

/**
 * RMS of one window of 8-bit time-domain samples, as returned by
 * `AnalyserNode.getByteTimeDomainData` (128 = silence).
 */
export function rmsFromByteTimeDomain(buf: Uint8Array): number {
  let sum = 0
  for (let i = 0; i < buf.length; i++) {
    const v = (buf[i] - 128) / 128
    sum += v * v
  }
  return Math.sqrt(sum / buf.length)
}

/**
 * RMS of one Int16 PCM frame, normalised to [-1, 1].
 *
 * This is the form the chat capture path has: the worklet hands over raw
 * Int16 samples, not an analyser buffer, so the byte-domain helper above does
 * not apply.
 */
export function rmsFromInt16(frame: Int16Array): number {
  if (frame.length === 0) return 0
  let sum = 0
  for (let i = 0; i < frame.length; i++) {
    const v = frame[i] / 32768
    sum += v * v
  }
  return Math.sqrt(sum / frame.length)
}

/**
 * Chains a high-pass and a low-pass in front of the recogniser.
 *
 * The high-pass is the one that earns its keep: 100 Hz and below carries
 * desk thumps, air-conditioning rumble and hand-held handling noise, none of
 * which contain speech. The low-pass only matters when the graph runs at a
 * high sample rate — the streaming capture path runs at 16 kHz, where the
 * resampler has already removed everything above 8 kHz. It is kept because the
 * same graph is correct at any rate, and because leaving the low end open is
 * what makes a hiss-heavy room recognisable as noise to the engine.
 *
 * Deliberately NOT applied to the meeting publish path: band-limiting what an
 * audience hears makes every voice sound like a phone call.
 */
export function buildBandFilter(ctx: BaseAudioContext, source: AudioNode): AudioNode {
  const highpass = ctx.createBiquadFilter()
  highpass.type = 'highpass'
  highpass.frequency.value = BAND_HIGHPASS_HZ
  highpass.Q.value = 0.7
  const lowpass = ctx.createBiquadFilter()
  lowpass.type = 'lowpass'
  // Clamped below Nyquist: setting it AT Nyquist is a no-op or an error
  // depending on the engine, and the 16 kHz capture path would hit exactly that.
  lowpass.frequency.value = Math.min(BAND_LOWPASS_HZ, ctx.sampleRate / 2 - 200)
  lowpass.Q.value = 0.7
  source.connect(highpass)
  highpass.connect(lowpass)
  return lowpass
}

/**
 * Whether a transcript is just the AI's own speech coming back through the
 * speakers.
 *
 * Echo cancellation does most of the work, but a Bluetooth headset with
 * drifting latency or a very loud speaker leaks fragments. Comparing against
 * what is being spoken is the cheap, reliable second line: a fragment that the
 * assistant is currently saying is almost certainly the assistant.
 *
 * Matching is done on characters with all whitespace and punctuation removed,
 * because recognition output punctuates and segments differently from the text
 * that was sent to TTS.
 *
 * @param heard      transcript produced while the AI was speaking
 * @param spokenText what TTS is reading out right now
 */
export function looksLikeOwnSpeech(heard: string, spokenText: string): boolean {
  const norm = (s: string) => s.replace(/[\s\p{P}\p{S}]/gu, '')
  const h = norm(heard)
  const s = norm(spokenText)
  if (h.length < 4 || s.length === 0) return false
  // Exact containment is the strongest signal and covers the common case where
  // recognition got the fragment right.
  if (s.includes(h)) return true
  // Otherwise compare overlapping bigrams. Character-exact matching is too
  // brittle for the job: one substituted character (or a leading filler like
  // 嗯) defeats a substring test entirely, and the leak this guards against is
  // exactly the kind that comes back slightly wrong. Bigrams tolerate that
  // while still rejecting unrelated speech, because unrelated Chinese shares
  // almost no two-character sequences with what is being read.
  if (h.length < 2) return false
  let matched = 0
  let total = 0
  for (let i = 0; i + 2 <= h.length; i++) {
    total++
    if (s.includes(h.slice(i, i + 2))) matched++
  }
  // Short fragments need a higher ratio: "好的" shares a bigram with almost any
  // polite Chinese sentence, and treating that as the assistant's own voice
  // would swallow a real interruption.
  const threshold = h.length <= 6 ? 0.75 : 0.6
  return total > 0 && matched / total >= threshold
}

/** Trailing punctuation a transcript may carry after the spoken trigger. */
const TRAILING_PUNCT_RE = /[\s,.!?;:，。！？；：、）》」』…—~～]+$/

/**
 * Matches a trigger word at the END of a transcript, and removes it.
 *
 * Tail-only on purpose. The trigger means "I am done, send it", which is a
 * statement about the end of an utterance — matching it anywhere would fire on
 * ordinary speech that merely contains the word. The default list is the
 * clearest case: 好了 is an extremely common verb suffix ("把文件改好了再告诉我"
 * would send immediately AND ship "把文件改再告诉我" to the model). Same for
 * 就这样, which is a normal way to end a sentence.
 *
 * Punctuation and whitespace after the word are tolerated, because recognition
 * output is punctuated: "帮我改一下，发送。" still matches.
 *
 * @returns `{ matched, text }` — `matched` is false when no trigger was found.
 */
export function extractTrigger(text: string, words: string[]): { matched: boolean; text: string } {
  const trimmed = text.replace(TRAILING_PUNCT_RE, '')
  // Longest first: with both 发送 and 发送并关闭 configured, the longer word has
  // to win or the remainder keeps a dangling fragment.
  const ordered = [...words].filter((w) => w.length > 0).sort((a, b) => b.length - a.length)
  for (const word of ordered) {
    if (!trimmed.endsWith(word)) continue
    const rest = trimmed.slice(0, trimmed.length - word.length).replace(TRAILING_PUNCT_RE, '')
    return { matched: true, text: rest.trim() }
  }
  return { matched: false, text: text.trim() }
}

/** Whether a transcript asks the assistant to stop talking. */
export function matchesBargeInWord(text: string, tuning: VoiceTuning): boolean {
  if (tuning.bargeIn === 'off') return false
  if (tuning.bargeIn === 'any') return true
  return tuning.bargeInWords.some((w) => w.length > 0 && text.includes(w))
}
