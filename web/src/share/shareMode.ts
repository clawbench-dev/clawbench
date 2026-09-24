/**
 * Share-mode global state for the public file-share view.
 *
 * The share SPA (/share/{token}) is a standalone Vue entry. It sets a token via
 * setShareToken() before mounting preview components so that URL builders
 * (buildLocalFileUrl, markdown image rewriting) emit token-scoped URLs under
 * /api/share/{token}/... instead of the auth-protected /api/file/ and
 * /api/fs/raw/ endpoints.
 *
 * baseDir records the directory of the shared file: relative media references
 * in the shared document resolve against it.
 */

let token: string | null = null
/** Absolute path of the shared file, used to derive baseDir for relative refs. */
let sharedFilePath: string | null = null
/** Display name of the shared file. */
let sharedFileName: string | null = null

export function setShareToken(t: string | null): void {
    token = t
}
export function getShareToken(): string | null {
    return token
}

export function setSharedFile(path: string, name: string): void {
    sharedFilePath = path
    sharedFileName = name
}
export function getSharedFilePath(): string | null {
    return sharedFilePath
}
export function getSharedFileName(): string | null {
    return sharedFileName
}

export function isShareMode(): boolean {
    return token !== null && token !== ''
}

/**
 * Build a token-scoped share API URL, e.g. `/api/share/{token}/file`.
 * Path segments must be URL-safe already; pass a leading '/'-free subpath.
 */
export function shareApiUrl(subpath: string): string {
    const t = token
    if (!t) throw new Error('shareApiUrl called outside share mode')
    const clean = subpath.startsWith('/') ? subpath.slice(1) : subpath
    return `/api/share/${t}/${clean}`
}

// ─── Session-share data provider ─────────────────────────────────────────────
//
// A session snapshot inlines every tool call's input/output and every thinking
// block's text, so the share page never needs the authenticated lazy-fetch
// endpoints (/api/ai/chat/tool-call, /api/ai/chat/thinking). These maps are the
// bridge: the render components consult them first and only fall back to the
// network when nothing is found.
//
// Both maps are null outside session-share mode, which makes every lookup a
// miss — that is what keeps the normal chat path unchanged.

/** One inlined tool call, keyed by `${messageId}:${toolId}`. */
export interface ShareToolCallData {
    input?: unknown
    output?: string
    status?: string
    done?: boolean
    durationMs?: number
    summary?: string
    truncated?: boolean
}

let shareThinking: Map<string, string> | null = null
let shareToolCalls: Map<string, ShareToolCallData> | null = null

/** Composite key for both maps. Exported so callers build it identically. */
export function shareDataKey(msgId: string | number, id: string): string {
    return `${msgId}:${id}`
}

/**
 * Install the session-share data provider. Called once by SessionShareView as
 * it parses the snapshot.
 */
export function setShareSessionData(
    thinking: Map<string, string>,
    toolCalls: Map<string, ShareToolCallData>
): void {
    shareThinking = thinking
    shareToolCalls = toolCalls
}

export function clearShareSessionData(): void {
    shareThinking = null
    shareToolCalls = null
}

/** Inlined thinking text for a block, or undefined when not in a session share. */
export function getShareThinking(msgId: string | number, thinkId: string): string | undefined {
    if (!shareThinking || !thinkId) return undefined
    return shareThinking.get(shareDataKey(msgId, thinkId))
}

/** Inlined tool call for a block, or undefined when not in a session share. */
export function getShareToolCall(msgId: string | number, toolId: string): ShareToolCallData | undefined {
    if (!shareToolCalls || !toolId) return undefined
    return shareToolCalls.get(shareDataKey(msgId, toolId))
}
