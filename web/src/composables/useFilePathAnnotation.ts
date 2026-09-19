import { escapeHtml } from '@/utils/html.ts'
import { splitPath, dirName, normalizeSlashes, isAbsolutePath, toProjectRelative } from '@/utils/path.ts'
import {
    parseLineRanges,
    serializeLineRanges,
    firstLineTarget,
    LINE_SUFFIX_RE,
    LINE_FRAGMENT_RE,
    LINE_TOKEN_SRC,
    type LineRange,
} from '@/utils/lineRanges.ts'
import { store } from '@/stores/app.ts'
import { gt } from '@/composables/useLocale'
import { clearCommitHashCache } from '@/composables/useCommitHashAnnotation.ts'
import type { NavigationSurface } from '@/composables/useNavigationContext'
// NOTE: do NOT import clearWorktreeCache from useWorktreeAnnotation here —
// that creates a circular dependency (useFilePathAnnotation ↔ useWorktreeAnnotation).
// Instead, we use a lazy indirection registered at init time.
let _clearWorktreeCache: (() => void) | null = null

export function registerWorktreeCacheClearter(fn: () => void) {
  _clearWorktreeCache = fn
}

// ── Dual-candidate resolution types ─────────────────────────────────────────────

/**
 * Result of dual-candidate file path resolution.
 * - primary: preferred path (baseDir-relative if applicable, else projectRoot-relative)
 * - fallback: alternative path (projectRoot-relative). Same as primary when no baseDir
 *   or when both resolutions produce the same result.
 */
export interface ResolveResult {
    primary: string
    fallback: string
}

// ── URI decoding ────────────────────────────────────────────────────────────────

/**
 * Try to decode a percent-encoded URI component.
 * Browsers/DOMPurify may encode non-ASCII chars (e.g. 中文 → %E4%B8%AD%E6%96%87)
 * in href attributes when HTML is inserted via innerHTML/v-html.
 */
function tryDecodeUri(uri: string): string {
    try {
        if (!uri.includes('%')) return uri
        return decodeURIComponent(uri)
    } catch {
        return uri
    }
}

// ── File URI parsing ───────────────────────────────────────────────────────────

export interface ParsedFileUri {
    /** Clean filesystem path (percent-decoded, no file:// / hash / :line suffix). */
    path: string
    /** Earliest line (backward-compat single-target view of lineRanges). */
    lineStart?: number
    /** Earliest range end (omitted for a single-line target). */
    lineEnd?: number
    /** Full parsed range list; empty when no line suffix was present. */
    lineRanges: LineRange[]
}

/**
 * Parse a raw URI/path string into a clean filesystem path and optional line ranges.
 *
 * Supported forms:
 *   - file:///abs/path, file://localhost/abs/path
 *   - /abs/path, rel/path, ../rel/path
 *   - Optional line target: #L10-L20, #L10, #10-20, #10, or a trailing
 *     :10-20 / :10 — plus comma-separated lists (e.g. :90-91,309,938-943)
 *     with optional spaces after commas and an optional `L` prefix per token.
 *
 * Percent-encoded path components are decoded (e.g. %E4%B8%AD → 中).
 * Non-numeric hashes (e.g. "#section") are dropped from the path.
 * A trailing ":N[-M][,N…]" is only treated as a line range when it is the
 * last thing in the string, so Windows drive letters (C:/…) are unaffected.
 */
export function parseFileUri(rawInput: string): ParsedFileUri {
    const input = (rawInput ?? '').trim()
    if (!input) return { path: '', lineRanges: [] }

    let raw = input

    // 1. Strip the file:// scheme (handles file:///path and file://host/path).
    if (raw.startsWith('file://')) {
        raw = raw.slice('file://'.length)
        if (!raw.startsWith('/')) {
            // file://host/… → drop the host, keep the absolute path.
            const slash = raw.indexOf('/')
            raw = slash === -1 ? '' : raw.slice(slash)
        }
        // file:///… leaves raw already starting with "/".
    }

    let lineRanges: LineRange[] = []

    // 2. Extract the line fragment from a hash (#L10-L20 / #10 / #section).
    const hashIdx = raw.indexOf('#')
    if (hashIdx !== -1) {
        const hash = raw.slice(hashIdx + 1)
        raw = raw.slice(0, hashIdx)
        if (LINE_FRAGMENT_RE.test(hash)) {
            lineRanges = parseLineRanges(hash)
        }
    }

    // 3. Fall back to a trailing ":N[-M][,N…]" suffix when no hash was present.
    // The suffix is stripped whenever it matches, even if every token is
    // invalid (e.g. ":0") — the path itself stays clean, matching the
    // historical single-range behavior.
    if (lineRanges.length === 0) {
        const cm = raw.match(LINE_SUFFIX_RE)
        if (cm) {
            raw = raw.slice(0, raw.length - cm[0].length)
            lineRanges = parseLineRanges(cm[1])
        }
    }

    // 4. Percent-decode the remaining path.
    if (raw.includes('%')) {
        try {
            raw = decodeURIComponent(raw)
        } catch {
            // Ignore malformed percent sequences; keep the raw path.
        }
    }

    const { lineStart, lineEnd } = firstLineTarget(lineRanges)
    return { path: raw, lineStart, lineEnd, lineRanges }
}

// ── Path resolution helpers ────────────────────────────────────────────────────

/**
 * Resolve a relative path against a base directory.
 * Returns project-relative path if within project, absolute path if outside,
 * or null if resolution fails.
 *
 * All inputs are expected to be normalized to forward slashes.
 * Windows drive prefixes ("E:") are preserved as the leading segment so the
 * result stays a valid absolute drive path (e.g. "E:/git/…") instead of
 * becoming "/E:/git/…".
 */
function resolveRelativePathAgainstBase(path: string, baseDir: string, projectRoot: string): string | null {
    const baseParts = baseDir.split('/').filter(Boolean)
    const segments = path.split('/')
    for (const seg of segments) {
        if (seg === '..') {
            if (baseParts.length > 0) baseParts.pop()
            else return null
        } else if (seg !== '.' && seg !== '') {
            baseParts.push(seg)
        }
    }
    const absolutePath = joinAbsolutePath(baseParts)
    if (projectRoot && absolutePath.startsWith(projectRoot + '/')) {
        return absolutePath.slice(projectRoot.length + 1)
    }
    if (projectRoot && absolutePath === projectRoot) return null
    return absolutePath
}

