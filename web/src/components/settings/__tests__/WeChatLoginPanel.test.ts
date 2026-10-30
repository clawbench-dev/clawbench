import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import WeChatLoginPanel from '@/components/settings/WeChatLoginPanel.vue'
import zh from '@/i18n/locales/zh'

const apiGet = vi.fn()
const apiPost = vi.fn()
const apiDelete = vi.fn()

vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => apiGet(...args),
  apiPost: (...args: unknown[]) => apiPost(...args),
  apiDelete: (...args: unknown[]) => apiDelete(...args),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const toDataURL = vi.fn().mockResolvedValue('data:image/png;base64,QR')
vi.mock('qrcode', () => ({
  default: { toDataURL: (...args: unknown[]) => toDataURL(...args) },
  toDataURL: (...args: unknown[]) => toDataURL(...args),
}))

vi.mock('lucide-vue-next', async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>()
  const stub = { template: '<span />' }
  return { ...actual, AlertCircle: stub, Check: stub, LogOut: stub, QrCode: stub }
})

vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { template: '<span class="loading-stub" />' },
}))

const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh } })

function mountPanel() {
  return mount(WeChatLoginPanel, { global: { plugins: [i18n] } })
}

function idleStatus(over: Record<string, unknown> = {}) {
  return {
    push_mode: 'wechat', has_token: false, bot_id: '', user_id: '', base_url: '',
    state: 'stopped', last_error: '',
    ...over,
  }
}

function connectedStatus(over: Record<string, unknown> = {}) {
  return idleStatus({ has_token: true, bot_id: 'bot-1', state: 'running', ...over })
}

function loginView(over: Record<string, unknown> = {}) {
  return {
    login_id: 'wl-1',
    state: 'pending',
    qr_url: 'https://liteapp.weixin.qq.com/q/abc',
    qr_seq: 1,
    refresh: 0,
    refresh_max: 3,
    bot_id: '',
    message: '',
    error: '',
    expires_at: 0,
    ...over,
  }
}

/** Clicks the first button whose text contains `label`. */
async function clickButton(wrapper: ReturnType<typeof mountPanel>, label: string) {
  const btn = wrapper.findAll('button').find(b => b.text().includes(label))
  expect(btn, `button "${label}" not found`).toBeDefined()
  await btn!.trigger('click')
  await flushPromises()
}

