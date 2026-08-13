import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, nextTick, defineComponent, h } from 'vue'
import type { AcpSessionInfo } from '@/composables/useAcpSession'

// ── Mocks ──

const mockAcpLoadSession = vi.fn()
const mockLoadAcpSessions = vi.fn()

// Controllable refs for useAcpSession mock
const mockSessions = ref<AcpSessionInfo[]>([])
const mockLoading = ref(false)
const mockNextCursor = ref<string | null>(null)

vi.mock('@/composables/useAcpSession', () => ({
  useAcpSession: () => ({
    acpSessions: mockSessions,
    acpSessionsLoading: mockLoading,
    acpResuming: ref(false),
    acpSessionsNotSupported: ref(false),
    nextCursor: mockNextCursor,
    loadAcpSessions: mockLoadAcpSessions,
    acpLoadSession: mockAcpLoadSession,
  }),
}))

// IntersectionObserver mock
const mockObserve = vi.fn()
const mockDisconnect = vi.fn()
let observerCallback: IntersectionObserverCallback | null = null

beforeEach(() => {
  observerCallback = null
  vi.stubGlobal('IntersectionObserver', class {
    constructor(cb: IntersectionObserverCallback) {
      observerCallback = cb
      return { observe: mockObserve, disconnect: mockDisconnect }
    }
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

vi.mock('@/composables/useSessionIdentity', () => ({
  currentAgentId: ref('agent-1'),
}))

const mockStoreState = vi.hoisted(() => ({ projectRoot: '/project' }))
vi.mock('@/stores/app.ts', () => ({
  store: { state: mockStoreState },
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    getAgentBackend: () => 'claude',
  }),
}))

vi.mock('@/utils/backendNames', () => ({
  getBackendDisplayName: (id: string) => (id === 'claude' ? 'Claude' : id),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('lucide-vue-next', () => ({
  Import: { name: 'ImportIcon', render: () => null },
  Loader2: {
    name: 'Loader2Icon',
    inheritAttrs: false,
    render: () => h('span', { class: 'loader2-icon' }),
  },
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    name: 'BottomSheet',
    props: { open: Boolean, title: String, auto: Boolean, instant: Boolean, compact: Boolean, noHeader: Boolean, handleOnly: Boolean, transparentOverlay: Boolean, fullscreen: Boolean, closeGuard: Boolean },
    emits: ['close'],
    inheritAttrs: true,
    template: `
      <div class="bottom-sheet-overlay">
        <div class="bottom-sheet">
          <div class="bs-header"><slot name="header" /></div>
          <div class="bs-body"><slot /></div>
          <div class="bs-footer"><slot name="footer" /></div>
        </div>
      </div>`,
  }),
}))

vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: defineComponent({
    name: 'AgentIcon',
    props: { backend: String, name: String, size: Number },
    template: '<span class="agent-icon" />',
  }),
}))

vi.mock('@/components/common/SearchInput.vue', () => ({
  default: defineComponent({
    name: 'SearchInput',
    props: { modelValue: String, placeholder: String },
    emits: ['update:modelValue'],
    template: '<input class="search-input-stub" :value="modelValue" />',
  }),
}))

import AcpSessionDrawer from '@/components/chat/AcpSessionDrawer.vue'

function mountDrawer() {
  return mount(AcpSessionDrawer, {
    props: { open: true, agentId: 'agent-1' },
  })
}

const testSession: AcpSessionInfo = { sessionId: 'acp-s1', title: 'Test', cwd: '/project', createdAt: '', updatedAt: '' }

