import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const mockConfirm = vi.fn()
const mockToastShow = vi.fn()
const mockRefresh = vi.fn()

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

vi.mock('@/i18n', () => ({
  STORAGE_KEY: 'clawbench-locale',
  default: {
    global: {
      t: (key: string) => key,
      locale: { value: 'en' },
    },
  },
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: mockConfirm }),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/composables/useCrossProjectSessions', () => ({
  refresh: (...args: unknown[]) => mockRefresh(...args),
}))

import OtherProjectCards from '@/components/session/OtherProjectCards.vue'

describe('OtherProjectCards — missing directory & direct session destroy', () => {
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    mockConfirm.mockReset()
    mockToastShow.mockReset()
    mockRefresh.mockReset()
    mockRefresh.mockResolvedValue(undefined)
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  const missingGroup = {
    name: '/home/old/deleted-project',
    projectId: 42,
    dirMissing: true,
    displayName: 'deleted-project',
    displayPath: '/home/old/deleted-project',
    lastActiveAt: '2026-11-22T00:00:00Z',
    sessions: [
      {
        id: 'sess-ghost-1',
        title: 'Ghost Session',
        running: false,
        pendingApproval: false,
        unreadCount: 1,
        updatedAt: '2026-11-22T00:00:00Z',
      },
    ],
  }

  it('shows missing-directory badge and blocks opening a missing project row with a warning toast', async () => {
    const wrapper = mount(OtherProjectCards, {
      props: { groups: [missingGroup] },
    })

    expect(wrapper.find('.pcard-missing-tag').exists()).toBe(true)
    expect(wrapper.find('.pcard-cleanup-btn').exists()).toBe(true)

    await wrapper.find('.pcard-row').trigger('click')
    expect(wrapper.emitted('select')).toBeFalsy()
    expect(mockToastShow).toHaveBeenCalledWith(
      'session.dirMissingCannotOpen',
      expect.objectContaining({ type: 'warning' }),
    )
  })

  it('destroys a cross-project session directly via DELETE /api/ai/session/destroy', async () => {
    mockConfirm.mockResolvedValue(true)
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })
    globalThis.fetch = fetchMock as unknown as typeof fetch

    const wrapper = mount(OtherProjectCards, {
      props: { groups: [missingGroup] },
    })

    await wrapper.find('.pcard-row-delete').trigger('click')
    await nextTick()
    await Promise.resolve()

    expect(mockConfirm).toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/ai/session/destroy?session_id=sess-ghost-1',
      { method: 'DELETE' },
    )
    expect(mockRefresh).toHaveBeenCalled()
  })

  it('cleans up a missing project via DELETE /api/projects/detail?id=', async () => {
    mockConfirm.mockResolvedValue(true)
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })
    globalThis.fetch = fetchMock as unknown as typeof fetch

    const wrapper = mount(OtherProjectCards, {
      props: { groups: [missingGroup] },
    })

    await wrapper.find('.pcard-cleanup-btn').trigger('click')
    await nextTick()
    await Promise.resolve()

    expect(mockConfirm).toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/projects/detail?id=42',
      { method: 'DELETE' },
    )
    expect(mockRefresh).toHaveBeenCalled()
  })
})
