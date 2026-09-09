// Mermaid diagram utilities
import { getMermaid } from './lazyMermaid.ts'
import { appLog } from '@/utils/appLog'
import { isDarkTheme } from './themeMeta'
import { gt } from '@/composables/useLocale'
import { isShareMode } from '@/share/shareMode'

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

// ── CSS-zoom suspension during render ──────────────────────────────────────
// The app's appearance "缩放" setting applies CSS zoom (html { zoom: s }) for a
// true browser-zoom feel (useSettingsConfig.applyUIScale). Mermaid v11 decides
// whether a node label wraps by measuring the label div and comparing the
// result with an EXACT equality against the wrapping width:
//     bbox = div.getBoundingClientRect(); if (bbox.width === 200) { …wrap… }
// Under CSS zoom != 1, getBoundingClientRect() returns zoom-scaled widths
// (e.g. 200 × 1.1 = 220), so the strict equality never holds and long labels
// stay frozen in white-space:nowrap, overflowing/clipping against the node
// border. Suspending zoom for the duration of mermaid.render() lets mermaid
// measure in unscaled layout coordinates; the produced SVG is vector content
// and re-scales cleanly when zoom is restored.
let _zoomSuspendDepth = 0
let _savedZoom: string | null = null

function suspendZoom(): void {
    const html = document.documentElement
    if (_zoomSuspendDepth === 0) {
        _savedZoom = html.style.zoom
        html.style.zoom = ''
    }
    _zoomSuspendDepth++
}

function restoreZoom(): void {
    _zoomSuspendDepth--
    if (_zoomSuspendDepth === 0) {
        const html = document.documentElement
        if (_savedZoom) html.style.zoom = _savedZoom
        _savedZoom = null
    }
}

let _initialized = false
let _initPromise: Promise<void> | null = null

/** Build mermaid initialize config for current theme */
function mermaidConfig() {
    const currentThemeId = document.documentElement.getAttribute('data-theme') || 'github-light'
    const theme = isDarkTheme(currentThemeId) ? 'dark' as const : 'default' as const
    // Mermaid renders its own SVG text, so it cannot inherit the page font.
    // Read the resolved --font-ui stack (set by fontConfig) so diagrams follow
    // the configured interface font; fall back to the historical system stack.
    let rootStyle = ''
    try {
      rootStyle = getComputedStyle(document.documentElement).getPropertyValue('--font-ui').trim()
    } catch { /* jsdom/SSR without layout — fall back below */ }
    const fontFamily = rootStyle || '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif'
    return {
        startOnLoad: false,
        theme,
        securityLevel: 'loose' as const,
        fontFamily,
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

/** Minimal paperclip glyph for the mermaid attach-to-chat badge (no font deps). */
const ATTACH_BADGE_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l8.57-8.57A4 4 0 1 1 18 8.84l-8.59 8.57a2 2 0 0 1-2.83-2.83l8.49-8.48"/></svg>'

/**
 * Arm the touch "attach to chat" badge on a rendered diagram.
 *
 * Only meaningful in a FILE-PREVIEW context where the diagram belongs to a
 * markdown file the user can reference: the container must sit inside a
 * `.markdown-body[data-file-path]` with a non-empty path, and the page must
 * not be the public share SPA (share viewers have no chat to attach to).
 * Chat messages and the static HTML exporter never match those conditions, so
 * they stay plain. Idempotent: re-renders (theme change / retry) that wipe the
 * container's innerHTML re-add the badge on the next successful render.
 */
function maybeArmMermaidAttachBadge(container: HTMLElement): void {
    if (container.querySelector(':scope > .mermaid-attach-badge')) return
    if (isShareMode()) return
    const mdBody = container.closest<HTMLElement>('.markdown-body[data-file-path]')
    const mdPath = mdBody?.getAttribute('data-file-path') || ''
    if (!mdPath) return
    const label = escapeHtml(gt('chat.attach.attachDiagramToChat'))
    const badge = document.createElement('span')
    badge.className = 'mermaid-attach-badge'
    badge.setAttribute('role', 'button')
    badge.setAttribute('tabindex', '-1')
    badge.setAttribute('title', label)
    badge.setAttribute('aria-label', label)
    badge.innerHTML = ATTACH_BADGE_SVG
    container.appendChild(badge)
}

/** Replace a mermaid container with a <pre class="mermaid"> for re-rendering */
function retryMermaidBlock(container: HTMLElement): void {
    const source = container.dataset.mermaid
    if (!source) return
    const pre = document.createElement('pre')
    pre.className = 'mermaid'
    pre.textContent = source
    // Preserve the source-line anchor so a retried diagram stays in sync
    // with the markdown source line after it re-renders.
    const srcLine = container.getAttribute('data-source-line')
    if (srcLine) pre.setAttribute('data-source-line', srcLine)
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
        // Preserve the source-line anchor from the original <pre> so the
        // rendered diagram still participates in line-based scroll sync.
        const srcLine = (block as HTMLElement).getAttribute('data-source-line')
        if (srcLine) container.setAttribute('data-source-line', srcLine)
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

    // Suspend the app's CSS UI zoom while mermaid measures label widths — the
    // zoom would otherwise break its exact-width wrap decision (see above).
    suspendZoom()
    try {
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
                // Add touch attach-to-chat badge in file-preview contexts
                maybeArmMermaidAttachBadge(container)
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
    } finally {
        restoreZoom()
    }
}

// Re-render all rendered mermaid diagrams on the page (called after theme switch)
export async function reRenderMermaid(): Promise<void> {
    if (!_initialized) return
    const mermaid = await getMermaid()
    const containers = Array.from(document.querySelectorAll<HTMLDivElement>('div.mermaid[data-mermaid]'))
        .filter(container => !container.dataset.mermaidError)
    if (containers.length === 0) return

    // Same CSS-zoom suspension as renderMermaidInElement — theme re-renders
    // re-measure label widths and would hit the same nowrap freeze under zoom.
    suspendZoom()
    try {
        await Promise.all(containers.map(async (container) => {
            const source = container.dataset.mermaid
            if (!source) return
            const id = container.id || `mermaid-${_idCounter++}`
            container.removeAttribute('id')
            const renderId = `mrender-${_idCounter++}`
            try {
                const result = await mermaid.render(renderId, source)
                container.innerHTML = result.svg
                container.id = id
                // Re-add expand icon after innerHTML replaces content
                const expandIcon = document.createElement('span')
                expandIcon.className = 'lightbox-expand-icon'
                container.appendChild(expandIcon)
                // Re-arm the touch attach-to-chat badge after the wipe
                maybeArmMermaidAttachBadge(container)
            } catch (err: unknown) {
                // Mermaid v11 inserts an error SVG + wrapper div before throwing
                cleanupMermaidOrphan(renderId)
                appLog.w('Mermaid', 'Re-render failed', err)
            }
        }))
    } finally {
        restoreZoom()
    }
}
