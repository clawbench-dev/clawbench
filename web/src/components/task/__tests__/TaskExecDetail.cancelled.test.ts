import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import TaskExecDetail from '../TaskExecDetail.vue'

/**
 * A cancelled task execution must show the "已中断" badge on its assistant
 * message.
 *
 * The badge is driven by the MESSAGE-level `cancelled` flag that the backend
 * stamps onto the assistant row when a run is abandoned — not by the
 * execution's `status`. Two consequences the component must honour:
 *
 * 1. A cancelled run whose reply has content must still show the badge
 *    (the execution row alone was previously not enough: the synthetic message
 *    hard-coded `cancelled: false`, so the badge never appeared).
 * 2. A run cancelled AFTER its reply finished keeps its reply unmarked, so the
 *    status alone must not stamp a badge.
 */

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
// Partial mock: the annotation module also registers a worktree-cache clearer
// at import time, and other modules on the render path call it. A narrow mock
// (only the two members this component uses) breaks that chain at import.
vi.mock('@/composables/useFilePathAnnotation.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useFilePathAnnotation.ts')>()
  return {
    ...actual,
    useFilePathAnnotation: () => ({ openFilePath: vi.fn(), verifyFilePaths: vi.fn(), readLineTargetFromEl: () => ({}) }),
  }
})
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
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({ getAgentBackend: () => 'acp', getAgentName: () => 'agent' }),
}))
vi.mock('@/stores/app.ts', () => ({
  store: { state: {}, loadGitBranch: vi.fn() },
}))
vi.mock('@/utils/taskExecUtils.ts', () => ({ terminateExecution: vi.fn() }))
vi.mock('@/utils/renderToolDetail.ts', () => ({ formatToolOutput: vi.fn() }))

// Keep the REAL content parser: the whole point of this file is that the
// message's `cancelled` flag survives parsing of the stored row. Only the
// rendering helpers around it are stubbed.
vi.mock('@/composables/useChatRender.ts', async () => {
  const { parseAssistantContent } = await import('@/utils/chatBlocks.ts')
  return {
    useChatRender: () => ({
      renderTextBlock: vi.fn(),
      toolCallSummary: vi.fn(),
      formatToolInput: vi.fn(),
      humanizeCron: vi.fn(),
      repeatLabel: vi.fn(),
      truncate: vi.fn(),
      hasImagesInContent: () => false,
      parseAssistantContent,
      formatDetailTime: vi.fn(),
    }),
  }
})

vi.mock('@/components/task/TaskBreadcrumb.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/RefreshButton.vue', () => ({ default: { template: '<button />' } }))
// Capture the flags the component hands to the message renderer. The badge
// itself lives inside ChatMessageItem/ContentBlocks; what this component owns is
// the synthetic message object, so the assertion is on that contract.
vi.mock('@/components/chat/ChatMessageItem.vue', () => ({
  default: {
    name: 'ChatMessageItem',
    props: { msg: Object },
    template: '<div class="msg-stub" :data-cancelled="String(!!msg.cancelled)" :data-streaming="String(!!msg.streaming)" />',
  },
}))
vi.mock('@/components/chat/ToolDetailDrawer.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/chat/ChatMetadataModal.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/SummaryToggle.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/TableRowModal.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/file/CodeLinkPreview.vue', () => ({ default: { template: '<div />' } }))

function mountWith(exec: Record<string, unknown>) {
  return mount(TaskExecDetail, { props: { execDetail: exec, taskId: 2 } })
}

