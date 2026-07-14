import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
    looksLikeCommitHash,
    commitOpenButtonHtml,
    annotateCommitHashes,
    verifyCommitHashes,
    getCachedCommitInfo,
    clearCommitHashCache,
} from '@/composables/useCommitHashAnnotation'

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

// ── looksLikeCommitHash ──

describe('looksLikeCommitHash', () => {
    it('accepts valid 7-char hex with letter', () => {
        expect(looksLikeCommitHash('abc1234')).toBe(true)
    })

    it('accepts valid 40-char full SHA', () => {
        expect(looksLikeCommitHash('a1b2c3d4e5f6789012345678901234567890abcd')).toBe(true)
    })

    it('accepts mixed case hex with letter', () => {
        expect(looksLikeCommitHash('AbC1234')).toBe(true)
    })

    it('rejects strings shorter than 7 chars', () => {
        expect(looksLikeCommitHash('abc123')).toBe(false)
    })

    it('rejects strings longer than 40 chars', () => {
        expect(looksLikeCommitHash('a1b2c3d4e5f6789012345678901234567890abcde1')).toBe(false)
    })

    it('rejects pure-decimal 7-digit numbers', () => {
        expect(looksLikeCommitHash('1234567')).toBe(false)
    })

    it('rejects strings with non-hex characters', () => {
        expect(looksLikeCommitHash('abc12xz')).toBe(false)
    })

    it('accepts hex string starting with digits but containing letters', () => {
        expect(looksLikeCommitHash('123456a')).toBe(true)
    })

    it('rejects empty string', () => {
        expect(looksLikeCommitHash('')).toBe(false)
    })

    it('rejects single letter string', () => {
        expect(looksLikeCommitHash('a')).toBe(false)
    })

    it('accepts 7-char all-letter hex', () => {
        expect(looksLikeCommitHash('abcdefa')).toBe(true)
    })

    it('rejects all-same-character strings', () => {
        expect(looksLikeCommitHash('aaaaaaa')).toBe(false)
        expect(looksLikeCommitHash('0000000')).toBe(false)
        expect(looksLikeCommitHash('fffffff')).toBe(false)
    })
})

// ── commitOpenButtonHtml ──

describe('commitOpenButtonHtml', () => {
    it('generates button HTML with commit SHA', () => {
        const html = commitOpenButtonHtml('abc1234')
        expect(html).toContain('chat-commit-open-btn')
        expect(html).toContain('data-commit-sha="abc1234"')
        expect(html).toContain('<svg')
    })

    it('includes title attribute', () => {
        const html = commitOpenButtonHtml('abc1234')
        expect(html).toContain('title=')
    })

    it('escapes special characters in SHA', () => {
        // In practice SHAs are hex, but escapeHtml should be used
        const html = commitOpenButtonHtml('abc"<>&')
        expect(html).not.toContain('data-commit-sha="abc"<>&"')
        // Should contain escaped version
        expect(html).toContain('data-commit-sha=')
    })
})

// ── annotateCommitHashes ──

