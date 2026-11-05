import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import { _resetLiveSFUConfigForTest } from '@/composables/useLiveSFUConfig'
import { apiGet, apiPatch, apiPost } from '@/utils/api'
import MeetingRelaySetting from '@/components/settings/MeetingRelaySetting.vue'

/**
 * 中继服务 (LiveKit), now edited in 设置 → 视频会议 instead of the create form.
 *
 * The rules that matter are the ones the move must not lose: the credential is
 * masked and only travels when the eye asks for it, a blank field is not sent
 * (so a stored credential survives), and nothing is saved unless the probe
 * passes — a stored-but-unreachable relay makes every later meeting fail at
 * start.
 */

vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiPatch: vi.fn(),
}))

const getMock = vi.mocked(apiGet)
const postMock = vi.mocked(apiPost)
const patchMock = vi.mocked(apiPatch)

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })

/** The dots the panel shows instead of a stored credential. */
const MASK = '•'.repeat(12)

function mountPanel() {
  return mount(MeetingRelaySetting, { global: { plugins: [i18n] } })
}

beforeEach(() => {
  _resetLiveSFUConfigForTest()
  getMock.mockReset()
  postMock.mockReset()
  patchMock.mockReset()
  getMock.mockResolvedValue({ url: '', hasSecret: false, configured: false })
})

afterEach(() => {
  _resetLiveSFUConfigForTest()
})

