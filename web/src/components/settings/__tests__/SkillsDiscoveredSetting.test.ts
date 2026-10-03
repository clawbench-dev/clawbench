import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SkillsDiscoveredSetting from '@/components/settings/SkillsDiscoveredSetting.vue'
import { loadSkills } from '@/composables/useSkillsState'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    getServerValueWithDefault: (key: string) => (key === 'skills.enabled' ? true : undefined),
    setServerValue: vi.fn(),
  }),
}))

// The jump primitive is mocked: what matters here is WHICH path and surface are
// passed (and that a missing path is never passed at all).
const revealInFileManager = vi.fn(async () => true)
vi.mock('@/composables/useFilePathAnnotation', () => ({
  revealInFileManager: (...a: unknown[]) => revealInFileManager(...(a as [])),
  // Mirrors the real contract: mark existing paths on the container, and mark
  // unknown ones as 'none' so the caller can tell them apart.
  verifyFilePaths: async (paths: string[], container: HTMLElement) => {
    for (const p of paths) {
      const el = container.querySelector(`[data-file-path="${p}"]`)
      if (!el) continue
      el.setAttribute('data-path-type', p.includes('missing') ? 'none' : 'file')
    }
  },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      settings: {
        items: {
          skillsDiscovered: '{count} total',
          skillsEmpty: 'No skills discovered yet.',
          skillsNameMismatch: 'Name mismatch',
          skillsNameMismatchDesc: 'desc',
          skillsSourceOwn: 'This agent (native)',
          skillsSourceUser: 'User directory',
          skillsSourceGit: 'Repository {name}',
          skillsSourceOther: 'Agent {agent}',
          skillsSourceShared: 'Generic',
          skillsOpenPath: 'Open in file manager',
          skillsPathMissing: 'File does not exist',
        },
      },
    },
  },
})

function skillsResponse(skills: unknown[]) {
  return {
    ok: true,
    json: async () => ({ enabled: true, dirs: [], refresh_hours: 6, last_sync_at: 0, repos: [], skills }),
  } as unknown as Response
}

function mountCard() {
  return mount(SkillsDiscoveredSetting, { global: { plugins: [i18n] } })
}

function row(over: Record<string, unknown> = {}) {
  return {
    name: 'demo',
    description: 'A demo skill',
    path: '/home/me/.agents/skills/demo/SKILL.md',
    source_kind: 'other',
    source_label: 'opencode native',
    agent_id: 'opencode',
    ...over,
  }
}

describe('SkillsDiscoveredSetting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse([])))
  })

  it('renders the count and each discovered skill', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse([row()])))
    const wrapper = mountCard()
    await flushPromises()

    expect(wrapper.text()).toContain('1 total')
    expect(wrapper.text()).toContain('demo')
    expect(wrapper.text()).toContain('A demo skill')
  })

  it('shows the empty state', async () => {
    const wrapper = mountCard()
    await flushPromises()
    expect(wrapper.text()).toContain('No skills discovered yet.')
  })

  // A skill from the cross-tool shared directory belongs to no single agent, so
  // naming whichever backend happens to read it would be misleading.
  it('labels a shared-directory skill as generic, not by agent', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse([
      row({ shared: true, agent_id: 'opencode', source_label: 'opencode native' }),
    ])))
    const wrapper = mountCard()
    await flushPromises()

    expect(wrapper.text()).toContain('Generic')
    expect(wrapper.text()).not.toContain('Agent opencode')
    expect(wrapper.text()).not.toContain('opencode native')
  })

  // The server omits agent_id for shared skills; even if one slipped through,
  // the label must stay "Generic" rather than rendering a roster.
  it('never renders an agent roster for a shared skill', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse([
      row({ shared: true, agent_id: 'codex,copilot,dsh,mimo,opencode,qoder', source_label: 'codex+copilot+dsh+mimo+opencode+qoder native' }),
    ])))
    const wrapper = mountCard()
    await flushPromises()

    expect(wrapper.text()).toContain('Generic')
    expect(wrapper.text()).not.toContain('codex,copilot')
    expect(wrapper.text()).not.toContain('codex+copilot')
  })

  it('still labels a genuinely agent-owned skill by its agent', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse([row()])))
    const wrapper = mountCard()
    await flushPromises()

    expect(wrapper.text()).toContain('Agent opencode')
    expect(wrapper.text()).not.toContain('Generic')
  })

  it('flags a name/directory mismatch', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse([
      row({ name: 'wrong', name_mismatch: true }),
    ])))
    const wrapper = mountCard()
    await flushPromises()

    expect(wrapper.findAll('.skills-item-warn')).toHaveLength(1)
    expect(wrapper.text()).toContain('Name mismatch')
  })

  it('opens an existing path in the file manager, recording the settings surface', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse([row()])))
    const wrapper = mountCard()
    await flushPromises()

    const btn = wrapper.find('.skills-item-path')
    expect(btn.attributes('disabled')).toBeUndefined()
    await btn.trigger('click')

    // The surface matters: it is what makes Back return to the settings page
    // instead of walking up the directory tree.
    expect(revealInFileManager).toHaveBeenCalledWith('/home/me/.agents/skills/demo/SKILL.md', 'settings')
  })

  // Adding a source in the configuration card reloads the shared skills list
  // (a local dir is rescanned by the server on PATCH; a new git repo is synced
  // right after being added). The newly listed paths were never verified, so
  // their jump buttons would stay disabled until the page reopened unless the
  // card re-verifies on change.
  it('re-verifies paths when the discovered list changes', async () => {
    const fetchMock = vi.fn(async () => skillsResponse([]))
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mountCard()
    await flushPromises()
    expect(wrapper.find('.skills-item-path').exists()).toBe(false)

    // Simulate the config card reloading the shared list with a new skill.
    fetchMock.mockImplementation(async () => skillsResponse([
      row({ path: '/home/me/.agents/skills/fresh/SKILL.md' }),
    ]))
    await loadSkills(true)
    await flushPromises()

    const btn = wrapper.find('.skills-item-path')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeUndefined()
  })

  it('disables the jump for a path that does not exist', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse([
      row({ path: '/home/me/.agents/skills/missing/SKILL.md' }),
    ])))
    const wrapper = mountCard()
    await flushPromises()

    const btn = wrapper.find('.skills-item-path')
    expect(btn.attributes('disabled')).toBeDefined()
    // The path text is still readable/copyable even when unopenable.
    expect(wrapper.text()).toContain('/home/me/.agents/skills/missing/SKILL.md')

    await btn.trigger('click')
    expect(revealInFileManager).not.toHaveBeenCalled()
  })
})
