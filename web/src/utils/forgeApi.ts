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
    /** Resolved API scheme ("http" or "https") requests to this host use. */
    scheme?: string
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
    /** True when this item has activity the user has not seen. */
    unread?: boolean
}

export interface ForgeComment {
    id: number
    author: string
    body: string
    createdAt: string
    updatedAt: string
}

/** Normalized CI run status, shared by both platforms. */
export type ForgePipelineStatus = 'success' | 'failure' | 'running' | 'cancelled' | 'skipped' | 'unknown'

export interface ForgePipelineRun {
    platform: string
    host: string
    owner: string
    repo: string
    /** Platform run id (GitHub run id / GitLab pipeline id). */
    id: number
    /** Workflow name; falls back to the ref on older GitLab instances. */
    name: string
    /** Human-facing run counter (GitHub run_number / GitLab iid). */
    number: number
    status: ForgePipelineStatus
    ref: string
    sha: string
    /** What triggered the run, in the platform's own vocabulary. */
    event: string
    actor: string
    url: string
    createdAt: string
    updatedAt: string
    /** Absent when the platform does not report it (GitHub's list endpoint). */
    durationSeconds?: number
    slug: string
    /** True when this run has activity the user has not seen. */
    unread?: boolean
}

export interface ForgePipelineJob {
    id: number
    name: string
    /** GitLab only; GitHub has no stage concept. */
    stage?: string
    status: ForgePipelineStatus
    /** GitLab only. */
    failureReason?: string
    runner?: string
    url: string
    durationSeconds?: number
    startedAt?: string
    completedAt?: string
}

export interface ForgePipelineListResult {
    pipelines: ForgePipelineRun[]
    hasMore: boolean
    nextPage: number
    binding: ForgeBinding
}

export interface ForgePipelineDetailResult {
    pipeline: ForgePipelineRun
    jobs: ForgePipelineJob[]
    binding: ForgeBinding
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
    /**
     * The remote's own API scheme, absent when it could not state one (an ssh or
     * scp remote). Distinct from an explicit "https": the UI resolves it for
     * display instead of treating an absent value as https.
     */
    scheme?: string
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

/**
 * List CI runs for the bound repository.
 *
 * `status` filters server-side; omit it for every status. The forge API is
 * read-only, so there is no corresponding write call (no re-run, no cancel).
 */
export function fetchForgePipelines(params: {
    status?: ForgePipelineStatus
    page?: number
    perPage?: number
    signal?: AbortSignal
} = {}): Promise<ForgePipelineListResult> {
    const q = new URLSearchParams()
    if (params.status) q.set('status', params.status)
    q.set('page', String(params.page ?? 1))
    q.set('perPage', String(params.perPage ?? 30))
    return forgeFetch<ForgePipelineListResult>(`/api/forge/pipelines?${q.toString()}`, { signal: params.signal })
}

/** Fetch one run and its jobs. Jobs are best-effort and may come back empty. */
export function fetchForgePipeline(id: number, signal?: AbortSignal): Promise<ForgePipelineDetailResult> {
    return forgeFetch(`/api/forge/pipeline?id=${id}`, { signal })
}

/**
 * Bind the project to a repository.
 *
 * Either pass `url` (a remote URL parsed server-side, which carries its own
 * scheme) or the explicit fields. `scheme` is optional: omit it when the source
 * did not state one, so the server keeps any scheme already recorded for the
 * host rather than overwriting it with a guess.
 */
export function setForgeBinding(input: {
    url?: string
    platform?: string
    host?: string
    scheme?: string
    owner?: string
    repo?: string
}): Promise<{ binding: ForgeBinding }> {
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

/**
 * Save a token for a forge host.
 *
 * `host` accepts what the user typed — a bare host or a full URL — and the
 * server normalizes it. A URL also records the API scheme for that host, which
 * is how an http-only instance is reached when the git remote cannot say.
 */
export function setForgeToken(host: string, token: string): Promise<{ host: string; scheme: string; has_token: boolean }> {
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
    scheme?: string
    code?: string
    error?: string
}> {
    return forgeFetch('/api/forge/verify-token', { method: 'POST', body: input })
}

export function fetchForgeUnread(signal?: AbortSignal): Promise<{ count: number }> {
    return forgeFetch('/api/forge/unread', { signal })
}

/**
 * One activity row in the overview panel.
 *
 * `itemKey` is opaque and must be passed back verbatim to markForgeRead: a
 * pipeline's `number` is always 0, so rebuilding the key from type+number would
 * produce "pipeline/0" and silently match nothing.
 */
export interface ForgeUnreadItem {
    itemKey: string
    type: 'issue' | 'pr' | 'pipeline'
    /** Issue/PR number. Always 0 for a pipeline. */
    number: number
    /** CI run id; 0 for anything that is not a pipeline. */
    runId: number
    /** Newest event type: opened / closed / merged / reopened / commented / pipeline_done. */
    eventType: string
    /** Total events for this item, not just the unread ones. */
    eventCount: number
    url: string
    /** owner/repo */
    slug: string
    updatedAt: string
    /**
     * Server-reported read state: true when EVERY event of this item has been
     * read. It comes from the same query that selected the row, so the styling
     * cannot disagree with the filter.
     *
     * Distinct from `locallyRead` below: this is what the server said, that is
     * what this client did a moment ago. Keeping them separate is what lets a
     * row be greyed out instantly without the optimistic flag ever being mistaken
     * for persisted state (or vice versa).
     */
    read?: boolean
    /**
     * Set locally after the user opens the row, so it can be greyed out without
     * being removed (removing it would shift the rows below mid-click).
     */
    locallyRead?: boolean
}

/** The read-state filter offered by the activity view. */
export type ForgeActivityFilter = 'unread' | 'read' | 'all'

/**
 * List the bound repository's activity items.
 *
 * `filter` selects by ITEM read state: an item counts as read only when all of
 * its events have been read.
 */
export function fetchForgeUnreadItems(
    filter: ForgeActivityFilter = 'unread',
    signal?: AbortSignal,
): Promise<{
    count: number
    items: ForgeUnreadItem[]
}> {
    const q = new URLSearchParams()
    q.set('filter', filter)
    return forgeFetch(`/api/forge/unread-items?${q.toString()}`, { signal })
}

/**
 * Mark forge activity read.
 *
 * With no itemKey the whole bound repository is marked read (the "mark all
 * read" action). With an itemKey only that item is marked, which is what
 * opening a row does.
 */
export function markForgeRead(itemKey?: string): Promise<{ count: number }> {
    return forgeFetch('/api/forge/read', {
        method: 'POST',
        body: itemKey ? { itemKey } : {},
    })
}
