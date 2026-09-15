import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskExecDetail from '../TaskExecDetail.vue'

/**
 * The execution detail page is a read-only record of one run. Two layout
 * guarantees matter and are easy to regress:
 *
 * 1. The 摘要/原文 tabs are pinned directly under the header and OUTSIDE the
 *    scroll container. Inside the scroller they would scroll away with the
 *    message (and a `position: sticky` bar could never reach the header, since
 *    the full-bleed event band renders above the message).
 * 2. The assistant bubble renders WITHOUT its bottom meta bar — fork/rewind
 *    have no handler here and the summary toggle duplicates the tab strip.
 */

// ── Mocks (mirrors TaskExecDetail.eventContext.test.ts) ──
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/i18n', () => ({
  default: { global: { t: (key: string) => key, locale: { value: 'en' } } },
}))

vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({
    name,
    props: { size: Number },
    template: `<svg :data-icon="'${name}'" />`,
  })
  return {
    MessageSquare: stub('MessageSquare'),
    Square: stub('Square'),
    Zap: stub('Zap'),
    ExternalLink: stub('ExternalLink'),
    ChevronDown: stub('ChevronDown'),
  }
})

vi.mock('@/composables/useTaskTab.ts', () => ({
  useTaskTab: () => ({ refreshExecDetail: vi.fn() }),
}))
vi.mock('@/composables/useSessionIdentity.ts', () => ({
  useSessionIdentity: () => ({ continueFromExecution: vi.fn() }),
}))
vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  useFilePathAnnotation: () => ({ openFilePath: vi.fn(), verifyFilePaths: vi.fn(), readLineTargetFromEl: () => ({}) }),
}))
vi.mock('@/composables/useCodeLinkPreview.ts', () => ({
  useCodeLinkPreview: () => ({ enabled: { value: false }, close: vi.fn() }),
  handleVerifiedFilePathClick: () => false,
}))
vi.mock('@/composables/useLocalhostAnnotation.ts', () => ({
  useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: () => false }),
}))
vi.mock('@/composables/useCodeBlockHeader.ts', () => ({
  handleCodeBlockClick: () => false,
  handleTableBlockClick: () => false,
}))
vi.mock('@/composables/useTableRowExpand.ts', () => ({
  useTableRowExpand: () => ({
    tableRowModal: {},
    closeTableRowModal: vi.fn(),
    tableRowPrev: vi.fn(),
    tableRowNext: vi.fn(),
    handleTableRowClick: () => false,
    onTableMouseDown: vi.fn(),
    onTableTouchStart: vi.fn(),
  }),
}))
vi.mock('@/composables/useAutoSpeech.ts', () => ({ useAutoSpeech: () => ({}) }))
vi.mock('@/composables/useToolDetailDrawer.ts', () => ({
  useToolDetailDrawer: () => ({
    drawer: { effectiveOpen: { value: false } },
    isOpen: { value: false },
    toolDetailOverlay: {},
    toolDetailData: { value: {} },
    activeToolOverlay: { value: null },
    handleShowToolDetail: vi.fn(),
    handleOverlayRetryClick: vi.fn(),
    handleFileOpenInOverlay: vi.fn(),
    fetchToolCallDetail: vi.fn(),
    closeOverlay: vi.fn(),
  }),
}))
vi.mock('@/composables/useTaskExecStream.ts', () => ({
  useTaskExecStream: () => ({
    isStreaming: { value: false },
    streamingMsg: { value: null },
    startPreview: vi.fn(),
    stopPreview: vi.fn(),
  }),
}))
vi.mock('@/composables/useChatRender.ts', () => ({
  useChatRender: () => ({
    renderTextBlock: vi.fn(),
    formatMessageTime: vi.fn(),
    toolCallSummary: vi.fn(),
    formatToolInput: vi.fn(),
    humanizeCron: vi.fn(),
    repeatLabel: vi.fn(),
    truncate: vi.fn(),
    hasImagesInContent: () => false,
    parseAssistantContent: () => ({ blocks: [{ type: 'text', text: 'answer' }], metadata: null }),
    formatDetailTime: vi.fn(),
  }),
}))
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({ getAgentBackend: () => 'acp', getAgentName: () => 'agent' }),
}))
vi.mock('@/stores/app.ts', () => ({
  store: { state: {}, loadGitBranch: vi.fn() },
}))
vi.mock('@/utils/taskExecUtils.ts', () => ({ terminateExecution: vi.fn() }))
vi.mock('@/utils/renderToolDetail.ts', () => ({ formatToolOutput: vi.fn() }))

vi.mock('@/components/task/TaskBreadcrumb.vue', () => ({ default: { template: '<div class="breadcrumb-stub" />' } }))
vi.mock('@/components/common/RefreshButton.vue', () => ({ default: { template: '<button />' } }))
// Real-ish stub that renders the tab-bar class the layout depends on, so the
// parent's :deep(.summary-toggle-bar) margin reset has a target.
vi.mock('@/components/common/SummaryToggle.vue', () => ({
  default: {
    name: 'SummaryToggle',
    props: ['mode', 'showingSummary', 'i18nPrefix'],
    template: '<div class="summary-toggle-bar" :data-mode="mode" />',
  },
}))
// Stub records the prop under test and renders a marker we can locate in the tree.
// hideSessionActions must be declared Boolean so the bare attribute is cast to
// true exactly as the real component does.
vi.mock('@/components/chat/ChatMessageItem.vue', () => ({
  default: {
    name: 'ChatMessageItem',
    props: {
      msg: Object,
      index: Number,
      expandedTools: Object,
      blockTasks: Object,
      blockAskQuestions: Object,
      hideSessionActions: { type: Boolean, default: false },
    },
    template: '<div class="msg-stub" :data-hide-session-actions="String(hideSessionActions)" />',
  },
}))
vi.mock('@/components/chat/ToolDetailDrawer.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/chat/ChatMetadataModal.vue', () => ({
  default: { name: 'ChatMetadataModal', template: '<div class="metadata-modal-stub" />' },
}))
vi.mock('@/components/common/TableRowModal.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/file/CodeLinkPreview.vue', () => ({ default: { template: '<div />' } }))

