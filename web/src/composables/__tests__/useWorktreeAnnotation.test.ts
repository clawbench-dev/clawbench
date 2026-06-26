import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
    worktreeButtonHtml,
    buildSearchEntries,
    annotateWorktreePaths,
    warmWorktreeCache,
    clearWorktreeCache,
    useWorktreeAnnotation,
} from '@/composables/useWorktreeAnnotation'

// Mock escapeHtml to pass through (for asserting HTML structure)
vi.mock('@/utils/html', () => ({
    escapeHtml: (s: string) => s.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c] || c)),
}))

vi.mock('@/composables/useLocale', () => ({
    gt: (key: string) => key,
}))

// Ensure CSS.escape is available in jsdom
if (typeof (globalThis as any).CSS === 'undefined') {
    ;(globalThis as any).CSS = {}
}
if (typeof (globalThis as any).CSS.escape === 'undefined') {
    ;(globalThis as any).CSS.escape = (s: string) => s.replace(/[!"#$%&'()*+,./:;<=>?@[\\\]^`{|}~]/g, '\\$&')
}

const PROJECT_ROOT = '/home/user/project'

const MOCK_WORKTREES = [
    {
        path: '/home/user/project',
        displayPath: '.',
        branch: 'main',
        isCurrent: true,
        dirty: false,
        changeCount: 0,
        untrackedCount: 0,
        locked: false,
        missing: false,
    },
    {
        path: '/home/user/project/.worktrees/feature-x',
        displayPath: './.worktrees/feature-x',
        branch: 'feature-x',
        isCurrent: false,
        dirty: false,
        changeCount: 0,
        untrackedCount: 0,
        locked: false,
        missing: false,
    },
    {
        path: '/home/user/project/.worktrees/bugfix-y',
        displayPath: './.worktrees/bugfix-y',
        branch: 'bugfix-y',
        isCurrent: false,
        dirty: true,
        changeCount: 3,
        untrackedCount: 1,
        locked: false,
        missing: false,
    },
]

/**
 * Helper: seed the worktree list cache so annotateWorktreePaths can use it.
 */
async function seedCache(projectRoot: string, worktrees: any[]) {
    const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ worktrees }),
    })
    vi.stubGlobal('fetch', mockFetch)
    await warmWorktreeCache(projectRoot)
    vi.unstubAllGlobals()
}

// ── buildSearchEntries ──

describe('buildSearchEntries', () => {
    it('excludes isCurrent worktree', () => {
        const entries = buildSearchEntries(MOCK_WORKTREES)
        expect(entries.every(e => e.absPath !== '/home/user/project')).toBe(true)
    })

    it('generates absolute path entries', () => {
        const entries = buildSearchEntries(MOCK_WORKTREES)
        expect(entries.some(e => e.keyword === '/home/user/project/.worktrees/feature-x')).toBe(true)
    })

    it('generates displayPath entries', () => {
        const entries = buildSearchEntries(MOCK_WORKTREES)
        expect(entries.some(e => e.keyword === './.worktrees/feature-x')).toBe(true)
    })

    it('generates displayPath without ./ prefix entries', () => {
        const entries = buildSearchEntries(MOCK_WORKTREES)
        expect(entries.some(e => e.keyword === '.worktrees/feature-x')).toBe(true)
    })

    it('sorts entries by keyword length descending', () => {
        const entries = buildSearchEntries(MOCK_WORKTREES)
        for (let i = 1; i < entries.length; i++) {
            expect(entries[i - 1].keyword.length).toBeGreaterThanOrEqual(entries[i].keyword.length)
        }
    })

    it('returns empty for empty worktree list', () => {
        const entries = buildSearchEntries([])
        expect(entries).toEqual([])
    })
})

// ── worktreeButtonHtml ──

