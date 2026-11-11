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