/**
 * Join path segments into an absolute path, preserving a Windows drive-letter
 * prefix ("E:") so "E:/git/x" stays "E:/git/x" rather than "/E:/git/x".
 * Segments are expected to be forward-slash separated.
 */
function joinAbsolutePath(parts: string[]): string {
    if (parts.length > 0 && /^[A-Za-z]:$/.test(parts[0])) {
        return parts.join('/')
    }
    return '/' + parts.join('/')
}

/**
 * Resolve a relative path against projectRoot only.
 * Returns ResolveResult where primary === fallback (single candidate).
 *
 * projectRoot is expected to be normalized to forward slashes.
 */
function resolveAgainstProjectRoot(path: string, projectRoot: string): ResolveResult | null {
    if (!projectRoot) return null
    const parts = projectRoot.split('/').filter(Boolean)
    const segments = path.split('/')
    for (const seg of segments) {
        if (seg === '..') {
            if (parts.length > 0) parts.pop()
            else return null
        } else if (seg !== '.' && seg !== '') {
            parts.push(seg)
        }
    }
    const absolutePath = joinAbsolutePath(parts)
    if (absolutePath.startsWith(projectRoot + '/')) {
        const rel = absolutePath.slice(projectRoot.length + 1)
        return { primary: rel, fallback: rel }
    }
    if (absolutePath === projectRoot) return null
    return { primary: absolutePath, fallback: absolutePath }
}

// ── Core dual-candidate resolution ─────────────────────────────────────────────

/**
 * Rejection checks shared by resolveFilePathDual and looksLikeFilePath.
 * Returns true if the path should be rejected (glob, URL, env var, bare identifier).
 */
