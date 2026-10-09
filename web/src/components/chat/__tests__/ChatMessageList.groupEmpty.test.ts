import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, h } from 'vue'

// ── Group empty state: real mount, mode selection ──
// The source-guard test asserts the two modes' keys are referenced, but a
// swapped ternary (`props.groupMode === 'free' ? HOST : FREE`) would still
// reference both keys and pass. This mounts the component in each mode and
// asserts the RENDERED text differs — the only thing that pins the branch.

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
    setup(props) {
      return () => h('div', { class: 'cmi-stub' }, `msg-${props.msg?.id}`)
    },
  }),
}))
// The roster stack is not what this test pins; render an identifiable node.
vi.mock('@/components/common/AvatarStack.vue', () => ({
  default: defineComponent({
    name: 'AvatarStack',
    props: ['members'],
    setup() {
      return () => h('div', { class: 'avatar-stack-stub' })
    },
  }),
}))
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
vi.mock('@/composables/useTextSelection.ts', () => ({ useTextSelectionActive: () => ({ value: false }) }))
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

// The REAL locale strings, so the assertion is against what a user actually
// reads (a minimal stub could make both modes render the same placeholder).
import en from '@/i18n/locales/en'

// The locale values carry vue-i18n's linked-message escape `{'@'}` for a
// literal @ (a bare @ is parsed as syntax). The DOM shows the RESOLVED text,
// so the expected strings must resolve the escape the same way.
const resolve = (s: string) => s.replace(/\{'@'\}/g, '@')
const ml = Object.fromEntries(
  Object.entries(en.chat.messageList).map(([k, v]) => [k, resolve(v as string)]),
) as typeof en.chat.messageList

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en },
})

function mountEmptyGroup(props: Record<string, unknown> = {}) {
  return mount(ChatMessageList, {
    props: {
      messages: [],
      expandedTools: {},
      blockTasks: {},
      blockAskQuestions: {},
      agents: [{ id: 'a1', name: 'A' }],
      staticBlockCache: {},
      active: true,
      isGroupSession: true,
      groupMembers: [
        { id: 'm1', agentId: 'a1', name: 'A', backend: 'claude', isHost: true },
        { id: 'm2', agentId: 'a2', name: 'B', backend: 'claude', isHost: false },
      ],
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('ChatMessageList — group empty state by mode', () => {
  it('renders host-mode copy in host mode', () => {
    const wrapper = mountEmptyGroup({ groupMode: 'host' })
    const text = wrapper.find('.group-welcome').text()
    expect(text).toContain(ml.groupModeHostTitle)
    expect(text).toContain(ml.groupModeHostDesc)
    expect(text).toContain(ml.groupModeHostTip1)
    // It must NOT leak the free-mode copy.
    expect(text).not.toContain(ml.groupModeFreeTitle)
    expect(text).not.toContain(ml.groupModeFreeTip1)
  })

  it('renders free-mode copy in free mode', () => {
    const wrapper = mountEmptyGroup({ groupMode: 'free' })
    const text = wrapper.find('.group-welcome').text()
    expect(text).toContain(ml.groupModeFreeTitle)
    expect(text).toContain(ml.groupModeFreeDesc)
    expect(text).toContain(ml.groupModeFreeTip1)
    // It must NOT leak the host-mode copy.
    expect(text).not.toContain(ml.groupModeHostTitle)
    expect(text).not.toContain(ml.groupModeHostTip1)
  })

  it('defaults to host mode when groupMode is omitted', () => {
    const wrapper = mountEmptyGroup()
    expect(wrapper.find('.group-welcome').text()).toContain(ml.groupModeHostTitle)
  })

  it('lists the three mode tips', () => {
    const wrapper = mountEmptyGroup({ groupMode: 'free' })
    expect(wrapper.findAll('.group-welcome-tips li')).toHaveLength(3)
  })
})
