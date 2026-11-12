import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { probeSystemSpeech, systemSpeechErrorKey } from '@/utils/systemSpeech'

const { holder } = vi.hoisted(() => ({
  holder: { native: undefined as unknown },
}))

vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => holder.native,
}))

function bridge(status: string) {
  return {
    ttsStatus: vi.fn(() => status),
    ttsSpeak: vi.fn(() => 'ready'),
    ttsStop: vi.fn(),
  }
}

describe('probeSystemSpeech', () => {
  beforeEach(() => {
    holder.native = undefined
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('reports no_bridge when the host has no TTS methods', async () => {
    expect(await probeSystemSpeech()).toEqual({ ok: false, reason: 'no_bridge' })
  })

  it('accepts a ready engine', async () => {
    holder.native = bridge('ready')
    expect(await probeSystemSpeech()).toEqual({ ok: true })
  })

  it('reports no_engine when the engine is unavailable', async () => {
    holder.native = bridge('unavailable')
    expect(await probeSystemSpeech()).toEqual({ ok: false, reason: 'no_engine' })
  })

  it('reports no_chinese separately from a missing engine', async () => {
    // The switch is for Chinese replies: an English-only engine must not be
    // accepted just because some voice exists.
    holder.native = bridge('no_chinese')
    expect(await probeSystemSpeech()).toEqual({ ok: false, reason: 'no_chinese' })
  })

  it('waits for the init event when initialization is still running', async () => {
    holder.native = bridge('initializing')
    const pending = probeSystemSpeech()

    window.dispatchEvent(new CustomEvent('clawbench-tts-init', { detail: { state: 'ready' } }))

    expect(await pending).toEqual({ ok: true })
  })

  it('turns a failed init event into no_engine', async () => {
    holder.native = bridge('initializing')
    const pending = probeSystemSpeech()

    window.dispatchEvent(new CustomEvent('clawbench-tts-init', { detail: { state: 'unavailable' } }))

    expect(await pending).toEqual({ ok: false, reason: 'no_engine' })
  })

  it('gives up instead of hanging when the event never arrives', async () => {
    vi.useFakeTimers()
    holder.native = bridge('initializing')

    const pending = probeSystemSpeech(1000)
    await vi.advanceTimersByTimeAsync(1000)

    expect(await pending).toEqual({ ok: false, reason: 'timeout' })
  })

  it('ignores a late event after the timeout already settled', async () => {
    vi.useFakeTimers()
    holder.native = bridge('initializing')

    const pending = probeSystemSpeech(1000)
    await vi.advanceTimersByTimeAsync(1000)
    window.dispatchEvent(new CustomEvent('clawbench-tts-init', { detail: { state: 'ready' } }))

    expect(await pending).toEqual({ ok: false, reason: 'timeout' })
  })
})

describe('systemSpeechErrorKey', () => {
  it('maps every failure to its own message', () => {
    const keys = new Set([
      systemSpeechErrorKey('no_bridge'),
      systemSpeechErrorKey('no_engine'),
      systemSpeechErrorKey('no_chinese'),
      systemSpeechErrorKey('timeout'),
    ])
    expect(keys.size).toBe(4)
  })
})
