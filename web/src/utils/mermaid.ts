// Mermaid diagram utilities
import { getMermaid } from './lazyMermaid.ts'
import { appLog } from '@/utils/appLog'
import { isDarkTheme } from './themeMeta'

type MermaidModule = Awaited<ReturnType<typeof getMermaid>>

/** Remove Mermaid v11 orphan error elements that render() inserts before throwing */
function cleanupMermaidOrphan(id: string): void {
    const orphan = document.getElementById(id)
    if (orphan) orphan.remove()
    const orphanDiv = document.getElementById(`d${id}`)
    if (orphanDiv) orphanDiv.remove()
}

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

    let mermaid: MermaidModule
    try {
        await ensureInit()
        mermaid = await getMermaid()
    } catch (err: unknown) {
        // Mermaid lazy-load failed (e.g. the dynamic chunk fetch over a flaky
        // tunnel rejected). Previously this rejected before touching any block,
        // silently leaving raw source visible. Instead, replace every block with
        // an error fallback so the user sees feedback.
        const escapeHtml = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        Array.from(blocks).forEach((block, index) => {
            ;(block as HTMLElement).setAttribute('data-rendered', '1')
            const container = document.createElement('div')
            container.className = 'mermaid'
            container.id = `${prefix}-fail-${index}`
            container.innerHTML = `<pre style="padding:12px;background:var(--code-bg);border-radius:6px;font-size:13px;overflow-x:auto;">Mermaid Error: ${escapeHtml((err as { message?: string })?.message || String(err))}</pre>`
            ;(block as Element).replaceWith(container)
        })
        return
    }

    const renderPromises = Array.from(blocks).map(async (block, index) => {
        (block as HTMLElement).setAttribute('data-rendered', '1')
        const id = `${prefix}-${Date.now()}-${index}`
        const source = block.textContent?.trim() || ''
        const container = document.createElement('div')
        container.className = 'mermaid'
        container.id = id

        try {
            const result = await mermaid.render(id, source)
            container.innerHTML = result.svg
            container.dataset.mermaid = source
            // Add expand icon for lightbox (real DOM element so PC clicks can target it)
            const expandIcon = document.createElement('span')
            expandIcon.className = 'lightbox-expand-icon'
            container.appendChild(expandIcon)
            ;(block as Element).replaceWith(container)
        } catch (err: unknown) {
            // Mermaid v11 inserts an error SVG + wrapper div into the DOM
            // with the render id before throwing — remove them so they don't
            // flash on page transitions
            cleanupMermaidOrphan(id)
            const escapeHtml = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
            container.innerHTML = `<pre style="padding:12px;background:var(--code-bg);border-radius:6px;font-size:13px;overflow-x:auto;">Mermaid Error: ${escapeHtml((err as { message?: string })?.message || String(err))}</pre>`
            ;(block as Element).replaceWith(container)
        }
    })

    await Promise.all(renderPromises)
}

// Re-render all rendered mermaid diagrams on the page (called after theme switch)
export async function reRenderMermaid(): Promise<void> {
    if (!_initialized) return
    const mermaid = await getMermaid()
    document.querySelectorAll<HTMLDivElement>('div.mermaid[data-mermaid]').forEach(container => {
        const source = container.dataset.mermaid
        if (!source) return
        const id = container.id || `mermaid-${Date.now()}`
        container.removeAttribute('id')
        mermaid.render(id, source).then(result => {
            container.innerHTML = result.svg
            container.id = id
            // Re-add expand icon after innerHTML replaces content
            const expandIcon = document.createElement('span')
            expandIcon.className = 'lightbox-expand-icon'
            container.appendChild(expandIcon)
        }).catch(err => {
            // Mermaid v11 inserts an error SVG + wrapper div before throwing
            cleanupMermaidOrphan(id)
            appLog.w('Mermaid', 'Re-render failed', err)
        })
    })
}
