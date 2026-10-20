// Cross-platform path utilities

/**
 * Join a directory path with a file/directory name.
 *
 * An ABSOLUTE dir keeps its root: `/tmp` + `a` → `/tmp/a`. The file manager can
 * browse project-external directories (absolute paths), and dropping the root
 * there produced a bogus project-relative path (`tmp/a`) that resolved against
 * the project root instead of the browsed directory.
 *
 * A relative dir is normalized to a clean project-relative path by stripping
 * leading/trailing slashes, as before. That is what lets a caller pass a
 * project-relative path written with a leading slash (`/src`) and still get a
 * backend-safe relative result.
 */
export function joinPath(dir: string, name: string): string {
    if (isAbsolutePath(dir)) {
        // A UNC dir keeps its "//server/share" prefix intact: the generic
        // collapse below would turn "//nas/media" into "/nas/media", silently
        // converting a share into a POSIX-style path.
        const unc = uncShareRoot(dir)
        if (unc) {
            const rest = normalizeSlashes(dir).slice(unc.length).replace(/^\/+|\/+$/g, '')
            return rest ? `${unc}/${rest}/${name}` : `${unc}/${name}`
        }
        // Normalize separators so the result matches the DOM data-path
        // convention (the backend returns forward slashes), collapse redundant
        // slashes, and drop a trailing slash — but keep the root's own "/":
        // `/` + `name` → `/name`, `/tmp/` + `name` → `/tmp/name`.
        const base = normalizeSlashes(dir).replace(/\/{2,}/g, '/').replace(/\/+$/, '')
        return base ? `${base}/${name}` : `/${name}`
    }
    const normalizedDir = dir.replace(/^\/+/, '').replace(/\/+$/, '')
    return normalizedDir ? normalizedDir + '/' + name : name
}

// Split a path into segments, handling both / and \ separators
export function splitPath(path: string): string[] {
    return path.split(/[/\\]/)
}

// Get the last segment of a path (filename or directory name)
export function baseName(path: string): string {
    const segments = splitPath(path)
    // Walk backwards to find the last non-empty segment
    // This handles trailing slashes correctly: /home/user/ → "user"
    for (let i = segments.length - 1; i >= 0; i--) {
        if (segments[i] !== '') return segments[i]
    }
    return path
}

/**
 * True for a Windows UNC share path — `\\server\share` or `//server/share`.
 *
 * Both spellings occur: the backend returns `filepath.ToSlash` output
 * (`//server/share`), while a user typing a path writes backslashes. The device
 * namespaces `\\.\` and `\\?\` are NOT share paths — they address devices, and
 * the backend rejects them, so they must not be treated as absolute here either
 * (doing so would offer a folder the picker can never open).
 *
 * A bare `\\server` is not a share: it is the network neighbourhood, which the
 * backend refuses to list.
 */
export function isUNCPath(path: string): boolean {
    const match = /^[\\/]{2}([^\\/]+)[\\/]/.exec(path)
    if (!match) return false
    const server = match[1]
    return server !== '.' && server !== '?' && server !== '..'
}

/**
 * Share root of a UNC path, normalized to the backend's form (`//server/share`).
 * Returns null for anything that is not a UNC share path.
 *
 * Used to keep the share prefix intact through joins/dirname/breadcrumbs — it
 * plays the role a drive root plays for `C:\` paths.
 */
export function uncShareRoot(path: string): string | null {
    const match = /^[\\/]{2}([^\\/]+)[\\/]+([^\\/]+)/.exec(path)
    if (!match) return null
    const [, server, share] = match
    if (server === '.' || server === '?' || server === '..') return null
    if (share === '.' || share === '..') return null
    return `//${server}/${share}`
}

// Detect Windows absolute paths: drive-letter roots (C:\ or C:/) and UNC
// share paths (\\server\share, or the //server/share form the backend emits).
// Unix absolute paths (leading "/") are matched separately by callers via
// startsWith('/').
export function isWindowsAbsolutePath(path: string): boolean {
    return /^[A-Za-z]:[/\\]/.test(path) || isUNCPath(path)
}

/**
 * Normalize Windows backslashes to forward slashes so all downstream path
 * matching (prefix checks, segment splitting, DOM data-path attributes) uses
 * a single separator convention. The Go backend returns absolute paths in
 * platform-native form (E:\… on Windows), while chat annotations and user
 * input may carry either separator style.
 */
export function normalizeSlashes(path: string): string {
    return path.replace(/\\/g, '/')
}

/**
 * True for an absolute path on any platform: Unix absolute ("/a/b") or
 * Windows drive/UNC absolute ("E:/…", "\\server\share"). Callers that need
 * normalized paths should pass the result of normalizeSlashes first.
 */
export function isAbsolutePath(path: string): boolean {
    return path.startsWith('/') || isWindowsAbsolutePath(path)
}

/**
 * Convert an absolute path that lies under `root` into a root-relative path.
 * Returns the original path when it is not under the root (or root is empty).
 * The comparison is case-insensitive for Windows drive letters, so "e:/…"
 * matches a root of "E:/…". Inputs may use either separator style.
 *
 * A POSIX root ("/") is never relativized: stripping the trailing separator
 * leaves "", which callers read as the PROJECT root — so browsing "/" would
 * silently land back inside the project instead of the filesystem root.
 */
export function toProjectRelative(path: string, root: string): string {
    if (!root) return path
    const normPath = normalizeSlashes(path).replace(/\/+$/, '')
    const normRoot = normalizeSlashes(root).replace(/\/+$/, '')
    if (normPath === '') return normalizeSlashes(path)
    if (normPath.toLowerCase() === normRoot.toLowerCase()) {
        return ''
    }
    const prefix = normRoot + '/'
    if (normPath.toLowerCase().startsWith(prefix.toLowerCase())) {
        return normPath.slice(normRoot.length + 1)
    }
    return normPath
}

