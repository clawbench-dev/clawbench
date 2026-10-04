import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ProjectDetailSetting from '@/components/settings/ProjectDetailSetting.vue'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

// The confirm dialog returns whatever the test sets here (the typed project
// name, or null for cancel).
const mockPrompt = vi.fn()
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ prompt: (...a: unknown[]) => mockPrompt(...a) }),
}))

vi.mock('@/stores/app', () => ({
  store: { state: { projectRoot: '/home/me/current' } },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: { loading: 'Loading…' },
      time: { justNow: 'just now', minutesAgo: '{count}m ago', hoursAgo: '{count}h ago', daysAgo: '{count}d ago', minutesFromNow: 'in {count}m', hoursFromNow: 'in {count}h', daysFromNow: 'in {count}d' },
      settings: {
        items: {
          projectLoadFailed: 'Failed to load project detail',
          projectDetailSessions: 'Sessions',
          projectDetailLastActive: 'Last active',
          projectDetailCreatedAt: 'Created',
          projectDetailDirStatus: 'Directory',
          projectDetailDirExists: 'Exists',
          projectDetailDirMissing: 'Deleted',
          projectDetailRepoKind: 'Repo kind',
          projectDetailNever: 'Never used',
          projectRepoMain: 'Main repo',
          projectRepoWorktree: 'Worktree',
          projectRepoSubdir: 'Subdirectory',
          projectRepoPlain: 'Not a repo',
          projectRepoUnknown: 'Unknown',
          projectSwitchTo: 'Switch to this project',
          projectSwitchCurrent: 'Current project',
          projectDelete: 'Delete project',
          projectDeleteConfirmTitle: 'Delete project',
          projectDeleteConfirmPrompt: 'Type {name} to confirm',
          projectDeleteNameMismatch: 'Project name does not match',
          projectDeleteFailed: 'Failed to delete project',
          projectSwitchFailed: 'Failed to switch project',
        },
      },
    },
  },
})

function detailResponse(over: Record<string, unknown> = {}) {
  return {
    ok: true,
    json: async () => ({
      id: 7,
      path: '/home/me/alpha',
      session_count: 3,
      last_active_at: '2026-01-01 00:00:00',
      created_at: '2025-12-01 00:00:00',
      exists: true,
      repo_kind: 'main',
      ...over,
    }),
  } as unknown as Response
}

function mountDetail(projectId = 7, provide: Record<string, unknown> = {}) {
  return mount(ProjectDetailSetting, {
    props: { projectId },
    global: { plugins: [i18n], provide },
  })
}

describe('ProjectDetailSetting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal('fetch', vi.fn(async () => detailResponse()))
  })

  it('fetches and renders the statistics', async () => {
    const fetchMock = vi.fn(async () => detailResponse())
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mountDetail(7)
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/projects/detail?id=7')
    expect(wrapper.text()).toContain('alpha')
    expect(wrapper.text()).toContain('Sessions')
    expect(wrapper.text()).toContain('3')
    expect(wrapper.text()).toContain('Main repo')
  })

  it('maps repo kinds to labels and unknown for a deleted dir', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => detailResponse({ exists: false, repo_kind: 'unknown' })))
    const wrapper = mountDetail()
    await flushPromises()

    expect(wrapper.text()).toContain('Unknown')
    expect(wrapper.text()).toContain('Deleted')
  })

  it('shows "never used" when there is no last-active time', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => detailResponse({ last_active_at: '' })))
    const wrapper = mountDetail()
    await flushPromises()
    expect(wrapper.text()).toContain('Never used')
  })

  it('re-fetches when the project id changes without a remount', async () => {
    const fetchMock = vi.fn(async (url: string) => {
      if (url.includes('id=7')) return detailResponse({ id: 7, path: '/home/me/alpha' })
      return detailResponse({ id: 8, path: '/home/me/beta' })
    })
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mountDetail(7)
    await flushPromises()
    expect(wrapper.text()).toContain('alpha')

    await wrapper.setProps({ projectId: 8 })
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/projects/detail?id=8')
    expect(wrapper.text()).toContain('beta')
    expect(wrapper.text()).not.toContain('alpha')
  })

  it('switches project and lands on the chat tab', async () => {
    const hotSwitch = vi.fn(async () => {})
    const switchTab = vi.fn()
    const wrapper = mountDetail(7, { hotSwitchProject: hotSwitch, switchTab })
    await flushPromises()

    const btn = wrapper.find('.project-detail__btn--primary')
    expect(btn.text()).toContain('Switch to this project')
    await btn.trigger('click')
    await flushPromises()

    expect(hotSwitch).toHaveBeenCalledWith('/home/me/alpha')
    expect(switchTab).toHaveBeenCalledWith('chat')
  })

  it('disables both actions for the current project', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => detailResponse({ path: '/home/me/current' })))
    const wrapper = mountDetail()
    await flushPromises()

    const primary = wrapper.find('.project-detail__btn--primary')
    expect(primary.text()).toContain('Current project')
    expect(primary.attributes('disabled')).toBeDefined()
    expect(wrapper.find('.project-detail__btn--danger').attributes('disabled')).toBeDefined()
  })

  it('deletes the project after the typed name matches', async () => {
    const fetchMock = vi.fn(async (url: string, opts?: RequestInit) => {
      if (opts?.method === 'DELETE') return { ok: true, json: async () => ({ ok: true }) } as unknown as Response
      return detailResponse()
    })
    vi.stubGlobal('fetch', fetchMock)
    mockPrompt.mockResolvedValue('alpha')

    const wrapper = mountDetail(7)
    await flushPromises()

    await wrapper.find('.project-detail__btn--danger').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/projects/detail?id=7', { method: 'DELETE' })
    expect(wrapper.emitted('deleted')).toBeTruthy()
  })

  it('does not delete when the typed name does not match', async () => {
    const fetchMock = vi.fn(async (url: string, opts?: RequestInit) => {
      if (opts?.method === 'DELETE') return { ok: true, json: async () => ({}) } as unknown as Response
      return detailResponse()
    })
    vi.stubGlobal('fetch', fetchMock)
    mockPrompt.mockResolvedValue('wrong-name')

    const wrapper = mountDetail(7)
    await flushPromises()

    await wrapper.find('.project-detail__btn--danger').trigger('click')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalledWith('/api/projects/detail?id=7', { method: 'DELETE' })
    expect(mockToastShow).toHaveBeenCalledWith('Project name does not match', expect.anything())
    expect(wrapper.emitted('deleted')).toBeFalsy()
  })

  it('does not delete when the dialog is cancelled', async () => {
    const fetchMock = vi.fn(async (url: string, opts?: RequestInit) => {
      if (opts?.method === 'DELETE') return { ok: true, json: async () => ({}) } as unknown as Response
      return detailResponse()
    })
    vi.stubGlobal('fetch', fetchMock)
    mockPrompt.mockResolvedValue(null)

    const wrapper = mountDetail(7)
    await flushPromises()

    await wrapper.find('.project-detail__btn--danger').trigger('click')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalledWith('/api/projects/detail?id=7', { method: 'DELETE' })
  })
})
