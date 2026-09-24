import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import MediaPreviewBody from '@/components/file/MediaPreviewBody.vue'
import { readAttachDragData, cleanupDragGhost } from '@/utils/attachDrag'

// buildLocalFileUrl is the single source of the media URL; mock it so the
// assertions read cleanly instead of depending on the path-encoding rules.
const mockBuildLocalFileUrl = vi.hoisted(() => vi.fn((p: string) => `/api/fs/raw/${p}`))
vi.mock('@/utils/download.ts', () => ({
  buildLocalFileUrl: (p: string) => mockBuildLocalFileUrl(p),
}))

// Drive the draggable gate directly instead of depending on jsdom's viewport.
const mockIsWideScreen = ref(true)
vi.mock('@/composables/useWideScreenLayout.ts', () => ({
  useWideScreenLayout: () => ({ isWideScreen: mockIsWideScreen }),
}))

// Minimal DataTransfer stand-in for the internal attach payload.
function mockDataTransfer(): DataTransfer {
  const store: Record<string, string> = {}
  const types: string[] = []
  return {
    setData(type: string, value: string) {
      store[type] = value
      if (!types.includes(type)) types.push(type)
    },
    getData(type: string) {
      return store[type] ?? ''
    },
    get types() {
      return Object.freeze([...types])
    },
    setDragImage: vi.fn(),
    effectAllowed: 'none',
  } as unknown as DataTransfer
}

// The PDF viewer pulls in pdf.js (~500KB); stub it for the media-body tests.
vi.mock('@/components/media/PdfPreview.vue', () => ({
  default: { name: 'PdfPreview', props: ['file'], template: '<div class="pdf-stub" />' },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      file: { codePreview: { mediaLoadError: 'Failed to load media file' } },
    },
  },
})

function mountBody(kind: 'image' | 'video' | 'audio' | 'pdf', path = 'assets/logo.png') {
  return mount(MediaPreviewBody, {
    props: { path, kind },
    global: {
      plugins: [i18n],
      // The PDF branch is a defineAsyncComponent; stub it so no dynamic import
      // (pdf.js) is triggered in the unit test.
      stubs: { PdfPreview: { name: 'PdfPreview', props: ['file'], template: '<div class="pdf-stub" />' } },
    },
  })
}

