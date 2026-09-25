import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { computed, ref } from 'vue'

const { openFilePath, closePreview, handleDblClick, pipelineHtml } = vi.hoisted(() => ({
  openFilePath: vi.fn(),
  closePreview: vi.fn(),
  handleDblClick: vi.fn(),
  pipelineHtml: { value: '' },
}))

vi.mock('@/composables/useMarkdownRenderer.ts', () => ({
  renderMermaidInElement: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('@/composables/usePlatformDetect.ts', () => ({
  usePlatformDetect: () => ({ isPC: ref(true) }),
}))

vi.mock('@/composables/useDoubleClickCopy.ts', () => ({
  useDoubleClickCopy: () => ({ handleDblClick }),
}))

vi.mock('@/composables/useQuoteQuestion.ts', () => ({
  useQuoteQuestion: () => ({ showBar: vi.fn() }),
}))

vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  useFilePathAnnotation: () => ({
    verifyFilePaths: vi.fn(),
    resolveRelativePath: vi.fn((href) => href),
    openFilePath,
    parseFileUri: vi.fn(),
    readLineTargetFromEl: (el: Element) => {
      const startAttr = el.getAttribute('data-line-start')
      const endAttr = el.getAttribute('data-line-end')
      return {
        filePath: el.getAttribute('data-file-path'),
        lineStart: startAttr ? parseInt(startAttr, 10) : undefined,
        lineEnd: endAttr ? parseInt(endAttr, 10) : undefined,
        lineRanges: el.getAttribute('data-line-ranges') || undefined,
      }
    },
  }),
}))

vi.mock('@/composables/useCodeBlockHeader.ts', () => ({
  handleCodeBlockClick: vi.fn().mockReturnValue(false),
  handleTableBlockClick: vi.fn().mockReturnValue(false),
}))

vi.mock('@/stores/app.ts', () => ({
  store: { state: { projectRoot: '/project', homeDir: '/home/user' } },
}))

vi.mock('@/composables/useMarkdownRenderPipeline.ts', () => ({
  buildMarkdownPreviewDom: () => ({
    html: pipelineHtml.value || '<p><span class="chat-file-path" data-file-path="docs/guides" data-path-type="dir">docs/guides</span><span class="chat-file-path file-target" data-file-path="docs/guide.md" data-path-type="file">docs/guide.md</span></p>',
    detectedPaths: [],
  }),
}))

vi.mock('@/composables/useTableRowExpand.ts', () => ({
  useTableRowExpand: () => ({
    tableRowModal: ref(null),
    closeTableRowModal: vi.fn(),
    tableRowPrev: vi.fn(),
    tableRowNext: vi.fn(),
    handleTableRowClick: vi.fn().mockReturnValue(false),
    onTableMouseDown: vi.fn(),
    onTableTouchStart: vi.fn(),
  }),
}))

vi.mock('@/composables/useMarkdownDiff.ts', () => ({
  diffMarkers: ref([]),
  clearDiffMarkers: vi.fn(),
  extractBlocks: vi.fn().mockReturnValue([]),
  extractBlockElements: vi.fn().mockReturnValue([]),
}))

vi.mock('@/composables/useDiffMarkerClick.ts', () => ({
  handleDiffMarkerClick: vi.fn().mockReturnValue(false),
}))

vi.mock('@/composables/useCodeLinkPreview.ts', () => ({
  useCodeLinkPreview: () => ({
    enabled: computed(() => true),
    isTouchDevice: () => false,
    handleClick: vi.fn(),
    close: closePreview,
  }),
  handleVerifiedFilePathClick: vi.fn().mockReturnValue(false),
}))

// Keep the real share-link behavior but make the interceptor observable, so a
// regression that drops the call from MarkdownPreview.handleClick is caught.
vi.mock('@/share/shareLinks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/share/shareLinks')>()
  return { ...actual, handleShareLinkClick: vi.fn(actual.handleShareLinkClick) }
})

import MarkdownPreview from '../MarkdownPreview.vue'
import { SHARE_OPEN_FILE_EVENT, SHARE_PATH_ATTR } from '@/share/shareLinks'