describe('annotateCommitHashes', () => {
    it('returns empty result for empty input', () => {
        const result = annotateCommitHashes('')
        expect(result.html).toBe('')
        expect(result.detectedSHAs).toEqual([])
    })

    it('annotates commit hash in <code> tag with pending class (no button)', () => {
        const result = annotateCommitHashes('<code>abc1234</code>')
        expect(result.detectedSHAs).toContain('abc1234')
        expect(result.html).toContain('chat-commit-hash-pending')
        expect(result.html).toContain('data-commit-sha="abc1234"')
        // Button should NOT be inserted yet (deferred to verification)
        expect(result.html).not.toContain('chat-commit-open-btn')
    })

    it('annotates commit hash inside <pre> block', () => {
        const html = '<pre><code>abc1234</code></pre>'
        const result = annotateCommitHashes(html)
        expect(result.detectedSHAs).toContain('abc1234')
        expect(result.html).toContain('chat-commit-hash-pending')
    })

    it('does NOT annotate <code> with chat-file-path class', () => {
        const html = '<code class="chat-file-path">abc1234</code>'
        const result = annotateCommitHashes(html)
        expect(result.detectedSHAs).toEqual([])
    })

    it('does NOT annotate pure-decimal strings', () => {
        const result = annotateCommitHashes('<code>1234567</code>')
        expect(result.detectedSHAs).toEqual([])
    })

    it('annotates commit hash in plain text with pending class (no button)', () => {
        const result = annotateCommitHashes('<p>commit abc1234 was merged</p>')
        expect(result.detectedSHAs).toContain('abc1234')
        expect(result.html).toContain('chat-commit-hash-pending')
        expect(result.html).not.toContain('chat-commit-open-btn')
    })

    it('does NOT annotate commit hash inside <a> tag', () => {
        const html = '<a href="#">abc1234</a>'
        const result = annotateCommitHashes(html)
        expect(result.detectedSHAs).toEqual([])
    })

    it('annotates commit hash inside <pre> without code', () => {
        const html = '<pre>abc1234</pre>'
        const result = annotateCommitHashes(html)
        expect(result.detectedSHAs).toEqual(['abc1234'])
    })

    it('detects multiple commit hashes in same text', () => {
        const html = '<p>abc1234 and def5678</p>'
        const result = annotateCommitHashes(html)
        expect(result.detectedSHAs).toContain('abc1234')
        expect(result.detectedSHAs).toContain('def5678')
    })

    it('does NOT re-annotate already-annotated chat-commit-hash-pending spans', () => {
        const html = '<span class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</span>'
        const result = annotateCommitHashes(html)
        // Should not add additional annotations
        const matches = result.html.match(/chat-commit-hash-pending/g)
        expect(matches).toHaveLength(1)
    })

    it('adds pending class to <code> element', () => {
        const result = annotateCommitHashes('<code>abc1234</code>')
        expect(result.html).toContain('class="chat-commit-hash-pending"')
    })

    it('does NOT insert open button after <code> tag (deferred)', () => {
        const result = annotateCommitHashes('<code>abc1234</code>')
        expect(result.html).not.toContain('chat-commit-open-btn')
    })

    it('wraps plain text hash in span with pending class', () => {
        const result = annotateCommitHashes('<p>abc1234</p>')
        expect(result.html).toContain('chat-commit-hash-pending')
        expect(result.html).not.toContain('chat-commit-hash"')
    })

    it('does NOT annotate CSS color values (# prefix)', () => {
        const result = annotateCommitHashes('<code>#abcdef0</code>')
        expect(result.detectedSHAs).toEqual([])
    })

    it('does NOT annotate hex literals (0x prefix)', () => {
        const result = annotateCommitHashes('<p>0xabcdef0</p>')
        expect(result.detectedSHAs).toEqual([])
    })

    it('does NOT annotate 0X prefix (uppercase hex literal)', () => {
        const result = annotateCommitHashes('<p>0Xabcdef0</p>')
        expect(result.detectedSHAs).toEqual([])
    })

    it('does NOT annotate Unicode escapes (\\u prefix)', () => {
        const result = annotateCommitHashes('<p>\\uabcdef0</p>')
        expect(result.detectedSHAs).toEqual([])
    })

    it('does NOT annotate URL-encoded segments (% prefix)', () => {
        const result = annotateCommitHashes('<p>%abcdef0</p>')
        expect(result.detectedSHAs).toEqual([])
    })

    it('does NOT annotate URL scheme hex (:// prefix)', () => {
        const result = annotateCommitHashes('<p>http://abcdef01</p>')
        expect(result.detectedSHAs).toEqual([])
    })

    it('STILL annotates commit:sha format (colon is allowed)', () => {
        const result = annotateCommitHashes('<p>commit:abc1234</p>')
        expect(result.detectedSHAs).toContain('abc1234')
    })

    it('does NOT annotate all-same-character hashes', () => {
        const result = annotateCommitHashes('<p>aaaaaaa</p>')
        expect(result.detectedSHAs).toEqual([])
    })

    it('still annotates real-looking commit hashes', () => {
        const result = annotateCommitHashes('<p>commit a1b2c3d modified the file</p>')
        expect(result.detectedSHAs).toContain('a1b2c3d')
    })
})

