// API utility functions
import i18n from '@/i18n'

function localeHeaders(): Record<string, string> {
    return { 'X-Locale': i18n.global.locale.value as string }
}

// Default timeout for API requests (10 seconds)
const API_TIMEOUT_MS = 10_000

/** Options shared by all API helper functions */
export interface ApiOptions {
    signal?: AbortSignal
    body?: unknown
    /** Override the default API timeout (ms). Defaults to API_TIMEOUT_MS (10s). */
    timeoutMs?: number
}

/**
 * Create an AbortSignal that aborts when either:
 * - The internal timeout fires (opts.timeoutMs ?? API_TIMEOUT_MS)
 * - The external signal (if provided) aborts
 * Returns the combined signal and a cleanup function.
 */
function createSignal(opts: ApiOptions = {}): { signal: AbortSignal; cleanup: () => void } {
    const controller = new AbortController()
    const timeout = opts.timeoutMs ?? API_TIMEOUT_MS
    // Pass a reason: a bare abort() makes fetch throw the opaque
    // "signal is aborted without reason", which tells neither the user nor a
    // log reader that this was a client-side timeout (the server may well have
    // answered — see the /btw case, where a ~10s LLM call raced the default 10s
    // budget). The reason is what surfaces in the rejection.
    //
    // It must be a DOMException with name 'AbortError', not a plain Error:
    // callers across the app distinguish a superseded request from a real
    // failure via `name === 'AbortError'` (some via `instanceof DOMException`),
    // and a plain Error would make an expected abort look like a hard error.
    const timer = setTimeout(
        () => controller.abort(new DOMException(`request timed out after ${timeout}ms`, 'AbortError')),
        timeout,
    )

    // Forward the caller's abort reason (when it has one) so a superseded
    // request reports why it was cancelled instead of the opaque default.
    const forwardAbort = () => {
        clearTimeout(timer)
        const reason = opts.signal?.reason
        if (reason !== undefined) controller.abort(reason)
        else controller.abort()
    }

    // If external signal is already aborted, abort immediately
    if (opts.signal?.aborted) {
        forwardAbort()
    }

    // Forward external abort to our controller
    opts.signal?.addEventListener('abort', forwardAbort)

    const cleanup = () => {
        clearTimeout(timer)
        opts.signal?.removeEventListener('abort', forwardAbort)
    }

    return { signal: controller.signal, cleanup }
}

export async function apiGet<T = unknown>(url: string, opts: ApiOptions = {}): Promise<T> {
    const { signal, cleanup } = createSignal(opts)
    try {
        const resp = await fetch(url, { headers: localeHeaders(), signal })
        const data = await resp.json().catch(() => ({})) as Record<string, unknown>
        if (!resp.ok) {
            const err = new Error(data.error ? String(data.error) : resp.statusText)
            const typedErr = err as Error & { status?: number }
            typedErr.status = resp.status
            if (data.msgKey) (err as Error & { msgKey?: string }).msgKey = String(data.msgKey)
            throw err
        }
        return data as T
    } finally {
        cleanup()
    }
}

export async function apiPost<T = unknown>(url: string, body: unknown, opts: ApiOptions = {}): Promise<T> {
    const { signal, cleanup } = createSignal(opts)
    try {
        const resp = await fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', ...localeHeaders() },
            body: JSON.stringify(body),
            signal,
        })
        const data = await resp.json().catch(() => ({})) as Record<string, unknown>
        if (!resp.ok) {
            const err = new Error(data.error ? String(data.error) : resp.statusText)
            const typedErr = err as Error & { msgKey?: string; detail?: unknown }
            if (data.msgKey) typedErr.msgKey = String(data.msgKey)
            if (data.detail) typedErr.detail = data.detail
            throw err
        }
        return data as T
    } finally {
        cleanup()
    }
}

export async function apiPut<T = unknown>(url: string, body: unknown, opts: ApiOptions = {}): Promise<T> {
    const { signal, cleanup } = createSignal(opts)
    try {
        const resp = await fetch(url, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', ...localeHeaders() },
            body: JSON.stringify(body),
            signal,
        })
        const data = await resp.json().catch(() => ({})) as Record<string, unknown>
        if (!resp.ok) throw new Error(data.error ? String(data.error) : resp.statusText)
        return data as T
    } finally {
        cleanup()
    }
}

export async function apiPatch<T = unknown>(url: string, body: unknown, opts: ApiOptions = {}): Promise<T> {
    const { signal, cleanup } = createSignal(opts)
    try {
        const resp = await fetch(url, {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json', ...localeHeaders() },
            body: JSON.stringify(body),
            signal,
        })
        const data = await resp.json().catch(() => ({})) as Record<string, unknown>
        if (!resp.ok) throw new Error(data.error ? String(data.error) : resp.statusText)
        return data as T
    } finally {
        cleanup()
    }
}

export async function apiDelete<T = unknown>(url: string, opts: ApiOptions = {}): Promise<T> {
    const { signal, cleanup } = createSignal(opts)
    try {
        const init: RequestInit = { method: 'DELETE', headers: localeHeaders(), signal }
        if (opts.body !== undefined) {
            init.headers = { 'Content-Type': 'application/json', ...localeHeaders() }
            init.body = JSON.stringify(opts.body)
        }
        const resp = await fetch(url, init)
        const data = await resp.json().catch(() => ({})) as Record<string, unknown>
        if (!resp.ok) {
            const err = new Error(data.error ? String(data.error) : resp.statusText)
            const typedErr = err as Error & { status?: number; msgKey?: string; detail?: unknown }
            typedErr.status = resp.status
            // Carry msgKey/detail like apiPost: callers (e.g. agent deletion)
            // branch on msgKey and read detail for authoritative counts.
            if (data.msgKey) typedErr.msgKey = String(data.msgKey)
            if (data.detail) typedErr.detail = data.detail
            throw err
        }
        return data as T
    } finally {
        cleanup()
    }
}

export async function cancelChat(sessionId: string): Promise<void> {
    await apiPost(`/api/ai/chat/cancel?session_id=${encodeURIComponent(sessionId)}`, {})
}
