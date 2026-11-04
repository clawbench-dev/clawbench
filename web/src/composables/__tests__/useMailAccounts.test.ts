import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useMailAccounts } from '../useMailAccounts'
import type { MailAccount, MailAccountInput } from '@/utils/mailTypes'

const apiGet = vi.fn()
const apiPost = vi.fn()
const apiPatch = vi.fn()
const apiDelete = vi.fn()

vi.mock('@/utils/api', () => ({
    apiGet: (...args: unknown[]) => apiGet(...args),
    apiPost: (...args: unknown[]) => apiPost(...args),
    apiPatch: (...args: unknown[]) => apiPatch(...args),
    apiDelete: (...args: unknown[]) => apiDelete(...args),
}))

function account(id: number, overrides: Partial<MailAccount> = {}): MailAccount {
    return {
        id,
        name: `acc${id}`,
        email: `a${id}@x.com`,
        username: `a${id}@x.com`,
        imapHost: 'imap.x.com',
        imapPort: 993,
        imapSecurity: 'tls',
        smtpHost: 'smtp.x.com',
        smtpPort: 465,
        smtpSecurity: 'tls',
        insecureTls: false,
        enabled: true,
        sortOrder: id,
        lastSyncAt: 0,
        lastError: '',
        hasPassword: true,
        createdAt: 0,
        ...overrides,
    }
}

const input: MailAccountInput = {
    name: 'acc1',
    email: 'a1@x.com',
    username: 'a1@x.com',
    password: '',
    imapHost: 'imap.x.com',
    imapPort: 993,
    imapSecurity: 'tls',
    smtpHost: 'smtp.x.com',
    smtpPort: 465,
    smtpSecurity: 'tls',
    insecureTls: false,
    enabled: true,
}

describe('useMailAccounts', () => {
    beforeEach(() => {
        apiGet.mockReset()
        apiPost.mockReset()
        apiPatch.mockReset()
        apiDelete.mockReset()
    })

    it('loads the account list', async () => {
        apiGet.mockResolvedValueOnce({ accounts: [account(1), account(2)] })
        const store = useMailAccounts()
        await store.load()

        expect(apiGet).toHaveBeenCalledWith('/api/mail/accounts')
        expect(store.accounts.value.map((a) => a.id)).toEqual([1, 2])
        expect(store.loading.value).toBe(false)
        expect(store.error.value).toBe('')
    })

    it('normalises a payload without an accounts array', async () => {
        apiGet.mockResolvedValueOnce({})
        const store = useMailAccounts()
        await store.load()

        expect(store.accounts.value).toEqual([])
    })

    it('surfaces a load failure as readable text and clears the spinner', async () => {
        apiGet.mockRejectedValueOnce({ message: '连接超时' })
        const store = useMailAccounts()
        await store.load()

        expect(store.error.value).toBe('连接超时')
        expect(store.loading.value).toBe(false)
    })

    it('fetches presets once and reuses them afterwards', async () => {
        apiGet.mockResolvedValueOnce({ presets: [{ id: 'qq', label: 'QQ 邮箱' }] })
        const store = useMailAccounts()
        await store.loadPresets()
        await store.loadPresets()

        expect(apiGet).toHaveBeenCalledTimes(1)
        expect(store.presets.value).toHaveLength(1)
    })

    it('refetches after create so server-managed fields stay authoritative', async () => {
        apiGet.mockResolvedValueOnce({ accounts: [] })
        const store = useMailAccounts()
        await store.load()

        apiPost.mockResolvedValueOnce({ account: account(3, { lastSyncAt: 123 }) })
        apiGet.mockResolvedValueOnce({ accounts: [account(3, { lastSyncAt: 123 })] })
        const created = await store.create(input)

        expect(apiPost).toHaveBeenCalledWith('/api/mail/accounts', input)
        expect(created.lastSyncAt).toBe(123)
        expect(store.accounts.value.map((a) => a.id)).toEqual([3])
    })

    it('refetches after update and delete', async () => {
        apiGet.mockResolvedValueOnce({ accounts: [account(1)] })
        const store = useMailAccounts()
        await store.load()

        apiPatch.mockResolvedValueOnce({ account: account(1, { enabled: false }) })
        apiGet.mockResolvedValueOnce({ accounts: [account(1, { enabled: false })] })
        await store.update(1, { ...input, enabled: false })
        expect(apiPatch).toHaveBeenCalledWith('/api/mail/accounts/1', { ...input, enabled: false })
        expect(store.accounts.value[0].enabled).toBe(false)

        apiDelete.mockResolvedValueOnce({ ok: true })
        apiGet.mockResolvedValueOnce({ accounts: [] })
        await store.remove(1)
        expect(apiDelete).toHaveBeenCalledWith('/api/mail/accounts/1')
        expect(store.accounts.value).toEqual([])
    })

    it('exposes the syncing account while a sync is in flight', async () => {
        let finish: (v: unknown) => void = () => {}
        apiPost.mockReturnValueOnce(new Promise((r) => { finish = r }))
        apiGet.mockResolvedValueOnce({ accounts: [account(1)] })
        const store = useMailAccounts()

        const pending = store.sync(1)
        // The row shows a spinner from this flag, so it must be set before the
        // (up to 90s) request settles.
        expect(store.syncingId.value).toBe(1)

        finish({ ok: true })
        await pending
        expect(store.syncingId.value).toBeNull()
        expect(apiPost).toHaveBeenCalledWith('/api/mail/accounts/1/sync', {}, { timeoutMs: 90_000 })
    })

    it('clears the syncing flag even when the sync fails', async () => {
        apiPost.mockRejectedValueOnce({ message: 'MailSyncBusy' })
        const store = useMailAccounts()

        await expect(store.sync(1)).rejects.toBeTruthy()
        // A stuck flag would leave the row spinning forever.
        expect(store.syncingId.value).toBeNull()
    })
})