describe('TaskExecDetail cancelled message', () => {
  it('shows the cancelled flag for a run whose reply was interrupted', () => {
    const exec = {
      id: 40,
      status: 'cancelled',
      createdAt: '2026-10-22T20:29:20Z',
      sessionId: 's-40',
      messageId: 64569,
      content: JSON.stringify({ blocks: [{ type: 'text', text: 'partial answer' }], cancelled: true }),
    }
    const wrapper = mountWith(exec)

    const stub = wrapper.find('.msg-stub')
    expect(stub.exists()).toBe(true)
    expect(stub.attributes('data-cancelled')).toBe('true')
    // The run is over: it must not still look like it is generating.
    expect(stub.attributes('data-streaming')).toBe('false')
  })

  it('does not mark a reply that completed before the execution was cancelled', () => {
    // A cancel that lands after the turn finished abandons the run, not the
    // reply — the assistant row carries no `cancelled` flag.
    const exec = {
      id: 41,
      status: 'cancelled',
      createdAt: '2026-10-22T20:29:20Z',
      sessionId: 's-41',
      messageId: 64570,
      content: JSON.stringify({ blocks: [{ type: 'text', text: 'full answer' }] }),
    }
    const wrapper = mountWith(exec)

    const stub = wrapper.find('.msg-stub')
    expect(stub.exists()).toBe(true)
    expect(stub.attributes('data-cancelled')).toBe('false')
  })

  it('does not mark a completed run', () => {
    const exec = {
      id: 42,
      status: 'completed',
      createdAt: '2026-10-22T20:00:00Z',
      sessionId: 's-42',
      messageId: 64571,
      content: JSON.stringify({ blocks: [{ type: 'text', text: 'done' }] }),
    }
    const wrapper = mountWith(exec)

    expect(wrapper.find('.msg-stub').attributes('data-cancelled')).toBe('false')
  })

  it('keeps the badge on the summary tab (same reply, different view)', () => {
    const exec = {
      id: 45,
      status: 'cancelled',
      createdAt: '2026-10-22T20:29:20Z',
      sessionId: 's-45',
      messageId: 64574,
      summary: 'a short summary of the partial answer',
      content: JSON.stringify({ blocks: [{ type: 'text', text: 'partial answer' }], cancelled: true }),
    }
    const wrapper = mountWith(exec)

    // A summary exists, so the detail page opens on the 摘要 tab.
    expect(wrapper.find('.exec-detail-tabs').exists()).toBe(true)
    expect(wrapper.find('.msg-stub').attributes('data-cancelled')).toBe('true')
  })

  it('stamps the badge immediately after a successful terminate, before the row carries the flag', async () => {
    // The refresh that follows the cancel request usually reads the row BEFORE
    // the executor's abandon path writes `cancelled:true`, and nothing refreshes
    // again — without the local stamp the badge would never appear until reload.
    const { terminateExecution } = await import('@/utils/taskExecUtils.ts')
    vi.mocked(terminateExecution).mockResolvedValue(true)

    const running = {
      id: 43,
      status: 'running',
      createdAt: '2026-10-22T21:00:00Z',
      sessionId: 's-43',
      messageId: 64572,
      content: JSON.stringify({ blocks: [{ type: 'text', text: 'in progress' }] }),
    }
    const wrapper = mountWith(running)
    expect(wrapper.find('.msg-stub').attributes('data-cancelled')).toBe('false')

    await wrapper.find('.exec-detail-actions .fbtn-danger').trigger('click')
    await flushPromises()

    // The parent's refresh returns the same pre-stamp row: status flipped to
    // cancelled, content still without the flag.
    await wrapper.setProps({ execDetail: { ...running, status: 'cancelled' } })

    const stub = wrapper.find('.msg-stub')
    expect(stub.attributes('data-cancelled')).toBe('true')
    expect(stub.attributes('data-streaming')).toBe('false')
  })

  it('does not stamp the badge when the terminate request fails', async () => {
    const { terminateExecution } = await import('@/utils/taskExecUtils.ts')
    vi.mocked(terminateExecution).mockResolvedValue(false)

    const running = {
      id: 44,
      status: 'running',
      createdAt: '2026-10-22T21:00:00Z',
      sessionId: 's-44',
      messageId: 64573,
      content: JSON.stringify({ blocks: [{ type: 'text', text: 'in progress' }] }),
    }
    const wrapper = mountWith(running)

    await wrapper.find('.exec-detail-actions .fbtn-danger').trigger('click')
    await flushPromises()
    await wrapper.setProps({ execDetail: { ...running, status: 'cancelled' } })

    expect(wrapper.find('.msg-stub').attributes('data-cancelled')).toBe('false')
  })
})
