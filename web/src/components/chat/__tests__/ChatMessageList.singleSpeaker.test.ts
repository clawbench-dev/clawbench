import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, h, ref } from 'vue'

// ── Single-chat speaker header wiring ──
// ChatMessageItem renders the header from a `sessionSpeaker` identity, and it
// is ChatMessageList's job to derive that identity from the session's own
// agent. Mounting the list with a recording stub is the only way to pin the
// derivation (a source guard would pass on a broken object shape).

vi.mock('lucide-vue-next', async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>()
  const out: Record<string, unknown> = {}
  for (const key of Object.keys(actual)) {
    out[key] = defineComponent({ name: key, render: () => null })
  }
  return out
})

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    isOpen: { value: false },
    effectiveOpen: { value: false },
    open: vi.fn(),
    close: vi.fn(),
    toggle: vi.fn(),
  }),
}))

// Record the props the list hands to each message row.
const rowProps: Array<Record<string, unknown>> = []
vi.mock('../ChatMessageItem.vue', () => ({
  default: defineComponent({
    name: 'ChatMessageItem',
    props: ['msg', 'sessionSpeaker', 'isGroupSession'],
    setup(props) {
      rowProps.push(props as unknown as Record<string, unknown>)
      return () => h('div', { class: 'cmi-stub' })
    },
  }),
}))
vi.mock('@/components/common/AvatarStack.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/common/AgentIcon.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/common/LoadingIndicator.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/common/ProviderIcon.vue', () => ({ default: { render: () => null } }))
vi.mock('../UserMsgIndexDrawer.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/common/TableRowModal.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/file/CodeLinkPreview.vue', () => ({ default: { render: () => null } }))
vi.mock('@/composables/useCodeLinkPreview.ts', () => ({
  useCodeLinkPreview: () => ({ enabled: { value: false }, containerRef: null }),
  handleVerifiedFilePathClick: () => {},
}))
vi.mock('@/composables/useTextSelection.ts', () => ({ useTextSelectionActive: () => ({ active: { value: false } }) }))
vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  useFilePathAnnotation: () => ({ openFilePath: vi.fn(), readLineTargetFromEl: vi.fn() }),
}))
vi.mock('@/composables/useCodeBlockHeader.ts', () => ({
  handleCodeBlockClick: vi.fn(),
  handleTableBlockClick: vi.fn(),
  closeAllTableBlockMenus: vi.fn(),
}))
vi.mock('@/composables/useLocalhostAnnotation.ts', () => ({
  useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: vi.fn() }),
}))
vi.mock('@/composables/useDoubleClickCopy.ts', () => ({ useDoubleClickCopy: () => ({ handleDblClick: vi.fn() }) }))
vi.mock('@/composables/useDialog', () => ({ useDialog: () => ({ confirm: vi.fn() }) }))
vi.mock('@/composables/useUserMsgIndex.ts', () => ({
  useUserMsgIndex: () => ({
    // The chat-area rail renders this list permanently, so it must be an array
    // even when the test never opens the index.
    // A real ref, not a bare { value }: the rail reads it as a prop array.
    userMsgIndexList: ref([]),
    drawer: { effectiveOpen: { value: false }, isOpen: { value: false }, open: vi.fn(), close: vi.fn() },
    effectiveOpen: { value: false },
    isOpen: { value: false },
    showUserMsgIndex: { value: false },
    open: vi.fn(),
    close: vi.fn(),
    indexOpen: { value: false },
    openIndex: vi.fn(),
    closeIndex: vi.fn(),
    jumpToUserMessage: vi.fn(),
    loadingTarget: { value: null },
    loadingIndex: { value: false },
    ensureIndexLoaded: vi.fn(),
    refreshIndexIfStale: vi.fn(),
    hasBtw: () => false,
    remainingCount: { value: 0 },
  }),
}))
vi.mock('@/composables/useTableRowExpand.ts', () => ({
  useTableRowExpand: () => ({
    tableRowModal: { value: null },
    closeTableRowModal: vi.fn(),
    tableRowPrev: vi.fn(),
    tableRowNext: vi.fn(),
    handleTableRowClick: vi.fn(),
    onTableMouseDown: vi.fn(),
    onTableTouchStart: vi.fn(),
  }),
}))
vi.mock('@/stores/app.ts', () => ({ store: { state: {} } }))
vi.mock('@/utils/appLog', () => ({ appLog: { w: vi.fn(), e: vi.fn(), i: vi.fn() } }))
vi.mock('@/utils/domFlash', () => ({ flashElement: vi.fn() }))

import ChatMessageList from '../ChatMessageList.vue'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: { chat: { messageList: {} } } } })

function mountList(props: Record<string, unknown> = {}) {
  rowProps.length = 0
  return mount(ChatMessageList, {
    props: {
      messages: [{ role: 'assistant', id: 1, content: 'hi', blocks: [] }],
      expandedTools: {},
      blockTasks: {},
      blockAskQuestions: {},
      agents: [],
      staticBlockCache: {},
      active: true,
      isGroupSession: false,
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

describe('ChatMessageList — single-chat speaker identity', () => {
  it('derives the speaker from the session agent and forwards it to each row', () => {
    mountList({ currentAgent: { id: 'agent-1', name: 'Claude', backend: 'claude', avatar: '<svg/>' } })
    expect(rowProps).toHaveLength(1)
    expect(rowProps[0].sessionSpeaker).toEqual({
      name: 'Claude',
      backend: 'claude',
      avatar: '<svg/>',
      agentName: '',
      agentId: 'agent-1',
    })
  })

  it('passes no speaker while the agent list is still empty', () => {
    // The header must not flash a placeholder identity before agents load.
    mountList({ currentAgent: null })
    expect(rowProps[0].sessionSpeaker).toBeNull()
  })

  it('keeps the group session flag on the row', () => {
    mountList({ isGroupSession: true, currentAgent: { id: 'agent-1', name: 'Claude', backend: 'claude' } })
    expect(rowProps[0].isGroupSession).toBe(true)
  })
})
