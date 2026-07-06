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
 * - The internal timeout fires (API_TIMEOUT_MS)
 * - The external signal (if provided) aborts
 * Returns the combined signal and a cleanup function.
 */
function createSignal(opts: ApiOptions = {}): { signal: AbortSignal; cleanup: () => void } {
    const controller = new AbortController()
    const timeout = opts.timeoutMs ?? API_TIMEOUT_MS
    const timer = setTimeout(() => controller.abort(), timeout)

    // If external signal is already aborted, abort immediately
    if (opts.signal?.aborted) {
        clearTimeout(timer)
        controller.abort()
    }

    // Forward external abort to our controller
    const onExternalAbort = () => {
        clearTimeout(timer)
        controller.abort()
    }
    opts.signal?.addEventListener('abort', onExternalAbort)

    const cleanup = () => {
        clearTimeout(timer)
        opts.signal?.removeEventListener('abort', onExternalAbort)
    }

    return { signal: controller.signal, cleanup }
}

export async function apiGet<T = unknown>(url: string, opts: ApiOptions = {}): Promise<T> {
    const { signal, cleanup } = createSignal(opts)
    try {
        const resp = await fetch(url, { headers: localeHeaders(), signal })
        if (!resp.ok) throw new Error(await resp.text())
        return resp.json()
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
            if (data.msgKey) (err as Error & { msgKey?: string }).msgKey = String(data.msgKey)
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
        if (!resp.ok) throw new Error(data.error ? String(data.error) : resp.statusText)
        return data as T
    } finally {
        cleanup()
    }
}

export async function cancelChat(sessionId: string): Promise<void> {
    await apiPost(`/api/ai/chat/cancel?session_id=${encodeURIComponent(sessionId)}`, {})
}
