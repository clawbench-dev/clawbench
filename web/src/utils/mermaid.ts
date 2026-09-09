// Mermaid diagram utilities
import { getMermaid } from './lazyMermaid.ts'
import { appLog } from '@/utils/appLog'
import { isDarkTheme } from './themeMeta'
import { gt } from '@/composables/useLocale'
import { isShareMode } from '@/share/shareMode'
import { ATTACH_BADGE_SVG } from '@/utils/attachSvg'

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

/** Maximize glyph for the mermaid header view button (matches image header). */
const MERMAID_VIEW_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3"/><path d="M21 8V5a2 2 0 0 0-2-2h-3"/><path d="M3 16v3a2 2 0 0 0 2 2h3"/><path d="M16 21h3a2 2 0 0 0 2-2v-3"/></svg>'

/** Build a header action button inside the mermaid header actions row. */
function makeMermaidHeaderButton(cls: string, action: string, i18nKey: string, svg: string): HTMLButtonElement {
    const btn = document.createElement('button')
    btn.type = 'button'
    btn.className = cls
    btn.dataset.action = action
    const label = gt(i18nKey)
    btn.title = label
    btn.setAttribute('aria-label', label)
    btn.innerHTML = svg
    return btn
}

/**
 * Arm the block-level header bar (view / attach) around a rendered diagram in
 * a FILE-PREVIEW context — uniform across mobile and PC, mirroring the image
 * and code/table headers. Chat messages, shares and the exporter never match
 * `isMermaidFilePreview`, so they stay bare (with their hover expand icon).
 * Idempotent: re-renders that wipe/replace the container re-arm via the parent
 * guard.
 */
function maybeArmMermaidHeader(container: HTMLElement): void {
    if (container.parentElement?.classList.contains('mermaid-block-wrapper')) return
    if (!isMermaidFilePreview(container)) return

    const wrapper = document.createElement('div')
    wrapper.className = 'mermaid-block-wrapper'

    const header = document.createElement('div')
    header.className = 'mermaid-block-header'
    const actions = document.createElement('span')
    actions.className = 'mermaid-block-header-actions'
    actions.appendChild(makeMermaidHeaderButton('mermaid-block-view-btn', 'view', 'imageBlock.view', MERMAID_VIEW_ICON_SVG))
    actions.appendChild(makeMermaidHeaderButton('mermaid-block-attach-btn', 'attach', 'chat.attach.attachDiagramToChat', ATTACH_BADGE_SVG))
    header.appendChild(actions)

    container.parentNode?.insertBefore(wrapper, container)
    wrapper.appendChild(header)
    wrapper.appendChild(container)
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
                if (isMermaidFilePreview(container)) {
                    // File preview: a block-level header (view / attach) replaces
                    // the corner expand icon + touch badge — uniform mobile/PC.
                    maybeArmMermaidHeader(container)
                } else {
                    // Chat / share / export stay bare with the hover expand icon.
                    const expandIcon = document.createElement('span')
                    expandIcon.className = 'lightbox-expand-icon'
                    container.appendChild(expandIcon)
                }
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
                if (isMermaidFilePreview(container)) {
                    // Re-arm the block header after the innerHTML wipe.
                    maybeArmMermaidHeader(container)
                } else {
                    // Bare (chat/share/export): re-add the hover expand icon.
                    const expandIcon = document.createElement('span')
                    expandIcon.className = 'lightbox-expand-icon'
                    container.appendChild(expandIcon)
                }
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