beforeEach(() => {
  apiGet.mockReset()
  apiPost.mockReset()
  apiDelete.mockReset()
  apiDelete.mockResolvedValue({})
  toDataURL.mockClear()
  toDataURL.mockResolvedValue('data:image/png;base64,QR')
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('WeChatLoginPanel', () => {
  it('shows a loading card until the status request answers', async () => {
    let resolveStatus: (v: unknown) => void = () => {}
    apiGet.mockReturnValue(new Promise(resolve => { resolveStatus = resolve }))

    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.text()).toContain(zh.settings.items.wechatLoading)
    // The critical part: it must NOT claim the user is logged out while the
    // answer is still in flight.
    expect(wrapper.text()).not.toContain(zh.settings.items.wechatNotLoggedIn)

    resolveStatus(idleStatus())
    await flushPromises()
    expect(wrapper.text()).toContain(zh.settings.items.wechatNotLoggedIn)
  })

  it('shows the not-logged-in state and offers a scan button', async () => {
    apiGet.mockResolvedValue(idleStatus())
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.text()).toContain(zh.settings.items.wechatNotLoggedIn)
    expect(wrapper.text()).toContain(zh.settings.items.wechatScanLogin)
    // The risk warning must be visible before the user scans, not after.
    expect(wrapper.text()).toContain(zh.settings.items.wechatRiskWarning)
  })

  it('renders the QR image after starting a login', async () => {
    apiGet.mockResolvedValue(idleStatus())
    apiPost.mockResolvedValue(loginView())

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatScanLogin)

    expect(apiPost).toHaveBeenCalledWith('/api/push/wechat/login', {}, expect.anything())
    expect(toDataURL).toHaveBeenCalledWith(
      'https://liteapp.weixin.qq.com/q/abc',
      expect.objectContaining({ width: 512 }),
    )

    const img = wrapper.find('.wechat-login__qr img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toBe('data:image/png;base64,QR')
    expect(wrapper.text()).toContain(zh.settings.items.wechatScanHint)
  })

  it('shows the connected state when a token is stored', async () => {
    apiGet.mockResolvedValue(connectedStatus())
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.text()).toContain(zh.settings.items.wechatLoggedIn)
    expect(wrapper.text()).toContain('bot-1')
    expect(wrapper.text()).toContain(zh.settings.items.wechatLogout)
  })

  // A stored token with an expired session must NOT read as connected: the
  // channel is dead and only a re-scan revives it.
  it('shows the expired-connection card when the manager reports expired', async () => {
    apiGet.mockResolvedValue(
      connectedStatus({ state: 'expired', last_error: 'wechat session expired (-14)' }),
    )
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.text()).toContain(zh.settings.items.wechatConnectionExpired)
    expect(wrapper.text()).toContain('-14')
    expect(wrapper.text()).not.toContain(zh.settings.items.wechatLoggedIn)
    expect(wrapper.text()).toContain(zh.settings.items.wechatRelogin)
  })

  it('logs out and returns to the idle state', async () => {
    apiGet.mockResolvedValueOnce(connectedStatus()).mockResolvedValue(idleStatus())
    apiPost.mockResolvedValue({})

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatLogout)

    expect(apiPost).toHaveBeenCalledWith('/api/push/wechat/logout', {}, expect.anything())
    expect(wrapper.text()).toContain(zh.settings.items.wechatNotLoggedIn)
  })

  it('surfaces a logout failure and stays connected', async () => {
    apiGet.mockResolvedValue(connectedStatus())
    apiPost.mockRejectedValue(new Error('boom'))

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatLogout)

    expect(wrapper.text()).toContain(zh.settings.items.wechatLogoutFailed)
    expect(wrapper.text()).toContain(zh.settings.items.wechatLoggedIn)
  })

  // The cache is the whole point of renderedSeq: an unchanged qr_seq must not
  // redraw. A fixture that also changes qr_url cannot tell the two apart.
  it('does not redraw when qr_seq is unchanged', async () => {
    apiGet
      .mockResolvedValueOnce(idleStatus())
      .mockResolvedValue(loginView({ qr_seq: 1 }))
    apiPost.mockResolvedValue(loginView({ qr_seq: 1 }))

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatScanLogin)
    expect(toDataURL).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1300)
    await flushPromises()

    expect(toDataURL).toHaveBeenCalledTimes(1)
  })

  it('redraws when qr_seq advances', async () => {
    apiGet
      .mockResolvedValueOnce(idleStatus())
      .mockResolvedValue(loginView({ qr_seq: 2, qr_url: 'https://liteapp.weixin.qq.com/q/second' }))
    apiPost.mockResolvedValue(loginView({ qr_seq: 1 }))

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatScanLogin)
    expect(toDataURL).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1300)
    await flushPromises()

    expect(toDataURL).toHaveBeenCalledTimes(2)
    expect(toDataURL).toHaveBeenLastCalledWith(
      'https://liteapp.weixin.qq.com/q/second',
      expect.anything(),
    )
  })

  // Cancelling must actually stop the loop. The backend keeps the session in its
  // registry for 10 minutes, so a still-running loop gets a 200 {state:
  // "cancelled"} on the next tick and puts the QR card back.
  it('cancel stops the loop and does not resurrect the card', async () => {
    apiGet
      .mockResolvedValueOnce(idleStatus())
      .mockResolvedValue(loginView({ state: 'cancelled' }))
    apiPost.mockResolvedValue(loginView())

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatScanLogin)
    expect(wrapper.text()).toContain(zh.settings.items.wechatCancel)

    await clickButton(wrapper, zh.settings.items.wechatCancel)
    expect(apiDelete).toHaveBeenCalledWith(
      expect.stringContaining('/api/push/wechat/login?id=wl-1'),
      expect.anything(),
    )

    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()

    expect(wrapper.text()).toContain(zh.settings.items.wechatNotLoggedIn)
    expect(wrapper.text()).not.toContain(zh.settings.items.wechatCancel)
    expect(apiGet).toHaveBeenCalledTimes(1) // only the initial status fetch
  })

  it('stops polling and cancels the server session on unmount', async () => {
    apiGet.mockResolvedValue(idleStatus())
    apiPost.mockResolvedValue(loginView())

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatScanLogin)

    const callsBefore = apiGet.mock.calls.length
    wrapper.unmount()

    // The pending sleep must be released at once, not left to expire.
    expect(vi.getTimerCount()).toBe(0)

    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()

    expect(apiGet.mock.calls.length).toBe(callsBefore)
    expect(apiDelete).toHaveBeenCalledWith(
      expect.stringContaining('/api/push/wechat/login?id=wl-1'),
    )
  })

  // Unmount while the POST is still in flight: `login` is null, so the unmount
  // handler has no id. The resolved POST must cancel the server session itself.
  it('cancels the server session when unmounted mid-start', async () => {
    apiGet.mockResolvedValue(idleStatus())
    let resolveStart: (v: unknown) => void = () => {}
    apiPost.mockReturnValue(new Promise(resolve => { resolveStart = resolve }))

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatScanLogin)

    wrapper.unmount()
    resolveStart(loginView())
    await flushPromises()

    expect(apiDelete).toHaveBeenCalledWith(
      expect.stringContaining('/api/push/wechat/login?id=wl-1'),
    )
  })

  it('shows the expired state with a retry action', async () => {
    apiGet
      .mockResolvedValueOnce(idleStatus())
      .mockResolvedValue(loginView({ state: 'expired', message: 'timeout' }))
    apiPost.mockResolvedValue(loginView())

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatScanLogin)

    await vi.advanceTimersByTimeAsync(1300)
    await flushPromises()

    expect(wrapper.text()).toContain(zh.settings.items.wechatStateExpired)
    expect(wrapper.text()).toContain(zh.settings.items.wechatRetry)
  })

  it('shows the scanned prompt', async () => {
    apiGet
      .mockResolvedValueOnce(idleStatus())
      .mockResolvedValue(loginView({ state: 'scanned' }))
    apiPost.mockResolvedValue(loginView())

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatScanLogin)

    await vi.advanceTimersByTimeAsync(1300)
    await flushPromises()

    expect(wrapper.text()).toContain(zh.settings.items.wechatStateScanned)
  })

  it('treats a 404 from poll as "session gone" and stops', async () => {
    apiGet.mockResolvedValueOnce(idleStatus())
    apiPost.mockResolvedValue(loginView())
    const err = Object.assign(new Error('not found'), { status: 404 })
    apiGet.mockRejectedValue(err)

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatScanLogin)

    await vi.advanceTimersByTimeAsync(1300)
    await flushPromises()

    expect(wrapper.text()).toContain(zh.settings.items.wechatNotLoggedIn)
    const callsAfter = apiGet.mock.calls.length
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(apiGet.mock.calls.length).toBe(callsAfter)
  })

  // Transient failures retry, but must not loop forever with a QR on screen that
  // can never complete.
  it('gives up after repeated transient poll failures', async () => {
    apiGet.mockResolvedValueOnce(idleStatus())
    apiPost.mockResolvedValue(loginView())
    apiGet.mockRejectedValue(Object.assign(new Error('boom'), { status: 500 }))

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatScanLogin)

    for (let i = 0; i < 6; i++) {
      await vi.advanceTimersByTimeAsync(1300)
      await flushPromises()
    }

    expect(wrapper.text()).toContain(zh.settings.items.wechatPollGaveUp)
    expect(wrapper.text()).toContain(zh.settings.items.wechatNotLoggedIn)
  })

  it('does not issue a second POST while one is in flight', async () => {
    apiGet.mockResolvedValue(idleStatus())
    let resolveStart: (v: unknown) => void = () => {}
    apiPost.mockReturnValue(new Promise(resolve => { resolveStart = resolve }))

    const wrapper = mountPanel()
    await flushPromises()

    const btn = wrapper.findAll('button').find(b => b.text().includes(zh.settings.items.wechatScanLogin))
    await btn!.trigger('click')
    await btn!.trigger('click')
    await flushPromises()

    expect(apiPost).toHaveBeenCalledTimes(1)
    resolveStart(loginView())
    await flushPromises()
  })

  // A render failure must not consume the qr_seq, or the plate stays on the
  // spinner for that QR with no way to recover.
  it('retries the QR render after a failure', async () => {
    apiGet.mockResolvedValueOnce(idleStatus()).mockResolvedValue(loginView({ qr_seq: 1 }))
    apiPost.mockResolvedValue(loginView({ qr_seq: 1 }))
    toDataURL.mockRejectedValueOnce(new Error('canvas')).mockResolvedValue('data:image/png;base64,OK')

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatScanLogin)

    expect(wrapper.text()).toContain(zh.settings.items.wechatQRRenderFailed)

    await vi.advanceTimersByTimeAsync(1300)
    await flushPromises()

    const img = wrapper.find('.wechat-login__qr img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toBe('data:image/png;base64,OK')
  })

  // After a successful scan the panel must not fall back to "not logged in" when
  // the follow-up status GET happens to fail.
  it('stays connected when the post-login status refresh fails', async () => {
    // Call order: #1 initial status, #2 the poll that reports connected,
    // #3 the follow-up status refresh that fails.
    apiGet
      .mockResolvedValueOnce(idleStatus())
      .mockResolvedValueOnce(loginView({ state: 'connected', bot_id: 'bot-9' }))
      .mockRejectedValue(new Error('network'))
    apiPost.mockResolvedValue(loginView())

    const wrapper = mountPanel()
    await flushPromises()
    await clickButton(wrapper, zh.settings.items.wechatScanLogin)

    await vi.advanceTimersByTimeAsync(1300)
    await flushPromises()

    expect(wrapper.text()).toContain(zh.settings.items.wechatLoggedIn)
    expect(wrapper.text()).toContain('bot-9')
    expect(wrapper.text()).not.toContain(zh.settings.items.wechatNotLoggedIn)
  })
})
