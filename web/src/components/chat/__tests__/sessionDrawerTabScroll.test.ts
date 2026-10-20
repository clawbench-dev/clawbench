import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref, defineComponent } from 'vue'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The settings drawer has five tabs, which no longer fit a phone width. The
 * strip must scroll sideways instead of squeezing labels:
 *
 *   - touch pans natively (overflow-x + nowrap, no touch-action hijack);
 *   - a mouse drags (attachDragScroll, gated on REAL overflow);
 *   - the wheel scrolls the strip, not the drawer body.
 *
 * jsdom reports scrollWidth === clientWidth === 0, so the drag path cannot be
 * exercised through the DOM. These tests therefore pin (a) the markup/CSS
 * contract that makes native panning possible, and (b) that the wiring only
 * activates on measured overflow — the drag module itself has its own tests.
 */
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
  createI18n: () => ({ global: { t: (key: string) => key } }),
}))
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    props: { open: Boolean },
    emits: ['close'],
    template: '<div class="bs-stub"><slot name="header" /><slot /></div>',
  }),
}))
vi.mock('@/components/common/PopupMenu.vue', () => ({
  default: defineComponent({ props: { show: Boolean }, template: '<div />' }),
}))
vi.mock('@/components/common/RefreshButton.vue', () => ({
  default: defineComponent({ template: '<button />' }),
}))
vi.mock('@/components/common/ModelRow.vue', () => ({
  default: defineComponent({ props: ['name', 'modelId', 'active'], template: '<span class="model-row-stub" />' }),
}))
vi.mock('@/components/chat/ContextUsageDetail.vue', () => ({
  default: defineComponent({ props: ['usage', 'showCompact', 'compacting'], template: '<div class="detail-stub" />' }),
}))
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    getAgentModels: () => [],
    getAgentThinkingEffortLevels: () => [],
    getAgent: () => ({ name: 'Claude', preferredModel: '', preferredThinkingEffort: '', preferredMode: '' }),
    updateAgentField: vi.fn(),
    canRefreshModels: () => false,
    supportsACP: () => true,
    supportsCLI: () => true,
    getAgentTransport: () => 'acp-stdio',
  }),
  restoreOriginalModels: vi.fn(),
  resetModelViewForTransport: vi.fn(),
  applyRefreshedModelList: vi.fn(),
  populateACPStateFromCache: vi.fn().mockResolvedValue(undefined),
  invalidateACPStateCache: vi.fn(),
}))
vi.mock('@/composables/useSessionIdentity', () => ({
  useSessionIdentity: () => ({
    currentModelId: ref('m'),
    currentThinkingEffort: ref(''),
    currentModeId: ref(''),
    currentTransport: ref('acp-stdio'),
    availableThinkingEfforts: ref([]),
    availableModes: ref([]),
    autoApprove: ref(false),
    toggleAutoApprove: vi.fn(),
    cruise: ref(false),
    toggleCruise: vi.fn(),
    contextUsed: ref(0),
    contextSize: ref(0),
    contextInputTokens: ref(0),
    contextOutputTokens: ref(0),
    contextTotalTokens: ref(0),
    contextCachedReadTokens: ref(0),
    contextCachedWriteTokens: ref(0),
    contextThoughtTokens: ref(0),
    contextCost: ref(0),
    contextCurrency: ref(''),
    contextCacheCreationTokens: ref(0),
    contextCacheHitTokens: ref(0),
    contextCacheMissTokens: ref(0),
    contextCredit: ref(0),
    contextUsageByCategory: ref(undefined),
  }),
  clearModeState: vi.fn(),
  clearCommandState: vi.fn(),
  clearThinkingEffortState: vi.fn(),
}))
vi.mock('@/composables/useListNav', () => ({
  useListNav: () => ({ activeIndex: ref(0), reset: vi.fn() }),
}))
vi.mock('@/composables/useListKeys', () => ({ useListKeys: vi.fn() }))
vi.mock('@/composables/useSettingsConfig', () => ({ patchAgentPref: vi.fn() }))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: vi.fn() }) }))
vi.mock('@/utils/api', () => ({
  apiGet: vi.fn().mockResolvedValue({}),
  apiPatch: vi.fn().mockResolvedValue({}),
  apiPost: vi.fn().mockResolvedValue({}),
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))
vi.mock('@/composables/useGroupChat', () => ({ compactGroupMember: vi.fn() }))

import SessionDrawer from '@/components/chat/SessionDrawer.vue'

function mountDrawer() {
  return mount(SessionDrawer, { props: { open: true, agentId: 'claude' } })
}

describe('session drawer tab strip scrolling', () => {
  beforeEach(() => {
    // jsdom has no ResizeObserver; the component must tolerate its absence.
    vi.stubGlobal('ResizeObserver', undefined)
  })

  it('makes the strip horizontally scrollable instead of squeezing tabs', () => {
    const src = readWebFile('src/components/chat/SessionDrawer.vue')
    const block = src.slice(src.indexOf('.session-setting-tabs {'), src.indexOf('.session-setting-tabs.is-overflowing'))

    // Native panning is only possible with overflow-x + no wrap.
    expect(block).toContain('overflow-x: auto')
    expect(block).toContain('overflow-y: hidden')
    expect(block).toContain('overscroll-behavior-x: contain')
    // The scrollbar would be taller than the tab row on a phone.
    expect(block).toContain('scrollbar-width: none')
    expect(src).toContain('.session-setting-tabs::-webkit-scrollbar')

    // Tabs keep their natural width — `flex: 1` is what forced the squeeze.
    // Slice from the STYLE block: `.model-tab` also appears in the template.
    const styleStart = src.indexOf('<style')
    const tab = src
      .slice(src.indexOf('.model-tab {', styleStart), src.indexOf('.model-tab.active', styleStart))
      // Strip comments first: the rule's own comment explains why `flex: 1` was
      // wrong, and a naive substring check would match that explanation.
      .replace(/\/\*[\s\S]*?\*\//g, '')
    expect(tab).toContain('flex: 0 0 auto')
    expect(tab).toContain('white-space: nowrap')
    expect(tab).not.toContain('flex: 1')
  })

  it('does NOT hijack touch panning (a finger must keep the native gesture)', () => {
    // dragScroll skips touch by design; assert we did not add a competing
    // touch-action rule on the strip.
    const src = readWebFile('src/components/chat/SessionDrawer.vue')
    const block = src.slice(src.indexOf('.session-setting-tabs {'), src.indexOf('.model-tab {'))
    expect(block).not.toContain('touch-action')
  })

  it('stays inert when the tabs fit (no grab cursor, no drag handler)', async () => {
    const wrapper = mountDrawer()
    await nextTick()
    await nextTick()
    // jsdom: scrollWidth === clientWidth === 0 → measured as NOT overflowing.
    const strip = wrapper.find('.session-setting-tabs')
    expect(strip.classes()).not.toContain('is-overflowing')
  })

  it('routes the wheel to horizontal scrolling', () => {
    // Vue compiles @wheel into an attached listener, not an onwheel attribute,
    // so this is asserted at the source (the behaviour lives in the shared
    // onHorizontalWheel util, which has its own tests).
    const src = readWebFile('src/components/chat/SessionDrawer.vue')
    expect(src).toContain('@wheel="onHorizontalWheel"')
    expect(src).toContain("from '@/utils/horizontalWheelScroll'")
  })

  it('renders all five tabs inside the strip', () => {
    const wrapper = mountDrawer()
    expect(wrapper.findAll('.session-setting-tabs .model-tab')).toHaveLength(5)
  })
})
