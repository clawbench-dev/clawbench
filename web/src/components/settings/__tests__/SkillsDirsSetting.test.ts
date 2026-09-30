import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SkillsDirsSetting from '@/components/settings/SkillsDirsSetting.vue'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// The local-directory card owns skills.dirs. The git-repo card is covered by
// SkillsReposSetting.test.ts.
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
  return mount(SkillsDirsSetting, { global: { plugins: [i18n] } })
}

describe('SkillsDirsSetting', () => {
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
    await wrapper.find('.skills-dir-add .fbtn-primary').trigger('click')
    await flushPromises()

    expect(setServerValue).toHaveBeenCalledWith('skills.dirs', ['/tmp/skills', '/tmp/more'])
  })

  it('removes a directory by patching the remaining array', async () => {
    const wrapper = mountSetting()
    await flushPromises()

    await wrapper.find('.skills-dir .fbtn').trigger('click')
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

  // The two cards persist different keys (skills.dirs vs skills.repos). They
  // used to share one error ref, so a directory validation failure painted the
  // error inside the git-repo card too.
  it('surfaces a save failure inside this card only', async () => {
    setServerValue.mockRejectedValueOnce(new Error('must be an absolute path'))
    const wrapper = mountSetting()
    await flushPromises()

    await wrapper.find('.skills-dir-add input').setValue('relative/path')
    await wrapper.find('.skills-dir-add .fbtn-primary').trigger('click')
    await flushPromises()

    expect(wrapper.find('.skills-error').exists()).toBe(true)
    expect(wrapper.text()).toContain('must be an absolute path')
  })

})
