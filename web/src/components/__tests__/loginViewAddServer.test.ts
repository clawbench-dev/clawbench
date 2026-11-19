/**
 * Regression test for ISS-268.
 *
 * `LoginView.vue` destructured `findNameConflict` out of the `useServerList()`
 * factory return, but that object never exposed it — `findNameConflict` is a
 * top-level named export. The destructured binding was therefore `undefined`,
 * and the call at the top of `handleAddServer()` threw
 * `TypeError: findNameConflict is not a function` BEFORE `loading` was set.
 * The whole add-server action silently did nothing: no error text, no request,
 * no navigation.
 *
 * Nothing caught it: the SFC's script block has no `lang="ts"` (vue-tsc skips
 * it), there was no test file for LoginView at all, and destructuring a missing
 * property only blows up when the function is actually invoked.
 *
 * These tests mount the component and drive the real add-server flow, so a
 * regression back to "imported but not destructured" fails here.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'
import LoginView from '@/components/LoginView.vue'

// APP mode is required to reach the add-server button, and the duplicate-name
// check is the first statement of the handler.
vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: ref(true), isDesktopApp: ref(false) }),
}))

// Force the plain-web fallback (no native host) so handleAddServer reaches its
// own fetch() path instead of handing off to native connectToServer().
vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => undefined,
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
        duplicateServerName: 'Duplicate name',
        invalidServerAddress: 'Enter a valid address',
        wrongPassword: 'Wrong password',
      },
    },
  },
})

function mountLogin() {
  return mount(LoginView, {
    global: {
      plugins: [i18n],
      stubs: { IosInstallDrawer: true },
    },
  })
}

/** Fill the add-server form and submit it. */
async function submitAddServer(
  wrapper: ReturnType<typeof mountLogin>,
  name: string,
  url: string,
) {
  await wrapper.find('.add-server-btn').trigger('click')
  await wrapper.find('input[type="text"]:not(.server-url-input)').setValue(name)
  await wrapper.find('.server-url-input').setValue(url)
  await wrapper.find('form').trigger('submit')
  await flushPromises()
}

describe('LoginView — add server (ISS-268)', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    localStorage.clear()
    vi.unstubAllGlobals()
  })

  it('rejects a duplicate name with an error instead of silently doing nothing', async () => {
    localStorage.setItem('clawbench-servers', JSON.stringify([
      { url: 'http://existing:8080', password: 'pw', name: 'Home' },
    ]))

    const wrapper = mountLogin()
    await flushPromises() // onMounted → loadServers()

    await submitAddServer(wrapper, 'Home', 'http://new:8080')

    // The duplicate must be rejected before any network attempt...
    expect(fetch).not.toHaveBeenCalled()
    // ...and the user must see why. Before the fix the handler threw at the
    // guard, so no error element existed at all (the reported symptom).
    const errorEl = wrapper.find('.error')
    expect(errorEl.exists()).toBe(true)
    expect(errorEl.text()).toContain('Duplicate name')
  })

  it('proceeds to save and connect when the name is free', async () => {
    localStorage.setItem('clawbench-servers', JSON.stringify([
      { url: 'http://existing:8080', password: 'pw', name: 'Home' },
    ]))
    vi.mocked(fetch).mockResolvedValue({ ok: false, status: 401 } as Response)

    const wrapper = mountLogin()
    await flushPromises()

    await submitAddServer(wrapper, 'Lab', 'new.example.com')

    // Guard must return null for a free name and let the flow continue.
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(vi.mocked(fetch).mock.calls[0][0]).toBe('https://new.example.com:20000/login')

    // The server (with its name) is persisted under the normalized URL.
    const stored = JSON.parse(localStorage.getItem('clawbench-servers')!) as Array<{
      url: string
      name?: string
    }>
    expect(stored).toContainEqual(
      expect.objectContaining({ url: 'https://new.example.com:20000', name: 'Lab' }),
    )
  })

  // Issue #696: the address field used to be type="url", and a scheme-less IPv6
  // literal was glued into "https://240e:…" — not a URL, so it could never be
  // saved. IPv6 has no letters to key on, so a plain text field per the
  // manual's `http://[IPv6]:port` example is the only thing worth asserting.
  it('uses a plain text address field so an IPv6 literal is not rejected', async () => {
    const wrapper = mountLogin()
    await wrapper.find('.add-server-btn').trigger('click')
    const url = wrapper.find('.server-url-input')
    expect(url.exists()).toBe(true)
    expect(url.attributes('type')).toBe('text')
    // Typing hints stay: no autocorrect/autocapitalise mangling of an address.
    expect(url.attributes('inputmode')).toBe('url')
    expect(url.attributes('autocapitalize')).toBe('none')
  })

  it('saves a bracketed IPv6 URL exactly as the manual documents it', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: false, status: 401 } as Response)
    const wrapper = mountLogin()
    await flushPromises()

    await submitAddServer(wrapper, 'V6', 'http://[240e:390:1234:5678::123]:20000')

    expect(vi.mocked(fetch).mock.calls[0][0]).toBe('http://[240e:390:1234:5678::123]:20000/login')
    const stored = JSON.parse(localStorage.getItem('clawbench-servers')!) as Array<{ url: string }>
    expect(stored).toContainEqual(
      expect.objectContaining({ url: 'http://[240e:390:1234:5678::123]:20000' }),
    )
  })

  it('brackets a bare IPv6 literal instead of producing an unloadable URL', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: false, status: 401 } as Response)
    const wrapper = mountLogin()
    await flushPromises()

    await submitAddServer(wrapper, 'V6', '240e:390:1234:5678::123')

    expect(vi.mocked(fetch).mock.calls[0][0]).toBe(
      'https://[240e:390:1234:5678::123]:20000/login',
    )
  })

  it('shows an error and does not save an unusable address', async () => {
    const wrapper = mountLogin()
    await flushPromises()

    await submitAddServer(wrapper, 'Bad', 'http://[240e::123:20000')

    expect(fetch).not.toHaveBeenCalled()
    expect(localStorage.getItem('clawbench-servers')).toBeNull()
    expect(wrapper.find('.error').text()).toContain('Enter a valid address')
  })
})