function shouldRejectPath(path: string): boolean {
    // Note: backslash is NOT rejected — it is the Windows path separator
    // (e.g. E:\git\...). Only glob wildcards and shell chars are rejected.
    if (/[*?[\]<>]/.test(path) || path.includes('**')) return true
    if (/^https?:\/\//i.test(path)) return true
    if (/\$/.test(path)) return true
    return false
}

/**
 * Resolve a file path with dual-candidate support.
 *
 * Returns a ResolveResult with:
 * - primary: the preferred resolution (baseDir-relative if available and project-internal)
 * - fallback: the projectRoot-relative resolution (for async verification fallback)
 *
 * When there is no baseDir or baseDir === projectRoot, primary === fallback.
 * When baseDir resolves to a project-external absolute path, primary === fallback (projectRoot wins).
 * When baseDir resolves to a different project-internal path, primary = baseDir result, fallback = projectRoot result.
 */
export function resolveFilePathDual(path: string, projectRoot: string, homeDir?: string, baseDir?: string): ResolveResult | null {
    // Normalize Windows backslashes to forward slashes so all prefix matching
    // and segment splitting below is consistent across platforms. The backend
    // returns absolute paths in platform-native form (E:\… on Windows), and
    // chat annotations may carry either separator style.
    // This must happen before the bare-identifier check below, so a Windows
    // directory path written with backslashes (E:\git\…, no extension) is not
    // rejected for lacking a "/" separator.
    path = normalizeSlashes(path)
    projectRoot = normalizeSlashes(projectRoot)
    if (homeDir) homeDir = normalizeSlashes(homeDir)
    if (baseDir) baseDir = normalizeSlashes(baseDir)

    // Reject glob patterns, URLs, env vars
    if (shouldRejectPath(path)) return null
    // Reject bare identifiers without / or file extension
    if (!/\//.test(path) && !/\.[a-zA-Z][a-zA-Z0-9]{0,3}$/.test(path.replace(LINE_SUFFIX_RE, ''))) return null

    // ── Tilde expansion ──
    if (path.startsWith('~/') || path === '~') {
        if (!homeDir) return null
        const expanded = homeDir + path.slice(1)
        if (!projectRoot) return { primary: expanded, fallback: expanded }
        if (expanded.startsWith(projectRoot + '/')) {
            const rel = expanded.slice(projectRoot.length + 1)
            return { primary: rel, fallback: rel }
        }
        if (expanded === projectRoot) return null
        return { primary: expanded, fallback: expanded }
    }

    // ── Absolute path (Unix "/" or Windows drive/UNC) ──
    if (isAbsolutePath(path)) {
        if (!projectRoot) return { primary: path, fallback: path }
        if (path.startsWith(projectRoot + '/')) {
            const rel = path.slice(projectRoot.length + 1)
            return { primary: rel, fallback: rel }
        }
        if (path === projectRoot) return null
        return { primary: path, fallback: path }
    }

    // ── Relative path without any root ──
    if (!projectRoot && !baseDir) {
        const clean = path.replace(/^\.\//, '')
        if (clean.startsWith('../')) return null
        return { primary: clean, fallback: clean }
    }

    // ── Relative path: compute projectRoot candidate (always the fallback) ──
    const projectResult = resolveAgainstProjectRoot(path, projectRoot)

    // No separate baseDir → single candidate
    if (!baseDir || baseDir === projectRoot) {
        return projectResult
    }

    // Normalize baseDir: if project-relative, convert to absolute
    const absBaseDir = isAbsolutePath(baseDir) ? baseDir : (projectRoot + '/' + baseDir)

    // Compute baseDir candidate
    const baseDirResult = resolveRelativePathAgainstBase(path, absBaseDir, projectRoot)

    // baseDir failed or resolved to project-external absolute → projectRoot wins
    if (!baseDirResult || isAbsolutePath(baseDirResult)) {
        return projectResult
    }

    // baseDir resolved to project-internal path → use as primary, projectRoot as fallback
    // If projectResult is project-external (e.g. ../README.md walks above projectRoot),
    // try a stripped fallback: resolve the path without leading ../ segments against projectRoot.
    // This handles the common pattern where ../README.md from a subdirectory is intended
    // to mean the project root's README.md.
    if (!projectResult) return { primary: baseDirResult, fallback: baseDirResult }

    // projectResult is project-external → try stripped fallback
    if (isAbsolutePath(projectResult.primary)) {
        const stripped = path.replace(/^(?:\.\.\/)+/, '')
        if (stripped !== path) {
            const strippedResult = resolveAgainstProjectRoot(stripped, projectRoot)
            if (strippedResult && !isAbsolutePath(strippedResult.primary)) {
                if (baseDirResult === strippedResult.primary) {
                    return strippedResult
                }
                return {
                    primary: baseDirResult,
                    fallback: strippedResult.primary,
                }
            }
        }
        // No valid stripped fallback → single candidate
        return { primary: baseDirResult, fallback: baseDirResult }
    }

    // Same path — no fallback needed
    if (baseDirResult === projectResult.primary) {
        return projectResult
    }

    return {
        primary: baseDirResult,
        fallback: projectResult.primary,
    }
}

/**
 * Convenience wrapper: resolve a file path and return only the primary candidate.
 * Used by renderToolDetail.ts (8 call sites) and other callers that don't need fallback.
 */
export function resolveFilePath(path: string, projectRoot: string, homeDir?: string, baseDir?: string): string | null {
    const result = resolveFilePathDual(path, projectRoot, homeDir, baseDir)
    return result?.primary ?? null
}

// ── SVG icon & button HTML ─────────────────────────────────────────────────────

export const FILE_OPEN_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>'

/**
 * Build the line-target attribute pairs shared by annotated paths and open
 * buttons. `data-line-start`/`data-line-end` carry the earliest range (legacy
 * consumers keep working); `data-line-ranges` carries the full comma list.
 * `data-line-ranges` is omitted for a single-line target (redundant).
 */
export function lineTargetAttrPairs(lineRanges: LineRange[] | undefined): Array<[string, string]> {
    if (!lineRanges || lineRanges.length === 0) return []
    const { lineStart, lineEnd } = firstLineTarget(lineRanges)
    if (!lineStart) return []
    const pairs: Array<[string, string]> = [['data-line-start', String(lineStart)]]
    if (lineEnd) pairs.push(['data-line-end', String(lineEnd)])
    const serialized = serializeLineRanges(lineRanges)
    if (lineRanges.length > 1) pairs.push(['data-line-ranges', serialized])
    return pairs
}

/** Apply line-target attributes directly onto a DOM element. */
export function applyLineTargetAttrs(el: Element, lineRanges: LineRange[] | undefined): void {
    for (const [name, value] of lineTargetAttrPairs(lineRanges)) el.setAttribute(name, value)
}

/**
 * Generate HTML for the small open-file button.
 * Optionally includes line range attributes and a fallback path for dual-candidate verification.
 */
export function fileOpenButtonHtml(resolvedPath: string, lineStart?: number, lineEnd?: number, fallbackPath?: string, lineRanges?: LineRange[]): string {
    const isExternal = isAbsolutePath(resolvedPath)
    const pairs = lineRanges && lineRanges.length > 0
        ? lineTargetAttrPairs(lineRanges)
        : (lineStart ? [['data-line-start', String(lineStart)] as [string, string], ...(lineEnd ? [['data-line-end', String(lineEnd)] as [string, string]] : [])] : [])
    const lineAttrs = pairs.map(([name, value]) => ` ${name}="${escapeHtml(value)}"`).join('')
    const externalClass = isExternal ? ' external' : ''
    const fallbackAttr = fallbackPath && fallbackPath !== resolvedPath ? ` data-fallback-path="${escapeHtml(fallbackPath)}"` : ''
    return `<button class="chat-file-open-btn${externalClass}" data-file-path="${escapeHtml(resolvedPath)}"${fallbackAttr}${lineAttrs} title="${escapeHtml(gt('chat.attach.openFile'))}">${FILE_OPEN_ICON_SVG}</button>`
}

/**
 * Read the file target (path + full line ranges) from an annotated DOM element.
 * Centralizes the attribute reads repeated across chat/task/file click handlers.
 * `lineRanges` is the canonical serialized string; `data-line-ranges` is
 * authoritative, `data-line-start`/`data-line-end` remain the fallback for
 * elements produced before multi-range support.
 */
export function readLineTargetFromEl(el: Element): { filePath: string | null; lineRanges?: string; lineStart?: number; lineEnd?: number } {
    const filePath = el.getAttribute('data-file-path')
    const rangesAttr = el.getAttribute('data-line-ranges')
    if (rangesAttr) {
        const ranges = parseLineRanges(rangesAttr)
        if (ranges.length > 0) {
            return { filePath, lineRanges: serializeLineRanges(ranges), ...firstLineTarget(ranges) }
        }
    }
    const startAttr = el.getAttribute('data-line-start')
    const endAttr = el.getAttribute('data-line-end')
    const lineStart = startAttr ? parseInt(startAttr, 10) : undefined
    const lineEnd = endAttr ? parseInt(endAttr, 10) : undefined
    return { filePath, lineStart, lineEnd }
}

// ── Line info extraction ────────────────────────────────────────────────────────

/**
 * Extract the bare file path and optional line ranges from a regex match whose
 * single capture group (match[1]) holds the whole comma list.
 * E.g. "src/main.go:70-81" → { path: "src/main.go", lineRanges: [{start:70,end:81}] }
 */
function extractLineInfo(matchStr: string, match: RegExpExecArray): { path: string; lineRanges: LineRange[] } {
    const suffix = match[1]
    if (!suffix) return { path: matchStr, lineRanges: [] }
    const lineRanges = parseLineRanges(suffix)
    if (lineRanges.length === 0) return { path: matchStr, lineRanges: [] }
    const path = matchStr.slice(0, matchStr.length - suffix.length - 1)
    return { path, lineRanges }
}

/**
 * Extract bare path and optional line ranges from a plain text string.
 * Used by Step 2 for <code> tag content.
 */
function extractLineInfoFromText(text: string): { path: string; lineRanges: LineRange[] } {
    const m = text.match(LINE_SUFFIX_RE)
    if (!m) return { path: text, lineRanges: [] }
    const lineRanges = parseLineRanges(m[1])
    if (lineRanges.length === 0) return { path: text, lineRanges: [] }
    return { path: text.slice(0, text.length - m[0].length), lineRanges }
}

// ── Path detection regex & helper ───────────────────────────────────────────────

// NOTE: segment classes exclude '/' (and '\\' for the Windows drive form) so that
// separators are structurally unique. Otherwise a long whitespace-free string with
// many slashes but no file extension (e.g. a 2KB+ Base64 blob) triggers catastrophic
// backtracking (2^slashCount) and freezes the UI thread. A dedicated dotfile branch
// preserves matching of hidden last segments (e.g. /project/.worktrees).
// The trailing suffix is a single capture group holding the whole comma list
// (see LINE_TOKEN_SRC). Each list iteration consumes a literal comma, so the
// suffix adds no backtracking risk.
const FILE_PATH_SRC = '(?:~?\\/[^/\\s<>"\')\\]]+(?:\\/[^/\\s<>"\')\\]]+)+\\.[a-zA-Z][a-zA-Z0-9]*|~?\\/[^/\\s<>"\')\\]]+(?:\\/[^/\\s<>"\')\\]]+)+\\/\\.[^/\\s<>"\')\\]]+|\\.\\.?\\/[^/\\s<>"\')\\]]+(?:\\/[^/\\s<>"\')\\]]+)*\\.[a-zA-Z][a-zA-Z0-9]*|[a-zA-Z0-9_-]+(?:\\/[a-zA-Z0-9_.-]+)+\\.[a-zA-Z][a-zA-Z0-9]*|[A-Za-z]:[\\\\/](?![\\\\/])[^\\\\/\\s<>"\')\\]]+(?:[\\\\/][^\\\\/\\s<>"\')\\]]+)*(?:\\.[a-zA-Z][a-zA-Z0-9]*)?)'
const FILE_PATH_RE = new RegExp(FILE_PATH_SRC + '(?::(' + LINE_TOKEN_SRC + '))?', 'g')

/**
 * Check if a string looks like a file path that should be annotated.
 * Rejects bare identifiers like `useAutoSpeech`, `onUnmounted`, `ref`.
 */
export function looksLikeFilePath(text: string): boolean {
    if (shouldRejectPath(text)) return false
    const bare = text.replace(LINE_SUFFIX_RE, '')
    return /\/|\.[a-zA-Z][a-zA-Z0-9]{0,3}$/.test(bare)
}

// ── HTML annotation ────────────────────────────────────────────────────────────

export interface AnnotateFilePathsOptions {
    projectRoot: string
    /** Base directory for resolving relative paths (e.g. the md file's dir) */
    baseDir?: string
    /** User's home directory (from backend), used to expand ~/ paths */
    homeDir?: string
}

/**
 * Helper: push primary and fallback paths to detectedPaths list.
 * Always pushes primary; pushes fallback only if it differs from primary.
 */
function pushDetectedPaths(detectedPaths: string[], result: ResolveResult): void {
    detectedPaths.push(result.primary)
    if (result.fallback !== result.primary) {
        detectedPaths.push(result.fallback)
    }
}

/**
 * Detect file paths in rendered HTML and insert open-file buttons after them.
 *
 * Uses DOMParser + TreeWalker for robust HTML traversal. Dual-candidate resolution
 * stores both primary (baseDir-relative) and fallback (projectRoot-relative) paths,
 * enabling verifyFilePaths to swap to the fallback when the primary doesn't exist.
 *
 * Processing order:
 *   1. <a href="..."> tags with local-file hrefs → append open button
 *   2. <code> tags whose text content looks like a path → add class + button
 *   3. Text nodes (outside a/code) → regex match paths → insert span + button
 */
export function annotateFilePaths(
    html: string,
    options: AnnotateFilePathsOptions
): { html: string; detectedPaths: string[] } {
    if (!html) return { html: '', detectedPaths: [] }

    const doc = new DOMParser().parseFromString(html, 'text/html')
    const detectedPaths = annotateFilePathsIn(doc, options)
    return { html: doc.body.innerHTML, detectedPaths }
}

/**
 * Annotate file paths inside an already-parsed Document, mutating it in place.
 *
 * Split out of `annotateFilePaths` so the markdown pipeline can run several
 * annotation steps over ONE parsed document instead of each step paying its own
 * `parseFromString` + `body.innerHTML` round trip. Behaviour is identical to
 * the string wrapper — that wrapper is now a thin parse/call/serialize shim.
 */
export function annotateFilePathsIn(
    doc: Document,
    options: AnnotateFilePathsOptions
): string[] {
    const { projectRoot, baseDir, homeDir } = options
    const detectedPaths: string[] = []

    // ── Step 1: <a> tags with local-file hrefs ──
    for (const a of doc.querySelectorAll('a[href]')) {
        const rawHref = a.getAttribute('href')!
        const href = tryDecodeUri(rawHref)
        if (/^(https?:|\/\/|mailto:|tel:|#)/i.test(href)) continue
        const parsed = parseFileUri(href)
        if (!parsed.path) continue
        const resolved = (isAbsolutePath(parsed.path) || !baseDir)
            ? resolveFilePath(parsed.path, projectRoot, homeDir)
            : resolveRelativePath(parsed.path, baseDir)
        if (!resolved) continue
        detectedPaths.push(resolved)
        // Mark the <a> so click handlers can open the resolved path with its
        // line range; the button is an additional affordance.
        a.setAttribute('data-file-path', resolved)
        applyLineTargetAttrs(a, parsed.lineRanges)
        a.classList.add('chat-file-path')
        a.insertAdjacentHTML('afterend', fileOpenButtonHtml(resolved, parsed.lineStart, parsed.lineEnd, undefined, parsed.lineRanges))
    }

    // ── Step 2: <code> tags whose content is purely a file path ──
    for (const code of doc.querySelectorAll('code')) {
        if (code.closest('a')) continue
        if (code.classList.contains('chat-worktree-path')) continue
        const stripped = (code.textContent || '').trim()
        if (!looksLikeFilePath(stripped)) continue
        const { path: barePath, lineRanges } = extractLineInfoFromText(stripped)
        const result = resolveFilePathDual(barePath, projectRoot, homeDir, baseDir)
        if (!result || result.primary.includes(' ') || result.primary.includes('"')) continue
        pushDetectedPaths(detectedPaths, result)
        code.classList.add('chat-file-path')
        code.setAttribute('data-file-path', result.primary)
        if (result.fallback !== result.primary) code.setAttribute('data-fallback-path', result.fallback)
        if (isAbsolutePath(result.primary)) code.setAttribute('data-external', 'true')
        applyLineTargetAttrs(code, lineRanges)
        code.insertAdjacentHTML('afterend', fileOpenButtonHtml(result.primary, undefined, undefined, result.fallback !== result.primary ? result.fallback : undefined, lineRanges))
    }

    // ── Step 3: Text nodes → regex match paths ──
    const textNodes: Text[] = []
    const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT, {
        acceptNode(node: Text) {
            const parent = node.parentElement
            if (!parent) return NodeFilter.FILTER_REJECT
            if (parent.tagName === 'A' || parent.closest('a')) return NodeFilter.FILTER_REJECT
            if (parent.classList.contains('chat-file-path')) return NodeFilter.FILTER_REJECT
            if (parent.classList.contains('chat-worktree-path') || parent.closest('.chat-worktree-path')) return NodeFilter.FILTER_REJECT
            return NodeFilter.FILTER_ACCEPT
        }
    })
    while (walker.nextNode()) textNodes.push(walker.currentNode as Text)

    for (let i = textNodes.length - 1; i >= 0; i--) {
        const textNode = textNodes[i]
        const text = textNode.textContent || ''
        FILE_PATH_RE.lastIndex = 0
        if (!FILE_PATH_RE.test(text)) continue

        FILE_PATH_RE.lastIndex = 0
        const parts: Array<{ text: string; result: ResolveResult | null; lineRanges?: LineRange[] }> = []
        let lastIndex = 0
        let match: RegExpExecArray | null
        while ((match = FILE_PATH_RE.exec(text)) !== null) {
            const pathStr = match[0]
            const { path: barePath, lineRanges } = extractLineInfo(pathStr, match)
            let result = resolveFilePathDual(barePath, projectRoot, homeDir, baseDir)
            // Directory-prefix suppression: if match is followed by /segment, skip it
            if (result) {
                const afterIdx = match.index + pathStr.length
                if (afterIdx < text.length && text[afterIdx] === '/') {
                    const rest = text.slice(afterIdx + 1)
                    if (rest.length > 0 && /^[a-zA-Z0-9_.-]/.test(rest)) {
                        result = null
                    }
                }
            }
            if (match.index > lastIndex) {
                parts.push({ text: text.slice(lastIndex, match.index), result: null })
            }
            parts.push({ text: pathStr, result, lineRanges: result ? lineRanges : undefined })
            lastIndex = match.index + pathStr.length
        }
        if (lastIndex < text.length) {
            parts.push({ text: text.slice(lastIndex), result: null })
        }

        // Build replacement nodes
        const parent = textNode.parentNode!
        const frag = doc.createDocumentFragment()
        let hasAnnotation = false
        for (const part of parts) {
            if (part.result) {
                hasAnnotation = true
                pushDetectedPaths(detectedPaths, part.result)
                const span = doc.createElement('span')
                span.className = 'chat-file-path'
                span.setAttribute('data-file-path', part.result.primary)
                if (part.result.fallback !== part.result.primary) span.setAttribute('data-fallback-path', part.result.fallback)
                if (isAbsolutePath(part.result.primary)) span.setAttribute('data-external', 'true')
                applyLineTargetAttrs(span, part.lineRanges)
                span.textContent = part.text
                frag.appendChild(span)
                const btnContainer = doc.createElement('span')
                btnContainer.innerHTML = fileOpenButtonHtml(part.result.primary, undefined, undefined, part.result.fallback !== part.result.primary ? part.result.fallback : undefined, part.lineRanges)
                while (btnContainer.firstChild) frag.appendChild(btnContainer.firstChild)
            } else {
                frag.appendChild(doc.createTextNode(part.text))
            }
        }

        if (hasAnnotation) {
            parent.replaceChild(frag, textNode)
        }
    }

    return detectedPaths
}

// ── Async verification with fallback swap ──────────────────────────────────────

/** Path type from batch-exists API: 'file', 'dir', or 'none' (not found). */
export type PathType = 'file' | 'dir' | 'none'

const MAX_CACHE_SIZE = 500
const verifiedCache = new Map<string, PathType>()

function cacheSet(key: string, value: PathType): void {
    if (verifiedCache.size >= MAX_CACHE_SIZE && !verifiedCache.has(key)) {
        const oldest = verifiedCache.keys().next().value
        if (oldest !== undefined) verifiedCache.delete(oldest)
    }
    verifiedCache.set(key, value)
}

let pendingPaths: string[] = []
let batchInFlight: Promise<void> | null = null

/**
 * Max paths per batch-exists request. `ServeFileBatchExists` rejects more than
 * this with 400 TooManyPaths, so requests are chunked to stay under it.
 */
const MAX_BATCH_PATHS = 100

/**
 * POST one chunk of paths and return the per-path types.
 *
 * Returns null when the response is unusable — a non-OK status (e.g. 400
 * TooManyPaths, or a 5xx) or a body without `results`. Callers must NOT treat
 * that as "these paths do not exist": the server never answered the question,
 * and caching 'none' would strip annotations from perfectly real files.
 */
async function fetchPathTypes(paths: string[]): Promise<Record<string, string> | null> {
    const resp = await fetch('/api/file/batch-exists', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ paths }),
    })
    if (!resp.ok) return null
    const data = await resp.json() as { results?: Record<string, string> }
    return data.results ?? null
}

async function drainBatch(): Promise<void> {
    const paths = [...new Set(pendingPaths)]
    pendingPaths = []

    for (let i = 0; i < paths.length; i += MAX_BATCH_PATHS) {
        const chunk = paths.slice(i, i + MAX_BATCH_PATHS)
        let results: Record<string, string> | null
        try {
            results = await fetchPathTypes(chunk)
        } catch {
            // Network error for THIS chunk. Assume these paths don't exist —
            // safer than assuming they do, which would leave dead annotations.
            //
            // Scoped to `chunk`, never the whole list: earlier chunks may have
            // already been verified as real files, and cacheSet would overwrite
            // those with 'none' (a cached 'none' is never re-checked and strips
            // the annotation outright).
            for (const p of chunk) cacheSet(p, 'none')
            return
        }
        if (!results) {
            // The server answered but told us nothing usable (non-OK status, or
            // no `results` field). Leave this and the remaining chunks uncached
            // rather than marking them missing, so their annotations survive and
            // a later pass can retry.
            return
        }
        for (const [path, type] of Object.entries(results)) {
            if (type === 'file' || type === 'dir') {
                cacheSet(path, type)
            } else {
                cacheSet(path, 'none')
            }
        }
    }
}

/**
 * Verify which file paths actually exist on the server.
 * For non-existent paths with a data-fallback-path, swap to the fallback
 * (if it exists) instead of removing the annotation entirely.
 * For project-external directories, remove the annotation (only external files are annotated).
 */
export async function verifyFilePaths(paths: string[], containerEl: HTMLElement): Promise<void> {
    const unique = [...new Set(paths)]
    if (unique.length === 0) return

    const uncached: string[] = []
    const results = new Map<string, PathType>()

    for (const p of unique) {
        if (verifiedCache.has(p)) {
            results.set(p, verifiedCache.get(p)!)
        } else {
            uncached.push(p)
        }
    }

    if (uncached.length > 0) {
        pendingPaths.push(...uncached)

        if (!batchInFlight) {
            batchInFlight = (async () => {
                while (pendingPaths.length > 0) {
                    await drainBatch()
                }
                batchInFlight = null
            })()
        }

        await batchInFlight

        for (const p of uncached) {
            if (verifiedCache.has(p)) {
                results.set(p, verifiedCache.get(p)!)
            }
        }
    }

    // Process paths based on type
    for (const [path, pathType] of results) {
        // Keep existing files
        if (pathType === 'file') {
            containerEl.querySelectorAll(`[data-file-path="${CSS.escape(path)}"]`).forEach(el => {
                el.setAttribute('data-path-type', 'file')
            })
            continue
        }

        // Keep project-internal directories (valid navigation targets)
        if (pathType === 'dir') {
            // Remove project-external directory annotations
            containerEl.querySelectorAll(`.chat-file-open-btn[data-file-path="${CSS.escape(path)}"].external`).forEach(btn => btn.remove())
            containerEl.querySelectorAll(`.chat-file-path[data-file-path="${CSS.escape(path)}"][data-external="true"], .code-file-path[data-file-path="${CSS.escape(path)}"][data-external="true"]`).forEach(el => {
                if (el.tagName === 'A' || el.tagName === 'CODE') {
                    // Keep the element but drop its file-open affordances.
                    el.classList.remove('chat-file-path', 'code-file-path', 'external')
                    el.removeAttribute('data-file-path')
                    el.removeAttribute('data-fallback-path')
                    el.removeAttribute('data-external')
                    el.removeAttribute('data-line-start')
                    el.removeAttribute('data-line-end')
                    el.removeAttribute('data-line-ranges')
                } else {
                    el.replaceWith(...el.childNodes)
                }
            })
            containerEl.querySelectorAll(`[data-file-path="${CSS.escape(path)}"]`).forEach(el => {
                el.setAttribute('data-path-type', 'dir')
            })
            continue
        }

        // pathType === 'none' — try fallback swap before removing.
        //
        // Swap when the fallback exists as a file, or as a project-INTERNAL
        // directory. The directory case matters because it is the normal shape
        // for a directory written in a doc: the primary candidate is resolved
        // relative to the FILE's own directory (which usually does not contain
        // that directory), while the real target is the project-root fallback.
        // e.g. `web/src/composables` in test/path-annotation/README.md →
        // primary `test/path-annotation/web/src/composables` (none) + fallback
        // `web/src/composables` (dir). Skipping dir fallbacks stripped the
        // annotation even though the directory exists.
        //
        // Project-EXTERNAL directories stay excluded: they are not valid
        // navigation targets, and the `pathType === 'dir'` branch above strips
        // them for the same reason.
        const els = containerEl.querySelectorAll(`[data-file-path="${CSS.escape(path)}"]`)
        let swapped = false
        for (const el of els) {
            const fallback = el.getAttribute('data-fallback-path')
            if (!fallback) continue
            const fallbackType = results.get(fallback)
            const isNowExternal = isAbsolutePath(fallback)
            const canSwap = fallbackType === 'file'
                || (fallbackType === 'dir' && !isNowExternal)
            if (!canSwap) continue

            // Swap data-file-path to fallback
            el.setAttribute('data-file-path', fallback)
            el.removeAttribute('data-fallback-path')
            el.setAttribute('data-path-type', fallbackType!)
            // Update external status
            if (isNowExternal) {
                el.setAttribute('data-external', 'true')
                el.classList.add('external')
            } else {
                el.removeAttribute('data-external')
                el.classList.remove('external')
            }
            swapped = true
        }
        if (swapped) continue

        // No fallback available — remove annotation
        containerEl.querySelectorAll(`.chat-file-open-btn[data-file-path="${CSS.escape(path)}"]`).forEach(btn => {
            btn.remove()
        })
        containerEl.querySelectorAll(`.chat-file-path[data-file-path="${CSS.escape(path)}"], .code-file-path[data-file-path="${CSS.escape(path)}"]`).forEach(el => {
            if (el.tagName === 'A' || el.tagName === 'CODE') {
                // Keep the element but drop its file-open affordances.
                el.classList.remove('chat-file-path', 'code-file-path')
                el.removeAttribute('data-file-path')
                el.removeAttribute('data-fallback-path')
                el.removeAttribute('data-external')
                el.removeAttribute('data-line-start')
                el.removeAttribute('data-line-end')
                el.removeAttribute('data-line-ranges')
            } else {
                el.replaceWith(...el.childNodes)
            }
        })
    }
}

export function clearVerifiedCache(): void {
    verifiedCache.clear()
    pendingPaths = []
    batchInFlight = null
    clearCommitHashCache()
    _clearWorktreeCache?.()
}

// ── Composable ─────────────────────────────────────────────────────────────────

export function useFilePathAnnotation() {
    return {
        parseFileUri,
        resolveFilePath,
        resolveFilePathDual,
        fileOpenButtonHtml,
        readLineTargetFromEl,
        annotateFilePaths,
        verifyFilePaths,
        resolveRelativePath,
        tryResolveCodeString,
        stripCodeString,
        openFilePath,
        navToFileInManager,
        revealInFileManager,
        clearVerifiedCache,
    }
}

// ── Shared helpers (used by CodePreview.vue) ───────────────────────────────────

/**
 * Resolve a relative href against a base directory.
 * Returns the resolved project-relative path.
 */
export function resolveRelativePath(href: string, baseDir: string): string {
    if (!baseDir) return href
    const parts = splitPath(baseDir + '/' + href)
    const normalized: string[] = []
    for (const part of parts) {
        if (part === '.' || part === '') continue
        if (part === '..') { normalized.pop(); continue }
        normalized.push(part)
    }
    return normalized.join('/')
}

/**
 * Strip surrounding quotes from a code string.
 * E.g. '"src/main.go"' → 'src/main.go'
 */
export function stripCodeString(rawText: string): string {
    return rawText.replace(/^['"`](.*)['"`]$/, '$1').trim()
}

/**
 * Try to resolve a code string (e.g. from a .hljs-string span) as a file path.
 * Returns ResolveResult with dual candidates for verification fallback.
 */
export function tryResolveCodeString(
    rawText: string,
    projectRoot: string,
    homeDir?: string,
    baseDir?: string,
): ResolveResult | null {
    const stripped = stripCodeString(rawText)
    if (!stripped || stripped.length < 3) return null
    if (!looksLikeFilePath(stripped)) return null
    return resolveFilePathDual(stripped, projectRoot, homeDir, baseDir)
}

// ── File opening ───────────────────────────────────────────────────────────────

/**
 * Open a file or directory path.
 * If the path is a directory, navigates to it and opens the file manager.
 * If it's a file, selects it in the store.
 * If the file doesn't exist, shows a toast and does not navigate.
 */
export async function openFilePath(resolvedPath: string, lineStart?: number, lineEnd?: number, source?: NavigationSurface, lineRanges?: string): Promise<boolean> {
    const parsed = parseFileUri(resolvedPath)
    let targetPath = parsed.path
    if (!targetPath) return false

    // Normalize Windows backslashes so the project-root prefix match and the
    // external-path check below work for drive-letter paths (C:\…/C:/…).
    targetPath = normalizeSlashes(targetPath)

    // Explicit ranges win; otherwise fall back to the path's own suffix. A
    // legacy (lineStart,lineEnd) pair becomes a single range when no full list
    // was supplied, so older callers keep working unchanged.
    const explicitRanges = lineRanges ? parseLineRanges(lineRanges) : []
    const effectiveRanges = explicitRanges.length > 0
        ? explicitRanges
        : (parsed.lineRanges.length > 0
            ? parsed.lineRanges
            : (lineStart ? [{ start: lineStart, end: lineEnd && lineEnd >= lineStart ? lineEnd : lineStart }] : []))
    const first = firstLineTarget(effectiveRanges)
    const finalLineStart = first.lineStart
    const finalLineEnd = first.lineEnd
    const finalLineRanges = effectiveRanges.length > 1 ? serializeLineRanges(effectiveRanges) : undefined

    // Normalize an absolute project path (e.g. file:///root/… or /root/…) to
    // a project-relative path so it is opened inside the current project.
    targetPath = toProjectRelative(targetPath, store.state.projectRoot)

    const isExternal = isAbsolutePath(targetPath)

    if (!isExternal) {
        try {
            const resp = await fetch(`/api/dir?path=${encodeURIComponent(targetPath)}`)
            if (resp.ok) {
                window.dispatchEvent(new CustomEvent('open-directory-from-context', {
                    detail: { path: targetPath, source },
                }))
                return true
            }
        } catch {
            // Ignore, fall through to open as file
        }
    }

    try {
        const resp = await fetch(`/api/file/batch-exists`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths: [targetPath] }),
        })
        if (resp.ok) {
            const data = await resp.json() as { results: Record<string, string> }
            const type = data.results?.[targetPath]
            if (type !== 'file' && type !== 'dir') {
                const { useToast } = await import('@/composables/useToast')
                const { gt } = await import('@/composables/useLocale')
                useToast().show(gt('file.toast.fileNotFound'), { type: 'error', icon: '⚠️', duration: 2000 })
                return false
            }
            if (isExternal && type === 'dir') {
                const { useToast } = await import('@/composables/useToast')
                const { gt } = await import('@/composables/useLocale')
                useToast().show(gt('file.toast.externalPathNotSupported'), { type: 'info', icon: '📁', duration: 2000 })
                return false
            }
            if (type === 'dir') {
                // Path is a directory — dispatch unified directory jump
                window.dispatchEvent(new CustomEvent('open-directory-from-context', {
                    detail: { path: targetPath, source },
                }))
                return true
            }
        }
    } catch {
        // Batch-exists check failed — proceed with selectFile as best-effort
    }

    const ok = await store.selectFile(targetPath)
    if (ok) {
        window.dispatchEvent(new CustomEvent('open-file-overlay', { detail: { path: targetPath, lineStart: finalLineStart, lineEnd: finalLineEnd, lineRanges: finalLineRanges, source } }))
        if (isExternal) {
            const { useToast } = await import('@/composables/useToast')
            useToast().show(gt('file.toast.externalFile'), { icon: 'ℹ️', type: 'info', duration: 2000 })
        }
    }
    return ok
}