describe('MarkdownPreview path clicks', () => {
  beforeEach(() => {
    openFilePath.mockReset()
    closePreview.mockReset()
    handleDblClick.mockReset()
    pipelineHtml.value = ''
  })

  it('opens a verified directory when its path text is clicked', async () => {
    const wrapper = mount(MarkdownPreview, {
      props: {
        file: { path: '/project/README.md', content: '[docs](docs/guides)' },
        viewMode: 'rendered',
      },
      global: {
        stubs: {
          TableRowModal: true,
          MarkdownSearchBar: true,
          CodeLinkPreview: true,
        },
      },
    })

    const path = wrapper.find('.chat-file-path[data-path-type="dir"]')
    expect(path.exists()).toBe(true)
    await path.trigger('click')

    expect(closePreview).toHaveBeenCalledOnce()
    expect(openFilePath).toHaveBeenCalledWith('docs/guides', undefined, undefined, 'file')
  })

  it('does not directly open file when clicking on a file path text', async () => {
    const wrapper = mount(MarkdownPreview, {
      props: {
        file: { path: '/project/README.md', content: '[doc](docs/guide.md)' },
        viewMode: 'rendered',
      },
      global: {
        stubs: {
          TableRowModal: true,
          MarkdownSearchBar: true,
          CodeLinkPreview: true,
        },
      },
    })

    const filePath = wrapper.find('.chat-file-path.file-target')
    expect(filePath.exists()).toBe(true)
    await filePath.trigger('click')

    // Since handleVerifiedFilePathClick was mocked to false and dblClick was not triggered,
    // openFilePath should NOT be called directly by single clicking the file path text
    expect(openFilePath).not.toHaveBeenCalled()
  })

  it('opens the file and stops the click from reaching ancestor handlers', async () => {
    // Mirror production: useDoubleClickCopy resolves an anchor click
    // *synchronously* inside the click dispatch — that dispatch is the only
    // moment stopPropagation can still have an effect.
    handleDblClick.mockImplementation((_event: any, onOpenFile: any) => {
      onOpenFile('docs/guide.md', 5, 10)
    })

    const host = document.createElement('div')
    document.body.appendChild(host)
    const onAncestorClick = vi.fn()
    host.addEventListener('click', onAncestorClick)

    const wrapper = mount(MarkdownPreview, {
      attachTo: host,
      props: {
        file: { path: '/project/README.md', content: '[doc](docs/guide.md)' },
        viewMode: 'rendered',
      },
      global: {
        stubs: {
          TableRowModal: true,
          MarkdownSearchBar: true,
          CodeLinkPreview: true,
        },
      },
    })

    try {
      const filePath = wrapper.find('.chat-file-path.file-target')
      expect(filePath.exists()).toBe(true)
      await filePath.trigger('click')

      expect(closePreview).toHaveBeenCalled()
      expect(openFilePath).toHaveBeenCalledWith('docs/guide.md', 5, 10, 'file')
      // The shell registers its own click handlers above the preview; without
      // stopPropagation one tap would both open the file and trigger them.
      expect(onAncestorClick).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
      host.remove()
    }
  })

  it('intercepts a share link click before the auth-bound openFilePath fallback', async () => {
    // Share mode annotates relative links with data-share-path. A plain click
    // must dispatch the share-open-file event instead of falling through to
    // openFilePath (which resolves against an empty project root and calls
    // auth-protected endpoints an anonymous reader cannot use).
    pipelineHtml.value =
      `<p><a ${SHARE_PATH_ATTR}="/repo/docs/guide.md" href="/share/tok1?path=x">Guide</a></p>`

    const wrapper = mount(MarkdownPreview, {
      props: {
        file: { path: '/repo/README.md', content: '[Guide](./docs/guide.md)' },
        viewMode: 'rendered',
      },
      global: {
        stubs: { TableRowModal: true, MarkdownSearchBar: true, CodeLinkPreview: true },
      },
    })

    const opened: string[] = []
    const onOpen = (e: Event) => opened.push((e as CustomEvent).detail.path)
    window.addEventListener(SHARE_OPEN_FILE_EVENT, onOpen)
    try {
      await wrapper.find(`a[${SHARE_PATH_ATTR}]`).trigger('click')
    } finally {
      window.removeEventListener(SHARE_OPEN_FILE_EVENT, onOpen)
    }

    expect(opened).toEqual(['/repo/docs/guide.md'])
    expect(openFilePath).not.toHaveBeenCalled()
  })
})
