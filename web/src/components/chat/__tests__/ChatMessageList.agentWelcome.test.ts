import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, h, ref } from 'vue'

// ── Empty-state agent glyph → agent config page ──
// The big glyph in a single-chat empty state is a BUTTON, not decoration: it
// opens the agent's config page, the same destination as the speaker avatar on
// a bubble and the header icon. The emit wiring (button → ChatMessageList →
// ChatPanelContent → App.vue) is guarded at source level; this file pins the
// thing a source guard cannot see — that the emitted VALUE is the agent id (not
// a member row id) and that the button disappears when there is no id, so a
// viewer never gets a control that opens nothing.

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

vi.mock('../ChatMessageItem.vue', () => ({
  default: defineComponent({
    name: 'ChatMessageItem',
    props: ['msg'],
    setup: () => () => h('div', { class: 'cmi-stub' }),
  }),
}))
vi.mock('@/components/common/AvatarStack.vue', () => ({
  default: defineComponent({ name: 'AvatarStack', setup: () => () => h('div') }),
}))
vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: defineComponent({
    name: 'AgentIcon',
    props: ['backend', 'name', 'avatar', 'size'],
    setup: () => () => h('span', { class: 'agent-icon-stub' }),
  }),
}))
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

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      session: { configAgent: 'Agent settings' },
      chat: {
        messageList: {
          backendTag: 'Backend',
          startConversation: 'Say something',
          noAgentsTitle: 'No agents',
          noAgentsDesc: '',
          noAgentsAction: 'Configure',
          startConversationAI: 'Start',
          startConversationAIDesc: '',
          groupModeHostTitle: '',
          groupModeHostDesc: '',
          groupModeHostTip1: '',
          groupModeHostTip2: '',
          groupModeHostTip3: '',
          groupModeFreeTitle: '',
          groupModeFreeDesc: '',
          groupModeFreeTip1: '',
          groupModeFreeTip2: '',
          groupModeFreeTip3: '',
          groupSoloMemberHint: '',
          groupSoloDisabledHint: '',
        },
        message: {},
      },
    },
  },
})

const AGENT = { id: 'agent-1', name: 'Claude', backend: 'claude', avatar: '', specialty: 'writes code', model: '' }

function mountEmpty(props: Record<string, unknown> = {}) {
  return mount(ChatMessageList, {
    props: {
      messages: [],
      expandedTools: {},
      blockTasks: {},
      blockAskQuestions: {},
      agents: [AGENT],
      currentAgent: AGENT,
      staticBlockCache: {},
      active: true,
      isGroupSession: false,
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

describe('ChatMessageList — empty-state agent glyph opens the agent config', () => {
  it('emits open-agent-config with the AGENT id when the glyph is clicked', async () => {
    const wrapper = mountEmpty()
    const btn = wrapper.find('.agent-welcome-icon-btn')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('type')).toBe('button')
    // The control announces what it does — an icon-only button with no label is
    // unreadable to a screen reader and a mystery on hover.
    expect(btn.attributes('title')).toBe('Agent settings')
    expect(btn.attributes('aria-label')).toBe('Agent settings')

    await btn.trigger('click')
    expect(wrapper.emitted('open-agent-config')?.[0]).toEqual(['agent-1'])
  })

  it('renders the glyph as a plain, non-clickable tile when the agent has no id', () => {
    const wrapper = mountEmpty({ currentAgent: { ...AGENT, id: '' } })
    expect(wrapper.find('.agent-welcome-icon').exists()).toBe(true)
    expect(wrapper.find('.agent-welcome-icon-btn').exists()).toBe(false)
    expect(wrapper.emitted('open-agent-config')).toBeUndefined()
  })

  it('keeps the glyph out of the group empty state (a group has no single agent to configure)', () => {
    const wrapper = mountEmpty({ isGroupSession: true, currentAgent: AGENT })
    expect(wrapper.find('.agent-welcome-icon-btn').exists()).toBe(false)
  })
})
