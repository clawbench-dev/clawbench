import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import DrawioViewer from '@/components/file/DrawioViewer.vue'
import { useFileEditor, _resetForTesting } from '@/composables/useFileEditor.ts'

// The component talks to the server through apiGet/apiPost; every test decides
// what those answer, so nothing touches the network.
const { apiGetMock, apiPostMock } = vi.hoisted(() => ({
  apiGetMock: vi.fn(),
  apiPostMock: vi.fn(),
}))
vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => apiGetMock(...args),
  apiPost: (...args: unknown[]) => apiPostMock(...args),
}))

const mockSaveFile = vi.fn().mockResolvedValue(true)
vi.mock('@/composables/useCodeEditorSave', () => ({
  useCodeEditorSave: () => ({ saving: { value: false }, saveFile: mockSaveFile }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  const { ref } = await import('vue')
  return {
    ...actual,
    // A real ref, not a plain object: the component watches `locale` and Vue
    // rejects a non-reactive watch source.
    useI18n: () => ({ t: (k: string) => k, locale: ref('zh-CN') }),
  }
})

let dialogChoice: boolean | null = true
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    confirm: vi.fn().mockImplementation(async () => dialogChoice),
  }),
}))

let postMessage: ReturnType<typeof vi.fn>
let contentWindowMock: Window | null = null

function mockContentWindow() {
  postMessage = vi.fn()
  contentWindowMock = { postMessage } as unknown as Window
  vi.spyOn(HTMLIFrameElement.prototype, 'contentWindow', 'get').mockReturnValue(contentWindowMock)
  return postMessage
}

function sendFromIframe(payload: Record<string, unknown>) {
  window.dispatchEvent(
    new MessageEvent('message', { data: JSON.stringify(payload), source: contentWindowMock }),
  )
}

function statusOf(overrides: Record<string, unknown> = {}) {
  return {
    installed: false,
    state: 'idle',
    version: 'v1',
    downloaded: 0,
    total: 0,
    error: '',
    basePath: '',
    ...overrides,
  }
}

const file = { path: 'docs/flow.drawio', name: 'flow.drawio', content: '<mxfile/>' }

