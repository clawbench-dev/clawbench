import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import type { AcpSessionInfo } from '@/composables/useAcpSession'

// ── Mocks ──

const mockAcpLoadSession = vi.fn()
const mockLoadAcpSessions = vi.fn()

vi.mock('@/composables/useAcpSession', () => ({
  useAcpSession: () => ({
    acpSessions: ref([]),
    acpSessionsLoading: ref(false),
    acpResuming: ref(false),
    acpSessionsNotSupported: ref(false),
    nextCursor: ref(null),
    loadAcpSessions: mockLoadAcpSessions,
    acpLoadSession: mockAcpLoadSession,
  }),
}))

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
})
