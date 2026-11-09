import { describe, expect, it, vi, beforeEach } from 'vitest'

/**
 * The parked "compose a mail" request.
 *
 * Two properties matter and both are easy to get wrong:
 *  - it refuses WITHOUT parking when there is no mailbox, so the caller can fall
 *    back to the system mail app instead of stranding the user on an empty tab;
 *  - it can be consumed exactly once, or a single request would open a composer
 *    again every time the panel re-activates.
 */

const h = vi.hoisted(() => ({
  apiGet: vi.fn(),
  events: [] as string[],
}))

vi.mock('@/utils/api', () => ({ apiGet: h.apiGet }))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))

import {
  _resetMailComposeForTest,
  composeMail,
  takePendingMailCompose,
} from '@/composables/useMailCompose'

beforeEach(() => {
  vi.clearAllMocks()
  _resetMailComposeForTest()
  h.events = []
  window.addEventListener('clawbench-compose-mail', () => h.events.push('compose'))
})

describe('composeMail', () => {
  it('parks the request and asks for the mail tab when a mailbox exists', async () => {
    h.apiGet.mockResolvedValue({ accounts: [{ id: 1 }] })

    const ok = await composeMail('Subject', 'Body')
    expect(ok).toBe(true)
    expect(h.events).toEqual(['compose'])

    const req = takePendingMailCompose()
    expect(req?.subject).toBe('Subject')
    expect(req?.body).toBe('Body')
  })

  it('refuses without parking when no mailbox is configured', async () => {
    h.apiGet.mockResolvedValue({ accounts: [] })

    const ok = await composeMail('Subject', 'Body')
    // The caller then opens the system mail app. Parking here would switch to
    // the mail tab and show "no mailbox configured" with the message lost.
    expect(ok).toBe(false)
    expect(h.events).toEqual([])
    expect(takePendingMailCompose()).toBeNull()
  })

  it('refuses when the account probe fails', async () => {
    h.apiGet.mockRejectedValue(new Error('404'))
    const ok = await composeMail('Subject', 'Body')
    expect(ok).toBe(false)
    expect(takePendingMailCompose()).toBeNull()
  })

  it('hands the request out exactly once', async () => {
    h.apiGet.mockResolvedValue({ accounts: [{ id: 1 }] })
    await composeMail('S', 'B')

    expect(takePendingMailCompose()).not.toBeNull()
    // A second consume must not re-open the composer on the next tab visit.
    expect(takePendingMailCompose()).toBeNull()
  })
})
