import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { apiGet, apiPatch, apiPost } from '@/utils/api'
import { _resetLiveSFUConfigForTest, useLiveSFUConfig } from '../useLiveSFUConfig'

/**
 * The relay configuration form's client.
 *
 * The two rules that matter: a probe failure must not save, and an empty
 * credential field must not be sent (the form never receives the stored secret,
 * so sending "" would wipe it).
 */

vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiPatch: vi.fn(),
}))

const getMock = vi.mocked(apiGet)
const postMock = vi.mocked(apiPost)
const patchMock = vi.mocked(apiPatch)

beforeEach(() => {
  _resetLiveSFUConfigForTest()
  getMock.mockReset()
  postMock.mockReset()
  patchMock.mockReset()
})

afterEach(() => {
  _resetLiveSFUConfigForTest()
})

describe('useLiveSFUConfig', () => {
  it('reads the stored endpoint once', async () => {
    getMock.mockResolvedValue({ url: 'wss://relay.example.com', hasSecret: true, configured: true })
    const { state, load } = useLiveSFUConfig()

    await load()
    await load()

    expect(getMock).toHaveBeenCalledTimes(1)
    expect(state.value.configured).toBe(true)
  })

  it('saves only after the probe passes', async () => {
    postMock.mockResolvedValue({ success: true, message: 'ok' })
    patchMock.mockResolvedValue({ url: 'wss://r', hasSecret: true, configured: true })
    const { saveAndTest } = useLiveSFUConfig()

    const res = await saveAndTest({ url: 'wss://r', apiKey: 'k', apiSecret: 's' })

    expect(res.ok).toBe(true)
    expect(patchMock).toHaveBeenCalledTimes(1)
  })

  it('does not save a configuration that failed the probe', async () => {
    postMock.mockResolvedValue({ success: false, message: 'bad key' })
    const { saveAndTest } = useLiveSFUConfig()

    const res = await saveAndTest({ url: 'wss://r', apiKey: 'k', apiSecret: 's' })

    expect(res).toEqual({ ok: false, message: 'bad key' })
    expect(patchMock).not.toHaveBeenCalled()
  })

  // The form never receives the secret back, so an untouched field is empty.
  // Sending "" would overwrite a working credential with nothing.
  it('omits empty credential fields so a stored secret survives', async () => {
    postMock.mockResolvedValue({ success: true, message: 'ok' })
    patchMock.mockResolvedValue({ url: 'wss://new', hasSecret: true, configured: true })
    const { saveAndTest } = useLiveSFUConfig()

    await saveAndTest({ url: 'wss://new', apiKey: '', apiSecret: '' })

    const body = patchMock.mock.calls[0][1] as Record<string, unknown>
    expect(body).toEqual({ url: 'wss://new' })
    expect(body).not.toHaveProperty('apiSecret')
  })

  // The eye button's request. Deliberately a separate call from `load`: reading
  // the panel must not pull a plaintext credential into the page.
  it('reveals the stored credential on an explicit request only', async () => {
    getMock.mockResolvedValue({ url: 'wss://r', hasSecret: true, configured: true, apiKey: 'k', apiSecret: 's' })
    const { load, reveal } = useLiveSFUConfig()

    await load()
    expect(getMock).toHaveBeenCalledTimes(1)
    expect(getMock).toHaveBeenCalledWith('/api/live-share/sfu')

    const values = await reveal()
    expect(getMock).toHaveBeenLastCalledWith('/api/live-share/sfu?reveal=1')
    expect(values).toEqual({ apiKey: 'k', apiSecret: 's' })
  })
})
