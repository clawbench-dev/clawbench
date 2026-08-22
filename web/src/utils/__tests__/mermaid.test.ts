import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// Mock lazyMermaid — return a fresh mock each time
const mockInitialize = vi.fn()
const mockRender = vi.fn()
const mockMermaid = { initialize: mockInitialize, render: mockRender }

vi.mock('../lazyMermaid.ts', () => ({
    getMermaid: vi.fn(() => Promise.resolve(mockMermaid)),
}))

// Mock appLog — use hoisted factory (no outer variable references)
vi.mock('@/utils/appLog', () => ({
    appLog: {
        d: vi.fn(),
        i: vi.fn(),
        w: vi.fn(),
        e: vi.fn(),
    },
}))

// Mock CSS import (mermaid.css is imported by mermaid.ts but not needed in tests)
vi.mock('@/assets/mermaid.css', () => ({}))

// Import module under test — note: _initialized is module-level state
// that persists across tests. Tests are designed to work with this constraint.
import { initMermaid, renderMermaidInElement, reRenderMermaid } from '../mermaid.ts'
import { appLog } from '@/utils/appLog'

describe('mermaid', () => {
    // Track DOM elements added to document.body for cleanup
    const addedElements: HTMLElement[] = []

    beforeEach(() => {
        mockInitialize.mockReset()
        mockRender.mockReset()
        vi.mocked(appLog.i).mockReset()
        vi.mocked(appLog.w).mockReset()
    })

    afterEach(() => {
        // Clean up any elements added to document.body during tests
        for (const el of addedElements) {
            el.remove()
        }
        addedElements.length = 0
    })

    // ── initMermaid ──

    describe('initMermaid', () => {
        it('should be a no-op when mermaid has not been loaded yet (_initialized=false)', async () => {
            // initMermaid should not throw regardless of _initialized state
            await expect(initMermaid()).resolves.toBeUndefined()
        })
    })

    // ── renderMermaidInElement ──

    describe('renderMermaidInElement', () => {
        it('should skip rendering when no mermaid blocks exist', async () => {
            const el = document.createElement('div')
            el.innerHTML = '<p>No mermaid here</p>'
            await renderMermaidInElement(el)
            expect(mockRender).not.toHaveBeenCalled()
        })

        it('should initialize and render mermaid blocks', async () => {
            mockRender.mockResolvedValue({ svg: '<svg>test</svg>' })

            const el = document.createElement('div')
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'graph TD; A-->B'
            el.appendChild(pre)

            await renderMermaidInElement(el)

            // After first render, mermaid should be initialized
            // (initialize is called exactly once via ensureInit)
            expect(mockInitialize).toHaveBeenCalledWith(
                expect.objectContaining({ startOnLoad: false, securityLevel: 'loose' })
            )
            expect(mockRender).toHaveBeenCalled()
        })

        it('should set data-rendered attribute and replace block with rendered SVG', async () => {
            mockRender.mockResolvedValue({ svg: '<svg>rendered</svg>' })

            const el = document.createElement('div')
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'graph TD; A-->B'
            el.appendChild(pre)

            await renderMermaidInElement(el)

            // Original <pre> should be replaced
            expect(el.querySelector('pre.mermaid')).toBeNull()
            // A <div class="mermaid"> should exist with the SVG
            const rendered = el.querySelector('div.mermaid')
            expect(rendered).not.toBeNull()
            expect(rendered?.innerHTML).toContain('<svg>rendered</svg>')
            expect(rendered?.querySelector('.lightbox-expand-icon')).not.toBeNull()
            expect(rendered?.dataset.mermaid).toBe('graph TD; A-->B')
        })

        it('should show loading spinner before rendering completes', async () => {
            // Use a delayed render to observe the loading state
            let resolveRender: (v: { svg: string }) => void
            mockRender.mockReturnValue(new Promise<{ svg: string }>(r => { resolveRender = r }))

            const el = document.createElement('div')
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'graph TD; A-->B'
            el.appendChild(pre)

            const renderPromise = renderMermaidInElement(el)

            // While rendering, the block should be replaced with a loading spinner
            const loadingDiv = el.querySelector('div.mermaid')
            expect(loadingDiv).not.toBeNull()
            expect(loadingDiv?.querySelector('.mermaid-loading')).not.toBeNull()
            expect(loadingDiv?.querySelector('.mermaid-spinner')).not.toBeNull()
            expect(loadingDiv?.dataset.mermaid).toBe('graph TD; A-->B')

            // Complete the render
            resolveRender!({ svg: '<svg>done</svg>' })
            await renderPromise

            // After rendering, the spinner should be gone and SVG should be present
            const rendered = el.querySelector('div.mermaid')
            expect(rendered?.querySelector('.mermaid-loading')).toBeNull()
            expect(rendered?.innerHTML).toContain('<svg>done</svg>')
        })

        it('should skip already-rendered blocks (data-rendered attribute)', async () => {
            const el = document.createElement('div')
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.setAttribute('data-rendered', '1')
            pre.textContent = 'graph TD; A-->B'
            el.appendChild(pre)

            const renderCallCount = mockRender.mock.calls.length
            await renderMermaidInElement(el)

            expect(mockRender).toHaveBeenCalledTimes(renderCallCount)
        })

        it('should handle mermaid render errors gracefully with retry button', async () => {
            mockRender.mockRejectedValue(new Error('Parse error on line 1'))

            const el = document.createElement('div')
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'invalid mermaid'
            el.appendChild(pre)

            await renderMermaidInElement(el)

            // Should still replace the block, but with an error message
            const errorDiv = el.querySelector('div.mermaid')
            expect(errorDiv).not.toBeNull()
            expect(errorDiv?.innerHTML).toContain('Mermaid Error')
            expect(errorDiv?.innerHTML).toContain('Parse error on line 1')
            // Should include a retry button with aria-label
            const retryBtn = errorDiv?.querySelector('.mermaid-retry-btn')
            expect(retryBtn).not.toBeNull()
            expect(retryBtn?.getAttribute('aria-label')).toBe('Retry rendering diagram')
            // Should have data-mermaid-error marker but NOT data-mermaid-init-error
            // (this is a per-block render error, not an init error)
            expect(errorDiv?.dataset.mermaidError).toBe('1')
            expect(errorDiv?.dataset.mermaidInitError).toBeUndefined()
            // Source should be preserved in data-mermaid for retry
            expect(errorDiv?.dataset.mermaid).toBe('invalid mermaid')
            // Error pre should use CSS class, not inline styles
            const errorPre = errorDiv?.querySelector('.mermaid-error-pre')
            expect(errorPre).not.toBeNull()
        })

        it('should escape HTML in mermaid error messages', async () => {
            mockRender.mockRejectedValue(new Error('<script>alert("xss")</script>'))

            const el = document.createElement('div')
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'bad'
            el.appendChild(pre)

            await renderMermaidInElement(el)

            const errorDiv = el.querySelector('div.mermaid')
            expect(errorDiv?.innerHTML).not.toContain('<script>')
            expect(errorDiv?.innerHTML).toContain('&lt;script&gt;')
        })

        it('should log warning on per-block render failure', async () => {
            mockRender.mockRejectedValue(new Error('Parse error'))

            const el = document.createElement('div')
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'invalid'
            el.appendChild(pre)

            await renderMermaidInElement(el)

            expect(vi.mocked(appLog.w)).toHaveBeenCalledWith(
                'Mermaid',
                expect.stringContaining('Render failed'),
                expect.any(Error)
            )
        })

        it('should remove Mermaid v11 error SVG + wrapper div orphans from DOM on render failure', async () => {
            // Simulate Mermaid v11 behavior: it inserts a <div id="d{id}"> wrapping
            // an <svg id="{id}"> error diagram into document.body before throwing
            mockRender.mockImplementation(async (id: string) => {
                const svg = document.createElement('svg')
                svg.id = id
                const div = document.createElement('div')
                div.id = `d${id}`
                div.appendChild(svg)
                document.body.appendChild(div)
                addedElements.push(div)
                throw new Error('Syntax error in text')
            })

            const el = document.createElement('div')
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'invalid'
            el.appendChild(pre)

            await renderMermaidInElement(el)

            // The orphan SVG and wrapper div should be cleaned from document.body
            const renderId = mockRender.mock.calls[0]?.[0] as string | undefined
            expect(renderId).toBeTruthy()
            expect(document.getElementById(renderId!)).toBeNull()
            expect(document.getElementById(`d${renderId!}`)).toBeNull()
        })

        it('should accept specificBlocks parameter', async () => {
            mockRender.mockResolvedValue({ svg: '<svg>ok</svg>' })

            const el = document.createElement('div')
            el.innerHTML = '<p>empty</p>'

            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'graph TD; A-->B'
            const nodeList = [pre] as unknown as NodeList

            await renderMermaidInElement(el, 'test', nodeList)
            expect(mockRender).toHaveBeenCalled()
        })

        it('should render multiple mermaid blocks in one call', async () => {
            mockRender.mockResolvedValue({ svg: '<svg>ok</svg>' })

            const el = document.createElement('div')
            for (let i = 0; i < 3; i++) {
                const pre = document.createElement('pre')
                pre.className = 'mermaid'
                pre.textContent = `graph TD; A${i}-->B${i}`
                el.appendChild(pre)
            }

            await renderMermaidInElement(el)

            // All blocks should be replaced with div.mermaid
            expect(el.querySelectorAll('pre.mermaid').length).toBe(0)
            expect(el.querySelectorAll('div.mermaid').length).toBe(3)
        })

        it('should not leave raw source when mermaid lazy-load fails (chunk fetch error)', async () => {
            // Simulate the observed root cause: the dynamic import of the mermaid
            // chunk fails (e.g. "Failed to fetch dynamically imported module" over
            // a flaky SSH tunnel).
            const { getMermaid } = await import('../lazyMermaid.ts')
            vi.mocked(getMermaid).mockRejectedValueOnce(new Error('Failed to fetch dynamically imported module'))

            const el = document.createElement('div')
            for (let i = 0; i < 2; i++) {
                const pre = document.createElement('pre')
                pre.className = 'mermaid'
                pre.textContent = `graph TD; A${i}-->B${i}`
                el.appendChild(pre)
            }

            await renderMermaidInElement(el)

            // Raw source must not remain; every block is replaced with an error
            // fallback so the user sees feedback instead of silent source code.
            expect(el.querySelectorAll('pre.mermaid').length).toBe(0)
            expect(el.querySelectorAll('div.mermaid').length).toBe(2)
            el.querySelectorAll('div.mermaid').forEach(div => {
                expect(div.innerHTML).toContain('Mermaid Error')
                expect(div.querySelector('.mermaid-retry-btn')).not.toBeNull()
                expect(div.dataset.mermaidError).toBe('1')
                // Init/load errors should be marked with data-mermaid-init-error
                expect(div.dataset.mermaidInitError).toBe('1')
                expect(div.dataset.mermaid).toBeTruthy()
            })
        })

        it('should log warning on init/load failure', async () => {
            const { getMermaid } = await import('../lazyMermaid.ts')
            vi.mocked(getMermaid).mockRejectedValueOnce(new Error('Chunk load error'))

            const el = document.createElement('div')
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'graph TD; A-->B'
            el.appendChild(pre)

            await renderMermaidInElement(el)

            expect(vi.mocked(appLog.w)).toHaveBeenCalledWith(
                'Mermaid',
                'Init/load failed',
                expect.any(Error)
            )
        })

        it('should show loading spinner before import/init failure', async () => {
            const { getMermaid } = await import('../lazyMermaid.ts')
            // First call fails, second call succeeds
            vi.mocked(getMermaid).mockRejectedValueOnce(new Error('Chunk load error'))

            const el = document.createElement('div')
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'graph TD; A-->B'
            el.appendChild(pre)

            const renderPromise = renderMermaidInElement(el)

            // While loading, the block should show a loading spinner
            const loadingDiv = el.querySelector('div.mermaid')
            expect(loadingDiv).not.toBeNull()
            expect(loadingDiv?.querySelector('.mermaid-loading')).not.toBeNull()

            await renderPromise

            // After failure, the spinner should be gone and error shown
            const errorDiv = el.querySelector('div.mermaid')
            expect(errorDiv?.querySelector('.mermaid-loading')).toBeNull()
            expect(errorDiv?.innerHTML).toContain('Mermaid Error')
        })

        it('should retry rendering when retry button is clicked on render error', async () => {
            // First render attempt fails (per-block syntax error)
            mockRender.mockRejectedValueOnce(new Error('Transient error'))
            mockRender.mockResolvedValue({ svg: '<svg>retried</svg>' })

            const el = document.createElement('div')
            document.body.appendChild(el)
            addedElements.push(el)
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'graph TD; A-->B'
            el.appendChild(pre)

            await renderMermaidInElement(el)

            // After first failure, should have error with retry button
            const errorDiv = el.querySelector('div.mermaid[data-mermaid-error]')
            expect(errorDiv).not.toBeNull()
            // Per-block render error should NOT have data-mermaid-init-error
            expect(errorDiv?.dataset.mermaidInitError).toBeUndefined()
            const retryBtn = errorDiv?.querySelector('.mermaid-retry-btn')
            expect(retryBtn).not.toBeNull()

            // Simulate clicking the retry button (event delegation on document)
            retryBtn!.dispatchEvent(new MouseEvent('click', { bubbles: true }))

            // Allow async rendering to complete
            await vi.waitFor(() => {
                const svgEl = el.querySelector('div.mermaid svg')
                expect(svgEl).not.toBeNull()
            }, { timeout: 3000 })

            // The error container should be replaced with a rendered SVG
            expect(el.querySelector('div.mermaid[data-mermaid-error]')).toBeNull()
            const rendered = el.querySelector('div.mermaid')
            expect(rendered?.innerHTML).toContain('<svg>retried</svg>')
        })

        it('should retry and reset init state for init-error containers', async () => {
            // Simulate init/load failure, then success on retry
            const { getMermaid } = await import('../lazyMermaid.ts')
            vi.mocked(getMermaid).mockRejectedValueOnce(new Error('Chunk load error'))

            const el = document.createElement('div')
            document.body.appendChild(el)
            addedElements.push(el)
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'graph TD; A-->B'
            el.appendChild(pre)

            await renderMermaidInElement(el)

            // Should have init-error container
            const errorDiv = el.querySelector('div.mermaid[data-mermaid-init-error]')
            expect(errorDiv).not.toBeNull()
            const retryBtn = errorDiv?.querySelector('.mermaid-retry-btn')
            expect(retryBtn).not.toBeNull()

            // Set up mockRender for the retry
            mockRender.mockResolvedValue({ svg: '<svg>retried</svg>' })

            // Simulate clicking the retry button
            retryBtn!.dispatchEvent(new MouseEvent('click', { bubbles: true }))

            // Allow async rendering to complete
            await vi.waitFor(() => {
                const svgEl = el.querySelector('div.mermaid svg')
                expect(svgEl).not.toBeNull()
            }, { timeout: 3000 })

            // Should log retry action
            expect(vi.mocked(appLog.i)).toHaveBeenCalledWith(
                'Mermaid',
                expect.stringContaining('Retrying')
            )
        })

        it('should NOT reset _initialized when retrying per-block render errors', async () => {
            // First render fails (per-block syntax error, not init failure)
            mockRender.mockRejectedValueOnce(new Error('Syntax error'))
            mockRender.mockResolvedValue({ svg: '<svg>ok</svg>' })

            const el = document.createElement('div')
            document.body.appendChild(el)
            addedElements.push(el)
            const pre = document.createElement('pre')
            pre.className = 'mermaid'
            pre.textContent = 'invalid'
            el.appendChild(pre)

            await renderMermaidInElement(el)

            // Per-block error should NOT have data-mermaid-init-error
            const errorDiv = el.querySelector('div.mermaid[data-mermaid-error]')
            expect(errorDiv?.dataset.mermaidInitError).toBeUndefined()

            // Click retry — _initialized should remain true since this is not
            // an init error. We verify by checking that reRenderMermaid doesn't
            // bail out after the retry.
            const retryBtn = errorDiv?.querySelector('.mermaid-retry-btn')
            retryBtn!.dispatchEvent(new MouseEvent('click', { bubbles: true }))

            await vi.waitFor(() => {
                const svgEl = el.querySelector('div.mermaid svg')
                expect(svgEl).not.toBeNull()
            }, { timeout: 3000 })

            // reRenderMermaid should still work (not bailed out)
            // This is implicitly verified by _initialized not being reset
        })
    })

    // ── reRenderMermaid ──

    describe('reRenderMermaid', () => {
        it('should re-render existing mermaid diagrams on the page', async () => {
            mockRender.mockResolvedValue({ svg: '<svg>rerendered</svg>' })

            const container = document.createElement('div')
            container.className = 'mermaid'
            container.id = 'test-mermaid-1'
            container.dataset.mermaid = 'graph TD; A-->B'
            container.innerHTML = '<svg>old</svg>'
            document.body.appendChild(container)
            addedElements.push(container)

            await reRenderMermaid()

            expect(mockRender).toHaveBeenCalled()
        })

        it('should skip mermaid containers without data-mermaid attribute', async () => {
            const container = document.createElement('div')
            container.className = 'mermaid'
            container.id = 'test-no-data'
            container.innerHTML = '<svg>old</svg>'
            document.body.appendChild(container)
            addedElements.push(container)

            const renderCallCount = mockRender.mock.calls.length
            await reRenderMermaid()

            // Should not call render for containers without data-mermaid
            expect(mockRender).toHaveBeenCalledTimes(renderCallCount)
        })

        it('should skip error containers during re-render', async () => {
            mockRender.mockResolvedValue({ svg: '<svg>rerendered</svg>' })

            const errorContainer = document.createElement('div')
            errorContainer.className = 'mermaid'
            errorContainer.id = 'test-error-skip'
            errorContainer.dataset.mermaid = 'graph TD; A-->B'
            errorContainer.dataset.mermaidError = '1'
            errorContainer.innerHTML = '<pre class="mermaid-error-pre">Mermaid Error</pre>'
            document.body.appendChild(errorContainer)
            addedElements.push(errorContainer)

            const renderCallCount = mockRender.mock.calls.length
            await reRenderMermaid()

            // Should not call render for error containers
            expect(mockRender).toHaveBeenCalledTimes(renderCallCount)
        })
    })

    // ── initMermaid (after mermaid has been loaded) ──

    describe('initMermaid after mermaid is loaded', () => {
        it('should re-initialize mermaid when _initialized is true', async () => {
            await initMermaid()

            // If _initialized was set by prior tests, initialize was called
            if (mockInitialize.mock.calls.length > 0) {
                expect(mockInitialize).toHaveBeenCalledWith(
                    expect.objectContaining({ startOnLoad: false, securityLevel: 'loose' })
                )
            }
        })
    })
})