/**
 * Ask the backend what a path is. Returns 'none' when it does not exist — or
 * when the check itself failed, matching navToFileInManager's best-effort
 * behavior (a failed check must not silently pass as "exists").
 */
async function fetchPathType(targetPath: string): Promise<'file' | 'dir' | 'none'> {
    try {
        const resp = await fetch('/api/file/batch-exists', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths: [targetPath] }),
        })
        if (resp.ok) {
            const data = await resp.json() as { results: Record<string, string> }
            return (data.results?.[targetPath] as 'file' | 'dir' | 'none') || 'none'
        }
    } catch { /* best-effort — treat as missing */ }
    return 'none'
}

/**
 * Reveal a path in the file manager *through the navigation coordinator*, so the
 * jump records a return origin and Back returns to the calling surface.
 *
 * This is the origin-recording counterpart of navToFileInManager. That primitive
 * navigates the manager directly and records nothing, so a reveal issued from a
 * jump-capable surface (chat, git history, task, forge) leaves no return target
 * and Back walks up the directory tree instead of going back. Prefer this one
 * whenever the caller is a jump; navToFileInManager stays for reveals that are
 * already inside the file/view context (its header menu, search results).
 *
 * Dispatches the same `open-directory-from-context` event the chat annotation
 * flow uses, so both end up in the same back-navigation stack. The parent
 * directory is the jump target and the path itself is highlighted inside it.
 *
 * The path is verified first, like openFilePath does for chat annotations. This
 * matters most for git history, which lists HISTORICAL commits: a file deleted
 * or renamed since is routine, and without the check the jump would land in the
 * parent directory and silently highlight nothing (the file manager retries for
 * 15s, then gives up with no explanation).
 *
 * Returns false when the path could not be revealed (missing or external), so a
 * caller can tell a no-op from a real jump.
 */
