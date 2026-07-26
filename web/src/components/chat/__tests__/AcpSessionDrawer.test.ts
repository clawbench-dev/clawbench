import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
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

vi.mock('lucide-vue-next', () => ({
  History: { name: 'HistoryIcon', render: () => null },
  RotateCw: { name: 'RotateCwIcon', render: () => null },
  Loader2: { name: 'Loader2Icon', render: () => null },
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div><slot name="header" /><slot /></div>',
    props: ['open', 'auto', 'title'],
    emits: ['close'],
  },
}))

import AcpSessionDrawer from '@/components/chat/AcpSessionDrawer.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: {} },
})

function mountDrawer() {
  return mount(AcpSessionDrawer, {
    props: { open: true, agentId: 'agent-1' },
    global: { plugins: [i18n] },
  })
}

const testSession: AcpSessionInfo = { sessionId: 'acp-s1', title: 'Test', createdAt: '', updatedAt: '' }

describe('AcpSessionDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockSessions.value = []
    mockLoading.value = false
    mockNextCursor.value = null
  })

  describe('handleSelect', () => {
    it('emits select and close when acpLoadSession returns a valid sessionId', async () => {
      mockAcpLoadSession.mockResolvedValue('new-session-123')

      const wrapper = mountDrawer()
      // Test handleSelect directly (no sessions in list by default)
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

  describe('infinite scroll', () => {
    it('triggers loadMore when sentinel intersects and nextCursor exists', async () => {
      mockSessions.value = [testSession]
      mockNextCursor.value = 'cursor-1'

      const wrapper = mountDrawer()
      await nextTick()

      // IntersectionObserver should have been set up
      expect(mockObserve).toHaveBeenCalled()

      // Simulate sentinel becoming visible
      expect(observerCallback).toBeTruthy()
      observerCallback!([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)

      expect(mockLoadAcpSessions).toHaveBeenCalledWith('agent-1', true)
    })

    it('does not load more when loading is in progress', async () => {
      mockSessions.value = [testSession]
      mockNextCursor.value = 'cursor-1'
      mockLoading.value = true

      mountDrawer()
      await nextTick()

      observerCallback!([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)

      // loadAcpSessions should NOT be called with append=true while loading
      expect(mockLoadAcpSessions).not.toHaveBeenCalledWith('agent-1', true)
    })

    it('does not load more when no nextCursor', async () => {
      mockSessions.value = [testSession]
      mockNextCursor.value = null

      mountDrawer()
      await nextTick()

      observerCallback!([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)

      // No append call should happen
      expect(mockLoadAcpSessions).not.toHaveBeenCalledWith('agent-1', true)
    })

    it('does not load more when sentinel is not intersecting', async () => {
      mockSessions.value = [testSession]
      mockNextCursor.value = 'cursor-1'

      mountDrawer()
      await nextTick()

      observerCallback!([{ isIntersecting: false } as IntersectionObserverEntry], {} as IntersectionObserver)

      expect(mockLoadAcpSessions).not.toHaveBeenCalledWith('agent-1', true)
    })

    it('disconnects observer on unmount', async () => {
      mockSessions.value = [testSession]

      const wrapper = mountDrawer()
      await nextTick()
      expect(mockObserve).toHaveBeenCalled()

      wrapper.unmount()
      expect(mockDisconnect).toHaveBeenCalled()
    })
  })
})
