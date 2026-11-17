import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

/**
 * The proxy (port mapping) dock tab must survive a failed `/api/ssh/info`.
 *
 * `usePortForward` leaves `sshInfo` null when the request throws, and it never
 * retries on its own. The dock's gate used to read that null as "SSH is off",
 * so one failed request at startup removed the tab for the rest of the
 * session — the only recovery was a page reload. The composable now reports
 * whether the answer actually arrived, and an unanswered request reads as
 * available (same rule as `useFeatureFlags`).
 */

const sshInfo = ref<{ enabled?: boolean; port?: number } | null>(null)
const sshInfoResolved = ref(false)
const transportAllowsH2 = ref(false)

vi.mock('@/composables/usePortForward', () => ({
  usePortForward: () => ({ sshInfo, sshInfoResolved, transportAllowsH2 }),
}))

vi.mock('@/stores/app', () => ({
  store: { state: { portForwardEnabledCount: 0 } },
}))

vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => ({ getCapabilities: () => ({ tunnel: true }) }),
}))

async function proxyPlugin() {
  return (await import('@/plugins/proxy/plugin')).default
}

describe('proxy dock plugin availability', () => {
  beforeEach(() => {
    sshInfo.value = null
    sshInfoResolved.value = false
    transportAllowsH2.value = false
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('keeps the tab when the SSH info request never landed', async () => {
    const plugin = await proxyPlugin()
    // Nothing has answered yet: unknown is not unavailable.
    expect(plugin.available()).toBe(true)
  })

  it('keeps the tab when the request FAILED (resolved, but no answer)', async () => {
    const plugin = await proxyPlugin()
    // This is the regression: an auth error / network hiccup / cold-start race
    // leaves sshInfo null forever, which used to hide the tab.
    sshInfoResolved.value = true
    sshInfo.value = null
    expect(plugin.available()).toBe(true)
  })

  it('hides the tab once the server answers that SSH is off and h2 is not allowed', async () => {
    const plugin = await proxyPlugin()
    sshInfoResolved.value = true
    sshInfo.value = { enabled: false }
    transportAllowsH2.value = false
    expect(plugin.available()).toBe(false)
  })

  it('keeps the tab on an h2-only install', async () => {
    const plugin = await proxyPlugin()
    sshInfoResolved.value = true
    sshInfo.value = { enabled: false }
    transportAllowsH2.value = true
    expect(plugin.available()).toBe(true)
  })

  it('keeps the tab when SSH is on', async () => {
    const plugin = await proxyPlugin()
    sshInfoResolved.value = true
    sshInfo.value = { enabled: true }
    expect(plugin.available()).toBe(true)
  })
})
