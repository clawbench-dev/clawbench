import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'

// The component renders through `t()`; the label is not what we assert on (the
// i18n literal-key guard covers existence), so a passthrough keeps this focused.
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

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

import WelcomeOverlay from '@/components/WelcomeOverlay.vue'

const BACKENDS = [
  { id: 'codebuddy', name: 'CodeBuddy', specialty: 'Coding', default_cmd: 'codebuddy' },
  { id: 'claude', name: 'Claude Code', specialty: 'Coding', default_cmd: 'claude' },
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
    const wrapper = mount(WelcomeOverlay)
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
    const wrapper = mount(WelcomeOverlay)
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

    const wrapper = mount(WelcomeOverlay)
    await nextTick()
    wrapper.vm.forceShow()
    await flushPromises()
    await nextTick()

    expect(wrapper.find('.backends-loading').exists()).toBe(false)

    wrapper.unmount()
  })
})