describe('worktreeButtonHtml', () => {
    it('contains chat-worktree-btn class', () => {
        const html = worktreeButtonHtml('/home/user/project/.worktrees/feature-x')
        expect(html).toContain('chat-worktree-btn')
    })

    it('contains data-worktree-path with the absolute path', () => {
        const path = '/home/user/project/.worktrees/feature-x'
        const html = worktreeButtonHtml(path)
        expect(html).toContain(`data-worktree-path="${path}"`)
    })

    it('contains data-file-path when resolvedPath is provided', () => {
        const html = worktreeButtonHtml('/home/user/project/.worktrees/feature-x', '.worktrees/feature-x')
        expect(html).toContain('data-file-path=".worktrees/feature-x"')
    })

    it('does NOT contain data-file-path when resolvedPath is omitted', () => {
        const html = worktreeButtonHtml('/home/user/project/.worktrees/feature-x')
        expect(html).not.toContain('data-file-path')
    })

    it('contains the SVG icon', () => {
        const html = worktreeButtonHtml('/home/user/project/.worktrees/feature-x')
        expect(html).toContain('<svg')
    })
})

// ── warmWorktreeCache ──

describe('warmWorktreeCache', () => {
    beforeEach(() => {
        clearWorktreeCache()
    })

    it('populates cache from API', async () => {
        const mockFetch = vi.fn().mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ worktrees: MOCK_WORKTREES }),
        })
        vi.stubGlobal('fetch', mockFetch)

        await warmWorktreeCache(PROJECT_ROOT)

        // Second call should use cache (no fetch)
        const mockFetch2 = vi.fn()
        vi.stubGlobal('fetch', mockFetch2)
        await warmWorktreeCache(PROJECT_ROOT)
        expect(mockFetch2).not.toHaveBeenCalled()

        vi.unstubAllGlobals()
    })

    it('does nothing when projectRoot is empty', async () => {
        const mockFetch = vi.fn()
        vi.stubGlobal('fetch', mockFetch)

        await warmWorktreeCache('')

        expect(mockFetch).not.toHaveBeenCalled()

        vi.unstubAllGlobals()
    })
})

// ── annotateWorktreePaths ──

