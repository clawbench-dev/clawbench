import { describe, it, expect } from 'vitest'
import {
  DEFAULT_BARGE_IN_WORDS,
  DEFAULT_TRIGGER_WORDS,
  SENSITIVITY_RMS,
  VOICE_TUNING_DEFAULTS,
  buildMicConstraints,
  extractTrigger,
  formatWordList,
  looksLikeOwnSpeech,
  matchesBargeInWord,
  parseWordList,
  readVoiceTuning,
  rmsFromByteTimeDomain,
  rmsFromInt16,
  rmsThreshold,
} from '../audioTuning'

describe('readVoiceTuning', () => {
  it('returns the shipped defaults for an empty config', () => {
    const t = readVoiceTuning({})
    expect(t).toEqual(VOICE_TUNING_DEFAULTS)
  })

  it('reads the nested stt section, which is what /api/config returns', () => {
    // `configResponse.STT` is `json:"stt"`, so the payload is
    // `{"stt": {"provider": ...}}` — NOT a flat map of dotted keys. Reading
    // flat keys yields undefined for every field, which leaves the defaults in
    // force forever while the settings page happily saves.
    const t = readVoiceTuning({ stt: { trigger_words: '开工', silence_send_seconds: 1.5, voice_gate: false } })
    expect(t.triggerWords).toEqual(['开工'])
    expect(t.silenceSendMs).toBe(1500)
    expect(t.voiceGate).toBe(false)
  })

  it('ignores a flat dotted key, which the server never sends', () => {
    // Guards against someone "fixing" this back to flat lookups: with a flat
    // key the value must NOT be picked up, because the real payload cannot
    // produce one.
    expect(readVoiceTuning({ 'stt.silence_send_seconds': 3 }).silenceSendMs).toBe(3000)
  })

  it('returns the shipped defaults for an undefined config (not loaded yet)', () => {
    // The settings page and the chat panel both read this before /api/config
    // resolves. Falling back to "gate everything off with a 0 threshold" here
    // would mute the first recording of every session.
    expect(readVoiceTuning(undefined)).toEqual(VOICE_TUNING_DEFAULTS)
  })

  it('reads the boolean switches and treats an explicit false as false', () => {
    const t = readVoiceTuning({
      stt: {
        mic_echo_cancel: false,
        mic_noise_suppress: false,
        mic_auto_gain: false,
        mic_band_filter: false,
        voice_gate: false,
      },
    })
    expect(t.echoCancellation).toBe(false)
    expect(t.noiseSuppression).toBe(false)
    expect(t.autoGainControl).toBe(false)
    expect(t.bandFilter).toBe(false)
    expect(t.voiceGate).toBe(false)
  })

  it('accepts a stringified boolean (a config written by an older page)', () => {
    expect(readVoiceTuning({ stt: { voice_gate: 'false' } }).voiceGate).toBe(false)
  })

  it('falls back to standard for an unknown sensitivity', () => {
    expect(readVoiceTuning({ stt: { voice_gate_sensitivity: 'wild' } }).sensitivity).toBe('standard')
  })

  it('converts the stored seconds into the milliseconds the loop uses', () => {
    expect(readVoiceTuning({ stt: { silence_send_seconds: 5 } }).silenceSendMs).toBe(5000)
    expect(readVoiceTuning({ stt: { silence_send_seconds: 0.5 } }).silenceSendMs).toBe(500)
  })

  it('reads the idle exit in seconds, defaulting to a minute', () => {
    expect(readVoiceTuning({ stt: { idle_exit_seconds: 5 } }).idleExitMs).toBe(5000)
    expect(readVoiceTuning({}).idleExitMs).toBe(60000)
  })

  it('keeps 0 for idle_exit_seconds (never leave on its own)', () => {
    expect(readVoiceTuning({ stt: { idle_exit_seconds: 0 } }).idleExitMs).toBe(0)
  })

  it('rejects a negative idle_exit_seconds', () => {
    expect(readVoiceTuning({ stt: { idle_exit_seconds: -1 } }).idleExitMs).toBe(60000)
  })

  it('keeps 0 for silence_send_seconds instead of restoring the default', () => {
    // 0 is the documented "trigger words only" value; treating it as absent
    // would silently re-enable auto-send for a user who turned it off.
    expect(readVoiceTuning({ stt: { silence_send_seconds: 0 } }).silenceSendMs).toBe(0)
  })

  it('rejects a negative silence_send_seconds', () => {
    expect(readVoiceTuning({ stt: { silence_send_seconds: -5 } }).silenceSendMs).toBe(3000)
  })

  it('falls back to "any" for an unknown barge-in mode', () => {
    expect(readVoiceTuning({ stt: { barge_in: 'sometimes' } }).bargeIn).toBe('any')
  })
})

