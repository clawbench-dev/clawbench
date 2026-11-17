import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'

// The component renders through `t()`; the label is not what we assert on (the
// i18n literal-key guard covers existence), so a passthrough keeps this focused.
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  // Keep the real module (createI18n etc.) so shared shells that import the
  // i18n singleton still load; only t() is stubbed.
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/utils/download', () => ({
  downloadByUrl: vi.fn(),
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({ rescanAgents: vi.fn().mockResolvedValue(undefined) }),
}))

vi.mock('@/composables/useBackHandler', () => ({
  registerBackHandler: () => () => {},
  PRIORITY_OVERLAY: 1000,
}))

vi.mock('@/composables/usePwaInstall', () => ({
  usePwaInstall: () => ({
    showPwaInstall: { value: false },
    showApkDownload: { value: false },
    canInstallPwa: { value: false },
    isIOS: { value: false },
    installPwa: vi.fn(),
  }),
}))

vi.mock('@/composables/useDesktopDownload', () => ({
  useDesktopDownload: () => ({
    isDesktop: false,
    currentDownloadUrl: () => '',
    loadLatest: vi.fn().mockResolvedValue(undefined),
    downloadDesktop: vi.fn(),
  }),
}))

vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', template: '<span class="agent-icon-stub" />' },
}))

// The layout step renders its tab chips from the plugin registry. Mocking it
// keeps this file about the wizard's own behaviour: the real registry pulls in
// every plugin module (and through them the chat session store), which this
// component has no other reason to touch.
vi.mock('@/composables/dockPluginRuntime', () => {
  const icon = { name: 'StubDockIcon', template: '<i class="stub-dock-icon" />' }
  return {
    DOCK_TABS_WITH_ICONS: [
      { id: 'browse', titleKey: 'nav.fileManager', icon },
      { id: 'view', titleKey: 'nav.fileViewer', icon },
      { id: 'forge', titleKey: 'nav.forge', icon },
      { id: 'terminal', titleKey: 'nav.terminal', icon },
      { id: 'settings', titleKey: 'nav.settings', icon },
    ],
  }
})

