import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

/**
 * Remote-control input on the viewer side.
 *
 * Both failures here are "the host keeps doing something nobody asked for":
 * keystrokes meant for the local chat box typed into the host's focused window,
 * and a mouse button left down because the release happened outside the
 * picture. Neither is visible from the markup, so they are driven through real
 * DOM events.
 */

type Cbs = Record<string, (v?: unknown) => void>
let lastViewer: { opts?: { callbacks?: Cbs } } | null = null
/** Every frame the component pushed onto the control channel. */
let sent: Array<Record<string, unknown>> = []
/** Kept so a failing assertion still tears the window listeners down. */
let wrapper: ReturnType<typeof mount> | null = null

vi.mock('@/share/liveViewer', () => ({
  LiveViewer: class {
    opts: { callbacks?: Cbs }
    viewerId = 'v-test'
    constructor(opts: { callbacks?: Cbs }) {
      this.opts = opts
      lastViewer = this as never
    }
    connect(): void {}
    close(): void {}
    sendControl(msg: Record<string, unknown>): boolean {
      sent.push(msg)
      return true
    }
    startTalking(): Promise<boolean> {
      return Promise.resolve(true)
    }
    stopTalking(): Promise<void> {
      return Promise.resolve()
    }
    /** The page calls this right after sending ctrl-req; a mock without it
     *  throws inside the click handler and hides real failures. */
    requestControl(): void {}
    getStats() {
      return Promise.resolve({ rttMs: null, lossPct: null, bitrateKbps: null, jitterMs: null })
    }
  },
}))

vi.mock('@/share/shareMode', () => ({ getShareToken: () => 'tok' }))
vi.mock('@/utils/clawbenchNative', () => ({ getNative: () => undefined }))

import LiveShareView from '@/share/LiveShareView.vue'
import { passJoinGate } from './joinGateTestHelper'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      share: {
        live: {
          title: '直播',
          chatTitle: '聊天',
          chat: '聊天',
          controlRelease: '停止控制',
          controlCancel: '取消',
          controlling: '正在控制',
          controlPending: '等待授权',
          statusConnecting: '连接中…',
          joinSubtitle: '主持人邀请你加入会议。填一个称谓，发布端与其他观众会看到它。',
          joinNick: '你的称谓',
          joinNickPlaceholder: '例如：小明',
          joinNickHint: '称谓会记住，下次加入不用再填。',
          joinEnter: '加入会议',
          noPictureTitle: '暂未共享画面',
          noPictureDesc: '主持人可以随时打开屏幕共享或摄像头',
          statusLive: '直播中',
          statusNegotiating: '协商中…',
        },
      },
    },
  },
})

beforeEach(() => {
  lastViewer = null
  sent = []
  Object.defineProperty(HTMLMediaElement.prototype, 'play', {
    configurable: true,
    writable: true,
    value: vi.fn(async () => undefined),
  })
})

afterEach(() => {
  // Unmount here rather than at the end of each test: a failed assertion would
  // otherwise skip it and leave this component's window listeners behind, which
  // turns one failure into a cascade of duplicated frames.
  wrapper?.unmount()
  wrapper = null
  // @ts-expect-error -- jsdom has no real implementation to restore to.
  delete HTMLMediaElement.prototype.play
  document.body.innerHTML = ''
})

/** Mount and put the viewer into the "granted control" state. */
async function mountControlling() {
  const w = mount(LiveShareView, { global: { plugins: [i18n] } })
  passJoinGate(w)
  wrapper = w
  await flushPromises()
  lastViewer?.opts?.callbacks?.onControl?.({ t: 'ctrl-grant', viewerId: 'v-test', granted: true })
  await flushPromises()
  // Ignore whatever the setup sent; the assertions below are about user input.
  sent = []
  return w
}

