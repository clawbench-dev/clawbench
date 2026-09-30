import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SkillsReposSetting from '@/components/settings/SkillsReposSetting.vue'
import { useSkillsState } from '@/composables/useSkillsState'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// The git-repo card owns skills.repos (write-only tokens) and the manual sync
// button. The local-directory card is covered by SkillsDirsSetting.test.ts.
const setServerValue = vi.fn(async () => ({ needsRestart: false, changedColdFields: [], warnings: [] }))
let serverValues: Record<string, unknown> = {}

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    getServerValueWithDefault: (key: string) => serverValues[key],
    setServerValue: (...a: unknown[]) => setServerValue(...(a as [])),
  }),
}))

vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<span class="li" />' },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      settings: {
        items: {
          skillsEnabled: 'Enable skill injection',
          skillsEnabledDesc: 'desc',
          skillsDirs: 'Custom skill directories',
          skillsDirsDesc: 'desc',
          skillsDirPlaceholder: 'Absolute path',
          skillsRepos: 'Git skill repositories',
          skillsRepoUrlPlaceholder: 'Repo URL',
          skillsRepoTokenPlaceholder: 'Token',
          skillsRepoTokenSet: 'Token set',
          skillsRepoAdd: 'Add',
          skillsRepoRemove: 'Remove',
          skillsRefresh: 'Sync now',
          skillsRefreshing: 'Syncing…',
          skillsLastSync: 'Last synced: {time}',
          skillsDiscovered: '{count} skills discovered',
          skillsEmpty: 'No skills discovered yet.',
          skillsNameMismatch: 'Name mismatch',
          skillsNameMismatchDesc: 'desc',
          skillsSourceOwn: 'This agent (native)',
          skillsSourceUser: 'User directory',
          skillsSourceGit: 'Repository {name}',
          skillsSourceOther: 'Agent {agent}',
        },
      },
    },
  },
})

function skillsResponse(repos: unknown[] = [], skills: unknown[] = [], lastSyncAt = 0) {
  return {
    ok: true,
    json: async () => ({
      enabled: true,
      dirs: ['/tmp/skills'],
      refresh_hours: 6,
      last_sync_at: lastSyncAt,
      repos,
      skills,
    }),
  } as unknown as Response
}

function mountSetting() {
  return mount(SkillsReposSetting, { global: { plugins: [i18n] } })
}

describe('SkillsReposSetting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    serverValues = { 'skills.enabled': true }
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse()))
  })

  it('renders configured repos', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse(
      [{ url: 'https://github.com/org/skills.git', slug: 'skills-1234', has_token: true }],
      [],
      1700000000,
    )))
    const wrapper = mountSetting()
    await flushPromises()

    expect(wrapper.text()).toContain('https://github.com/org/skills.git')
    expect(wrapper.text()).toContain('Last synced')
    // has_token renders a badge but never a value.
    expect(wrapper.find('.skills-repo-badge').exists()).toBe(true)
    // The discovered listing lives in its own component/card now.
    expect(wrapper.find('.skills-item').exists()).toBe(false)
  })

  it('adds a repo by patching the whole array', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse(
      [{ url: 'https://github.com/a/one.git', slug: 'one-1', has_token: false }],
    )))
    const wrapper = mountSetting()
    await flushPromises()

    await wrapper.find('.skills-repo-add input').setValue('https://github.com/b/two.git')
    await wrapper.find('.skills-repo-add .fbtn-primary').trigger('click')
    await flushPromises()

    expect(setServerValue).toHaveBeenCalledWith('skills.repos', [
      { url: 'https://github.com/a/one.git', slug: 'one-1', token: '' },
      { url: 'https://github.com/b/two.git', slug: '', token: '' },
    ])
  })

  it('removes a repo by patching the remaining array', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse([
      { url: 'https://github.com/a/one.git', slug: 'one-1', has_token: false },
      { url: 'https://github.com/b/two.git', slug: 'two-2', has_token: false },
    ])))
    const wrapper = mountSetting()
    await flushPromises()

    await wrapper.findAll('.skills-repo .fbtn')[0].trigger('click')
    await flushPromises()

    expect(setServerValue).toHaveBeenCalledWith('skills.repos', [
      { url: 'https://github.com/b/two.git', slug: 'two-2', token: '' },
    ])
  })

  it('never pre-fills a stored token', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse(
      [{ url: 'https://github.com/a/one.git', slug: 'one-1', has_token: true }],
    )))
    const wrapper = mountSetting()
    await flushPromises()

    // The server never returns the token, so the field must start empty.
    const input = wrapper.find('.skills-repo .skills-input')
    expect((input.element as HTMLInputElement).value).toBe('')
  })

  it('sends a typed per-row token with the next patch', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse(
      [{ url: 'https://github.com/a/one.git', slug: 'one-1', has_token: true }],
    )))
    const wrapper = mountSetting()
    await flushPromises()

    await wrapper.find('.skills-repo .skills-input').setValue('new-token')
    await wrapper.find('.skills-repo-add input').setValue('https://github.com/b/two.git')
    await wrapper.find('.skills-repo-add .fbtn-primary').trigger('click')
    await flushPromises()

    const call = setServerValue.mock.calls.find(c => c[0] === 'skills.repos')
    expect(call).toBeTruthy()
    const payload = call![1] as Array<{ url: string; token: string }>
    expect(payload[0]).toEqual({ url: 'https://github.com/a/one.git', slug: 'one-1', token: 'new-token' })
  })

  it('posts to the refresh endpoint and surfaces per-repo errors', async () => {
    const fetchMock = vi.fn(async (url: string) => {
      if (url === '/api/skills/refresh') {
        return { ok: true, json: async () => ({ ok: false, skill_count: 1, errors: { broken: 'clone failed' } }) } as unknown as Response
      }
      return skillsResponse()
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountSetting()
    await flushPromises()

    const refreshBtn = wrapper.findAll('.skills-sync .fbtn')[0]
    await refreshBtn.trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/skills/refresh', { method: 'POST' })
    expect(wrapper.text()).toContain('clone failed')
  })

  // Isolated from the directory card's error state (they used to share one ref).
  //
  // useSkillsState is a MODULE-LEVEL singleton, so an earlier test's failure
  // would still be set here. Reset the two error refs rather than weakening the
  // assertion — the point is that THIS card shows nothing when its own save did
  // not fail.
  it('shows no error when nothing failed in this card', async () => {
    const { reposError, syncError, dirsError } = useSkillsState()
    reposError.value = ''
    syncError.value = ''
    // A directory-card error must not appear here at all.
    dirsError.value = 'directory card failed'

    const wrapper = mountSetting()
    await flushPromises()

    expect(wrapper.find('.skills-error').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('directory card failed')
  })

})
