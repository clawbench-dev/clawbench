import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'

const { openFilePath, buildMarkdownPreviewDom, renderMermaidInElement } = vi.hoisted(() => ({
  openFilePath: vi.fn(),
  buildMarkdownPreviewDom: vi.fn(),
  renderMermaidInElement: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('@/composables/useMarkdownRenderPipeline.ts', () => ({
  buildMarkdownPreviewDom,
}))

vi.mock('@/composables/useMarkdownRenderer.ts', () => ({
  renderMermaidInElement,
}))

vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  useFilePathAnnotation: () => ({
    verifyFilePaths: vi.fn(),
    openFilePath,
    parseFileUri: (p: string) => ({ path: p, lineRanges: [] }),
    resolveRelativePath: (href: string) => href,
    readLineTargetFromEl: (el: Element) => ({
      filePath: el.getAttribute('data-file-path'),
      lineStart: undefined,
      lineEnd: undefined,
      lineRanges: undefined,
    }),
  }),
}))

vi.mock('@/composables/useCodeLinkPreview.ts', () => ({
  useCodeLinkPreview: () => ({
    enabled: ref(false),
    isTouchDevice: () => false,
    handleClick: vi.fn(),
    close: vi.fn(),
  }),
  handleVerifiedFilePathClick: vi.fn().mockReturnValue(false),
}))

vi.mock('@/composables/useDoubleClickCopy.ts', () => ({
  useDoubleClickCopy: () => ({ handleDblClick: vi.fn() }),
}))

vi.mock('@/composables/useChatContext.ts', () => ({
  useChatContext: () => ({
    removeAttachedFileByPath: vi.fn(),
    hasAttachedFile: vi.fn(),
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

vi.mock('@/composables/usePlatformDetect.ts', () => ({
  usePlatformDetect: () => ({ isTouchPrimary: ref(false) }),
}))

vi.mock('@/composables/useConversationTarget.ts', () => ({
  requestAttachmentTarget: vi.fn(),
}))

vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/stores/app.ts', () => ({
  store: { state: { projectRoot: '/project', homeDir: '/home/user' } },
}))

import MeetingMarkdown from '../MeetingMarkdown.vue'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })

const PATH = '/project/.clawbench/notes/2026-11-01-1430-review/minutes.md'

function mountBody(content = '# Hello') {
  return mount(MeetingMarkdown, {
    props: { content, path: PATH },
    global: {
      plugins: [i18n],
      stubs: { TableRowModal: true, CodeLinkPreview: true },
    },
    attachTo: document.body,
  })
}

beforeEach(() => {
  openFilePath.mockReset()
  buildMarkdownPreviewDom.mockReset()
  buildMarkdownPreviewDom.mockReturnValue({
    html: '<p class="body-text">Hello <span class="chat-file-path" data-file-path="docs/a.md" data-path-type="file">docs/a.md</span><span class="chat-file-open-btn" data-file-path="docs/a.md">open</span></p>',
    detectedPaths: ['docs/a.md'],
  })
})

describe('MeetingMarkdown', () => {
  it('renders through the file-preview pipeline with sanitize enabled', async () => {
    const wrapper = mountBody()
    await flushPromises()
    await nextTick()

    expect(buildMarkdownPreviewDom).toHaveBeenCalled()
    const [, opts] = buildMarkdownPreviewDom.mock.calls[0]
    // Minutes are AI-generated: unlike a trusted local file, they must be
    // sanitized.
    expect(opts).toMatchObject({ sanitize: true })
    const [source] = buildMarkdownPreviewDom.mock.calls[0]
    expect(source.path).toBe(PATH)
    expect(wrapper.find('.meeting-md').attributes('data-file-path')).toBe(PATH)
    expect(wrapper.find('.markdown-content').html()).toContain('Hello')
    wrapper.unmount()
  })

  it("opens an inline file path with the 'live' navigation source", async () => {
    const wrapper = mountBody()
    await flushPromises()
    await nextTick()

    await wrapper.find('.chat-file-open-btn').trigger('click')

    expect(openFilePath).toHaveBeenCalledWith('docs/a.md', undefined, undefined, 'live')
    wrapper.unmount()
  })

  it('emits copy on a desktop double-click of a text block', async () => {
    const wrapper = mountBody()
    await flushPromises()
    await nextTick()

    await wrapper.find('.body-text').trigger('dblclick')

    expect(wrapper.emitted('copy')?.[0]).toEqual(['Hello docs/a.mdopen'])
    wrapper.unmount()
  })

  it('does not copy when the double-click lands on interactive chrome', async () => {
    const wrapper = mountBody()
    await flushPromises()
    await nextTick()

    await wrapper.find('.chat-file-open-btn').trigger('dblclick')

    expect(wrapper.emitted('copy')).toBeUndefined()
    wrapper.unmount()
  })
})