describe('annotateWorktreePaths', () => {
    beforeEach(() => {
        clearWorktreeCache()
    })

    it('annotates .worktrees/ path in text node (via worktree list)', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<p>.worktrees/feature-x</p>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toContain('/home/user/project/.worktrees/feature-x')
        expect(result.html).toContain('chat-worktree-path')
        expect(result.html).toContain('chat-worktree-btn')
    })

    it('annotates ./.worktrees/ path in text node', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<p>./.worktrees/feature-x</p>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toContain('/home/user/project/.worktrees/feature-x')
        expect(result.html).toContain('chat-worktree-path')
    })

    it('annotates absolute path in text node', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const absPath = '/home/user/project/.worktrees/feature-x'
        const result = annotateWorktreePaths(`<p>${absPath}</p>`, { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toContain(absPath)
        expect(result.html).toContain('chat-worktree-path')
    })

    it('annotates external worktree path (not under .worktrees/)', async () => {
        const externalWorktrees = [
            { ...MOCK_WORKTREES[0] },
            { path: '/tmp/my-worktree', displayPath: './my-worktree', branch: 'my-worktree', isCurrent: false, dirty: false, changeCount: 0, untrackedCount: 0, locked: false, missing: false },
        ]
        await seedCache(PROJECT_ROOT, externalWorktrees)
        const result = annotateWorktreePaths('<p>/tmp/my-worktree</p>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toContain('/tmp/my-worktree')
        expect(result.html).toContain('chat-worktree-path')
    })

    it('appends button after <a> tag with worktree href', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<a href=".worktrees/feature-x">link</a>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toContain('/home/user/project/.worktrees/feature-x')
        expect(result.html).toContain('chat-worktree-btn')
    })

    it('adds class + button to <code> tag with worktree content', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<code>.worktrees/feature-x</code>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toContain('/home/user/project/.worktrees/feature-x')
        expect(result.html).toContain('chat-worktree-path')
        expect(result.html).toContain('chat-worktree-btn')
    })

    it('annotates paths inside <pre> blocks', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<pre><code>.worktrees/feature-x</code></pre>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toContain('/home/user/project/.worktrees/feature-x')
        expect(result.html).toContain('chat-worktree-path')
    })

    it('does NOT double-annotate paths inside <a> tags in text node step', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<a href=".worktrees/feature-x">.worktrees/feature-x</a>', { projectRoot: PROJECT_ROOT })
        const path = '/home/user/project/.worktrees/feature-x'
        const count = result.detectedWorktreePaths.filter(p => p === path).length
        expect(count).toBe(1)
    })

    it('does NOT annotate non-worktree paths', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<p>src/main.go</p>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toEqual([])
        expect(result.html).not.toContain('chat-worktree-path')
    })

    it('annotates multiple worktree paths in one text node', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<p>.worktrees/feature-x and .worktrees/bugfix-y</p>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toContain('/home/user/project/.worktrees/feature-x')
        expect(result.detectedWorktreePaths).toContain('/home/user/project/.worktrees/bugfix-y')
    })

    it('returns empty result for empty input', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('', { projectRoot: PROJECT_ROOT })
        expect(result.html).toBe('')
        expect(result.detectedWorktreePaths).toEqual([])
    })

    it('sets data-worktree-path attribute with absolute path', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<code>.worktrees/feature-x</code>', { projectRoot: PROJECT_ROOT })
        expect(result.html).toContain('data-worktree-path="/home/user/project/.worktrees/feature-x"')
    })

    it('skips <a> tags with non-worktree href', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<a href="https://example.com">link</a>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toEqual([])
        expect(result.html).not.toContain('chat-worktree-btn')
    })

    it('skips <code> with chat-commit-hash class', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<code class="chat-commit-hash">.worktrees/feature-x</code>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toEqual([])
    })

    it('does NOT annotate when cache is empty (triggers background fetch)', () => {
        const result = annotateWorktreePaths('<p>.worktrees/feature-x</p>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toEqual([])
        expect(result.html).not.toContain('chat-worktree-path')
    })

    it('does NOT annotate paths not in the worktree list', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<p>.worktrees/nonexistent-wt</p>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toEqual([])
        expect(result.html).not.toContain('chat-worktree-path')
    })

    it('does NOT annotate when all worktrees are current (no search entries)', async () => {
        const onlyCurrent = [{ ...MOCK_WORKTREES[0] }]
        await seedCache(PROJECT_ROOT, onlyCurrent)
        const result = annotateWorktreePaths('<p>.worktrees/feature-x</p>', { projectRoot: PROJECT_ROOT })
        expect(result.detectedWorktreePaths).toEqual([])
    })

    it('single button with data-worktree-path and data-file-path for path under projectRoot', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const absPath = '/home/user/project/.worktrees/feature-x'
        const result = annotateWorktreePaths(`<p>${absPath}</p>`, { projectRoot: PROJECT_ROOT })
        expect(result.html).toContain('chat-worktree-btn')
        expect(result.html).toContain('data-worktree-path="/home/user/project/.worktrees/feature-x"')
        expect(result.html).toContain('data-file-path=".worktrees/feature-x"')
    })

    it('adds data-file-path with absolute path for worktree path outside projectRoot', async () => {
        const externalWorktrees = [
            { ...MOCK_WORKTREES[0] },
            { path: '/other/location/worktree-x', displayPath: './worktree-x', branch: 'worktree-x', isCurrent: false, dirty: false, changeCount: 0, untrackedCount: 0, locked: false, missing: false },
        ]
        await seedCache(PROJECT_ROOT, externalWorktrees)
        const result = annotateWorktreePaths('<p>/other/location/worktree-x</p>', { projectRoot: PROJECT_ROOT })
        expect(result.html).toContain('chat-worktree-btn')
        expect(result.html).toContain('data-worktree-path="/other/location/worktree-x"')
        // data-file-path is the absolute path since /other/location is outside projectRoot
        expect(result.html).toContain('data-file-path="/other/location/worktree-x"')
    })

    it('skips <code> that already has chat-worktree-path class (already annotated)', async () => {
        await seedCache(PROJECT_ROOT, MOCK_WORKTREES)
        const result = annotateWorktreePaths('<code class="chat-worktree-path" data-worktree-path="/home/user/project/.worktrees/feature-x">.worktrees/feature-x</code>', { projectRoot: PROJECT_ROOT })
        const btnCount = (result.html.match(/chat-worktree-btn/g) || []).length
        expect(btnCount).toBe(0)
    })
})

// ── useWorktreeAnnotation composable ──

describe('useWorktreeAnnotation', () => {
    it('returns annotateWorktreePaths, warmWorktreeCache and clearWorktreeCache', () => {
        const { annotateWorktreePaths: annotate, warmWorktreeCache: warm, clearWorktreeCache: clear } = useWorktreeAnnotation()
        expect(typeof annotate).toBe('function')
        expect(typeof warm).toBe('function')
        expect(typeof clear).toBe('function')
    })
})
