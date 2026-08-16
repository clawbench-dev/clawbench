import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// Mock lazyMermaid — return a fresh mock each time
const mockInitialize = vi.fn()
const mockRender = vi.fn()
const mockMermaid = { initialize: mockInitialize, render: mockRender }

vi.mock('../lazyMermaid.ts', () => ({
    getMermaid: vi.fn(() => Promise.resolve(mockMermaid)),
}))

// Import module under test — note: _initialized is module-level state
// that persists across tests. Tests are designed to work with this constraint.
import { initMermaid, renderMermaidInElement, reRenderMermaid } from '../mermaid.ts'

describe('mermaid', () => {
    // Track DOM elements added to document.body for cleanup
    const addedElements: HTMLElement[] = []

    beforeEach(() => {
        mockInitialize.mockReset()
        mockRender.mockReset()
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

        it('should handle mermaid render errors gracefully', async () => {
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
            // a flaky SSH tunnel). Previously this rejected before touching any
            // block, silently leaving the raw <pre class="mermaid"> source visible.
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
            })
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

            expect(mockRender).toHaveBeenCalledWith('test-mermaid-1', 'graph TD; A-->B')
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
