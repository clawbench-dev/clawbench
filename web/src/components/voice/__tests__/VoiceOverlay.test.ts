import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import VoiceOverlay from '../VoiceOverlay.vue'
import i18n from '@/i18n'
import { useVoiceInput } from '@/composables/useVoiceInput'
import { _resetPlatformForTest, _setPlatformForTest } from '@/composables/usePlatformDetect'
import { useToast } from '@/composables/useToast'
import { serverConfig } from '@/composables/useSettingsConfig'

/**
 * The global shortcut is the whole point of this feature: F9 must reach
 * whichever field owns the caret, wherever that is. These tests drive the real
 * composable through the real overlay so the chain (keydown → target lookup →
 * caret write) is exercised end to end.
 */
describe('VoiceOverlay', () => {
  let wrapper: ReturnType<typeof mount> | null = null

  beforeEach(() => {
    serverConfig.value = { stt: { provider: 'vllm' } }
    const track = { stop: vi.fn() }
    const stream = { getTracks: () => [track] }
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
    vi.stubGlobal('MediaRecorder', class {
      state = 'recording'
      ondataavailable: unknown = null
      onstop: (() => void) | null = null
      start() { this.state = 'recording' }
      stop() { this.state = 'inactive'; this.onstop?.() }
      static isTypeSupported = vi.fn(() => true)
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ text: '听写' }),
    }))
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    _resetPlatformForTest()
    // The actions show a toast, whose auto-dismiss timer would otherwise
    // outlive the test (and the environment).
    useToast().dismiss()
    document.body.innerHTML = ''
  })

  function mountOverlay() {
    wrapper = mount(VoiceOverlay, { global: { plugins: [i18n] }, attachTo: document.body })
    return wrapper
  }

  function field(value: string, caret = value.length): HTMLTextAreaElement {
    const el = document.createElement('textarea')
    el.value = value
    document.body.appendChild(el)
    el.focus()
    el.setSelectionRange(caret, caret)
    return el
  }

  it('hides the touch mic button when no recognition engine is configured', async () => {
    _setPlatformForTest({ isTouchPrimary: true })
    serverConfig.value = { stt: { provider: '' } }
    mountOverlay()
    field('abc')
    await new Promise((r) => setTimeout(r, 0))
    expect(document.querySelector('.voice-fab')).toBeNull()
  })

  it('shows the touch mic button on a touch device once an engine is configured', async () => {
    _setPlatformForTest({ isTouchPrimary: true })
    serverConfig.value = { stt: { provider: 'vllm' } }
    mountOverlay()
    field('abc')
    await vi.waitFor(() => expect(document.querySelector('.voice-fab')).not.toBeNull())
  })

  it('keeps the touch mic button off the terminal', async () => {
    _setPlatformForTest({ isTouchPrimary: true })
    serverConfig.value = { stt: { provider: 'vllm' } }
    mountOverlay()

    // xterm focuses a hidden textarea; dictating there would type a spoken
    // sentence into a shell prompt.
    const term = document.createElement('div')
    term.className = 'xterm'
    const hidden = document.createElement('textarea')
    term.appendChild(hidden)
    document.body.appendChild(term)
    hidden.focus()
    await new Promise((r) => setTimeout(r, 0))
    expect(document.querySelector('.voice-fab')).toBeNull()
  })

  it('keeps the touch mic button off fields that opt out (terminal command box)', async () => {
    _setPlatformForTest({ isTouchPrimary: true })
    serverConfig.value = { stt: { provider: 'vllm' } }
    mountOverlay()

    const opted = document.createElement('textarea')
    opted.value = 'git status'
    opted.setAttribute('data-voice-ignore', '')
    document.body.appendChild(opted)
    opted.focus()
    await new Promise((r) => setTimeout(r, 0))
    expect(document.querySelector('.voice-fab')).toBeNull()
  })

  it('writes into whichever field holds the caret when F9 is released', async () => {
    mountOverlay()
    const el = field('帮我', 2)

    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'F9' }))
    await vi.waitFor(() => expect(useVoiceInput().state.value).toBe('recording'))
    window.dispatchEvent(new KeyboardEvent('keyup', { code: 'F9' }))

    await vi.waitFor(() => expect(el.value).toBe('帮我听写'))
    expect(el.selectionStart).toBe(4)
  })

  // The gesture that started a take cannot be released into a window that has
  // gone away: the keyup goes to the other app. Losing the window therefore ends
  // the take on its own — including while the microphone is still opening, the
  // window where the old `state === 'recording'` guard did nothing and the take
  // started *after* the release had been dropped.
  it('drops a take whose release lands while the microphone is still opening', async () => {
    let openMic: ((stream: unknown) => void) | null = null
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: () => new Promise((resolve) => { openMic = resolve }) },
    })
    mountOverlay()
    const el = field('帮我')

    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'F9' }))
    // Focus leaves before the microphone arrives; the keyup is gone for good.
    window.dispatchEvent(new Event('blur'))
    const track = { stop: vi.fn() }
    openMic!({ getTracks: () => [track] })

    // The microphone is released instead of recording to nobody.
    await vi.waitFor(() => expect(track.stop).toHaveBeenCalled())
    expect(useVoiceInput().state.value).toBe('idle')
    expect(el.value).toBe('帮我')
  })

  it('ends the take when the window loses focus while recording', async () => {
    mountOverlay()
    const el = field('帮我')

    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'F9' }))
    await vi.waitFor(() => expect(useVoiceInput().state.value).toBe('recording'))
    // No keyup: it would have gone to whatever window took the focus.
    window.dispatchEvent(new Event('blur'))

    await vi.waitFor(() => expect(el.value).toBe('帮我听写'))
    expect(useVoiceInput().state.value).toBe('idle')
  })

  // A WebView does not always blur the window when the app goes to the
  // background (Android), so the microphone must also be released on hide.
  it('ends the take when the page is hidden', async () => {
    mountOverlay()
    const el = field('帮我')

    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'F9' }))
    await vi.waitFor(() => expect(useVoiceInput().state.value).toBe('recording'))

    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true })
    try {
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.waitFor(() => expect(useVoiceInput().state.value).toBe('idle'))
      expect(el.value).toBe('帮我听写')
    } finally {
      delete (document as unknown as Record<string, unknown>).hidden
    }
  })

  it('does not start when the caret is in a password field (falls back to the capsule)', async () => {
    mountOverlay()
    const pwd = document.createElement('input')
    pwd.type = 'password'
    document.body.appendChild(pwd)
    pwd.focus()

    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'F9' }))
    await vi.waitFor(() => expect(useVoiceInput().state.value).toBe('recording'))
    // The capsule path, not a field target.
    expect(useVoiceInput().mode.value).toBe('buffer')
    expect(document.querySelector('.voice-capsule')).not.toBeNull()
    window.dispatchEvent(new KeyboardEvent('keyup', { code: 'F9' }))
    await vi.waitFor(() => expect(useVoiceInput().state.value).toBe('idle'))
  })

  it('keeps the finished transcript in the capsule when nothing could take it', async () => {
    mountOverlay()
    document.body.focus()
    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'F9' }))
    await vi.waitFor(() => expect(useVoiceInput().state.value).toBe('recording'))
    window.dispatchEvent(new KeyboardEvent('keyup', { code: 'F9' }))

    await vi.waitFor(() => expect(useVoiceInput().draftPending.value).toBe(true))
    const capsule = document.querySelector('.voice-capsule')
    expect(capsule?.textContent).toContain('听写')
    // Three ways out, and the text is not thrown away on its own. Copy is an
    // icon button now (it renders CopyButton, not a .voice-cap-btn), so the
    // labelled actions are "to chat" alone.
    expect(capsule?.querySelectorAll('.voice-cap-btn').length).toBe(1)
    expect(capsule?.querySelector('.voice-cap-icon-btn')).not.toBeNull()
    expect(capsule?.querySelector('.voice-cap-x')).not.toBeNull()
  })

  it('drops the pending draft into the chat box at its caret', async () => {
    mountOverlay()
    // The chat composer exists but is not focused, so dictation goes to the
    // capsule; "放到聊天框" must still find it.
    const chat = document.createElement('textarea')
    chat.setAttribute('data-voice-chat-input', '')
    chat.value = '开头结尾'
    document.body.appendChild(chat)
    chat.focus()
    chat.setSelectionRange(2, 2)
    chat.blur()

    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'F9' }))
    await vi.waitFor(() => expect(useVoiceInput().state.value).toBe('recording'))
    window.dispatchEvent(new KeyboardEvent('keyup', { code: 'F9' }))
    await vi.waitFor(() => expect(useVoiceInput().draftPending.value).toBe(true))

    ;(document.querySelector('.voice-cap-btn.primary') as HTMLElement).click()
    await vi.waitFor(() => expect(chat.value).toBe('开头听写结尾'))
    expect(useVoiceInput().draftPending.value).toBe(false)
  })

  it('drops the pending draft when discarded', async () => {
    mountOverlay()
    document.body.focus()
    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'F9' }))
    await vi.waitFor(() => expect(useVoiceInput().state.value).toBe('recording'))
    window.dispatchEvent(new KeyboardEvent('keyup', { code: 'F9' }))
    await vi.waitFor(() => expect(useVoiceInput().draftPending.value).toBe(true))

    ;(document.querySelector('.voice-cap-x') as HTMLElement).click()
    await vi.waitFor(() => expect(useVoiceInput().draftPending.value).toBe(false))
    expect(useVoiceInput().inputText.value).toBe('')
  })

  it('Esc takes the dictation back out of the field', async () => {
    mountOverlay()
    const el = field('abc', 3)

    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'F9' }))
    await vi.waitFor(() => expect(useVoiceInput().state.value).toBe('recording'))
    // Cancel while the engine still owns the recording: nothing is inserted
    // and the field is exactly as it was.
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await vi.waitFor(() => expect(useVoiceInput().state.value).toBe('idle'))
    expect(el.value).toBe('abc')
    expect(el.selectionStart).toBe(3)
  })

  /* ── touch mic: press to talk ─────────────────────────────────────── */

  function pointer(type: string, el: Element, clientX: number, clientY = 300) {
    el.dispatchEvent(new PointerEvent(type, {
      clientX,
      clientY,
      bubbles: true,
      cancelable: true,
      pointerId: 1,
      pointerType: 'touch',
    }))
  }

  async function showFab() {
    _setPlatformForTest({ isTouchPrimary: true })
    mountOverlay()
    field('abc')
    await vi.waitFor(() => expect(document.querySelector('.voice-fab')).not.toBeNull())
    return document.querySelector('.voice-fab') as HTMLElement
  }

  it('starts dictating when the mic button is held down', async () => {
    const fab = await showFab()
    const el = document.querySelector('textarea') as HTMLTextAreaElement

    pointer('pointerdown', fab, 900)
    await vi.waitFor(() => expect(useVoiceInput().state.value).toBe('recording'))
    // The button is unmounted while recording; release reaches the window
    // listener registered on press.
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, pointerType: 'touch' }))

    await vi.waitFor(() => expect(el.value).toBe('abc 听写'))
    // No "已写入" badge afterwards: the words in the field are the whole
    // confirmation, and the pill's box must not stay on screen.
    await vi.waitFor(() => expect(document.querySelector('.voice-pill')).toBeNull())
  })

  it('does not start a take when the finger is lifted while the microphone is still opening', async () => {
    // The reported bug: the finger came up while getUserMedia was still
    // pending. The take started afterwards and, with the button already gone
    // from the screen, recorded forever.
    const fab = await showFab()
    let release!: (s: MediaStream) => void
    const pending = new Promise<MediaStream>((resolve) => { release = resolve })
    navigator.mediaDevices.getUserMedia = vi.fn(() => pending)

    pointer('pointerdown', fab, 900)
    // The microphone has not answered yet.
    await new Promise((r) => setTimeout(r, 200))
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, pointerType: 'touch' }))
    release({ getTracks: () => [{ stop: vi.fn() }] } as unknown as MediaStream)

    await new Promise((r) => setTimeout(r, 50))
    expect(useVoiceInput().state.value).toBe('idle')
  })

  it('does not start a take when the gesture is cancelled while the microphone is opening', async () => {
    const fab = await showFab()
    let release!: (s: MediaStream) => void
    const pending = new Promise<MediaStream>((resolve) => { release = resolve })
    navigator.mediaDevices.getUserMedia = vi.fn(() => pending)

    pointer('pointerdown', fab, 900)
    await new Promise((r) => setTimeout(r, 200))
    pointer('pointercancel', fab, 900)
    release({ getTracks: () => [{ stop: vi.fn() }] } as unknown as MediaStream)

    await new Promise((r) => setTimeout(r, 50))
    expect(useVoiceInput().state.value).toBe('idle')
  })
})
