import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskExecDetail from '../TaskExecDetail.vue'

// ── Mocks ──
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
    parseAssistantContent: () => ({ blocks: [], metadata: null }),
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

vi.mock('@/components/task/TaskBreadcrumb.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/RefreshButton.vue', () => ({ default: { template: '<button />' } }))
vi.mock('@/components/chat/ChatMessageItem.vue', () => ({ default: { template: '<div class="msg-stub" />' } }))
vi.mock('@/components/chat/ToolDetailDrawer.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/chat/ChatMetadataModal.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/SummaryToggle.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/TableRowModal.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/file/CodeLinkPreview.vue', () => ({ default: { template: '<div />' } }))

const baseExec = {
  id: 10,
  status: 'completed',
  createdAt: '2026-08-23T02:00:00Z',
  sessionId: 's-10',
  content: '{}',
}

describe('TaskExecDetail event trigger context', () => {
  it('shows the source link for an event-triggered run', () => {
    const exec = {
      ...baseExec,
      eventUrl: 'https://github.com/acme/widgets/pull/123',
      eventSummary: '## Forge 事件\n- 事件类型：merged',
    }
    const wrapper = mount(TaskExecDetail, { props: { execDetail: exec, taskId: 2 } })
    const link = wrapper.find('.exec-event-source')
    expect(link.exists()).toBe(true)
    expect(link.text()).toContain('acme/widgets PR #123')
    expect(link.attributes('href')).toBe('https://github.com/acme/widgets/pull/123')
  })

  it('keeps the injected context collapsed until toggled', async () => {
    const exec = {
      ...baseExec,
      eventUrl: 'https://github.com/acme/widgets/pull/123',
      eventSummary: '## Forge 事件\n- 事件类型：merged',
    }
    const wrapper = mount(TaskExecDetail, { props: { execDetail: exec, taskId: 2 } })
    expect(wrapper.find('.exec-event-context').exists()).toBe(false)

    await wrapper.find('.exec-event-toggle').trigger('click')
    const pre = wrapper.find('.exec-event-context')
    expect(pre.exists()).toBe(true)
    expect(pre.text()).toContain('事件类型：merged')
  })

  it('renders no event card for a run without an event payload', () => {
    const wrapper = mount(TaskExecDetail, { props: { execDetail: { ...baseExec }, taskId: 2 } })
    expect(wrapper.find('.exec-event-card').exists()).toBe(false)
  })
})