beforeEach(() => {
  apiGetMock.mockReset()
  apiPostMock.mockReset()
  mockSaveFile.mockClear()
  _resetForTesting()
  dialogChoice = true
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('DrawioViewer install gate', () => {
  it('offers the download when the editor is not installed', async () => {
    apiGetMock.mockResolvedValue(statusOf())
    const wrapper = mount(DrawioViewer, { props: { file } })
    await nextTick()
    await nextTick()

    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.text()).toContain('file.viewer.drawioTitle')
    expect(wrapper.find('.drawio-action').text()).toBe('file.viewer.drawioInstall')
    wrapper.unmount()
  })

  it('shows the server error and a retry button when the last attempt failed', async () => {
    apiGetMock.mockResolvedValue(statusOf({ state: 'failed', error: 'mirror unreachable' }))
    const wrapper = mount(DrawioViewer, { props: { file } })
    await nextTick()
    await nextTick()

    expect(wrapper.text()).toContain('mirror unreachable')
    expect(wrapper.find('.drawio-action').text()).toBe('file.viewer.drawioRetry')
    wrapper.unmount()
  })

  it('starts the install and follows its progress', async () => {
    apiGetMock
      .mockResolvedValueOnce(statusOf())
      .mockResolvedValue(statusOf({ state: 'downloading', downloaded: 12 * 1048576, total: 48 * 1048576 }))
    apiPostMock.mockResolvedValue(statusOf({ state: 'downloading' }))

    const wrapper = mount(DrawioViewer, { props: { file } })
    await nextTick()
    await nextTick()

    await wrapper.find('.drawio-action').trigger('click')
    await nextTick()
    await nextTick()

    expect(apiPostMock).toHaveBeenCalledWith('/api/drawio/install', {})
    expect(wrapper.find('.drawio-progress').exists()).toBe(true)
    wrapper.unmount()
  })

  it('mounts the iframe with draw.io embed parameters once installed', async () => {
    apiGetMock.mockResolvedValue(statusOf({ installed: true, state: 'ready', basePath: '/vendor/drawio/v1/' }))
    mockContentWindow()
    const wrapper = mount(DrawioViewer, { props: { file } })
    await nextTick()
    await nextTick()

    const iframe = wrapper.find('iframe')
    expect(iframe.exists()).toBe(true)
    const src = iframe.attributes('src')
    expect(src).toContain('/vendor/drawio/v1/')
    expect(src).toContain('embed=1')
    expect(src).toContain('proto=json')
    wrapper.unmount()
  })
})

describe('DrawioViewer bridge', () => {
  async function mountReady() {
    apiGetMock.mockResolvedValue(statusOf({ installed: true, state: 'ready', basePath: '/vendor/drawio/v1/' }))
    mockContentWindow()
    const wrapper = mount(DrawioViewer, { props: { file } })
    await nextTick()
    await nextTick()
    return wrapper
  }

  it('hands the file content to the editor when it reports init', async () => {
    const wrapper = await mountReady()
    sendFromIframe({ event: 'init' })
    await nextTick()

    expect(postMessage).toHaveBeenCalled()
    const sent = JSON.parse(postMessage.mock.calls.at(-1)[0])
    expect(sent.action).toBe('load')
    expect(sent.xml).toBe('<mxfile/>')
    wrapper.unmount()
  })

  it('saves the exported XML when leaving with unsaved changes', async () => {
    const wrapper = await mountReady()
    sendFromIframe({ event: 'init' })
    sendFromIframe({ event: 'autosave', xml: '<mxfile>edited</mxfile>' })
    await nextTick()

    const exit = wrapper.vm.requestExit()
    // The confirm dialog resolves first; the export request only goes out after
    // that, so wait for it rather than racing it.
    await vi.waitFor(() => {
      expect(postMessage.mock.calls.some((c) => String(c[0]).includes('"export"'))).toBe(true)
    })
    sendFromIframe({ event: 'export', data: '<mxfile>exported</mxfile>' })

    await expect(exit).resolves.toBe(true)
    expect(mockSaveFile).toHaveBeenCalledWith('docs/flow.drawio', '<mxfile>exported</mxfile>')
    wrapper.unmount()
  })

  it('falls back to the last autosave when the export does not answer', async () => {
    vi.useFakeTimers()
    const wrapper = await mountReady()
    sendFromIframe({ event: 'init' })
    sendFromIframe({ event: 'autosave', xml: '<mxfile>edited</mxfile>' })
    await nextTick()

    const exit = wrapper.vm.requestExit()
    await vi.advanceTimersByTimeAsync(3100)

    await expect(exit).resolves.toBe(true)
    expect(mockSaveFile).toHaveBeenCalledWith('docs/flow.drawio', '<mxfile>edited</mxfile>')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('does not save when the user discards the changes', async () => {
    dialogChoice = null
    const wrapper = await mountReady()
    sendFromIframe({ event: 'init' })
    sendFromIframe({ event: 'autosave', xml: '<mxfile>edited</mxfile>' })
    await nextTick()

    await expect(wrapper.vm.requestExit()).resolves.toBe(true)
    expect(mockSaveFile).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('registers itself as the active editor so the back gesture asks before leaving', async () => {
    const wrapper = await mountReady()
    expect(useFileEditor().isEditing()).toBe(true)
    wrapper.unmount()
    expect(useFileEditor().isEditing()).toBe(false)
  })
})

describe('DrawioViewer offline upload', () => {
  class FakeXHR {
    static instances: FakeXHR[] = []
    upload: { onprogress?: (e: { lengthComputable: boolean; loaded: number; total: number }) => void } = {}
    status = 200
    responseText = '{}'
    timeout = 0
    onload: (() => void) | null = null
    onerror: (() => void) | null = null
    ontimeout: (() => void) | null = null
    sentBody: FormData | null = null

    constructor() {
      FakeXHR.instances.push(this)
    }
    open() {}
    send(body: FormData) {
      this.sentBody = body
      this.upload.onprogress?.({ lengthComputable: true, loaded: 25, total: 100 })
      queueMicrotask(() => this.onload?.())
    }
  }

  let originalXHR: typeof XMLHttpRequest

  beforeEach(() => {
    originalXHR = globalThis.XMLHttpRequest
    FakeXHR.instances = []
    ;(globalThis as unknown as { XMLHttpRequest: unknown }).XMLHttpRequest = FakeXHR
  })

  afterEach(() => {
    ;(globalThis as unknown as { XMLHttpRequest: unknown }).XMLHttpRequest = originalXHR
  })

  async function mountAbsent() {
    apiGetMock.mockResolvedValueOnce(statusOf())
    const wrapper = mount(DrawioViewer, { props: { file } })
    await nextTick()
    await nextTick()
    return wrapper
  }

  async function chooseFile(wrapper: Awaited<ReturnType<typeof mountAbsent>>) {
    const input = wrapper.find('input[type="file"]')
    const picked = new File(['war-bytes'], 'draw.war')
    Object.defineProperty(input.element, 'files', { value: [picked], configurable: true })
    await input.trigger('change')
    return picked
  }

  it('uploads the chosen archive and switches to the editor once installed', async () => {
    const wrapper = await mountAbsent()
    apiGetMock.mockResolvedValue(statusOf({ installed: true, state: 'ready', basePath: '/vendor/drawio/v1/' }))

    const picked = await chooseFile(wrapper)
    await vi.waitFor(() => expect(wrapper.find('iframe').exists()).toBe(true))

    expect(FakeXHR.instances).toHaveLength(1)
    expect(FakeXHR.instances[0].sentBody?.get('file')).toBe(picked)
    wrapper.unmount()
  })

  it('shows the reason the server rejected the archive', async () => {
    const wrapper = await mountAbsent()
    const reject = FakeXHR.prototype.send
    FakeXHR.prototype.send = function sendRejected(this: FakeXHR) {
      this.status = 400
      this.responseText = JSON.stringify({ error: 'checksum mismatch: got aa, want bb' })
      queueMicrotask(() => this.onload?.())
    }

    try {
      await chooseFile(wrapper)
      await vi.waitFor(() => expect(wrapper.text()).toContain('checksum mismatch'))
      // The gate must stay up: a rejected upload is not a silent success.
      expect(wrapper.find('iframe').exists()).toBe(false)
    } finally {
      FakeXHR.prototype.send = reject
      wrapper.unmount()
    }
  })
})

describe('DrawioViewer theme', () => {
  function mountInstalled() {
    apiGetMock.mockResolvedValue(statusOf({ installed: true, state: 'ready', basePath: '/vendor/drawio/v1/' }))
    mockContentWindow()
    return mount(DrawioViewer, { props: { file } })
  }

  afterEach(() => {
    document.documentElement.removeAttribute('data-theme-base')
  })

  it('asks for the dark palette when the app theme is dark', async () => {
    // The editor takes its palette from the URL; without this a dark app shows a
    // white editor in the middle of the screen.
    document.documentElement.setAttribute('data-theme-base', 'dark')
    const wrapper = mountInstalled()
    await nextTick()
    await nextTick()

    expect(wrapper.find('iframe').attributes('src')).toContain('dark=1')
    wrapper.unmount()
  })

  it('leaves the palette light when the app theme is light', async () => {
    document.documentElement.setAttribute('data-theme-base', 'light')
    const wrapper = mountInstalled()
    await nextTick()
    await nextTick()

    expect(wrapper.find('iframe').attributes('src')).not.toContain('dark=1')
    wrapper.unmount()
  })

  it('reloads the editor when the theme flips', async () => {
    document.documentElement.setAttribute('data-theme-base', 'light')
    const wrapper = mountInstalled()
    await nextTick()
    await nextTick()
    expect(wrapper.find('iframe').attributes('src')).not.toContain('dark=1')

    document.documentElement.setAttribute('data-theme-base', 'dark')
    // MutationObserver callbacks are microtasks, and the watcher then re-renders.
    await new Promise((r) => setTimeout(r, 0))
    await nextTick()

    expect(wrapper.find('iframe').attributes('src')).toContain('dark=1')
    wrapper.unmount()
  })
})

describe('DrawioViewer export', () => {
  async function mountReady() {
    apiGetMock.mockResolvedValue(statusOf({ installed: true, state: 'ready', basePath: '/vendor/drawio/v1/' }))
    mockContentWindow()
    const wrapper = mount(DrawioViewer, { props: { file } })
    await nextTick()
    await nextTick()
    sendFromIframe({ event: 'init' })
    await nextTick()
    return wrapper
  }

  it('returns the payload drawio answers with', async () => {
    const wrapper = await mountReady()
    const pending = wrapper.vm.requestExport('png')
    sendFromIframe({ event: 'export', format: 'png', data: 'data:image/png;base64,AQID' })
    await expect(pending).resolves.toEqual({ format: 'png', data: 'data:image/png;base64,AQID' })
    wrapper.unmount()
  })

  it('times out instead of hanging when the editor never answers', async () => {
    vi.useFakeTimers()
    const wrapper = await mountReady()
    const pending = wrapper.vm.requestExport('png')
    const assertion = expect(pending).rejects.toThrow()
    await vi.advanceTimersByTimeAsync(20100)
    await assertion
    wrapper.unmount()
    vi.useRealTimers()
  })
})

describe('DrawioViewer save from the editor menu', () => {
  it('saves the XML drawio sends with its own Save & Exit', async () => {
    // drawio's File menu is still reachable inside the frame, and its Save item
    // used to be a dead click.
    apiGetMock.mockResolvedValue(statusOf({ installed: true, state: 'ready', basePath: '/vendor/drawio/v1/' }))
    mockContentWindow()
    const wrapper = mount(DrawioViewer, { props: { file } })
    await nextTick()
    await nextTick()
    sendFromIframe({ event: 'init' })
    await nextTick()

    sendFromIframe({ event: 'save', xml: '<mxfile>edited</mxfile>' })
    await vi.waitFor(() => expect(mockSaveFile).toHaveBeenCalledWith('docs/flow.drawio', '<mxfile>edited</mxfile>'))
    wrapper.unmount()
  })
})
