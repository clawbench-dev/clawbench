import '../../../css/layout.css'
import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import FileViewer from '../file/FileViewer.vue'
import MarkdownPreview from '../file/MarkdownPreview.vue'
import CodePreview from '../file/CodePreview.vue'

// Minimal i18n instance for tests
const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      welcome: {
        selectFile: 'Select a file to start',
        description: 'Open a file and chat with AI to read, edit, and modify it directly.',
      },
      file: {
        header: {
          toc: 'TOC',
          search: 'Search',
          more: 'More',
          openAsText: 'Open as text',
          sourceView: 'Source',
          renderedView: 'Rendered',
          wordWrap: 'Word Wrap',
          fileHistory: 'File history',
        },
      },
    },
  },
})

vi.mock('@/composables/useMarkdownRenderer.ts', () => ({
  useMarkdownRenderer: () => ({
    renderMarkdown: (content: string) => `<p>${content}</p>`,
    renderMermaidInElement: vi.fn().mockResolvedValue(undefined),
  }),
}))

vi.mock('@/composables/useDoubleClickCopy.ts', () => ({
  useDoubleClickCopy: () => ({
    handleDblClick: vi.fn(),
  }),
}))

vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  useFilePathAnnotation: () => ({
    annotateFilePaths: (html: string) => ({ html, detectedPaths: [] }),
    verifyFilePaths: vi.fn(),
    resolveRelativePath: (href: string) => href,
    openFilePath: vi.fn(),
  }),
}))

vi.mock('@/stores/app.ts', () => ({
  store: {
    state: {
      projectRoot: '/tmp/project',
      currentFile: null,
      currentDir: '',
    },
    selectFile: vi.fn(),
  },
}))

vi.mock('@/composables/useFileRefresh.ts', () => ({
  refreshCurrentFile: vi.fn(),
  flashRanges: { value: [] },
  flashType: { value: '' },
}))

vi.mock('@/composables/useSettingsConfig.ts', () => ({
  useSettingsConfig: () => ({
    localConfig: { wordWrap: false, lineNumbers: true, stickyScroll: true },
    setLocalConfig: vi.fn(),
  }),
  getZoomedViewport: () => ({ width: 1024, height: 768 }),
  toFixedCSS: (v: number) => Math.round(v * 100) / 100,
}))

vi.mock('@/composables/useDiffDrawer.ts', () => ({
  useDiffDrawer: () => ({
    drawerVisible: { value: false },
    drawerMarkerType: { value: '' },
    drawerCharDiff: { value: false },
    drawerDiffLines: { value: [] },
    closeDrawer: vi.fn(),
  }),
}))

vi.mock('@/composables/useAppMode.ts', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
}))

vi.mock('@/composables/useChatContext', () => ({
  useChatContext: () => ({
    addAttachedFile: vi.fn(),
    hasAttachedFile: vi.fn(() => false),
    removeAttachedFileByPath: vi.fn(),
    toggleAttachedFile: vi.fn(),
    attachedFiles: { value: [] },
    quoteData: { value: null },
    setQuoteData: vi.fn(),
    removeAttachedFile: vi.fn(),
    clearAll: vi.fn(),
  }),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useFileNavStack.ts', () => ({
  useFileNavStack: () => ({
    overlayOpen: { value: false },
    canGoBack: { value: false },
  }),
}))

const TeleportStub = { template: '<div><slot /></div>' }

describe('preview layout contract', () => {
  it('renders file viewer with expected root element', () => {
    const wrapper = mount(FileViewer, {
      props: {
        file: {
          name: 'README.md',
          path: '/tmp/README.md',
          content: '# Hello',
        },
      },
      global: {
        plugins: [i18n],
        stubs: {
          FileHeader: { template: '<div class="file-header-stub" />' },
          ImagePreview: true,
          AudioPreview: true,
          VideoPreview: true,
          CodePreview: true,
          MarkdownPreview: { template: '<div class="markdown-preview-stub" />' },
        },
      },
    })

    expect(wrapper.find('.file-viewer').exists()).toBe(true)
    expect(wrapper.find('.file-viewer-content').exists()).toBe(true)
  })

  it('renders markdown preview with content area', async () => {
    const wrapper = mount(MarkdownPreview, {
      props: {
        file: {
          path: '/tmp/README.md',
          content: '# Hello',
        },
        viewMode: 'rendered',
      },
      global: {
        plugins: [i18n],
      },
    })

    await nextTick()
    await nextTick()

    expect(wrapper.find('.markdown-preview').exists()).toBe(true)
    expect(wrapper.find('.markdown-body').exists()).toBe(true)
  })

  it('renders code preview with raw content', () => {
    const wrapper = mount(CodePreview, {
      props: {
        content: 'const x = 1',
        language: 'typescript',
        editable: false,
      },
      global: {
        plugins: [i18n],
        stubs: {
          BottomSheet: true,
          Teleport: TeleportStub,
        },
      },
    })

    expect(wrapper.find('.raw-content-pre').exists()).toBe(true)
  })

  it('renders file viewer child content for markdown files', () => {
    const wrapper = mount(FileViewer, {
      props: {
        file: {
          name: 'README.md',
          path: '/tmp/README.md',
          content: '# Hello',
        },
      },
      global: {
        plugins: [i18n],
        stubs: {
          BottomSheet: true,
          Teleport: TeleportStub,
        },
      },
    })

    // MarkdownPreview should render inside file-viewer-content for .md files
    expect(wrapper.find('.file-viewer-content .markdown-preview').exists()).toBe(true)
  })

  it('renders file viewer child content for code files', () => {
    const wrapper = mount(FileViewer, {
      props: {
        file: {
          name: 'main.ts',
          path: '/tmp/main.ts',
          content: 'const x = 1',
        },
      },
      global: {
        plugins: [i18n],
        stubs: {
          BottomSheet: true,
          Teleport: TeleportStub,
        },
      },
    })

    // CodePreview should render inside .raw-content-viewer for code files
    expect(wrapper.find('.file-viewer-content .raw-content-viewer').exists()).toBe(true)
  })
})