export async function revealInFileManager(resolvedPath: string, source?: NavigationSurface): Promise<boolean> {
    const parsed = parseFileUri(resolvedPath)
    let targetPath = parsed.path
    if (!targetPath) return false

    // Normalize Windows backslashes and relativize an absolute in-project path,
    // so the prefix match and the /api/dir listing below agree on the form.
    targetPath = normalizeSlashes(targetPath)
    targetPath = toProjectRelative(targetPath, store.state.projectRoot)

    // /api/dir only browses inside the project root, so an external path cannot
    // be revealed — report it instead of failing later as a dir-load error.
    const isExternal = isAbsolutePath(targetPath)

    const pathType = await fetchPathType(targetPath)
    if (pathType === 'none') {
        const { useToast } = await import('@/composables/useToast')
        useToast().show(gt('file.toast.fileNotFound'), { type: 'error', icon: '⚠️', duration: 2000 })
        return false
    }
    if (isExternal) {
        const { useToast } = await import('@/composables/useToast')
        useToast().show(gt('file.toast.externalPathNotSupported'), { type: 'info', icon: '📁', duration: 2000 })
        return false
    }

    window.dispatchEvent(new CustomEvent('open-directory-from-context', {
        detail: { path: dirName(targetPath), revealPath: targetPath, source },
    }))
    return true
}