// `applyPreset` is the one write the wizard performs. The real one persists to
// localStorage and PATCHes the server; here we only need to know which preset
// was chosen, and that the overlay closes afterwards.
const { applyPresetMock } = vi.hoisted(() => ({
  applyPresetMock: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('@/composables/useDockPlugins', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useDockPlugins')>()
  return {
    ...actual,
    useDockPlugins: () => ({ preset: { value: 'developer' }, applyPreset: applyPresetMock }),
  }
})

vi.mock('@/components/AgentInstallDialog.vue', () => ({
  default: {
    name: 'AgentInstallDialog',
    props: ['backendId', 'backendName', 'installCmd'],
    emits: ['close', 'installed'],
    template: '<div class="mock-install-dialog" :data-backend="backendId" />',
  },
}))

import WelcomeOverlay from '@/components/WelcomeOverlay.vue'

const BACKENDS = [
  { id: 'codebuddy', name: 'CodeBuddy', specialty: 'Coding', default_cmd: 'codebuddy', install_cmd: 'npm install -g @tencent-ai/codebuddy-code' },
  { id: 'claude', name: 'Claude Code', specialty: 'Coding', default_cmd: 'claude', install_cmd: 'npm install -g @anthropic-ai/claude-code' },
]

/**
 * `/api/agents` runs a real backend detection scan and can take seconds. The
 * component only assigns `backends` once BOTH fetches resolve, so during that
 * window `sortedBackends` is empty. Before the fix the whole `.backends-list`
 * rendered blank — this test pins the section-level indicator that fills it.
 */
function mockFetchWithPendingAgents() {
  let resolveAgents!: (v: unknown) => void
  const agentsPending = new Promise((res) => {
    resolveAgents = res
  })

  const fetchMock = vi.fn(async (url: string) => {
    if (url === '/api/backends') {
      return { ok: true, json: async () => ({ backends: BACKENDS }) }
    }
    if (url === '/api/agents') {
      // Deliberately never resolves until the test says so.
      await agentsPending
      return { ok: true, json: async () => ({ agents: [] }) }
    }
    return { ok: true, json: async () => ({}) }
  })
  vi.stubGlobal('fetch', fetchMock)
  return { resolveAgents: (agents: unknown[]) => resolveAgents({ agents }) }
}

describe('WelcomeOverlay — backend detection loading indicator', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('shows a section-level spinner while agent detection is in flight', async () => {
    const { resolveAgents } = mockFetchWithPendingAgents()
    const wrapper = mount(WelcomeOverlay, { global: { stubs: { teleport: true } } })
    await nextTick()
    wrapper.vm.forceShow()
    await nextTick()

    // /api/agents is still pending, so the catalogue is empty and the old
    // implementation rendered nothing at all in this region.
    expect(wrapper.find('.backends-loading').exists()).toBe(true)
    expect(wrapper.findAll('.backend-item')).toHaveLength(0)

    // Drain the pending scan so the test leaves no dangling promise behind.
    resolveAgents([])
    await flushPromises()
    wrapper.unmount()
  })

  it('replaces the spinner with the backend list once detection resolves', async () => {
    const { resolveAgents } = mockFetchWithPendingAgents()
    const wrapper = mount(WelcomeOverlay, { global: { stubs: { teleport: true } } })
    await nextTick()
    wrapper.vm.forceShow()
    await nextTick()

    expect(wrapper.find('.backends-loading').exists()).toBe(true)

    resolveAgents([])
    await flushPromises()
    await nextTick()

    expect(wrapper.find('.backends-loading').exists()).toBe(false)
    expect(wrapper.findAll('.backend-item')).toHaveLength(BACKENDS.length)

    wrapper.unmount()
  })

  it('hides the spinner once loading ends even when the list is empty', async () => {
    // Pins the `loading` half of the gate: a `sortedBackends.length === 0`
    // -only condition would leave the spinner up forever on an empty catalogue.
    const fetchMock = vi.fn(async (url: string) => {
      if (url === '/api/backends') return { ok: true, json: async () => ({ backends: [] }) }
      if (url === '/api/agents') return { ok: true, json: async () => ({ agents: [] }) }
      return { ok: true, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(WelcomeOverlay, { global: { stubs: { teleport: true } } })
    await nextTick()
    wrapper.vm.forceShow()
    await flushPromises()
    await nextTick()

    expect(wrapper.find('.backends-loading').exists()).toBe(false)

    wrapper.unmount()
  })
})

describe('WelcomeOverlay — grid, filters and install entry points', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  function mockFetchResolved(agents: Array<{ id: string; backend: string }> = []) {
    const fetchMock = vi.fn(async (url: string) => {
      if (url === '/api/backends') return { ok: true, json: async () => ({ backends: BACKENDS }) }
      if (url === '/api/agents') return { ok: true, json: async () => ({ agents }) }
      return { ok: true, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)
    return fetchMock
  }

  async function mountLoaded(agents: Array<{ id: string; backend: string }> = []) {
    mockFetchResolved(agents)
    const wrapper = mount(WelcomeOverlay, { global: { stubs: { teleport: true } } })
    await nextTick()
    wrapper.vm.forceShow()
    await flushPromises()
    await nextTick()
    return wrapper
  }

  it('puts installed backends first and filters by state', async () => {
    const wrapper = await mountLoaded([{ id: 'agent-claude', backend: 'claude' }])

    const cards = wrapper.findAll('.backend-item')
    expect(cards).toHaveLength(2)
    expect(cards[0].attributes('data-backend')).toBe('claude')

    const tabs = wrapper.findAll('.segmented-tab')
    await tabs[1].trigger('click')
    expect(wrapper.findAll('.backend-item')).toHaveLength(1)
    expect(wrapper.find('.backend-item').attributes('data-backend')).toBe('claude')

    await tabs[2].trigger('click')
    expect(wrapper.findAll('.backend-item')).toHaveLength(1)
    expect(wrapper.find('.backend-item').attributes('data-backend')).toBe('codebuddy')

    wrapper.unmount()
  })

  it('filters by name or specialty through the search box', async () => {
    const wrapper = await mountLoaded()

    await wrapper.find('.welcome-search-input').setValue('codebuddy')
    expect(wrapper.findAll('.backend-item')).toHaveLength(1)
    expect(wrapper.find('.backend-item').attributes('data-backend')).toBe('codebuddy')

    await wrapper.find('.welcome-search-input').setValue('nope')
    expect(wrapper.findAll('.backend-item')).toHaveLength(0)
    expect(wrapper.find('.welcome-empty').exists()).toBe(true)

    wrapper.unmount()
  })

  it('opens the install dialog when an uninstalled card is clicked', async () => {
    const wrapper = await mountLoaded()

    await wrapper.find('[data-backend="codebuddy"]').trigger('click')

    const dialog = wrapper.findComponent({ name: 'AgentInstallDialog' })
    expect(dialog.exists()).toBe(true)
    expect(dialog.props('backendId')).toBe('codebuddy')

    wrapper.unmount()
  })

  it('starts a chat with the installed agent when its card is clicked', async () => {
    const wrapper = await mountLoaded([{ id: 'agent-claude', backend: 'claude' }])

    await wrapper.find('[data-backend="claude"]').trigger('click')

    expect(wrapper.emitted('startChat')).toEqual([['agent-claude']])
    expect((wrapper.vm as unknown as { visible: boolean }).visible).toBe(false)

    wrapper.unmount()
  })

  it('does not emit startChat for an installable card', async () => {
    const wrapper = await mountLoaded()

    await wrapper.find('[data-backend="codebuddy"]').trigger('click')

    expect(wrapper.emitted('startChat')).toBeUndefined()

    wrapper.unmount()
  })
})

describe('WelcomeOverlay — two-step wizard', () => {
  beforeEach(() => {
    localStorage.clear()
    applyPresetMock.mockClear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  async function mountLoaded() {
    const fetchMock = vi.fn(async (url: string) => {
      if (url === '/api/backends') return { ok: true, json: async () => ({ backends: BACKENDS }) }
      if (url === '/api/agents') {
        return { ok: true, json: async () => ({ agents: [{ id: 'agent-claude', backend: 'claude' }] }) }
      }
      return { ok: true, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mount(WelcomeOverlay, { global: { stubs: { teleport: true } } })
    await nextTick()
    wrapper.vm.forceShow()
    await flushPromises()
    await nextTick()
    return wrapper
  }

  it('opens on the install step and moves between the two steps', async () => {
    const wrapper = await mountLoaded()

    expect(wrapper.find('[data-step="1"]').exists()).toBe(true)
    expect(wrapper.find('[data-step="2"]').exists()).toBe(false)
    expect(wrapper.find('.btn-back').exists()).toBe(false)

    await wrapper.find('.btn-ok').trigger('click')
    await nextTick()
    // `v-if`, not `v-show`: jsdom caches computed styles, so an `isVisible()`
    // assertion taken before the switch keeps answering "visible" afterwards.
    expect(wrapper.find('[data-step="1"]').exists()).toBe(false)
    expect(wrapper.find('[data-step="2"]').exists()).toBe(true)
    expect(wrapper.find('.btn-back').exists()).toBe(true)

    await wrapper.find('.btn-back').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-step="1"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('shows the three presets with the tabs each one keeps', async () => {
    const wrapper = await mountLoaded()
    await wrapper.find('.btn-ok').trigger('click')
    await nextTick()

    const cards = wrapper.findAll('.preset-card')
    expect(cards.map((c) => c.attributes('data-preset'))).toEqual(['developer', 'standard', 'minimal'])

    // The current profile is `developer`, so the recommended preset is what the
    // user is offered — not "nothing selected".
    expect(wrapper.find('[data-preset="standard"]').classes()).toContain('is-selected')

    const chips = (preset: string) =>
      wrapper.findAll(`[data-preset="${preset}"] .preset-tab`).map((c) => c.text())

    expect(chips('developer')).toContain('nav.forge')
    expect(chips('developer')).toContain('nav.terminal')
    expect(chips('standard')).not.toContain('nav.forge')
    expect(chips('standard')).not.toContain('nav.terminal')
    // Minimal keeps the permanent tabs only (file manager, viewer, settings).
    expect(chips('minimal')).toEqual(['nav.fileManager', 'nav.fileViewer', 'nav.settings'])

    wrapper.unmount()
  })

  it('applies the chosen preset when the wizard is finished', async () => {
    const wrapper = await mountLoaded()
    await wrapper.find('.btn-ok').trigger('click')
    await wrapper.find('[data-preset="minimal"]').trigger('click')
    await wrapper.find('.btn-ok').trigger('click')
    await flushPromises()

    expect(applyPresetMock).toHaveBeenCalledTimes(1)
    expect(applyPresetMock.mock.calls[0][0]).toBe('minimal')
    expect(typeof applyPresetMock.mock.calls[0][1]).toBe('function')
    expect((wrapper.vm as unknown as { visible: boolean }).visible).toBe(false)

    wrapper.unmount()
  })

  it('writes nothing when the selection already matches the current profile', async () => {
    const wrapper = await mountLoaded()
    await wrapper.find('.btn-ok').trigger('click')
    await wrapper.find('[data-preset="developer"]').trigger('click')
    await wrapper.find('.btn-ok').trigger('click')
    await flushPromises()

    expect(applyPresetMock).not.toHaveBeenCalled()
    expect((wrapper.vm as unknown as { visible: boolean }).visible).toBe(false)

    wrapper.unmount()
  })
})
