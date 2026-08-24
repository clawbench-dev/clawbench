import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ExcalidrawViewer from '@/components/file/ExcalidrawViewer.vue'
import { useFileEditor, _resetForTesting } from '@/composables/useFileEditor.ts'
import { store } from '@/stores/app.ts'

// Mock the save path so tests never hit the network.
const mockSaveFile = vi.fn().mockResolvedValue(true)
vi.mock('@/composables/useCodeEditorSave', () => ({
  useCodeEditorSave: () => ({ saving: { value: false }, saveFile: mockSaveFile }),
}))

// Mock i18n used by the component and useCodeEditorSave — keep real createI18n.
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (k: string) => k, locale: { value: 'zh-CN' } }),
  }
})

// Mock toast used by useCodeEditorSave.
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

// Mock appLog and file refresh.
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))
vi.mock('@/composables/useFileRefresh', () => ({
  markFileSaved: vi.fn(),
}))

let postMessage: ReturnType<typeof vi.fn>

function mockContentWindow() {
  postMessage = vi.fn()
  vi.spyOn(HTMLIFrameElement.prototype, 'contentWindow', 'get')
    .mockReturnValue({ postMessage } as unknown as Window)
  return postMessage
}

function sendFromIframe(event: string, data?: Record<string, unknown>) {
  const msg = JSON.stringify({ event, data })
  window.dispatchEvent(new MessageEvent('message', { data: msg }))
}

const file = {
  name: 'flow.excalidraw',
  path: '/tmp/project/flow.excalidraw',
  content: '{"type":"excalidraw","version":2,"source":"x","elements":[],"appState":{},"files":{}}',
  isExcalidraw: true,
}

describe('ExcalidrawViewer', () => {
  let wrapper: ReturnType<typeof mount> | null = null

  beforeEach(() => {
    _resetForTesting()
    mockSaveFile.mockClear()
    store.state.currentFile = file as never
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('renders an iframe pointing at the vendor host', () => {
    wrapper = mount(ExcalidrawViewer, { props: { file } })
    const iframe = wrapper.find('iframe')
    expect(iframe.exists()).toBe(true)
    expect(iframe.attributes('src')).toBe('/vendor/excalidraw/index.html')
    // The Excalidraw host is a trusted first-party static build served at
    // /vendor/excalidraw/ (same origin). It needs allow-same-origin so ES
    // module scripts load (opaque origins are CORS-blocked) and postMessage
    // with the parent works. Unlike the HTML preview iframe (ISS-021), this
    // does NOT render untrusted user content.
    const sandbox = iframe.attributes('sandbox') || ''
    const tokens = sandbox.split(/\s+/).filter(Boolean)
    expect(tokens).toContain('allow-scripts')
    expect(tokens).toContain('allow-same-origin')
  })

  it('sends the initial load after the iframe signals ready', async () => {
    mockContentWindow()
    wrapper = mount(ExcalidrawViewer, { props: { file } })
    postMessage.mockClear()

    sendFromIframe('ready')
    await nextTick()

    expect(postMessage).toHaveBeenCalledTimes(1)
    const [payload] = postMessage.mock.calls[0]
    const parsed = JSON.parse(payload)
    expect(parsed.event).toBe('load')
    expect(parsed.data.content).toBe(file.content)
    expect(parsed.data.lang).toBe('zh-CN')
    expect(['light', 'dark']).toContain(parsed.data.theme)
  })

  it('still sends the load when content arrives before the iframe is ready', async () => {
    mockContentWindow()
    // Mount with content initially null, then set it while iframe is not ready.
    wrapper = mount(ExcalidrawViewer, { props: { file: { ...file, content: null } } })
    postMessage.mockClear()

    // Content arrives first (fetch resolves) — queued, not sent yet.
    await wrapper.setProps({ file: { ...file, content: file.content } })
    await nextTick()
    expect(postMessage).not.toHaveBeenCalled()

    // iframe signals ready afterwards — queued content must now be flushed.
    sendFromIframe('ready')
    await nextTick()

    expect(postMessage).toHaveBeenCalledTimes(1)
    const [payload] = postMessage.mock.calls[0]
    const parsed = JSON.parse(payload)
    expect(parsed.event).toBe('load')
    expect(parsed.data.content).toBe(file.content)
  })

  it('marks the file dirty when the iframe reports a change', async () => {
    wrapper = mount(ExcalidrawViewer, { props: { file } })
    const fileEditor = useFileEditor()

    expect(fileEditor.isEditorDirty()).toBe(false)
    sendFromIframe('changed')
    await nextTick()
    expect(fileEditor.isEditorDirty()).toBe(true)
  })

  it('persists content when the iframe returns a save payload', async () => {
    wrapper = mount(ExcalidrawViewer, { props: { file } })
    sendFromIframe('changed')
    await nextTick()

    const newContent = '{"type":"excalidraw","version":2,"source":"x","elements":[],"appState":{},"files":{}}'
    sendFromIframe('save', { content: newContent })
    await nextTick()

    expect(mockSaveFile).toHaveBeenCalledWith(file.path, newContent)
    expect(useFileEditor().isEditorDirty()).toBe(false)
  })

  it('registers the dirty getter so the global back gesture sees unsaved edits', () => {
    wrapper = mount(ExcalidrawViewer, { props: { file } })
    const fileEditor = useFileEditor()

    sendFromIframe('changed')
    expect(fileEditor.isEditorDirty()).toBe(true)
  })

  it('requests a save when exiting edit mode', async () => {
    mockContentWindow()
    wrapper = mount(ExcalidrawViewer, { props: { file } })
    // Mark the iframe ready so the saveRequest is actually sent.
    sendFromIframe('ready')
    await nextTick()
    postMessage.mockClear()

    const fileEditor = useFileEditor()
    await fileEditor.exitEdit?.()

    expect(postMessage).toHaveBeenCalledTimes(1)
    const [payload] = postMessage.mock.calls[0]
    const parsed = JSON.parse(payload)
    expect(parsed.event).toBe('saveRequest')
  })
})