// ── verifyCommitHashes ──

describe('verifyCommitHashes', () => {
    beforeEach(() => {
        clearCommitHashCache()
    })

    it('makes no request for empty SHA list', async () => {
        const mockFetch = vi.fn()
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        await verifyCommitHashes([], container)

        expect(mockFetch).not.toHaveBeenCalled()
        vi.unstubAllGlobals()
    })

    it('sends batch verification request', async () => {
        const mockFetch = vi.fn().mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ results: { abc1234: { hash: 'abc1234' } } }),
        })
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        container.innerHTML = '<span class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</span>'
        await verifyCommitHashes(['abc1234'], container)

        expect(mockFetch).toHaveBeenCalledWith('/api/git/verify-commits', expect.objectContaining({
            method: 'POST',
            body: JSON.stringify({ shas: ['abc1234'] }),
        }))

        vi.unstubAllGlobals()
    })

    it('upgrades valid SHAs: pending → verified with button', async () => {
        const mockFetch = vi.fn().mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ results: { abc1234: { hash: 'abc1234', subject: 'test' } } }),
        })
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        container.innerHTML = '<span class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</span>'

        await verifyCommitHashes(['abc1234'], container)

        // Pending class should be replaced with verified class
        expect(container.querySelectorAll('.chat-commit-hash-pending')).toHaveLength(0)
        expect(container.querySelectorAll('.chat-commit-hash')).toHaveLength(1)
        // Button should be inserted
        expect(container.querySelectorAll('.chat-commit-open-btn')).toHaveLength(1)
        // Text should remain
        expect(container.textContent).toContain('abc1234')

        vi.unstubAllGlobals()
    })

    it('upgrades valid SHAs in <code> elements', async () => {
        const mockFetch = vi.fn().mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ results: { abc1234: { hash: 'abc1234' } } }),
        })
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        container.innerHTML = '<code class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</code>'

        await verifyCommitHashes(['abc1234'], container)

        expect(container.querySelectorAll('.chat-commit-hash-pending')).toHaveLength(0)
        expect(container.querySelectorAll('code.chat-commit-hash')).toHaveLength(1)
        expect(container.querySelectorAll('.chat-commit-open-btn')).toHaveLength(1)

        vi.unstubAllGlobals()
    })

    it('removes pending annotations for invalid SHAs', async () => {
        const mockFetch = vi.fn().mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ results: { abc1234: null } }),
        })
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        container.innerHTML = '<span class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</span>'

        await verifyCommitHashes(['abc1234'], container)

        // Span should be unwrapped (text remains, class/attribute removed)
        expect(container.textContent).toContain('abc1234')
        expect(container.querySelectorAll('.chat-commit-hash-pending')).toHaveLength(0)
        expect(container.querySelectorAll('.chat-commit-hash')).toHaveLength(0)
        expect(container.querySelectorAll('.chat-commit-open-btn')).toHaveLength(0)

        vi.unstubAllGlobals()
    })

    it('removes pending code annotations for invalid SHAs', async () => {
        const mockFetch = vi.fn().mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ results: { abc1234: null } }),
        })
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        container.innerHTML = '<code class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</code>'

        await verifyCommitHashes(['abc1234'], container)

        // Code should still exist but without pending class and data attribute
        expect(container.querySelectorAll('code')).toHaveLength(1)
        expect(container.querySelectorAll('.chat-commit-hash-pending')).toHaveLength(0)
        expect(container.querySelector('code')!.hasAttribute('data-commit-sha')).toBe(false)

        vi.unstubAllGlobals()
    })

    it('handles network error — leaves pending annotations as-is', async () => {
        const mockFetch = vi.fn().mockRejectedValue(new Error('Network error'))
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        container.innerHTML = '<span class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</span>'

        await verifyCommitHashes(['abc1234'], container)

        // Pending annotation should remain (will retry on next render)
        expect(container.querySelectorAll('.chat-commit-hash-pending')).toHaveLength(1)
        expect(container.querySelectorAll('.chat-commit-open-btn')).toHaveLength(0)

        vi.unstubAllGlobals()
    })

    it('handles non-ok response — leaves pending annotations as-is', async () => {
        const mockFetch = vi.fn().mockResolvedValue({ ok: false })
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        container.innerHTML = '<span class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</span>'

        await verifyCommitHashes(['abc1234'], container)

        // Pending annotation should remain
        expect(container.querySelectorAll('.chat-commit-hash-pending')).toHaveLength(1)

        vi.unstubAllGlobals()
    })

    it('deduplicates SHAs before sending', async () => {
        const mockFetch = vi.fn().mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ results: {} }),
        })
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        await verifyCommitHashes(['abc1234', 'abc1234', 'def5678'], container)

        const sentBody = JSON.parse(mockFetch.mock.calls[0][1].body)
        expect(sentBody.shas).toEqual(['abc1234', 'def5678'])

        vi.unstubAllGlobals()
    })

    it('handles mixed valid/invalid SHAs in single batch', async () => {
        const mockFetch = vi.fn().mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({
                results: {
                    abc1234: { hash: 'abc1234', subject: 'valid' },
                    def5678: null,
                },
            }),
        })
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        container.innerHTML = '<span class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</span><span class="chat-commit-hash-pending" data-commit-sha="def5678">def5678</span>'

        await verifyCommitHashes(['abc1234', 'def5678'], container)

        // Valid SHA should be upgraded
        expect(container.querySelectorAll('.chat-commit-hash')).toHaveLength(1)
        expect(container.querySelectorAll('.chat-commit-open-btn')).toHaveLength(1)
        // Invalid SHA should be removed
        expect(container.querySelectorAll('.chat-commit-hash-pending')).toHaveLength(0)

        vi.unstubAllGlobals()
    })

    it('does not insert duplicate buttons on repeated verification of already-verified element', async () => {
        const commitInfo = { hash: 'abc1234', subject: 'test' }
        const mockFetch = vi.fn().mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ results: { abc1234: commitInfo } }),
        })
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        container.innerHTML = '<span class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</span>'

        // First verification upgrades pending → verified
        await verifyCommitHashes(['abc1234'], container)
        expect(container.querySelectorAll('.chat-commit-hash')).toHaveLength(1)
        expect(container.querySelectorAll('.chat-commit-open-btn')).toHaveLength(1)

        // Second verification — element is already verified (no longer pending)
        // The querySelector for pending won't find it, so no duplicate button
        await verifyCommitHashes(['abc1234'], container)
        expect(container.querySelectorAll('.chat-commit-hash')).toHaveLength(1)
        expect(container.querySelectorAll('.chat-commit-open-btn')).toHaveLength(1)

        vi.unstubAllGlobals()
    })
})

describe('getCachedCommitInfo and clearCommitHashCache', () => {
    beforeEach(() => {
        clearCommitHashCache()
    })

    it('returns null for uncached SHA', () => {
        expect(getCachedCommitInfo('abc1234')).toBeNull()
    })

    it('returns commit info after verification populates cache', async () => {
        const commitInfo = { hash: 'abc1234', subject: 'Test commit' }
        const mockFetch = vi.fn().mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ results: { abc1234: commitInfo } }),
        })
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        container.innerHTML = '<span class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</span>'
        await verifyCommitHashes(['abc1234'], container)

        expect(getCachedCommitInfo('abc1234')).toEqual(commitInfo)

        vi.unstubAllGlobals()
    })

    it('clears cache', async () => {
        const mockFetch = vi.fn().mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ results: { abc1234: { hash: 'abc1234' } } }),
        })
        vi.stubGlobal('fetch', mockFetch)

        const container = document.createElement('div')
        container.innerHTML = '<span class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</span>'
        await verifyCommitHashes(['abc1234'], container)

        expect(getCachedCommitInfo('abc1234')).not.toBeNull()

        clearCommitHashCache()
        expect(getCachedCommitInfo('abc1234')).toBeNull()

        vi.unstubAllGlobals()
    })
})