describe('AcpSessionDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockSessions.value = []
    mockLoading.value = false
    mockNextCursor.value = null
    mockStoreState.projectRoot = '/project'
  })

  describe('handleSelect', () => {
    it('emits select and close when acpLoadSession returns a valid sessionId', async () => {
      mockAcpLoadSession.mockResolvedValue('new-session-123')

      const wrapper = mountDrawer()
      await (wrapper.vm as { handleSelect: (s: AcpSessionInfo) => Promise<void> }).handleSelect(testSession)
      await nextTick()

      expect(mockAcpLoadSession).toHaveBeenCalledWith('acp-s1')
      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual(['new-session-123'])
      expect(wrapper.emitted('close')).toBeTruthy()
    })

    it('does not emit select when acpLoadSession returns not-found', async () => {
      mockAcpLoadSession.mockResolvedValue('not-found')

      const wrapper = mountDrawer()
      await (wrapper.vm as { handleSelect: (s: AcpSessionInfo) => Promise<void> }).handleSelect(testSession)
      await nextTick()

      expect(mockAcpLoadSession).toHaveBeenCalledWith('acp-s1')
      expect(wrapper.emitted('select')).toBeFalsy()
      expect(wrapper.emitted('close')).toBeFalsy()
    })

    it('does not emit select when acpLoadSession returns null', async () => {
      mockAcpLoadSession.mockResolvedValue(null)

      const wrapper = mountDrawer()
      await (wrapper.vm as { handleSelect: (s: AcpSessionInfo) => Promise<void> }).handleSelect(testSession)
      await nextTick()

      expect(wrapper.emitted('select')).toBeFalsy()
      expect(wrapper.emitted('close')).toBeFalsy()
    })

    it('does not emit select when acpLoadSession returns empty string', async () => {
      mockAcpLoadSession.mockResolvedValue('')

      const wrapper = mountDrawer()
      await (wrapper.vm as { handleSelect: (s: AcpSessionInfo) => Promise<void> }).handleSelect(testSession)
      await nextTick()

      expect(wrapper.emitted('select')).toBeFalsy()
      expect(wrapper.emitted('close')).toBeFalsy()
    })
  })

  describe('current-project filtering', () => {
    const inProject = (id: string, title: string, cwd: string): AcpSessionInfo =>
      ({ sessionId: id, title, cwd, createdAt: '', updatedAt: '' })

    it('only renders sessions whose cwd exactly matches the current project root', async () => {
      mockSessions.value = [
        inProject('s1', 'Project Session', '/project'),
        inProject('s2', 'Other Project', '/other'),
        inProject('s3', 'Subdir Session', '/project/web/src'),
        inProject('s4', 'No Cwd', ''),
      ]

      const wrapper = mountDrawer()
      await nextTick()
      const text = wrapper.text()
      expect(text).toContain('Project Session')
      expect(text).not.toContain('Other Project')
      expect(text).not.toContain('Subdir Session')
      expect(text).not.toContain('No Cwd')
    })

    it('shows a hint counting hidden other-project sessions', async () => {
      mockSessions.value = [
        inProject('s1', 'Project Session', '/project'),
        inProject('s2', 'Other Project', '/other'),
        inProject('s3', 'Subdir Session', '/project/web/src'),
      ]

      const wrapper = mountDrawer()
      await nextTick()
      expect(wrapper.text()).toContain('chat.acpSession.hiddenInOtherProjects')
      expect((wrapper.vm as { hiddenOtherProjectCount: number }).hiddenOtherProjectCount).toBe(2)
    })

    it('matches the current project ignoring a trailing slash on cwd', async () => {
      mockStoreState.projectRoot = '/project/'
      mockSessions.value = [inProject('s1', 'Project Session', '/project')]

      const wrapper = mountDrawer()
      await nextTick()
      expect(wrapper.text()).toContain('Project Session')
    })
  })

  describe('loading indicator', () => {
    it('shows a spinner icon while initial loading with no sessions', async () => {
      mockLoading.value = true
      mockSessions.value = []

      const wrapper = mountDrawer()
      await nextTick()

      expect(wrapper.find('.acp-session-empty .loader2-icon').exists()).toBe(true)
      expect(wrapper.text()).toContain('chat.acpSession.loading')
    })

    it('does not show the initial-loading spinner once sessions have loaded', async () => {
      mockLoading.value = false
      mockSessions.value = [testSession]

      const wrapper = mountDrawer()
      await nextTick()

      expect(wrapper.find('.acp-session-empty .loader2-icon').exists()).toBe(false)
    })

    it('shows a spinner while loading more sessions with existing list', async () => {
      mockLoading.value = true
      mockSessions.value = [testSession]

      const wrapper = mountDrawer()
      await nextTick()

      expect(wrapper.find('.acp-session-loading-more .loader2-icon').exists()).toBe(true)
    })
  })

  describe('infinite scroll', () => {
    it('triggers loadMore when sentinel intersects and nextCursor exists', async () => {
      mockSessions.value = [testSession]
      mockNextCursor.value = 'cursor-1'

      const wrapper = mountDrawer()
      await nextTick()

      // Manually set sentinelRef to simulate the element being present,
      // then trigger the watcher by setting it again
      const vm = wrapper.vm as any
      const sentinelEl = document.createElement('div')
      vm.sentinelRef = sentinelEl
      await nextTick()

      // IntersectionObserver should have been set up
      expect(mockObserve).toHaveBeenCalledWith(sentinelEl)

      // Simulate sentinel becoming visible
      expect(observerCallback).toBeTruthy()
      observerCallback!([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)

      expect(mockLoadAcpSessions).toHaveBeenCalledWith('agent-1', true)
    })

    it('does not load more when loading is in progress', async () => {
      mockSessions.value = [testSession]
      mockNextCursor.value = 'cursor-1'
      mockLoading.value = true

      const wrapper = mountDrawer()
      await nextTick()

      const vm = wrapper.vm as any
      vm.sentinelRef = document.createElement('div')
      await nextTick()

      observerCallback!([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)

      // loadAcpSessions should NOT be called with append=true while loading
      expect(mockLoadAcpSessions).not.toHaveBeenCalledWith('agent-1', true)
    })

    it('does not load more when no nextCursor', async () => {
      mockSessions.value = [testSession]
      mockNextCursor.value = null

      const wrapper = mountDrawer()
      await nextTick()

      const vm = wrapper.vm as any
      vm.sentinelRef = document.createElement('div')
      await nextTick()

      observerCallback!([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)

      // No append call should happen
      expect(mockLoadAcpSessions).not.toHaveBeenCalledWith('agent-1', true)
    })

    it('does not load more when sentinel is not intersecting', async () => {
      mockSessions.value = [testSession]
      mockNextCursor.value = 'cursor-1'

      const wrapper = mountDrawer()
      await nextTick()

      const vm = wrapper.vm as any
      vm.sentinelRef = document.createElement('div')
      await nextTick()

      observerCallback!([{ isIntersecting: false } as IntersectionObserverEntry], {} as IntersectionObserver)

      expect(mockLoadAcpSessions).not.toHaveBeenCalledWith('agent-1', true)
    })

    it('disconnects observer on unmount', async () => {
      mockSessions.value = [testSession]

      const wrapper = mountDrawer()
      await nextTick()

      const vm = wrapper.vm as any
      vm.sentinelRef = document.createElement('div')
      await nextTick()
      expect(mockObserve).toHaveBeenCalled()

      wrapper.unmount()
      expect(mockDisconnect).toHaveBeenCalled()
    })
  })
})
