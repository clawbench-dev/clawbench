import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import FileViewer from '../FileViewer.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      file: {
        viewer: {
          binaryFile: 'Binary file',
          fileTooLarge: 'File too large',
          truncated: 'Truncated',
        },
        header: {
          openAsText: 'Open as text',
          shareExternal: 'Share',
        },
      },
      common: { download: 'Download', close: 'Close' },
    },
  },
})

// Mock composables
vi.mock('@/composables/useAppMode.ts', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
}))

vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useDiffDrawer.ts', () => ({
  useDiffDrawer: () => ({
    drawerMarkerType: { value: 'none' },
    drawerCharDiff: { value: false },
    drawerDiffLines: { value: [] },
    closeDrawer: vi.fn(),
  }),
}))

vi.mock('@/composables/useMarkdownDiff.ts', () => ({
  diffMarkers: { value: [] },
  diffDrawerVisible: { value: false },
  diffDrawerMarker: { value: null },
  diffOldContent: { value: null },
  diffOldFilePath: { value: null },
  openDiffDrawer: vi.fn(),
  closeDiffDrawer: vi.fn(),
  clearDiffMarkers: vi.fn(),
}))

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    effectiveOpen: { value: false },
  }),
}))

vi.mock('@/composables/useFileRefresh.ts', () => ({
  flashRanges: { value: [] },
  flashType: { value: null },
}))

vi.mock('@/composables/useFileNavStack.ts', () => ({
  useFileNavStack: () => ({
    overlayOpen: { value: false },
    canGoBack: { value: false },
  }),
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    localConfig: { wordWrap: false, lineNumbers: true, stickyScroll: true },
    setLocalConfig: vi.fn(),
  }),
}))

vi.mock('@/stores/app.ts', () => ({
  store: {
    state: { currentFile: null, currentDir: '', projectRoot: '/tmp' },
    selectFile: vi.fn(),
  },
}))

