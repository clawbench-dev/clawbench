/**
 * The login page shows the server version at the bottom, and the client
 * version as well when running inside a native shell.
 *
 * The two sources are easy to get wrong in ways no type checker catches:
 *   - the server version may only come from /api/health — /api/config needs
 *     auth, which is exactly what the user is on this page to obtain;
 *   - the client version comes from the native bridge and must NOT be shown on
 *     plain web, where it would either be missing or be the server's version
 *     masquerading as a client one.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'

const isAppMode = ref(false)
vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode, isDesktopApp: ref(false), isHarmonyApp: ref(false) }),
}))

const getNative = vi.fn()
vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => getNative(),
  isNativeApp: () => false,
  isDesktopApp: () => false,
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: { cancel: 'Cancel' },
      login: {
        slogan: 'slogan',
        subtitle: 'subtitle',
        savedServers: 'Saved servers',
        deleteServer: 'Delete server',
        serverNamePlaceholder: 'Name',
        serverUrlPlaceholder: 'Address',
        serverPasswordPlaceholder: 'Password',
        passwordPlaceholder: 'Password',
        submit: 'Login',
        addServer: 'Add server',
        addServerSubmit: 'Connect',
        verifying: 'Verifying...',
        versionServer: 'Server {v}',
        versionClient: 'Client {v}',
      },
    },
  },
})

/** fetch stub that answers /api/health and refuses everything else. */
function stubHealth(version: string | null, ok = true) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (!url.includes('/api/health')) return { ok: false, status: 401, json: async () => ({}) } as Response
    return {
      ok,
      status: ok ? 200 : 500,
      json: async () => (version === null ? {} : { app: 'clawbench', version }),
    } as Response
  }))
}

async function mountLogin() {
  const LoginView = (await import('@/components/LoginView.vue')).default
  const wrapper = mount(LoginView, {
    global: { plugins: [i18n], stubs: { IosInstallDrawer: true } },
  })
  await flushPromises()
  return wrapper
}

describe('LoginView — version line', () => {
  beforeEach(() => {
    localStorage.clear()
    isAppMode.value = false
    getNative.mockReset()
    getNative.mockReturnValue(undefined)
  })

  afterEach(() => {
    localStorage.clear()
    vi.unstubAllGlobals()
  })

  it('shows the server version read from the unauthenticated health probe', async () => {
    stubHealth('v0.130.2')
    const wrapper = await mountLogin()

    const line = wrapper.find('.login-versions')
    expect(line.exists()).toBe(true)
    expect(line.text()).toContain('Server v0.130.2')
    // Reads the public endpoint, not /api/config (which would 401 pre-login).
    expect(vi.mocked(fetch).mock.calls.some(c => String(c[0]).includes('/api/health'))).toBe(true)
    expect(vi.mocked(fetch).mock.calls.some(c => String(c[0]).includes('/api/config'))).toBe(false)
  })

  it('adds the client version in a native shell', async () => {
    stubHealth('v0.130.2')
    isAppMode.value = true
    getNative.mockReturnValue({ getAppVersion: async () => 'v0.129.0' })

    const wrapper = await mountLogin()

    const text = wrapper.find('.login-versions').text()
    expect(text).toContain('Server v0.130.2')
    expect(text).toContain('Client v0.129.0')
  })

  it('never renders a client version on plain web, even if a bridge exists', async () => {
    stubHealth('v0.130.2')
    // A stray bridge in a browser tab must not make the page claim a client.
    getNative.mockReturnValue({ getAppVersion: async () => 'v9.9.9' })

    const wrapper = await mountLogin()

    const text = wrapper.find('.login-versions').text()
    expect(text).toContain('Server v0.130.2')
    expect(text).not.toContain('Client')
    expect(getNative).not.toHaveBeenCalled()
  })

  it('hides the line when the probe fails instead of showing a placeholder', async () => {
    stubHealth('v0.130.2', false)
    const wrapper = await mountLogin()

    expect(wrapper.find('.login-versions').exists()).toBe(false)
  })

  it('survives a health response without a version field', async () => {
    stubHealth(null)
    const wrapper = await mountLogin()

    expect(wrapper.find('.login-versions').exists()).toBe(false)
  })
})