describe('parseWordList / formatWordList', () => {
  it('splits on half-width, full-width and newline separators', () => {
    expect(parseWordList('发送,好了；就这样\n停', ['x'])).toEqual(['发送', '好了', '就这样', '停'])
  })

  it('treats an explicitly cleared list as empty, not as "use the defaults"', () => {
    // Otherwise the setting can never be cleared: the user deletes the words,
    // saves, and the defaults come back.
    expect(parseWordList(' , ，', ['默认'])).toEqual([])
    expect(parseWordList('', ['默认'])).toEqual([])
  })

  it('keeps the defaults only when the field is absent', () => {
    expect(parseWordList(undefined, ['默认'])).toEqual(['默认'])
  })

  it('never yields an empty string entry (it would match every transcript)', () => {
    const words = parseWordList('发送,,好了', ['x'])
    expect(words).toEqual(['发送', '好了'])
    expect(words).not.toContain('')
  })

  it('round-trips through formatWordList', () => {
    expect(parseWordList(formatWordList(DEFAULT_TRIGGER_WORDS), [])).toEqual(DEFAULT_TRIGGER_WORDS)
  })
})

describe('buildMicConstraints', () => {
  it('maps the tuning onto the constraint names the browser expects', () => {
    const c = buildMicConstraints(VOICE_TUNING_DEFAULTS)
    expect(c).toEqual({ echoCancellation: true, noiseSuppression: true, autoGainControl: true })
  })

  it('passes explicit false through rather than dropping the key', () => {
    // Omitting the key means "whatever the browser wants", which is not the
    // same as "off" — the user turned it off on purpose.
    const c = buildMicConstraints({ ...VOICE_TUNING_DEFAULTS, echoCancellation: false })
    expect(c.echoCancellation).toBe(false)
  })

  it('uses an exact device id when one is given', () => {
    expect(buildMicConstraints(VOICE_TUNING_DEFAULTS, 'mic-2').deviceId).toEqual({ exact: 'mic-2' })
  })

  it('omits deviceId entirely for the default device', () => {
    expect(buildMicConstraints(VOICE_TUNING_DEFAULTS)).not.toHaveProperty('deviceId')
  })
})

describe('rmsThreshold', () => {
  it('orders the three sensitivities from permissive to strict', () => {
    expect(SENSITIVITY_RMS.conservative).toBeLessThan(SENSITIVITY_RMS.standard)
    expect(SENSITIVITY_RMS.standard).toBeLessThan(SENSITIVITY_RMS.aggressive)
  })

  it('keeps "standard" low enough that ordinary speech is not dropped', () => {
    // It used to be 0.01 (the value 听记 shipped with). Measured on a real
    // capture, that line cut roughly four out of five 100 ms windows of speech
    // — the gate silently swallowed the sentence, which is far worse than
    // paying for a little silence. It is now just above room tone.
    expect(SENSITIVITY_RMS.standard).toBeLessThanOrEqual(0.005)
    expect(SENSITIVITY_RMS.standard).toBeGreaterThan(0.001)
  })

  it('resolves through the tuning', () => {
    expect(rmsThreshold({ ...VOICE_TUNING_DEFAULTS, sensitivity: 'aggressive' })).toBe(SENSITIVITY_RMS.aggressive)
  })
})

describe('rms helpers', () => {
  it('reports 0 for an all-silence byte buffer', () => {
    expect(rmsFromByteTimeDomain(new Uint8Array(128).fill(128))).toBe(0)
  })

  it('reports a positive value for a non-silent byte buffer', () => {
    const buf = new Uint8Array(128)
    for (let i = 0; i < buf.length; i++) buf[i] = i % 2 === 0 ? 255 : 1
    expect(rmsFromByteTimeDomain(buf)).toBeGreaterThan(0.4)
  })

  it('reports 0 for an all-zero Int16 frame', () => {
    expect(rmsFromInt16(new Int16Array(160))).toBe(0)
  })

  it('reports 0 for an empty Int16 frame instead of NaN', () => {
    expect(rmsFromInt16(new Int16Array(0))).toBe(0)
  })

  it('scales Int16 to the same range as the byte-domain helper', () => {
    // A full-scale square wave: both helpers must land near 1, otherwise the
    // gate threshold would mean different things on the two capture paths.
    const half = new Int16Array(160)
    for (let i = 0; i < half.length; i++) half[i] = i % 2 === 0 ? 32767 : -32768
    expect(rmsFromInt16(half)).toBeCloseTo(1, 2)
    const bytes = new Uint8Array(160)
    for (let i = 0; i < bytes.length; i++) bytes[i] = i % 2 === 0 ? 255 : 0
    expect(rmsFromByteTimeDomain(bytes)).toBeCloseTo(1, 1)
  })
})