// Get the parent directory of a path
export function dirName(path: string): string {
    // A UNC share root is its own top: `\\server\share` has no parent this app
    // can browse (the network neighbourhood is not listable), so it is
    // self-referential exactly like "/" and "C:\". Without this the join below
    // would produce "/server" — a POSIX path that silently escapes the share.
    // The input's own separator style is preserved so callers comparing
    // `dirName(dir) !== dir` see identity rather than a reformatted path.
    const unc = uncShareRoot(path)
    if (unc && normalizeSlashes(path).replace(/\/+$/, '').toLowerCase() === unc.toLowerCase()) {
        return path.replace(/[\\/]+$/, '') || unc
    }
    const parts = splitPath(path)
    parts.pop()
    if (parts.length === 0) return ''
    // Rejoin with original separator style
    const useBackslash = path.includes('\\') && !path.includes('/')
    const result = useBackslash ? parts.join('\\') : parts.join('/')
    // A depth-1 absolute path ("/tmp", "/etc") pops down to a single EMPTY
    // segment, which joins to "". Callers read "" as the PROJECT root, so an
    // external browse would silently jump back inside the project. Return the
    // filesystem root instead (POSIX dirname("/tmp") === "/").
    if (result === '' && path.startsWith('/')) return '/'
    // On Windows, a lone "C:" should be the drive root. Match the separator
    // style of the input so joinPath (which uses "/") stays consistent when
    // the path was normalized to forward slashes.
    if (/^[A-Za-z]:$/.test(result)) return useBackslash ? result + '\\' : result + '/'
    return result
}

/**
 * Convert an absolute path to a relative path based on a base path.
 * Returns the original path if base is empty or absPath does not start with basePath.
 * Returns '/' if the result would be empty (i.e., the path equals the base).
 * Handles mixed separators (forward/backslash) for cross-platform compatibility.
 */
export function toRelativePath(absPath: string, basePath: string): string {
    if (!basePath) return absPath
    // Normalize separators for comparison (Windows paths may mix / and \)
    const normAbs = absPath.replace(/\\/g, '/')
    const normBase = basePath.replace(/\\/g, '/')
    if (!normAbs.startsWith(normBase)) return absPath
    const rel = normAbs.slice(normBase.length).replace(/^\//, '')
    return rel || '/'
}

/**
 * Normalize a path for file-identity comparison:
 * forward slashes, no leading "./", no duplicate slashes, no trailing slash.
 * Does NOT resolve project-relative vs absolute — that's handled by callers
 * via toProjectRelative / sameFilePath.
 */
export function normalizeForCompare(path: string): string {
    // A UNC path's leading "//" is its root marker, not a duplicated slash:
    // collapsing it would turn `//nas/share/x` into `/nas/share/x` and make a
    // share path compare equal to a POSIX path.
    const unc = uncShareRoot(path)
    const rest = unc ? normalizeSlashes(path).slice(unc.length) : normalizeSlashes(path)
    const normalized = rest
        .replace(/^\.\/+/, '') // strip one leading "./" segment
        .replace(/\/+/g, '/') // collapse duplicate slashes
        .replace(/\/+$/, '') // strip trailing slash
    return unc ? unc + normalized : normalized
}

/**
 * Decide whether a tool-reported file path (Write/Edit file_path, possibly
 * absolute, project-relative, "./"-prefixed, or relative to a subdirectory)
 * refers to the currently viewed file.
 *
 * Strategy:
 * 1. Normalize both sides (slashes, "./", duplicates, trailing slash).
 * 2. When a project root is known, relativize any path that lies under it.
 *    This turns "E:/git/app/web/src/x.ts" and "web/src/x.ts" into the same
 *    comparison key. Paths outside the root are left untouched (absolute).
 * 3. Match by full equality; otherwise fall back to a "/"-boundary suffix
 *    match in either direction.
 *
 * Suffix-match rules (intentionally conservative to avoid false positives):
 * - The shorter side must contain at least one "/", so a bare basename
 *   ("x.ts") only matches by full equality and can never collide with a
 *   deeper same-named file ("web/src/x.ts").
 * - The match must align on a "/" boundary, so partial segment fragments
 *   ("ther.ts") never match.
 *
 * Known, intentional limit (kept for compatibility with tool paths relative
 * to a project subdirectory): when one side is a project-external absolute
 * path and the other a project-relative path whose tail is a "/"-boundary
 * suffix of it, they are treated as the same file. E.g. viewing an external
 * "/home/u/other/web/src/x.ts" and the agent editing the project's
 * "web/src/x.ts" would spuriously match. This mirrors the pre-existing
 * heuristic and is harmless (a refresh of unchanged content).
 */
export function sameFilePath(
    a: string,
    b: string,
    projectRoot?: string,
): boolean {
    if (!a || !b) return false
    let normA = normalizeForCompare(a)
    let normB = normalizeForCompare(b)
    if (projectRoot) {
        normA = toProjectRelative(normA, projectRoot)
        normB = toProjectRelative(normB, projectRoot)
    }
    if (normA === normB) return true
    // Suffix fallback: only when the suffix side itself carries a directory,
    // so a bare basename never collides with a deeper same-named file.
    const aHasDir = normA.includes('/')
    const bHasDir = normB.includes('/')
    if (bHasDir && normA.endsWith('/' + normB)) return true
    if (aHasDir && normB.endsWith('/' + normA)) return true
    return false
}