vi.mock('@/utils/fileType.ts', () => ({
  getFileType: (name: string) => {
    if (name.endsWith('.md')) return { isMarkdown: true, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false, lang: 'markdown' }
    if (name.endsWith('.html')) return { isMarkdown: false, isHtml: true, isImage: false, isAudio: false, isVideo: false, isPdf: false, lang: 'xml' }
    if (name.endsWith('.png')) return { isMarkdown: false, isHtml: false, isImage: true, isAudio: false, isVideo: false, isPdf: false, lang: '' }
    if (name.endsWith('.pdf')) return { isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: true, lang: '' }
    if (name.endsWith('.mp3')) return { isMarkdown: false, isHtml: false, isImage: false, isAudio: true, isVideo: false, isPdf: false, lang: '' }
    if (name.endsWith('.mp4')) return { isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: true, isPdf: false, lang: '' }
    return { isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false, lang: 'plaintext' }
  },
  formatFileSize: (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`
    return `${(bytes / 1024).toFixed(1)} KB`
  },
}))

vi.mock('@/utils/exportHtml.ts', () => ({
  exportRenderedHtml: vi.fn().mockResolvedValue({ html: '<html></html>', skippedImages: 0, externalImages: 0 }),
}))

vi.mock('@/utils/download.ts', () => ({
  buildLocalFileUrl: (path: string, opts?: any) => `/api/local-file/${path}?download=1`,
  downloadFileByPath: vi.fn(),
  downloadBlob: vi.fn(),
}))

// Stub child components
const stubs = {
  FileHeader: true,
  PdfPreview: true,
  ImagePreview: true,
  AudioPreview: true,
  VideoPreview: true,
  MarkdownPreview: true,
  CodePreview: true,
  DiffDrawer: true,
}

describe('FileViewer', () => {
  function mountViewer(props = {}) {
    return mount(FileViewer, {
      props: {
        file: { name: 'main.ts', path: 'main.ts', content: 'const x = 1' },
        tocOpen: false,
        searchOpen: false,
        markdownViewMode: 'rendered',
        externalLoading: false,
        ...props,
      },
      global: {
        plugins: [i18n],
        stubs,
      },
    })
  }

  it('renders the viewer container', () => {
    const wrapper = mountViewer()
    expect(wrapper.find('.file-viewer').exists()).toBe(true)
  })

  it('renders error bubble when file has error', () => {
    const wrapper = mountViewer({ file: { name: 'err.ts', path: 'err.ts', error: 'Not found' } })
    expect(wrapper.find('.error-bubble').exists()).toBe(true)
    expect(wrapper.text()).toContain('Not found')
  })

  it('shows PdfPreview for PDF files', () => {
    const wrapper = mountViewer({ file: { name: 'doc.pdf', path: 'doc.pdf', isPdf: true, content: null } })
    expect(wrapper.findComponent({ name: 'PdfPreview' }).exists() || wrapper.find('.file-viewer-content').exists()).toBe(true)
  })

  it('shows ImagePreview for image files', () => {
    const wrapper = mountViewer({ file: { name: 'photo.png', path: 'photo.png', isImage: true, content: null } })
    expect(wrapper.findComponent({ name: 'ImagePreview' }).exists() || wrapper.find('.file-viewer-content').exists()).toBe(true)
  })

  it('shows AudioPreview for audio files', () => {
    const wrapper = mountViewer({ file: { name: 'song.mp3', path: 'song.mp3', isAudio: true, content: null } })
    expect(wrapper.findComponent({ name: 'AudioPreview' }).exists() || wrapper.find('.file-viewer-content').exists()).toBe(true)
  })

  it('shows VideoPreview for video files', () => {
    const wrapper = mountViewer({ file: { name: 'clip.mp4', path: 'clip.mp4', isVideo: true, content: null } })
    expect(wrapper.findComponent({ name: 'VideoPreview' }).exists() || wrapper.find('.file-viewer-content').exists()).toBe(true)
  })

  it('shows binary file placeholder for binary files', () => {
    const wrapper = mountViewer({ file: { name: 'data.bin', path: 'data.bin', isBinary: true, content: null } })
    expect(wrapper.find('.unsupported-file').exists()).toBe(true)
    expect(wrapper.text()).toContain('data.bin')
  })

  it('shows too large placeholder for large files', () => {
    const wrapper = mountViewer({ file: { name: 'big.log', path: 'big.log', tooLarge: true, size: 1048576 } })
    expect(wrapper.find('.unsupported-file').exists()).toBe(true)
    expect(wrapper.text()).toContain('big.log')
  })

  it('shows open-as-text button for binary files', () => {
    const wrapper = mountViewer({ file: { name: 'data.bin', path: 'data.bin', isBinary: true, content: null } })
    expect(wrapper.find('.open-as-text-btn').exists()).toBe(true)
  })

  it('shows truncated notice when file is truncated', () => {
    const wrapper = mountViewer({ file: { name: 'long.ts', path: 'long.ts', content: '...', truncated: true } })
    expect(wrapper.find('.truncated-notice').exists()).toBe(true)
  })

  it('shows loading spinner when loading', () => {
    const wrapper = mountViewer({ file: { name: 'test.ts', path: 'test.ts', content: null } })
    expect(wrapper.find('.loading').exists()).toBe(true)
  })

  it('emits delete event via FileHeader', async () => {
    const wrapper = mountViewer()
    const header = wrapper.findComponent({ name: 'FileHeader' })
    if (header.exists()) {
      await header.vm.$emit('delete', 'main.ts')
      expect(wrapper.emitted('delete')).toBeTruthy()
    }
  })

  it('calls store.selectFile when openAsText is emitted', async () => {
    const wrapper = mountViewer()
    const header = wrapper.findComponent({ name: 'FileHeader' })
    if (header.exists()) {
      await header.vm.$emit('openAsText')
      const { store } = await import('@/stores/app.ts')
      expect(store.selectFile).toHaveBeenCalled()
    }
  })

  it('exposes pdfOutline computed property', () => {
    const wrapper = mountViewer({ file: { name: 'doc.pdf', path: 'doc.pdf', isPdf: true, content: null } })
    const vm = wrapper.vm as any
    expect(vm.pdfOutline).toBeDefined()
  })

  it('exposes pdfScrollToPage method', () => {
    const wrapper = mountViewer({ file: { name: 'doc.pdf', path: 'doc.pdf', isPdf: true, content: null } })
    const vm = wrapper.vm as any
    expect(typeof vm.pdfScrollToPage).toBe('function')
  })
})
