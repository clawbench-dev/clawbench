// Mermaid diagram utilities
import { getMermaid } from './lazyMermaid.ts'
import { appLog } from '@/utils/appLog'
import { isDarkTheme } from './themeMeta'

// Import shared mermaid CSS (loading spinner, error, retry button styles)
import '@/assets/mermaid.css'

type MermaidModule = Awaited<ReturnType<typeof getMermaid>>

/** Remove Mermaid v11 orphan error elements that render() inserts before throwing */
function cleanupMermaidOrphan(id: string): void {
    const orphan = document.getElementById(id)
    if (orphan) orphan.remove()
    const orphanDiv = document.getElementById(`d${id}`)
    if (orphanDiv) orphanDiv.remove()
}

const escapeHtml = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

/** Monotonic counter for unique container/render IDs (avoids Date.now() collisions) */
let _idCounter = 0

let _initialized = false
let _initPromise: Promise<void> | null = null

/** Build mermaid initialize config for current theme */
function mermaidConfig() {
    const currentThemeId = document.documentElement.getAttribute('data-theme') || 'github-light'
    const theme = isDarkTheme(currentThemeId) ? 'dark' as const : 'default' as const
    return {
        startOnLoad: false,
        theme,
        securityLevel: 'loose' as const,
        fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
    }
}

/**
 * Initialize Mermaid — only initializes if mermaid has already been loaded.
 * On first app load, this is a no-op. Mermaid is initialized lazily when
 * renderMermaidInElement() is first called.
 *
 * This is called from App.vue on theme change.
 * On theme change, mermaid is already loaded so this re-initializes with
 * the new theme. On first load, mermaid hasn't been loaded yet so we skip.
 */
export async function initMermaid(): Promise<void> {
    if (!_initialized) return
    const mermaid = await getMermaid()
    mermaid.initialize(mermaidConfig())
}

/** Ensure mermaid is initialized (called lazily on first render) */
async function ensureInit(): Promise<void> {
    if (_initialized) return
    if (_initPromise) return _initPromise
    _initPromise = (async () => {
        try {
            const mermaid = await getMermaid()
            mermaid.initialize(mermaidConfig())
            _initialized = true
        } finally {
            if (!_initialized) _initPromise = null
        }
    })()
    return _initPromise
}

/** Build error fallback HTML with retry button */
function mermaidErrorHtml(errorMessage: string): string {
    return `<pre class="mermaid-error-pre">Mermaid Error: ${errorMessage}</pre><button class="mermaid-retry-btn" type="button" aria-label="Retry rendering diagram">Retry</button>`
}

/** Replace a mermaid container with a <pre class="mermaid"> for re-rendering */
function retryMermaidBlock(container: HTMLElement): void {
    const source = container.dataset.mermaid
    if (!source) return
    const pre = document.createElement('pre')
    pre.className = 'mermaid'
    pre.textContent = source
    container.replaceWith(pre)
}

/** Set up event delegation for mermaid retry buttons (called once on module load) */
function setupRetryListener(): void {
    document.addEventListener('click', (e) => {
        const btn = (e.target as HTMLElement).closest('.mermaid-retry-btn')
        if (!btn) return
        const container = btn.closest<HTMLElement>('.mermaid[data-mermaid-error]')
        if (!container) return

        // Only reset initialization state for import/init failures (where
        // mermaid itself failed to load). Per-block render errors (e.g.
        // syntax errors) don't need this — mermaid is already loaded.
        if (container.dataset.mermaidInitError) {
            _initialized = false
            _initPromise = null
        }

        appLog.i('Mermaid', `Retrying block: ${container.id}`)

        // Replace the error container with a fresh <pre class="mermaid"> and
        // find the nearest parent to call renderMermaidInElement on.
        const parent = container.parentElement
        if (!parent) return
        retryMermaidBlock(container)
        // Re-render mermaid blocks within the parent
        renderMermaidInElement(parent, 'mermaid-retry')
    })
}

// Register the retry listener when this module is first imported (lazy, so only
// when mermaid rendering is actually needed).
setupRetryListener()

