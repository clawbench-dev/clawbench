import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ProjectDetailSetting from '@/components/settings/ProjectDetailSetting.vue'
import ProjectDialog from '@/components/ProjectDialog.vue'
import { useFileEditor, _resetForTesting } from '@/composables/useFileEditor'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

// The confirm dialog returns whatever the test sets here (the typed project
// name, or null for cancel). `confirm` is the boolean variant used when the
// directory is already gone.
const mockPrompt = vi.fn()
const mockConfirm = vi.fn()
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    prompt: (...a: unknown[]) => mockPrompt(...a),
    confirm: (...a: unknown[]) => mockConfirm(...a),
  }),
}))

vi.mock('@/stores/app', () => ({
  store: { state: { projectRoot: '/home/me/current', rootPaths: [], homeDir: '/home/me' } },
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
          projectRename: 'Change path',
          projectRenamePickTitle: 'Choose the project\'s new directory',
          projectRenameSuccess: 'Project path updated',
          projectRenameFailed: 'Failed to update project path',
          projectSwitchFailed: 'Failed to switch project',
          projectDelete: 'Delete project',
          projectDeleteConfirmTitle: 'Delete project',
          projectDeleteConfirmPrompt: 'Type {name} to confirm',
          projectDeleteConfirmPromptMissing: 'Directory for {name} is gone. Delete?',
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
    _resetForTesting()
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

  it('opens the directory picker from the rename button', async () => {
    const wrapper = mountDetail(7)
    await flushPromises()

    const renameBtn = wrapper.find('[data-action="project-rename"]')
    expect(renameBtn.text()).toContain('Change path')
    await renameBtn.trigger('click')

    const picker = wrapper.findComponent(ProjectDialog)
    expect(picker.props('open')).toBe(true)
    expect(picker.props('mode')).toBe('pick')
    // The project's directory was moved, so browsing starts at its parent.
    expect(picker.props('initialPath')).toBe('/home/me')
  })

  it('PATCHes the new path and re-points the current project', async () => {
    const fetchMock = vi.fn(async (url: string, opts?: RequestInit) => {
      if (opts?.method === 'PATCH') {
        return { ok: true, json: async () => ({ ok: true, id: 7, path: '/home/me/moved', old_path: '/home/me/current' }) } as unknown as Response
      }
      return detailResponse({ path: '/home/me/current' })
    })
    vi.stubGlobal('fetch', fetchMock)
    const hotSwitch = vi.fn(async () => {})
    const wrapper = mountDetail(7, { hotSwitchProject: hotSwitch })
    await flushPromises()

    wrapper.findComponent(ProjectDialog).vm.$emit('pick', '/home/me/moved')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/projects/detail?id=7', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: '/home/me/moved' }),
    })
    expect(hotSwitch).toHaveBeenCalledWith('/home/me/moved')
    expect(mockToastShow).toHaveBeenCalledWith('Project path updated', expect.anything())
  })

  it('aborts before PATCH when the editor leave guard is declined', async () => {
    const fetchMock = vi.fn(async (url: string, opts?: RequestInit) => {
      if (opts?.method === 'PATCH') {
        return { ok: true, json: async () => ({ ok: true, path: '/home/me/moved' }) } as unknown as Response
      }
      return detailResponse({ path: '/home/me/current' })
    })
    vi.stubGlobal('fetch', fetchMock)
    const hotSwitch = vi.fn(async () => {})
    const wrapper = mountDetail(7, { hotSwitchProject: hotSwitch })
    await flushPromises()
    useFileEditor().registerLeaveGuard(() => false)
    fetchMock.mockClear()

    wrapper.findComponent(ProjectDialog).vm.$emit('pick', '/home/me/moved')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalled()
    expect(hotSwitch).not.toHaveBeenCalled()
  })

  it('does not re-point the SPA when a different project is renamed', async () => {
    const fetchMock = vi.fn(async (url: string, opts?: RequestInit) => {
      if (opts?.method === 'PATCH') {
        return { ok: true, json: async () => ({ ok: true, id: 7, path: '/home/me/moved', old_path: '/home/me/alpha' }) } as unknown as Response
      }
      return detailResponse()
    })
    vi.stubGlobal('fetch', fetchMock)
    const hotSwitch = vi.fn(async () => {})
    const wrapper = mountDetail(7, { hotSwitchProject: hotSwitch })
    await flushPromises()

    wrapper.findComponent(ProjectDialog).vm.$emit('pick', '/home/me/moved')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/projects/detail?id=7', expect.objectContaining({ method: 'PATCH' }))
    expect(hotSwitch).not.toHaveBeenCalled()
  })

  it('reports a switch failure separately after the rename already succeeded', async () => {
    const fetchMock = vi.fn(async (url: string, opts?: RequestInit) => {
      if (opts?.method === 'PATCH') {
        return { ok: true, json: async () => ({ ok: true, id: 7, path: '/home/me/moved', old_path: '/home/me/current' }) } as unknown as Response
      }
      return detailResponse({ path: '/home/me/current' })
    })
    vi.stubGlobal('fetch', fetchMock)
    const hotSwitch = vi.fn(async () => { throw new Error('switch failed') })
    const wrapper = mountDetail(7, { hotSwitchProject: hotSwitch })
    await flushPromises()

    wrapper.findComponent(ProjectDialog).vm.$emit('pick', '/home/me/moved')
    await flushPromises()

    expect(mockToastShow).toHaveBeenCalledWith('Failed to switch project', expect.anything())
    expect(mockToastShow).not.toHaveBeenCalledWith('Project path updated', expect.anything())
    // The registry DID change, so the panel must show the new path even though
    // the SPA stayed on the old one.
    expect(fetchMock).toHaveBeenCalledWith('/api/projects/detail?id=7')
  })

  it('shows the server error and keeps the project when the rename is refused', async () => {
    const fetchMock = vi.fn(async (url: string, opts?: RequestInit) => {
      if (opts?.method === 'PATCH') {
        return { ok: false, json: async () => ({ error: 'another project already uses this path' }) } as unknown as Response
      }
      return detailResponse()
    })
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mountDetail(7)
    await flushPromises()

    wrapper.findComponent(ProjectDialog).vm.$emit('pick', '/home/me/taken')
    await flushPromises()

    expect(mockToastShow).toHaveBeenCalledWith('another project already uses this path', expect.anything())
    expect(wrapper.text()).toContain('alpha')
  })

  it('skips the request when the picked path is unchanged', async () => {
    const fetchMock = vi.fn(async () => detailResponse())
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mountDetail(7)
    await flushPromises()
    fetchMock.mockClear()

    wrapper.findComponent(ProjectDialog).vm.$emit('pick', '/home/me/alpha/')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalled()
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

  // A project whose directory is already gone only needs a confirmation — the
  // typed-name gate exists to protect a live project from a misclick, and there
  // is nothing live left here.
  it('deletes a missing-directory project after a plain confirmation (no name typing)', async () => {
    const fetchMock = vi.fn(async (url: string, opts?: RequestInit) => {
      if (opts?.method === 'DELETE') return { ok: true, json: async () => ({ ok: true }) } as unknown as Response
      return detailResponse({ exists: false, repo_kind: 'unknown' })
    })
    vi.stubGlobal('fetch', fetchMock)
    mockConfirm.mockResolvedValue(true)

    const wrapper = mountDetail(7)
    await flushPromises()

    await wrapper.find('.project-detail__btn--danger').trigger('click')
    await flushPromises()

    // Uses confirm(), never the typed-name prompt.
    expect(mockConfirm).toHaveBeenCalled()
    expect(mockPrompt).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledWith('/api/projects/detail?id=7', { method: 'DELETE' })
    expect(wrapper.emitted('deleted')).toBeTruthy()
  })

  it('does not delete a missing-directory project when the confirmation is declined', async () => {
    const fetchMock = vi.fn(async (url: string, opts?: RequestInit) => {
      if (opts?.method === 'DELETE') return { ok: true, json: async () => ({}) } as unknown as Response
      return detailResponse({ exists: false, repo_kind: 'unknown' })
    })
    vi.stubGlobal('fetch', fetchMock)
    mockConfirm.mockResolvedValue(false)

    const wrapper = mountDetail(7)
    await flushPromises()

    await wrapper.find('.project-detail__btn--danger').trigger('click')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalledWith('/api/projects/detail?id=7', { method: 'DELETE' })
    expect(wrapper.emitted('deleted')).toBeFalsy()
  })

  // An existing directory still requires the typed name — the relaxed path must
  // not leak into the live-project case.
  it('still requires the typed name for a live project', async () => {
    const fetchMock = vi.fn(async (url: string, opts?: RequestInit) => {
      if (opts?.method === 'DELETE') return { ok: true, json: async () => ({}) } as unknown as Response
      return detailResponse({ exists: true })
    })
    vi.stubGlobal('fetch', fetchMock)
    mockPrompt.mockResolvedValue('wrong-name')

    const wrapper = mountDetail(7)
    await flushPromises()

    await wrapper.find('.project-detail__btn--danger').trigger('click')
    await flushPromises()

    expect(mockPrompt).toHaveBeenCalled()
    expect(mockConfirm).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/projects/detail?id=7', { method: 'DELETE' })
  })
})
