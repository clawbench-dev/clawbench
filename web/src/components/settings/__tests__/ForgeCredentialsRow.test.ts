import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ForgeCredentialsRow from '@/components/settings/ForgeCredentialsRow.vue'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const setForgeToken = vi.fn(async () => ({ host: 'h', scheme: '', has_token: true }))
const deleteForgeToken = vi.fn(async () => {})
const verifyForgeToken = vi.fn(async () => ({ ok: true, identity: 'octocat', scheme: 'http' }))

vi.mock('@/utils/forgeApi', () => ({
  setForgeToken: (...a: unknown[]) => setForgeToken(...(a as [])),
  deleteForgeToken: (...a: unknown[]) => deleteForgeToken(...(a as [])),
  verifyForgeToken: (...a: unknown[]) => verifyForgeToken(...(a as [])),
  ForgeApiError: class ForgeApiError extends Error {},
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      settings: {
        items: {
          forgeTokenSet: 'Set',
          forgeTokenClear: 'Clear',
          forgeTokenSave: 'Save',
          forgeTokenVerify: 'Verify',
          forgeHostPlaceholder: 'Host',
          forgeTokenPlaceholder: 'Token',
          forgeVerifyOk: 'Valid — identity: {identity}',
          forgeVerifyOkScheme: 'Valid — identity: {identity} ({scheme})',
          forgeVerifyAuth: 'Rejected',
          forgeVerifyNetwork: 'Unreachable',
          forgeVerifyRateLimit: 'Rate limited',
          forgeVerifyNoToken: 'No token',
          forgeVerifyFailed: 'Failed',
        },
      },
    },
  },
})

/** Config response the component reads on mount. */
function configResponse(hosts: string[], schemes: Record<string, string> = {}) {
  return {
    ok: true,
    json: async () => ({ forge: { credential_hosts: hosts, credential_schemes: schemes } }),
  } as unknown as Response
}

function mountRow() {
  return mount(ForgeCredentialsRow, {
    props: { description: 'desc' },
    global: { plugins: [i18n] },
  })
}

describe('ForgeCredentialsRow', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal('fetch', vi.fn(async () => configResponse([])))
  })

  it('shows the resolved scheme for a configured host', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => configResponse(['gitlab.internal'], { 'gitlab.internal': 'http' })))
    const wrapper = mountRow()
    await flushPromises()

    // The scheme is the whole point of showing it: an http-only instance looks
    // identical to an https one otherwise.
    expect(wrapper.text()).toContain('gitlab.internal')
    expect(wrapper.find('.forge-cred-scheme').text()).toBe('http')
  })

  it('omits the scheme chip when the host has no recorded scheme', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => configResponse(['gitlab.internal'])))
    const wrapper = mountRow()
    await flushPromises()

    expect(wrapper.find('.forge-cred-scheme').exists()).toBe(false)
  })

  it('sends the host exactly as typed so the server can read the scheme', async () => {
    const wrapper = mountRow()
    await flushPromises()

    const inputs = wrapper.findAll('.forge-cred-input')
    await inputs[0].setValue('http://gitlab.internal')
    await inputs[1].setValue('glpat-x')

    // The URL is the reported failing input: it must be forwarded verbatim, not
    // lowercased or stripped here, so the server records the scheme.
    const saveBtn = wrapper.findAll('button').find(b => b.text() === 'Save')
    expect(saveBtn).toBeTruthy()
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(setForgeToken).toHaveBeenCalledWith('http://gitlab.internal', 'glpat-x')
  })

  it('reports the scheme used when verification succeeds', async () => {
    const wrapper = mountRow()
    await flushPromises()

    const inputs = wrapper.findAll('.forge-cred-input')
    await inputs[0].setValue('http://gitlab.internal')
    await inputs[1].setValue('glpat-x')

    const verifyBtn = wrapper.findAll('button').find(b => b.text() === 'Verify')
    await verifyBtn!.trigger('click')
    await flushPromises()

    // Which scheme the check actually used is part of the outcome: it is what
    // tells the user their http instance resolved as http.
    //
    // The assertion is on "(http)", not "http": the fallback wording contains
    // no scheme, and "https" also contains "http", so a looser check would pass
    // even if the component hardcoded https.
    expect(wrapper.text()).toContain('(http)')
    expect(wrapper.text()).not.toContain('(https)')
  })

  it('omits the scheme when the server did not report one', async () => {
    // A verify response with no scheme must fall back to the plain wording
    // rather than inventing one.
    verifyForgeToken.mockResolvedValueOnce({ ok: true, identity: 'octocat' })

    const wrapper = mountRow()
    await flushPromises()

    const inputs = wrapper.findAll('.forge-cred-input')
    await inputs[0].setValue('gitlab.internal')
    await inputs[1].setValue('glpat-x')

    const verifyBtn = wrapper.findAll('button').find(b => b.text() === 'Verify')
    await verifyBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('octocat')
    expect(wrapper.text()).not.toContain('(http')
  })

  it('clears the input after a successful save', async () => {
    const wrapper = mountRow()
    await flushPromises()

    const inputs = wrapper.findAll('.forge-cred-input')
    await inputs[0].setValue('http://gitlab.internal')
    await inputs[1].setValue('glpat-x')

    const saveBtn = wrapper.findAll('button').find(b => b.text() === 'Save')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect((inputs[0].element as HTMLInputElement).value).toBe('')
    expect((inputs[1].element as HTMLInputElement).value).toBe('')
  })

  it('does not save when either field is empty', async () => {
    const wrapper = mountRow()
    await flushPromises()

    const inputs = wrapper.findAll('.forge-cred-input')
    await inputs[0].setValue('gitlab.internal')

    const saveBtn = wrapper.findAll('button').find(b => b.text() === 'Save')
    expect(saveBtn!.attributes('disabled')).toBeDefined()
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(setForgeToken).not.toHaveBeenCalled()
  })
})