describe('MediaPreviewBody', () => {
  beforeEach(() => {
    mockBuildLocalFileUrl.mockClear()
  })

  // A dragstart without a matching dragend leaves the 5s ghost safety timer
  // pending; clear it so the suite reports no async leaks.
  afterEach(() => {
    cleanupDragGhost()
  })

  it('renders an <img> pointing at the local-file endpoint for images', () => {
    const wrapper = mountBody('image')
    const img = wrapper.find('img.code-preview-media-img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toContain('/api/fs/raw/assets/logo.png')
    // Cache-busting param keeps a re-opened file from serving stale bytes.
    expect(img.attributes('src')).toMatch(/t=\d+/)
  })

  it('renders an <img> for SVG (served as image bytes, not source)', () => {
    const wrapper = mountBody('image', 'diagram.svg')
    expect(wrapper.find('img.code-preview-media-img').exists()).toBe(true)
    expect(wrapper.find('img').attributes('src')).toContain('diagram.svg')
  })

  it('renders a <video controls> for video', () => {
    const wrapper = mountBody('video', 'clip.mp4')
    const video = wrapper.find('video.code-preview-media-video')
    expect(video.exists()).toBe(true)
    expect(video.attributes('controls')).toBeDefined()
    expect(video.attributes('src')).toContain('clip.mp4')
  })

  it('renders an <audio controls> plus the file name for audio', () => {
    const wrapper = mountBody('audio', 'dir/voice.mp3')
    const audio = wrapper.find('audio.code-preview-media-audio-player')
    expect(audio.exists()).toBe(true)
    expect(audio.attributes('controls')).toBeDefined()
    expect(wrapper.find('.code-preview-media-audio-name').text()).toBe('voice.mp3')
  })

  it('renders the PDF viewer with path + name for pdf', () => {
    const wrapper = mountBody('pdf', 'docs/report.pdf')
    const pdf = wrapper.findComponent({ name: 'PdfPreview' })
    expect(pdf.exists()).toBe(true)
    expect(pdf.props('file')).toEqual({ path: 'docs/report.pdf', name: 'report.pdf' })
  })

  it('shows the fallback when the media fails to load', async () => {
    const wrapper = mountBody('image')
    expect(wrapper.find('.code-preview-media-error').exists()).toBe(false)

    await wrapper.find('img').trigger('error')
    expect(wrapper.find('.code-preview-media-error').exists()).toBe(true)
    expect(wrapper.find('.code-preview-media-error').text()).toContain('Failed to load media file')
  })

  it('clears the error and re-fetches when the path changes', async () => {
    const wrapper = mountBody('image')
    await wrapper.find('img').trigger('error')
    expect(wrapper.find('.code-preview-media-error').exists()).toBe(true)

    await wrapper.setProps({ path: 'other.png' })
    await flushPromises()

    expect(wrapper.find('.code-preview-media-error').exists()).toBe(false)
    expect(wrapper.find('img').attributes('src')).toContain('other.png')
  })

  it('emits loaded when the media loads', async () => {
    const wrapper = mountBody('image')
    await wrapper.find('img').trigger('load')
    expect(wrapper.emitted('loaded')).toHaveLength(1)
  })

  it('does not emit loaded on error', async () => {
    const wrapper = mountBody('image')
    await wrapper.find('img').trigger('error')
    expect(wrapper.emitted('loaded')).toBeUndefined()
  })

  it('shows the fallback for video and audio errors too', async () => {
    const video = mountBody('video', 'clip.mp4')
    await video.find('video').trigger('error')
    expect(video.find('.code-preview-media-error').exists()).toBe(true)

    const audio = mountBody('audio', 'voice.mp3')
    await audio.find('audio').trigger('error')
    expect(audio.find('.code-preview-media-error').exists()).toBe(true)
  })

  it('re-requests the media when refreshNonce changes', async () => {
    const wrapper = mountBody('image')
    const first = wrapper.find('img').attributes('src')
    expect(wrapper.find('.code-preview-media-error').exists()).toBe(false)

    await wrapper.find('img').trigger('error')
    expect(wrapper.find('.code-preview-media-error').exists()).toBe(true)

    // A refresh bump clears the error state and changes the cache-busting URL.
    await wrapper.setProps({ refreshNonce: 1 })
    await flushPromises()

    expect(wrapper.find('.code-preview-media-error').exists()).toBe(false)
    expect(wrapper.find('img').attributes('src')).not.toBe(first)
  })

  // ── Drag-to-attach (internal payload, no re-upload) ──

  it('makes the image draggable and writes the attach payload on dragstart', () => {
    const wrapper = mountBody('image', 'assets/logo.png')
    const img = wrapper.find('img.code-preview-media-img')
    expect(img.attributes('draggable')).toBe('true')

    const dt = mockDataTransfer()
    img.element.dispatchEvent(Object.assign(new Event('dragstart'), { dataTransfer: dt }))
    // Attaches the EXISTING project path — never uploads the bytes.
    expect(readAttachDragData(dt)).toEqual({ path: 'assets/logo.png', isDir: false })
  })

  it('removes the drag ghost on dragend', () => {
    const wrapper = mountBody('image', 'assets/logo.png')
    const img = wrapper.find('img.code-preview-media-img')
    img.element.dispatchEvent(Object.assign(new Event('dragstart'), { dataTransfer: mockDataTransfer() }))
    expect(document.querySelector('[data-attach-ghost]')).toBeTruthy()

    img.element.dispatchEvent(new Event('dragend'))
    expect(document.querySelector('[data-attach-ghost]')).toBeNull()
  })

  it('is not draggable on narrow screens', () => {
    mockIsWideScreen.value = false
    const wrapper = mountBody('image')
    expect(wrapper.find('img.code-preview-media-img').attributes('draggable')).toBe('false')
    mockIsWideScreen.value = true
  })

  it('exposes the no-op scroll helpers the card contract calls', () => {
    const wrapper = mountBody('image')
    // A <script setup> component without defineExpose still resolves to a
    // truthy empty proxy, so the card's `?.scrollToTargetLine()` would throw.
    expect(typeof (wrapper.vm as unknown as { scrollToTargetLine?: unknown }).scrollToTargetLine).toBe('function')
    expect(typeof (wrapper.vm as unknown as { scrollLineIntoView?: unknown }).scrollLineIntoView).toBe('function')
  })
})
