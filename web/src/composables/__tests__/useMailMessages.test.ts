import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useMailMessages } from '../useMailMessages'
import type { MailMessage } from '@/utils/mailTypes'

const apiGet = vi.fn()
const apiPost = vi.fn()

vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => apiGet(...args),
  apiPost: (...args: unknown[]) => apiPost(...args),
}))

function msg(id: number, sentAt: number): MailMessage {
  return {
    id,
    accountId: 1,
    folderId: 1,
    uid: id,
    messageId: `m${id}`,
    subject: `s${id}`,
    fromName: '',
    fromAddr: 'a@example.com',
    to: [],
    cc: [],
    replyTo: '',
    inReplyTo: '',
    references: '',
    sentAt,
    size: 1,
    seen: true,
    flagged: false,
    answered: false,
    hasAttachments: false,
    snippet: '',
    bodyFetched: false,
  }
}

function page(ids: number[], hasMore: boolean) {
  return { messages: ids.map((id) => msg(id, 1000 - id)), hasMore }
}

describe('useMailMessages', () => {
  beforeEach(() => {
    apiGet.mockReset()
    apiPost.mockReset()
  })

  it('loads the first page of a folder', async () => {
    apiGet.mockResolvedValue(page([3, 2, 1], false))
    const list = useMailMessages()
    await list.selectFolder(1)

    expect(apiGet).toHaveBeenCalledWith('/api/mail/messages?folderId=1&limit=50')
    expect(list.messages.value.map((m) => m.id)).toEqual([3, 2, 1])
    expect(list.hasMore.value).toBe(false)
  })

  it('appends a full local page without touching the server', async () => {
    const full = Array.from({ length: 50 }, (_, i) => 50 - i)
    apiGet.mockResolvedValueOnce(page(full, true))
    const list = useMailMessages()
    await list.selectFolder(1)

    // A FULL page: still walking the local cache, so no server round trip.
    apiGet.mockResolvedValueOnce(page(Array.from({ length: 50 }, (_, i) => 1000 - i), true))
    await list.loadMore()

    expect(apiPost).not.toHaveBeenCalled()
    expect(list.messages.value).toHaveLength(100)
    expect(list.hasMore.value).toBe(true)
  })

  it('asks the server for an older page when the local cache runs dry', async () => {
    apiGet.mockResolvedValueOnce(page([3, 2], true))
    const list = useMailMessages()
    await list.selectFolder(1)

    apiGet.mockResolvedValueOnce(page([1], true))
    apiPost.mockResolvedValueOnce(page([0, -1], false))
    await list.loadMore()

    expect(apiPost).toHaveBeenCalledWith('/api/mail/folders/1/load-older', {}, { timeoutMs: 60_000 })
    expect(list.messages.value.map((m) => m.id)).toEqual([3, 2, 1, 0, -1])
    expect(list.hasMore.value).toBe(false)
  })

  it('deduplicates rows that overlap between cache and server', async () => {
    apiGet.mockResolvedValueOnce(page([3, 2], true))
    const list = useMailMessages()
    await list.selectFolder(1)

    apiGet.mockResolvedValueOnce(page([2], true))
    apiPost.mockResolvedValueOnce(page([2, 1], false))
    await list.loadMore()

    expect(list.messages.value.map((m) => m.id)).toEqual([3, 2, 1])
  })

  it('discards a response that arrives after switching folders', async () => {
    let resolveFirst: (v: unknown) => void = () => {}
    const first = new Promise((r) => { resolveFirst = r })
    apiGet.mockReturnValueOnce(first)

    const list = useMailMessages()
    const pending = list.selectFolder(1)

    apiGet.mockResolvedValueOnce(page([9], false))
    await list.selectFolder(2)

    resolveFirst(page([1, 2, 3], false))
    await pending

    expect(list.messages.value.map((m) => m.id)).toEqual([9])
    expect(list.activeFolderId.value).toBe(2)
  })

  it('patches and removes rows in place', async () => {
    apiGet.mockResolvedValueOnce(page([2, 1], false))
    const list = useMailMessages()
    await list.selectFolder(1)

    list.patchMessage({ ...msg(2, 998), seen: true, flagged: true })
    expect(list.messages.value[0]).toMatchObject({ seen: true, flagged: true })

    list.removeMessage(2)
    expect(list.messages.value.map((m) => m.id)).toEqual([1])
  })
})
