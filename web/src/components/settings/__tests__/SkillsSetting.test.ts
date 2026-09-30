import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SkillsSetting from '@/components/settings/SkillsSetting.vue'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// The component reads skills.enabled from the shared settings store and writes
// it back through setServerValue; the directory list is edited inline and
// persisted as a whole array.
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
  return mount(SkillsSetting, {
    props: { description: 'desc' },
    global: { plugins: [i18n], stubs: { SettingsItem: { template: '<div class="settings-item" />' } } },
  })
}

describe('SkillsSetting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    serverValues = { 'skills.enabled': true }
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse()))
  })

  it('renders configured repos and discovered skills', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse(
      [{ url: 'https://github.com/org/skills.git', slug: 'skills-1234', has_token: true }],
      [{ name: 'demo', description: 'A demo', path: '/x/demo/SKILL.md', source_kind: 'git', source_label: 'skills-1234' }],
      1700000000,
    )))
    const wrapper = mountSetting()
    await flushPromises()

    expect(wrapper.text()).toContain('https://github.com/org/skills.git')
    expect(wrapper.text()).toContain('demo')
    expect(wrapper.text()).toContain('Repository skills-1234')
    // has_token renders a badge but never a value.
    expect(wrapper.find('.skills-repo-badge').exists()).toBe(true)
  })

  it('flags a skill whose name disagrees with its directory', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse([], [
      { name: 'good', description: 'ok', path: '/x/good/SKILL.md', source_kind: 'own', source_label: 'a1 native' },
      { name: 'wrong', description: 'bad', path: '/x/wrong-dir/SKILL.md', source_kind: 'own', source_label: 'a1 native', name_mismatch: true },
    ])))
    const wrapper = mountSetting()
    await flushPromises()

    const warns = wrapper.findAll('.skills-item-warn')
    expect(warns).toHaveLength(1)
    expect(wrapper.text()).toContain('Name mismatch')
  })

  it('shows the empty state when nothing was discovered', async () => {
    const wrapper = mountSetting()
    await flushPromises()
    expect(wrapper.text()).toContain('No skills discovered yet.')
  })

  it('adds a repo by patching the whole array', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse(
      [{ url: 'https://github.com/a/one.git', slug: 'one-1', has_token: false }],
    )))
    const wrapper = mountSetting()
    await flushPromises()

    await wrapper.find('.skills-repo-add input').setValue('https://github.com/b/two.git')
    await wrapper.find('.skills-repo-add .sbtn-primary').trigger('click')
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

    await wrapper.findAll('.skills-repo .sbtn')[0].trigger('click')
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
    await wrapper.find('.skills-repo-add .sbtn-primary').trigger('click')
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

    const refreshBtn = wrapper.findAll('.skills-sync .sbtn')[0]
    await refreshBtn.trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/skills/refresh', { method: 'POST' })
    expect(wrapper.text()).toContain('clone failed')
  })
})

describe('SkillsSetting — local directories', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    serverValues = { 'skills.enabled': true }
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse()))
  })

  it('renders one row per configured directory', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse()))
    const wrapper = mountSetting()
    await flushPromises()

    // The fixture reports a single resolved dir.
    expect(wrapper.findAll('.skills-dir')).toHaveLength(1)
    expect((wrapper.find('.skills-dir .skills-input').element as HTMLInputElement).value).toBe('/tmp/skills')
  })

  it('adds a directory by patching the whole array', async () => {
    const wrapper = mountSetting()
    await flushPromises()

    await wrapper.find('.skills-dir-add input').setValue('/tmp/more')
    await wrapper.find('.skills-dir-add .sbtn-primary').trigger('click')
    await flushPromises()

    expect(setServerValue).toHaveBeenCalledWith('skills.dirs', ['/tmp/skills', '/tmp/more'])
  })

  it('removes a directory by patching the remaining array', async () => {
    const wrapper = mountSetting()
    await flushPromises()

    await wrapper.find('.skills-dir .sbtn').trigger('click')
    await flushPromises()

    // Removing the only entry sends an empty array; the server then falls back
    // to the default directory.
    expect(setServerValue).toHaveBeenCalledWith('skills.dirs', [])
  })

  it('edits a directory in place and sends the full array', async () => {
    const wrapper = mountSetting()
    await flushPromises()

    const input = wrapper.find('.skills-dir .skills-input')
    await input.setValue('/tmp/renamed')
    await input.trigger('change')
    await flushPromises()

    expect(setServerValue).toHaveBeenCalledWith('skills.dirs', ['/tmp/renamed'])
  })
})