describe('looksLikeOwnSpeech', () => {
  const spoken = '好的，我来帮你检查一下这个配置文件里的端口设置是否正确。'

  it('recognises a fragment of what is being read out', () => {
    expect(looksLikeOwnSpeech('帮你检查一下这个配置文件', spoken)).toBe(true)
  })

  it('ignores punctuation and whitespace differences', () => {
    expect(looksLikeOwnSpeech('帮你检查一下，这个配置文件', spoken)).toBe(true)
  })

  it('does not treat unrelated speech as the assistant', () => {
    expect(looksLikeOwnSpeech('等一下，先别改那个文件', spoken)).toBe(false)
  })

  it('refuses to judge very short fragments', () => {
    // "好" appears in almost any Chinese reply; matching it would swallow real
    // interruptions.
    expect(looksLikeOwnSpeech('好', spoken)).toBe(false)
    expect(looksLikeOwnSpeech('好的', spoken)).toBe(false)
  })

  it('returns false when nothing is being spoken', () => {
    expect(looksLikeOwnSpeech('帮我检查一下', '')).toBe(false)
  })

  it('tolerates a few recognition errors in a long fragment', () => {
    // 60% character-block overlap is the threshold; one wrong block out of
    // several must still match.
    expect(looksLikeOwnSpeech('好的我来帮你检查一下这个配置文件里的端口设置是否正觉', spoken)).toBe(true)
  })
})

describe('extractTrigger', () => {
  it('strips a trigger word and returns the remaining text', () => {
    expect(extractTrigger('帮我看下这个报错 发送', ['发送'])).toEqual({ matched: true, text: '帮我看下这个报错' })
  })

  it('does NOT match a trigger word in the middle of a sentence', () => {
    // The default list contains 好了, which is an ordinary verb suffix. Matching
    // anywhere would send "把那个文件改好了再告诉我" the moment it is spoken,
    // and hand the model "把那个文件改再告诉我".
    expect(extractTrigger('把那个文件改好了再告诉我', ['好了']).matched).toBe(false)
    expect(extractTrigger('看下这个 发送 报错', ['发送']).matched).toBe(false)
  })

  it('matches a trigger word followed by punctuation', () => {
    expect(extractTrigger('帮我改一下，发送。', ['发送'])).toEqual({ matched: true, text: '帮我改一下' })
  })

  it('prefers the longest matching trigger word', () => {
    expect(extractTrigger('帮我改一下 发送并关闭', ['发送', '发送并关闭']).text).toBe('帮我改一下')
  })

  it('reports no match when no trigger word is present', () => {
    expect(extractTrigger('帮我看下这个报错', ['发送'])).toEqual({ matched: false, text: '帮我看下这个报错' })
  })

  it('matches any of the configured words', () => {
    expect(extractTrigger('就这样', ['发送', '就这样']).matched).toBe(true)
  })

  it('ignores empty entries in the word list', () => {
    // An empty word would otherwise match everything.
    expect(extractTrigger('你好', ['', '']).matched).toBe(false)
  })

  it('returns empty text for a bare trigger word', () => {
    expect(extractTrigger('发送', ['发送'])).toEqual({ matched: true, text: '' })
  })
})

describe('matchesBargeInWord', () => {
  it('never matches when barge-in is off', () => {
    expect(matchesBargeInWord('等等', { ...VOICE_TUNING_DEFAULTS, bargeIn: 'off' })).toBe(false)
  })

  it('matches any speech in "any" mode', () => {
    expect(matchesBargeInWord('随便说点什么', { ...VOICE_TUNING_DEFAULTS, bargeIn: 'any' })).toBe(true)
  })

  it('requires one of the words in "keyword" mode', () => {
    const t = { ...VOICE_TUNING_DEFAULTS, bargeIn: 'keyword' as const }
    expect(matchesBargeInWord('等等，先停', t)).toBe(true)
    expect(matchesBargeInWord('我想问个别的问题', t)).toBe(false)
  })

  it('uses the documented default interrupt words', () => {
    expect(DEFAULT_BARGE_IN_WORDS.length).toBeGreaterThan(0)
    const t = { ...VOICE_TUNING_DEFAULTS, bargeIn: 'keyword' as const }
    for (const word of DEFAULT_BARGE_IN_WORDS) {
      expect(matchesBargeInWord(word, t)).toBe(true)
    }
  })
})
