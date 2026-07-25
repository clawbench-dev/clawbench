import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import SessionSearchDetailModal from '@/components/session/SessionSearchDetailModal.vue'
import type { SessionSearchResult } from '@/composables/useSessionSearch'

// ── Mocks ────────────────────────────────────────────────────
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: {
    name: 'ModalDialog',
    template: '<div class="modal-stub"><slot /><slot name="footer" /></div>',
  },
}))

vi.mock('lucide-vue-next', () => ({
  ChevronRight: defineComponent({
    name: 'ChevronRight',
    render() { return h('span', { class: 'chevron-right-stub' }) },
  }),
}))

vi.mock('@/utils/searchUtils', () => ({
  highlightTextByPositions: (text: string, positions: { start: number; end: number }[]) => {
    if (!positions || positions.length === 0) return text
    return text + '<mark>highlighted</mark>'
  },
}))

vi.mock('@/utils/format', () => ({
  formatRelativeTime: (d: string) => d || 'now',
}))

const mockSession: SessionSearchResult = {
  session_id: 's1',
  session_title: 'Test Session',
  score: 0.95,
  backend: 'claude',
  project_path: '/home/test',
  deleted: false,
  created_at: '2026-01-15T10:00:00Z',
  match_count: 2,
  chunks: [
    {
      chunk_id: 1,
      chunk_text: 'Hello world this is a test',
      match_positions: [{ start: 0, end: 5 }],
      score: 0.9,
      role: 'user',
      message_id: 1,
      created_at: '2026-01-15T10:00:00Z',
    },
    {
      chunk_id: 2,
      chunk_text: 'This is the assistant reply',
      match_positions: [{ start: 0, end: 4 }],
      score: 0.85,
      role: 'assistant',
      message_id: 2,
      created_at: '2026-01-15T10:01:00Z',
    },
  ],
}

describe('SessionSearchDetailModal', () => {
  it('renders chunk text when session is provided', () => {
    const wrapper = mount(SessionSearchDetailModal, {
      props: {
        open: true,
        session: mockSession,
      },
    })

    expect(wrapper.text()).toContain('Hello world this is a test')
    expect(wrapper.text()).toContain('This is the assistant reply')
    expect(wrapper.text()).toContain('sessionSearch.roleUser')
    expect(wrapper.text()).toContain('sessionSearch.roleAssistant')
    expect(wrapper.text()).toContain('sessionSearch.resume')
  })

  it('renders session title and meta', () => {
    const wrapper = mount(SessionSearchDetailModal, {
      props: {
        open: true,
        session: mockSession,
      },
    })

    expect(wrapper.text()).toContain('Test Session')
    expect(wrapper.text()).toContain('claude')
    expect(wrapper.text()).toContain('sessionSearch.chunks')
  })

  it('emits resume with session when button is clicked', async () => {
    const wrapper = mount(SessionSearchDetailModal, {
      props: {
        open: true,
        session: mockSession,
      },
    })

    await wrapper.find('.search-detail-resume-btn').trigger('click')
    expect(wrapper.emitted('resume')).toBeTruthy()
    expect(wrapper.emitted('resume')![0]).toEqual([mockSession])
  })

  it('shows untitledSession fallback when session has no title', () => {
    const noTitleSession = { ...mockSession, session_title: '' }
    const wrapper = mount(SessionSearchDetailModal, {
      props: {
        open: true,
        session: noTitleSession,
      },
    })

    expect(wrapper.text()).toContain('sessionSearch.untitledSession')
  })

  it('renders chunk without match positions as plain text', () => {
    const sessionNoPositions: SessionSearchResult = {
      ...mockSession,
      chunks: [
        {
          chunk_id: 3,
          chunk_text: 'Plain text without highlights',
          match_positions: [],
          score: 0.7,
          role: 'user',
          message_id: 3,
          created_at: '2026-01-15T10:02:00Z',
        },
      ],
    }

    const wrapper = mount(SessionSearchDetailModal, {
      props: {
        open: true,
        session: sessionNoPositions,
      },
    })

    expect(wrapper.text()).toContain('Plain text without highlights')
  })
})