/**
 * Render mermaid blocks in a DOM element.
 * Lazy-loads and initializes mermaid on first call.
 */
export async function renderMermaidInElement(
    el: HTMLElement,
    prefix: string = 'mermaid',
    specificBlocks?: NodeList
): Promise<void> {
    const blocks = specificBlocks || el.querySelectorAll('pre.mermaid:not([data-rendered])')
    if (blocks.length === 0) return

    // Phase 1: Immediately replace each <pre class="mermaid"> with a loading
    // placeholder so the user sees a spinner instead of raw source code while
    // the 608KB mermaid chunk is being fetched and rendering is in progress.
    const containers: { container: HTMLDivElement; source: string }[] = []
    Array.from(blocks).forEach((block) => {
        (block as HTMLElement).setAttribute('data-rendered', '1')
        const source = block.textContent?.trim() || ''
        const container = document.createElement('div')
        container.className = 'mermaid'
        container.dataset.mermaid = source
        container.id = `${prefix}-${_idCounter++}`
        container.innerHTML = '<div class="mermaid-loading"><span class="mermaid-spinner"></span></div>'
        ;(block as Element).replaceWith(container)
        containers.push({ container, source })
    })

    // Phase 2: Lazy-load mermaid and render each block
    let mermaid: MermaidModule
    try {
        await ensureInit()
        mermaid = await getMermaid()
    } catch (err: unknown) {
        // Mermaid lazy-load failed (e.g. the dynamic chunk fetch over a flaky
        // tunnel rejected). Replace every loading placeholder with an error
        // fallback including a retry button.
        appLog.w('Mermaid', 'Init/load failed', err)
        const errMsg = escapeHtml((err as { message?: string })?.message || String(err))
        containers.forEach(({ container }) => {
            container.dataset.mermaidError = '1'
            container.dataset.mermaidInitError = '1'
            container.innerHTML = mermaidErrorHtml(errMsg)
        })
        return
    }

    const renderPromises = containers.map(async ({ container, source }) => {
        // Use a separate render ID (different from container.id) so that
        // cleanupMermaidOrphan() doesn't accidentally remove our container.
        const renderId = `mrender-${_idCounter++}`
        try {
            const result = await mermaid.render(renderId, source)
            container.innerHTML = result.svg
            // Add expand icon for lightbox (real DOM element so PC clicks can target it)
            const expandIcon = document.createElement('span')
            expandIcon.className = 'lightbox-expand-icon'
            container.appendChild(expandIcon)
        } catch (err: unknown) {
            // Mermaid v11 inserts an error SVG + wrapper div into the DOM
            // with the render id before throwing — remove them so they don't
            // flash on page transitions
            cleanupMermaidOrphan(renderId)
            appLog.w('Mermaid', `Render failed for ${container.id}`, err)
            const errMsg = escapeHtml((err as { message?: string })?.message || String(err))
            container.dataset.mermaidError = '1'
            container.innerHTML = mermaidErrorHtml(errMsg)
        }
    })

    await Promise.all(renderPromises)
}

// Re-render all rendered mermaid diagrams on the page (called after theme switch)
export async function reRenderMermaid(): Promise<void> {
    if (!_initialized) return
    const mermaid = await getMermaid()
    document.querySelectorAll<HTMLDivElement>('div.mermaid[data-mermaid]').forEach(container => {
        // Skip error containers — re-render only successfully rendered diagrams
        if (container.dataset.mermaidError) return
        const source = container.dataset.mermaid
        if (!source) return
        const id = container.id || `mermaid-${_idCounter++}`
        container.removeAttribute('id')
        const renderId = `mrender-${_idCounter++}`
        mermaid.render(renderId, source).then(result => {
            container.innerHTML = result.svg
            container.id = id
            // Re-add expand icon after innerHTML replaces content
            const expandIcon = document.createElement('span')
            expandIcon.className = 'lightbox-expand-icon'
            container.appendChild(expandIcon)
        }).catch(err => {
            // Mermaid v11 inserts an error SVG + wrapper div before throwing
            cleanupMermaidOrphan(renderId)
            appLog.w('Mermaid', 'Re-render failed', err)
        })
    })
}
