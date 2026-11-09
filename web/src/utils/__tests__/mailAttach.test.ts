import { describe, expect, it, vi, beforeEach } from 'vitest'

const mocks = vi.hoisted(() => ({
  apiPost: vi.fn(),
  composeMail: vi.fn(),
  config: { value: { mail: { enabled: true } } } as { value: Record<string, unknown> },
}))

vi.mock('@/utils/api', () => ({ apiPost: mocks.apiPost }))
vi.mock('@/composables/useMailCompose', () => ({ composeMail: mocks.composeMail }))
vi.mock('@/composables/useSettingsConfig', () => ({ serverConfig: mocks.config }))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import { sendPathsByMail, stageMailAttachments } from '../mailAttach'

beforeEach(() => {
  mocks.apiPost.mockReset()
  mocks.composeMail.mockReset()
  mocks.config.value = { mail: { enabled: true } }
})

describe('stageMailAttachments', () => {
  it('returns the server descriptors', async () => {
    const staged = [{ filename: 'a.zip', path: '/data/mail/outbox/a.zip', size: 12 }]
    mocks.apiPost.mockResolvedValue({ ok: true, attachments: staged })

    await expect(stageMailAttachments(['a'])).resolves.toEqual(staged)
    expect(mocks.apiPost).toHaveBeenCalledWith('/api/mail/attachments', { paths: ['a'] })
  })

  it('returns an empty list when the server sends none', async () => {
    mocks.apiPost.mockResolvedValue({ ok: true })
    await expect(stageMailAttachments(['a'])).resolves.toEqual([])
  })
})

describe('sendPathsByMail', () => {
  it('stages the paths and opens the composer with them attached', async () => {
    const staged = [{ filename: 'a.txt', path: '/p/a.txt', size: 3 }]
    mocks.apiPost.mockResolvedValue({ attachments: staged })
    mocks.composeMail.mockResolvedValue(true)

    const res = await sendPathsByMail(['a.txt'], 'a.txt')

    expect(res).toEqual({ outcome: 'opened' })
    // The composer gets staged attachments, not paths: it cannot resolve paths
    // itself, and the server has already turned a folder into one zip.
    expect(mocks.composeMail).toHaveBeenCalledWith('a.txt', '', staged)
  })

  it('does not stage anything when the mail feature is switched off', async () => {
    mocks.config.value = { mail: { enabled: false } }

    const res = await sendPathsByMail(['a.txt'], 'a.txt')

    expect(res).toEqual({ outcome: 'no-mailbox' })
    expect(mocks.apiPost).not.toHaveBeenCalled()
  })

  it('reports no-mailbox when the panel refuses to open a composer', async () => {
    mocks.apiPost.mockResolvedValue({ attachments: [{ filename: 'a.txt', path: '/p/a.txt', size: 3 }] })
    mocks.composeMail.mockResolvedValue(false)

    const res = await sendPathsByMail(['a.txt'], 'a.txt')

    expect(res).toEqual({ outcome: 'no-mailbox' })
  })

  it('reports the server message when staging fails', async () => {
    mocks.apiPost.mockRejectedValue(new Error('单个附件不能超过 25 MB'))

    const res = await sendPathsByMail(['huge'], 'huge.zip')

    expect(res).toEqual({ outcome: 'failed', error: '单个附件不能超过 25 MB' })
    expect(mocks.composeMail).not.toHaveBeenCalled()
  })

  it('treats an empty staging result as a failure', async () => {
    mocks.apiPost.mockResolvedValue({ attachments: [] })

    const res = await sendPathsByMail(['a.txt'], 'a.txt')

    expect(res.outcome).toBe('failed')
    expect(mocks.composeMail).not.toHaveBeenCalled()
  })
})