describe('remote control keyboard', () => {
  it('does not forward keystrokes aimed at a local input', async () => {
    const w = await mountControlling()
    const input = document.createElement('input')
    document.body.appendChild(input)

    const ev = new KeyboardEvent('keydown', { key: 'a', bubbles: true, cancelable: true })
    input.dispatchEvent(ev)

    // Forwarding it would type into whatever window the host has focused (a
    // password field, a terminal) and swallow the character here.
    expect(sent).toEqual([])
    expect(ev.defaultPrevented).toBe(false)
  })

  it('does not forward keystrokes aimed inside a contenteditable', async () => {
    const w = await mountControlling()
    const box = document.createElement('div')
    box.setAttribute('contenteditable', 'true')
    const inner = document.createElement('span')
    box.appendChild(inner)
    document.body.appendChild(box)

    const ev = new KeyboardEvent('keydown', { key: 'b', bubbles: true, cancelable: true })
    inner.dispatchEvent(ev)

    expect(sent).toEqual([])
    expect(ev.defaultPrevented).toBe(false)
  })

  it('still forwards a keystroke that is not aimed at an editable field', async () => {
    const w = await mountControlling()

    const ev = new KeyboardEvent('keydown', { key: 'a', bubbles: true, cancelable: true })
    document.body.dispatchEvent(ev)

    expect(sent).toEqual([{ t: 'ctrl-ev', kind: 'text', text: 'a' }])
    expect(ev.defaultPrevented).toBe(true)
  })

  it('sends a modified printable key as a key press, never as literal text', async () => {
    const w = await mountControlling()

    document.body.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'c', ctrlKey: true, bubbles: true, cancelable: true }),
    )
    document.body.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'f', altKey: true, bubbles: true, cancelable: true }),
    )

    // As `text` the host would receive the bare character and lose the
    // modifier, so Ctrl+C would just type "c".
    expect(sent).toEqual([
      { t: 'ctrl-ev', kind: 'key', key: 'c', modifiers: ['ctrl'] },
      { t: 'ctrl-ev', kind: 'key', key: 'f', modifiers: ['alt'] },
    ])
  })
})

describe('remote control pointer', () => {
  it('sends `up` when the press is released outside the picture', async () => {
    const w = await mountControlling()
    const stage = w.find('.live-stage').element

    stage.dispatchEvent(
      new PointerEvent('pointerdown', { pointerId: 1, button: 0, clientX: 30, clientY: 20, bubbles: true }),
    )
    expect(sent).toEqual([expect.objectContaining({ kind: 'down', button: 0 })])

    // Dragged out of the stage and released over the dock: `pointerup` is
    // targeted at whatever is under the cursor, so the stage never sees it and
    // the host's button would stay down.
    document.body.dispatchEvent(
      new PointerEvent('pointerup', { pointerId: 1, button: 0, clientX: 400, clientY: 300, bubbles: true }),
    )

    expect(sent).toEqual([
      expect.objectContaining({ kind: 'down' }),
      expect.objectContaining({ kind: 'up', button: 0 }),
    ])
  })

  it('sends exactly one `up` when the release happens on the picture', async () => {
    const w = await mountControlling()
    const stage = w.find('.live-stage').element

    stage.dispatchEvent(
      new PointerEvent('pointerdown', { pointerId: 2, button: 0, clientX: 10, clientY: 10, bubbles: true }),
    )
    // The stage handler runs first and clears the open press, so the window
    // fallback that also sees this bubbling release must stay quiet.
    stage.dispatchEvent(
      new PointerEvent('pointerup', { pointerId: 2, button: 0, clientX: 12, clientY: 12, bubbles: true }),
    )

    expect(sent.filter((m) => m.kind === 'up')).toHaveLength(1)
  })

  it('releases a held button before giving control back', async () => {
    const w = await mountControlling()
    const stage = w.find('.live-stage').element
    stage.dispatchEvent(
      new PointerEvent('pointerdown', { pointerId: 3, button: 0, clientX: 10, clientY: 10, bubbles: true }),
    )

    await w.find('[data-action="control-release"]').trigger('click')

    // Order matters: after `ctrl-release` the publisher drops our events, so an
    // `up` sent afterwards would never reach the host.
    expect(sent.map((m) => m.kind ?? m.t)).toEqual(['down', 'up', 'ctrl-release'])
  })
})

