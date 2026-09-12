// Forge (GitHub / GitLab) API client.
//
// Mirrors the gitFetch pattern in @/utils/gitApi: a timeout plus an optional
// external abort signal, so a stalled request can never leave the UI stuck in a
// loading state. All forge endpoints live under /api/forge/*.
import i18n from '@/i18n'
import { appLog } from '@/utils/appLog'

const TAG = 'ForgeApi'

export const FORGE_TIMEOUT_MS = 20_000

export interface ForgeBinding {
    platform: string
    host: string
    owner: string
    repo: string
    slug: string
    source?: string
}

export interface ForgeItem {
    platform: string
    host: string
    owner: string
    repo: string
    type: 'issue' | 'pr'
    number: number
    title: string
    body?: string
    state: 'open' | 'closed' | 'merged'
    draft?: boolean
    author: string
    assignees?: string[]
    labels?: string[]
    commentCount: number
    url: string
    createdAt: string
    updatedAt: string
    slug: string
}

export interface ForgeComment {
    id: number
    author: string
    body: string
    createdAt: string
    updatedAt: string
}

export interface ForgeItemListResult {
    items: ForgeItem[]
    hasMore: boolean
    nextPage: number
    binding: ForgeBinding
}

export interface ForgeRemote {
    name: string
    url: string
    platform?: string
    host?: string
    owner?: string
    repo?: string
    slug?: string
}

/** Error carrying the backend's classified code so callers can react to it. */
export class ForgeApiError extends Error {
    code: string
    status: number
    retryAfterSeconds?: number
    constructor(message: string, code: string, status: number, retryAfterSeconds?: number) {
        super(message)
        this.name = 'ForgeApiError'
        this.code = code
        this.status = status
        this.retryAfterSeconds = retryAfterSeconds
    }
}

interface ForgeFetchOptions {
    signal?: AbortSignal
    timeoutMs?: number
    method?: string
    body?: unknown
}

async function forgeFetch<T>(path: string, opts: ForgeFetchOptions = {}): Promise<T> {
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), opts.timeoutMs ?? FORGE_TIMEOUT_MS)
    if (opts.signal?.aborted) controller.abort()
    opts.signal?.addEventListener('abort', () => controller.abort(), { once: true })

    try {
        const res = await fetch(path, {
            method: opts.method ?? 'GET',
            headers: {
                'X-Locale': i18n.global.locale.value as string,
                ...(opts.body ? { 'Content-Type': 'application/json' } : {}),
            },
            body: opts.body ? JSON.stringify(opts.body) : undefined,
            signal: controller.signal,
        })

        if (res.status === 204) {
            return undefined as T
        }

        const text = await res.text()
        let payload: unknown = null
        if (text) {
            try {
                payload = JSON.parse(text)
            } catch {
                payload = null
            }
        }

        if (!res.ok) {
            const p = (payload ?? {}) as Record<string, unknown>
            const code = typeof p.code === 'string' ? p.code : 'ForgeError'
            const message = typeof p.error === 'string' ? p.error : `forge request failed (${res.status})`
            const retryAfter = typeof p.retryAfterSeconds === 'number' ? p.retryAfterSeconds : undefined
            throw new ForgeApiError(message, code, res.status, retryAfter)
        }

        return payload as T
    } catch (err) {
        if (err instanceof ForgeApiError) throw err
        if (err instanceof Error && err.name === 'AbortError') {
            throw new ForgeApiError('forge request timed out', 'ForgeTimeout', 0)
        }
        appLog.w(TAG, 'request failed', path, err)
        throw new ForgeApiError(err instanceof Error ? err.message : 'network error', 'ForgeNetworkError', 0)
    } finally {
        clearTimeout(timer)
    }
}

export function fetchForgeItems(params: {
    type: 'issue' | 'pr'
    state?: string
    page?: number
    perPage?: number
    query?: string
    mine?: boolean
    mineScope?: string
    signal?: AbortSignal
}): Promise<ForgeItemListResult> {
    const q = new URLSearchParams()
    q.set('type', params.type)
    q.set('state', params.state ?? 'open')
    q.set('page', String(params.page ?? 1))
    q.set('perPage', String(params.perPage ?? 30))
    if (params.query) q.set('q', params.query)
    if (params.mine) {
        q.set('mine', '1')
        if (params.mineScope) q.set('mineScope', params.mineScope)
    }
    return forgeFetch<ForgeItemListResult>(`/api/forge/items?${q.toString()}`, { signal: params.signal })
}

export function fetchForgeItem(type: 'issue' | 'pr', number: number, signal?: AbortSignal): Promise<{ item: ForgeItem }> {
    return forgeFetch(`/api/forge/item?type=${type}&number=${number}`, { signal })
}

export function fetchForgeComments(
    type: 'issue' | 'pr',
    number: number,
    page = 1,
    perPage = 30,
    signal?: AbortSignal,
): Promise<{ comments: ForgeComment[] }> {
    return forgeFetch(`/api/forge/comments?type=${type}&number=${number}&page=${page}&perPage=${perPage}`, { signal })
}

export function fetchForgeBinding(signal?: AbortSignal): Promise<{ binding: ForgeBinding | null; suggested?: Record<string, string> | null }> {
    return forgeFetch('/api/forge/binding', { signal })
}

export function setForgeBinding(input: { url?: string; platform?: string; host?: string; owner?: string; repo?: string }): Promise<{ binding: ForgeBinding }> {
    return forgeFetch('/api/forge/binding', { method: 'POST', body: input })
}

export function deleteForgeBinding(): Promise<void> {
    return forgeFetch('/api/forge/binding', { method: 'DELETE' })
}

export function fetchForgeRemotes(signal?: AbortSignal): Promise<{ remotes: ForgeRemote[] }> {
    return forgeFetch('/api/forge/remotes', { signal })
}

export function testForgeConnection(): Promise<{ ok: boolean; identity?: string; error?: string; code?: string }> {
    return forgeFetch('/api/forge/test', { method: 'POST' })
}

export function setForgeToken(host: string, token: string): Promise<{ host: string; has_token: boolean }> {
    return forgeFetch('/api/forge/credentials', { method: 'POST', body: { host, token } })
}

export function deleteForgeToken(host: string): Promise<void> {
    return forgeFetch(`/api/forge/credentials?host=${encodeURIComponent(host)}`, { method: 'DELETE' })
}

/**
 * Verify that a token authenticates against a forge host.
 *
 * Independent of saving: pass `token` to check one you are about to save, or
 * omit it to re-check the stored credential. A failed check never mutates
 * stored credentials.
 *
 * Resolves with `ok:false` (rather than rejecting) when the platform rejects
 * the token, so the caller can tell "bad token" (code `auth`) apart from
 * "host unreachable" (code `network`).
 */
export function verifyForgeToken(input: { host: string; token?: string }): Promise<{
    ok: boolean
    identity?: string
    name?: string
    code?: string
    error?: string
}> {
    return forgeFetch('/api/forge/verify-token', { method: 'POST', body: input })
}

export function fetchForgeUnread(signal?: AbortSignal): Promise<{ count: number }> {
    return forgeFetch('/api/forge/unread', { signal })
}

export function markForgeRead(): Promise<{ count: number }> {
    return forgeFetch('/api/forge/read', { method: 'POST' })
}
