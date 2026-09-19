/**
 * Coalesce concurrent GETs of the same URL.
 *
 * When two parts of the UI need the same resource at the same moment they
 * usually both call `fetch`. That is wasteful and, on a busy switch, visible:
 * `SessionList` is mounted twice (pinned sidebar + mobile drawer), so a single
 * project switch issued `/api/ai/sessions` and `/api/ai/session/tags` four
 * times each.
 *
 * This shares the in-flight request, so N concurrent callers of one URL cost
 * one round-trip. It is deliberately NOT a response cache: the entry is dropped
 * as soon as the request settles, so a later caller always gets fresh data.
 * Only *concurrent* duplicates are collapsed.
 *
 * Callers must not mutate the resolved value — they receive the same object.
 */

const inFlight = new Map<string, Promise<unknown>>()

/**
 * GET `url` and parse the JSON body, reusing an identical request that is
 * already in flight. Rejects on network failure or a non-2xx status; each
 * caller handles that itself.
 */
export function coalescedJson<T>(url: string, init?: RequestInit): Promise<T> {
    const existing = inFlight.get(url)
    if (existing) return existing as Promise<T>

    const request = (async () => {
        const resp = await fetch(url, init)
        if (!resp.ok) throw new Error(`GET ${url} failed: HTTP ${resp.status}`)
        return resp.json() as Promise<T>
    })().finally(() => {
        // Only clear our own entry: a newer request may have replaced it.
        if (inFlight.get(url) === request) inFlight.delete(url)
    })

    inFlight.set(url, request)
    return request
}

/** @internal Drop in-flight bookkeeping — for tests only. */
export function resetCoalescedRequestsForTest(): void {
    inFlight.clear()
}