/**
 * Open the containing directory of a file/dir path in the file manager,
 * then highlight and scroll to the target item.
 * If the path is a directory itself, navigate into its parent and highlight it.
 */
export async function navToFileInManager(resolvedPath: string): Promise<boolean> {
    const parsed = parseFileUri(resolvedPath)
    let targetPath = parsed.path
    if (!targetPath) return false

    // Normalize Windows backslashes to forward slashes so the project-root
    // prefix match below works for drive-letter paths (C:\…/C:/…).
    targetPath = normalizeSlashes(targetPath)

    // Convert an absolute project path to a project-relative one so the /api/dir
    // listing (whose relative paths resolve against the project root) can
    // navigate into its parent directory.
    targetPath = toProjectRelative(targetPath, store.state.projectRoot)

    // /api/dir only browses inside the project root, so external paths
    // (Unix absolute outside the project, or other drives on Windows) cannot
    // be revealed in the file manager — show the unsupported toast instead.
    const isExternal = isAbsolutePath(targetPath)

    // Verify the path exists. Project-relative paths resolve against the
    // project root on the backend; external paths are stat'd directly.
    let pathType: 'file' | 'dir' | 'none' = 'none'
    try {
        const resp = await fetch('/api/file/batch-exists', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths: [targetPath] }),
        })
        if (resp.ok) {
            const data = await resp.json() as { results: Record<string, string> }
            pathType = (data.results?.[targetPath] as 'file' | 'dir' | 'none') || 'none'
        }
    } catch { /* proceed as best-effort */ }

    if (pathType === 'none') {
        const { useToast } = await import('@/composables/useToast')
        useToast().show(gt('file.toast.fileNotFound'), { type: 'error', icon: '⚠️', duration: 2000 })
        return false
    }

    // External paths (directories AND files) cannot be revealed: /api/dir only
    // lists directories inside the project root.
    if (isExternal && (pathType === 'dir' || pathType === 'file')) {
        const { useToast } = await import('@/composables/useToast')
        useToast().show(gt('file.toast.externalPathNotSupported'), { type: 'info', icon: '📁', duration: 2000 })
        return false
    }

    // Dismiss any file overlay purely visually before revealing the file in the
    // manager. `force: true` bypasses the back-navigation state machine: going
    // through it would resolve to the `origin` step whenever a jump origin is
    // active, bouncing the user back to the chat surface and consuming the
    // origin that the reveal-in-manager action should leave untouched.
    window.dispatchEvent(new CustomEvent('close-file-overlay', { detail: { force: true } }))
    window.dispatchEvent(new CustomEvent('open-file-manager'))

    // Wait for any in-flight directory load to finish before navigating
    const maxWait = 3000
    const waitStart = Date.now()
    while (store.state.dirLoading && (Date.now() - waitStart) < maxWait) {
        await new Promise(r => setTimeout(r, 50))
    }

    // Navigate to the containing directory using loadFiles directly
    // (navigateToDir silently no-ops when dirLoading is true, which can race)
    const parentDir = dirName(targetPath)
    await store.loadFiles(parentDir, false, 0, true)

    // Brief delay to let DOM settle after loadFiles before highlighting the target
    setTimeout(() => {
        window.dispatchEvent(new CustomEvent('highlight-file-item', { detail: { path: targetPath } }))
    }, 50)

    return true
}
