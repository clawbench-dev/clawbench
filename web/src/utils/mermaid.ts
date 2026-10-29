// Mermaid diagram utilities
import { getMermaid } from './lazyMermaid.ts'
import { appLog } from '@/utils/appLog'
import { isDarkTheme } from './themeMeta'
import { isShareMode } from '@/share/shareMode'
import { armMermaidFigure } from '@/utils/mediaBlockFactory.ts'
import { stampSvgFigure } from '@/utils/svgMediaFit.ts'
// Global `t` rather than `useI18n()`: this module builds markup OUTSIDE any
// component (a plain string assigned to innerHTML), so there is no setup
// context to call it from — the same reason mediaBlockFactory.ts uses it.
import { gt } from '@/composables/useLocale'

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

/**
 * Loading placeholder markup: the standard picture glyph, breathing.
 *
 * Why not a hand-drawn skeleton: a diagram's real shape is unknowable before it
 * renders, so any invented node layout is a guess — and a guessed layout drawn
 * out of bars reads as a chart-ish blob rather than as a diagram.
 *
 * Why the plain picture glyph rather than a diagram-specific one: the
 * placeholder marks a MEDIA BLOCK that has not arrived yet, which is exactly
 * what an image icon means. A `network` glyph over-specified the content (it
 * promised a node graph, which a sequence diagram or a pie chart is not), and
 * every extra stroke is a claim the placeholder cannot keep.
 *
 * The glyph is drawn INLINE rather than imported: this markup is built as a
 * string for `innerHTML`, and several test files mock `lucide-vue-next` with a
 * narrow set of names — a new import would be `undefined` there and crash the
 * render. The path data below is copied verbatim from `lucide-vue-next`'s
 * `image.js` (ISC), so it matches the component pixel for pixel.
 *
 * The glyph sits ON a shimmering tile rather than pulsing on its own: a sweep
 * needs SURFACE to be seen, and a 1.5px outline gives it almost none — the
 * earlier opacity pulse on the bare glyph measured as a no-op at 56dp (the
 * stroke is too thin for a brightness change to register). The tile is the same
 * `.skeleton-block` language the rest of the app uses, and the glyph is what
 * keeps it specific to media rather than a generic grey square.
 *
 * Deliberately NO spinner: one mark, one motion. The sweep is the motion
 * signal; a ring beside it would say the same thing twice.
 *
 * Everything visible is `aria-hidden`; the single `role="status"` line carries
 * the announcement, because otherwise a screen reader walking into the diagram
 * position would hear nothing at all for the seconds the render takes.
 */
const DIAGRAM_LOADING_ICON = '<svg class="mermaid-loading-icon" viewBox="0 0 24 24"'
    + ' fill="none" stroke="currentColor" stroke-width="1.5"'
    + ' stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">'
    + '<rect width="18" height="18" x="3" y="3" rx="2" ry="2"></rect>'
    + '<circle cx="9" cy="9" r="2"></circle>'
    + '<path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"></path>'
    + '</svg>'

function mermaidLoadingHtml(): string {
    const label = gt('media.diagramLoading')
    return '<div class="mermaid-loading" role="status" aria-live="polite">'
        + `<span class="mermaid-loading-sr">${escapeHtml(label)}</span>`
        + '<span class="mermaid-loading-tile" aria-hidden="true">'
        + DIAGRAM_LOADING_ICON
        + '</span>'
        + '</div>'
}

/** Replace a mermaid container with a <pre class="mermaid"> for re-rendering */
function retryMermaidBlock(container: HTMLElement): void {
    const source = container.dataset.mermaid
    if (!source) return
    const pre = document.createElement('pre')
    pre.className = 'mermaid'
    pre.textContent = source
    // Preserve the source-line range so a retried diagram stays in sync
    // with the markdown source lines after it re-renders.
    const srcLine = container.getAttribute('data-source-line')
    if (srcLine) pre.setAttribute('data-source-line', srcLine)
    const srcEnd = container.getAttribute('data-source-end')
    if (srcEnd) pre.setAttribute('data-source-end', srcEnd)
    container.replaceWith(pre)
}

/** Whether a diagram sits in a file-preview context (has an md file to reference). */
function isMermaidFilePreview(container: HTMLElement): boolean {
    if (isShareMode()) return false
    const mdBody = container.closest<HTMLElement>('.markdown-body[data-file-path]')
    const mdPath = mdBody?.getAttribute('data-file-path') || ''
    return !!mdPath
}

/**
 * Arm the unified bordered block figure around a rendered diagram. Every
 * context (file preview / chat / share / export) gets the same
 * `.image-block-wrapper > .image-block-header` shell with a view button; only
 * a file-preview context (non-share, has an md file ancestor) additionally
 * gets the range-reference attach button. Idempotent: re-renders that
 * wipe/replace the container re-arm via the parent guard.
 */
function armMermaidFigureForContext(container: HTMLElement): void {
    armMermaidFigure(container, { attach: isMermaidFilePreview(container) })
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
        // Preserve the source-line range anchor from the original <pre> so the
        // rendered diagram still participates in line-based scroll sync and the
        // attach-to-chat range reference keeps the authoritative closing-fence
        // line (data-source-end is dropped by textContent.trim() below, so it
        // must be carried over explicitly rather than recomputed from the body).
        const srcLine = (block as HTMLElement).getAttribute('data-source-line')
        if (srcLine) container.setAttribute('data-source-line', srcLine)
        const srcEnd = (block as HTMLElement).getAttribute('data-source-end')
        if (srcEnd) container.setAttribute('data-source-end', srcEnd)
        container.innerHTML = mermaidLoadingHtml()
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
                // Uniform bordered figure: header view button always; attach
                // button only inside a file-preview markdown body.
                armMermaidFigureForContext(container)
                // Size the figure in the SAME task as the SVG injection.
                //
                // Until `stampSvgFigure` writes the media's pixel width, the
                // stylesheet falls back to `width: calc(60dvh * var(--svg-ar, 1))`
                // — i.e. EVERY diagram is painted 60dvh wide regardless of its
                // own ratio, then snaps to its real width on the next pass. For
                // a wide diagram that is a visible grow (540 → 880 measured); for
                // a narrow one, a visible shrink. Stamping here removes the
                // intermediate frame entirely instead of relying on the caller
                // (which only runs after `Promise.all` below, i.e. after the
                // SLOWEST diagram in the batch).
                //
                // The wrapper is the direct parent after arming; `stampSvgFigure`
                // is idempotent and a no-op for a container it cannot measure.
                const wrapper = container.closest('.image-block-wrapper')
                if (wrapper) stampSvgFigure(wrapper)
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
                // Re-arm the bordered figure after the innerHTML wipe.
                armMermaidFigureForContext(container)
                // Re-stamp too: the fresh <svg> may have a different size (a
                // theme/font change re-measures every label), and the figure
                // still carries the px width computed for the OLD one. The
                // mutation observer would eventually catch the replaced node,
                // but doing it here keeps the same "size in the injection task"
                // rule as `renderMermaidInElement` — otherwise the diagram
                // flashes at the stale width first.
                const wrapper = container.closest('.image-block-wrapper')
                if (wrapper) stampSvgFigure(wrapper)
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