describe('remote control escape hatch', () => {
  it('releases control on Escape and never forwards the key', async () => {
    await mountControlling()

    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))

    // Exactly one frame: `ctrl-release`. A forwarded Escape would land in
    // whatever the host has focused (closing its dialog, leaving fullscreen).
    expect(sent).toEqual([{ t: 'ctrl-release' }])
  })

  it('releases control on Escape even when the chat box has focus', async () => {
    await mountControlling()
    const input = document.createElement('input')
    document.body.appendChild(input)
    input.focus()

    // Escape is not typing. The editable-target guard exists to keep keystrokes
    // out of the host's windows, but it must not become the thing that traps the
    // viewer in control — that was the reported bug ("pressing Esc does nothing").
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))

    expect(sent).toEqual([{ t: 'ctrl-release' }])
  })

  it('cancels a pending request on Escape', async () => {
    const w = mount(LiveShareView, { global: { plugins: [i18n] } })
    passJoinGate(w)
    wrapper = w
    await flushPromises()
    lastViewer?.opts?.callbacks?.onControlEnabled?.(true)
    await flushPromises()

    await w.find('[data-action="request-control"]').trigger('click')
    expect(sent).toEqual([{ t: 'ctrl-req' }])

    // Before the grant arrives the page is in `requested`: Escape has to give up
    // the request rather than only working once someone approves it.
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))

    expect(sent).toEqual([{ t: 'ctrl-req' }, { t: 'ctrl-release' }])
  })

  it('sends one release when Escape is held down', async () => {
    await mountControlling()

    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))

    // Key auto-repeat must not spam the publisher once control is already back.
    expect(sent).toEqual([{ t: 'ctrl-release' }])
  })
})

describe('remote control never swallows the page itself', () => {
  /** Replace the stage's (jsdom-absent) pointer capture with a spy. */
  function spyOnCapture(w: Awaited<ReturnType<typeof mountControlling>>) {
    const stage = w.find('.live-stage').element as HTMLElement
    const capture = vi.fn()
    Object.defineProperty(stage, 'setPointerCapture', { configurable: true, value: capture })
    return capture
  }

  it('does not forward a press that lands on the release pill', async () => {
    const w = await mountControlling()
    const capture = spyOnCapture(w)
    const btn = w.find('[data-action="control-release"]')

    btn.element.dispatchEvent(
      new PointerEvent('pointerdown', { pointerId: 11, button: 0, clientX: 30, clientY: 20, bubbles: true }),
    )

    // The reported bug: pressing the way out moved the HOST's mouse, and the
    // capture it took meant the button never received its own click either.
    expect(sent).toEqual([])
    expect(capture).not.toHaveBeenCalled()

    // …and the click still gets through to the button.
    await btn.trigger('click')
    expect(sent).toEqual([{ t: 'ctrl-release' }])
  })

  it('does not move the host cursor while the pointer is over the page chrome', async () => {
    const w = await mountControlling()
    const btn = w.find('[data-action="control-release"]')

    btn.element.dispatchEvent(
      new PointerEvent('pointermove', { pointerId: 12, clientX: 30, clientY: 20, bubbles: true }),
    )
    btn.element.dispatchEvent(new WheelEvent('wheel', { deltaY: 120, clientX: 30, clientY: 20, bubbles: true }))

    expect(sent).toEqual([])
  })

  it('keeps driving the host when a drag that began on the picture crosses the chrome', async () => {
    const w = await mountControlling()
    const stage = w.find('.live-stage').element
    const btn = w.find('[data-action="control-release"]').element

    stage.dispatchEvent(
      new PointerEvent('pointerdown', { pointerId: 13, button: 0, clientX: 30, clientY: 20, bubbles: true }),
    )
    btn.dispatchEvent(
      new PointerEvent('pointermove', { pointerId: 13, clientX: 60, clientY: 40, bubbles: true }),
    )

    // Dragging a window edge past the picture has to keep working; only a press
    // that STARTS on the chrome belongs to the page.
    expect(sent.map((m) => m.kind)).toEqual(['down', 'move'])
  })
})
