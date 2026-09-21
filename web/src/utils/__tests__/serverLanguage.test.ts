import { describe, expect, it, vi, beforeEach } from 'vitest'

vi.mock('@/utils/api', () => ({
  apiPatch: vi.fn(),
}))

import { apiPatch } from '@/utils/api'
import { syncServerLanguage } from '@/utils/serverLanguage'

const mockedApiPatch = vi.mocked(apiPatch)

describe('syncServerLanguage', () => {
  beforeEach(() => {
    mockedApiPatch.mockReset()
    mockedApiPatch.mockResolvedValue(undefined as never)
  })

  it('PATCHes the language config field', async () => {
    await syncServerLanguage('zh')
    expect(mockedApiPatch).toHaveBeenCalledWith('/api/config', { language: 'zh' })
  })

  it('does nothing for an empty language (no request)', async () => {
    await syncServerLanguage('')
    expect(mockedApiPatch).not.toHaveBeenCalled()
  })

  it('swallows failures — a sync error must never surface or reject', async () => {
    mockedApiPatch.mockRejectedValueOnce(new Error('network down'))
    await expect(syncServerLanguage('en')).resolves.toBeUndefined()
  })
})
