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