const completedExec = {
  id: 10,
  status: 'completed',
  createdAt: '2026-08-23T02:00:00Z',
  sessionId: 's-10',
  content: '{"blocks":[{"type":"text","text":"answer"}]}',
}

describe('TaskExecDetail pinned tabs', () => {
  it('renders the tab strip as a sibling between the header and the scroller', () => {
    const wrapper = mount(TaskExecDetail, {
      props: { execDetail: { ...completedExec, summary: 'A short summary' }, taskId: 2 },
    })

    const page = wrapper.find('.exec-detail-page')
    const children = Array.from(page.element.children).map(el => (el as HTMLElement).className)

    const headerIdx = children.findIndex(c => c.includes('exec-detail-header'))
    const tabsIdx = children.findIndex(c => c.includes('exec-detail-tabs'))
    const contentIdx = children.findIndex(c => c.includes('exec-detail-content'))

    expect(headerIdx).toBeGreaterThanOrEqual(0)
    expect(tabsIdx).toBe(headerIdx + 1)
    expect(contentIdx).toBe(tabsIdx + 1)
  })

  it('keeps the tab strip OUTSIDE the scroll container', () => {
    const wrapper = mount(TaskExecDetail, {
      props: { execDetail: { ...completedExec, summary: 'A short summary' }, taskId: 2 },
    })

    const content = wrapper.find('.exec-detail-content')
    expect(content.find('.exec-detail-tabs').exists()).toBe(false)
    expect(content.find('.summary-toggle-bar').exists()).toBe(false)
  })

  it('renders the tab bar in tab mode', () => {
    const wrapper = mount(TaskExecDetail, {
      props: { execDetail: { ...completedExec, summary: 'A short summary' }, taskId: 2 },
    })

    const bar = wrapper.find('.exec-detail-tabs .summary-toggle-bar')
    expect(bar.exists()).toBe(true)
    expect(bar.attributes('data-mode')).toBe('tab')
  })

  it('omits the tab strip when the execution has no summary', () => {
    const wrapper = mount(TaskExecDetail, {
      props: { execDetail: { ...completedExec }, taskId: 2 },
    })
    expect(wrapper.find('.exec-detail-tabs').exists()).toBe(false)
  })

  it('omits the tab strip while the execution is still running', () => {
    const wrapper = mount(TaskExecDetail, {
      props: { execDetail: { ...completedExec, status: 'running', summary: 'partial' }, taskId: 2 },
    })
    expect(wrapper.find('.exec-detail-tabs').exists()).toBe(false)
  })
})

describe('TaskExecDetail assistant bubble', () => {
  it('tells ChatMessageItem to hide only the fork/rewind pair', () => {
    const wrapper = mount(TaskExecDetail, {
      props: { execDetail: { ...completedExec, summary: 'A short summary' }, taskId: 2 },
    })

    const msg = wrapper.find('.msg-stub')
    expect(msg.exists()).toBe(true)
    expect(msg.attributes('data-hide-session-actions')).toBe('true')
  })

  // The details button stays visible, so the modal it opens must stay wired up.
  it('renders the metadata modal the details button opens', () => {
    const wrapper = mount(TaskExecDetail, {
      props: { execDetail: { ...completedExec, summary: 'A short summary' }, taskId: 2 },
    })
    expect(wrapper.findComponent({ name: 'ChatMetadataModal' }).exists()).toBe(true)
  })
})

/**
 * Layout assertions that jsdom cannot evaluate (scoped CSS is not applied by
 * the test environment), so they are pinned against the component source.
 */
describe('TaskExecDetail tab-strip CSS', () => {
  const getSource = async () => {
    const mod = await import('@/components/task/TaskExecDetail.vue?raw')
    return typeof mod.default === 'string' ? mod.default : ''
  }

  it('zeroes the tab bar margin so it sits flush against the message', async () => {
    const source = await getSource()
    expect(source).toContain('.exec-detail-tabs :deep(.summary-toggle-bar)')
    expect(source).toMatch(/\.exec-detail-tabs :deep\(\.summary-toggle-bar\)\s*\{[^}]*margin-bottom:\s*0/)
  })

  it('pins the strip with flex-shrink:0 so it never collapses with the scroller', async () => {
    const source = await getSource()
    expect(source).toMatch(/\.exec-detail-tabs\s*\{[^}]*flex-shrink:\s*0/)
  })

  it('adds no vertical padding of its own — the header border is the only separator', async () => {
    const source = await getSource()
    const block = source.match(/\.exec-detail-tabs\s*\{([^}]*)\}/)?.[1] ?? ''
    expect(block).not.toContain('padding')
    expect(block).not.toContain('margin')
  })
})