describe('MeetingRelaySetting', () => {
  it('shows the stored endpoint and says it is configured', async () => {
    getMock.mockResolvedValue({ url: 'wss://relay.example.com', hasSecret: true, configured: true })
    const wrapper = mountPanel()
    await flushPromises()

    expect((wrapper.find('[data-role="relay-url"]').element as HTMLInputElement).value).toBe(
      'wss://relay.example.com',
    )
    expect(wrapper.find('[data-role="relay-state"]').text()).toBe(
      enMessages.settings.items.meetingRelayConfigured,
    )
    // The credential never comes back with the plain read: both fields show
    // dots, so "a credential is saved" is visible without fetching it.
    expect((wrapper.find('[data-role="relay-key"]').element as HTMLInputElement).value).toBe(MASK)
    const secret = wrapper.find('[data-role="relay-secret"]').element as HTMLInputElement
    expect(secret.value).toBe(MASK)
    expect(secret.placeholder).toBe(enMessages.settings.items.meetingRelaySecretStored)
    // Masked means NOT fetched: reading the panel must not pull the plaintext.
    expect(getMock).not.toHaveBeenCalledWith('/api/live-share/sfu?reveal=1')
  })

  it('reveals the stored credential only when the eye asks', async () => {
    getMock.mockResolvedValueOnce({ url: 'wss://relay.example.com', hasSecret: true, configured: true })
    getMock.mockResolvedValueOnce({
      url: 'wss://relay.example.com',
      hasSecret: true,
      configured: true,
      apiKey: 'lk-key',
      apiSecret: 'lk-secret',
    })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="relay-key-toggle"]').trigger('click')
    await flushPromises()

    expect(getMock).toHaveBeenLastCalledWith('/api/live-share/sfu?reveal=1')
    const key = wrapper.find('[data-role="relay-key"]').element as HTMLInputElement
    const secret = wrapper.find('[data-role="relay-secret"]').element as HTMLInputElement
    expect(key.value).toBe('lk-key')
    // The pair is one request, so revealing either field fills both.
    expect(secret.value).toBe('lk-secret')
    expect(key.type).toBe('text')

    // Hiding goes back to bullets without dropping the value.
    await wrapper.find('[data-action="relay-key-toggle"]').trigger('click')
    expect((wrapper.find('[data-role="relay-key"]').element as HTMLInputElement).type).toBe('password')
  })

  it('says so when the server cannot reveal, instead of showing empty fields', async () => {
    getMock.mockResolvedValue({ url: 'wss://relay.example.com', hasSecret: true, configured: true })
    const wrapper = mountPanel()
    await flushPromises()

    // An older binary ignores `reveal` and answers with the plain read: empty
    // fields would read as "the credential is gone".
    await wrapper.find('[data-action="relay-key-toggle"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-role="relay-result"]').text()).toBe(
      enMessages.settings.items.meetingRelayRevealFailed,
    )
    expect((wrapper.find('[data-role="relay-key"]').element as HTMLInputElement).value).toBe(MASK)
  })

  it('never treats the mask as the credential', async () => {    getMock.mockResolvedValue({ url: 'wss://relay.example.com', hasSecret: true, configured: true })
    postMock.mockResolvedValue({ success: true, message: 'connected' })
    patchMock.mockResolvedValue({ url: 'wss://relay.example.com', hasSecret: true, configured: true })
    const wrapper = mountPanel()
    await flushPromises()

    // Clicking into the mask without selecting it and typing leaves bullets in
    // the box; they are stripped rather than sent as part of the key.
    await wrapper.find('[data-role="relay-key"]').setValue(MASK + 'new-key')
    expect((wrapper.find('[data-role="relay-key"]').element as HTMLInputElement).value).toBe('new-key')

    await wrapper.find('[data-action="relay-save-test"]').trigger('click')
    await flushPromises()

    expect(postMock).toHaveBeenCalledWith('/api/live-share/sfu/test', {
      url: 'wss://relay.example.com',
      apiKey: 'new-key',
      apiSecret: '',
    })
  })

  it('tests first, then saves, and keeps a stored secret untouched', async () => {
    getMock.mockResolvedValue({ url: 'wss://relay.example.com', hasSecret: true, configured: true })
    postMock.mockResolvedValue({ success: true, message: 'connected' })
    patchMock.mockResolvedValue({
      url: 'wss://relay2.example.com',
      hasSecret: true,
      configured: true,
    })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-role="relay-url"]').setValue('wss://relay2.example.com')
    await wrapper.find('[data-action="relay-save-test"]').trigger('click')
    await flushPromises()

    expect(postMock).toHaveBeenCalledWith('/api/live-share/sfu/test', {
      url: 'wss://relay2.example.com',
      apiKey: '',
      apiSecret: '',
    })
    // The blank key/secret are omitted from the PATCH: sending "" would wipe
    // the credential the form never received.
    expect(patchMock).toHaveBeenCalledWith('/api/live-share/sfu', { url: 'wss://relay2.example.com' })
    expect(wrapper.find('[data-role="relay-result"]').text()).toBe('connected')
    expect(wrapper.find('[data-role="relay-state"]').text()).toBe(
      enMessages.settings.items.meetingRelayConfigured,
    )
  })

  it('does not save a configuration that failed the probe', async () => {
    postMock.mockResolvedValue({ success: false, message: 'bad key' })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-role="relay-url"]').setValue('wss://relay.example.com')
    await wrapper.find('[data-role="relay-key"]').setValue('key')
    await wrapper.find('[data-role="relay-secret"]').setValue('secret')
    await wrapper.find('[data-action="relay-save-test"]').trigger('click')
    await flushPromises()

    expect(patchMock).not.toHaveBeenCalled()
    expect(wrapper.find('[data-role="relay-result"]').text()).toBe('bad key')
    expect(wrapper.find('[data-role="relay-state"]').text()).toBe(
      enMessages.settings.items.meetingRelayUnconfigured,
    )
  })

  it('refuses to probe without an endpoint, and asks for the missing field', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="relay-save-test"]').trigger('click')
    await flushPromises()

    expect(postMock).not.toHaveBeenCalled()
    expect(wrapper.find('[data-role="relay-result"]').text()).toBe(
      enMessages.settings.items.meetingRelayMissing,
    )
  })

  it('clears the typed secret after a successful save', async () => {
    postMock.mockResolvedValue({ success: true, message: 'connected' })
    patchMock.mockResolvedValue({ url: 'wss://r', hasSecret: true, configured: true })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-role="relay-url"]').setValue('wss://r')
    await wrapper.find('[data-role="relay-key"]').setValue('key')
    await wrapper.find('[data-role="relay-secret"]').setValue('secret')
    await wrapper.find('[data-action="relay-save-test"]').trigger('click')
    await flushPromises()

    // Stored server-side now; keeping it in the field would resend it for no
    // reason on the next save, so the field goes back to the dots.
    const secret = wrapper.find('[data-role="relay-secret"]').element as HTMLInputElement
    expect(secret.value).toBe(MASK)
    expect(secret.value).not.toContain('secret')
  })
})
