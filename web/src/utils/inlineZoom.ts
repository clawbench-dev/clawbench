/**
 * Inline zoom/pan for media figures inside rendered markdown.
 *
 * The problem
 * -----------
 * A rendered diagram has to fit a 60dvh budget, so a portrait flowchart is
 * scaled down until its labels are unreadable — the reader either squints or
 * opens the full-screen lightbox. This module turns the figure itself into a
 * small viewport: click it (pointer devices only) and the diagram can be zoomed
 * and panned in place, without leaving the document.
 *
 * Interaction model (deliberately narrow)
 * ---------------------------------------
 * - **Pointer devices only.** `(hover: hover) and (pointer: fine)` gates every
 *   listener. Touch never activates inline zoom: a pinch would fight page
 *   scrolling, and the issue that motivated this feature explicitly rejected
 *   "manual zoom" on mobile for exactly that reason. Touch keeps the lightbox.
 * - **Click enters the zoom state; click outside (or Escape) leaves it.** This
 *   is the load-bearing decision. Hover-activation was tried first and it
 *   hijacked the wheel: scrolling UP a long document with the pointer drifting
 *   over a diagram zoomed the diagram instead of scrolling the page, and with
 *   trackpad momentum every remaining negative delta kept multiplying until the
 *   figure hit max scale. Requiring an explicit click gives the wheel an
 *   unambiguous owner — while a figure is NOT zoomed, every wheel event belongs
 *   to the page, no matter which direction it goes.
 * - **The pointer leaving the figure does NOT end the zoom.** Only an explicit
 *   act does. Moving the mouse off — to read a paragraph beside the diagram, to
 *   reach a toolbar, or simply to reposition the hand — must not throw away the
 *   zoom and pan the reader just set up. `pointerout` is deliberately not
 *   listened to; the exit paths are the outside click, Escape, and clicking a
 *   different figure.
 * - **Wheel zooms around the cursor while zoomed.** Anchoring on the pointer
 *   (not the centre) is what makes "zoom into that node" feel direct.
 * - **Drag pans while zoomed.** Panning is clamped so the media cannot be
 *   dragged out of its frame; at fit scale there is nothing to pan.
 * - **Keyboard focus also enters the zoom state**, so the feature is reachable
 *   without a pointer; `+`/`-` zoom, arrows pan, Escape leaves.
 *
 * Why a module and not a component
 * --------------------------------
 * The markup is produced by `v-html` / `innerHTML` in several independent
 * pipelines (chat, file preview, share SPA, HTML export), so there is no Vue
 * component instance to attach to. A single delegated listener on `document`
 * covers every surface, the same approach `localMediaFallback.ts` and
 * `mediaFullscreenGuard.ts` already use for injected markup.
 */

/** Marker class on a figure while it is the active zoom target. */
export const ZOOM_ACTIVE_CLASS = 'is-zooming'

/**
 * Scale bounds. `MIN_SCALE` is the resting state (the frame exactly filled).
 *
 * There is no upper bound: a diagram is vector art, so zooming further costs
 * nothing in quality and the user is the best judge of how far they need to go.
 * A hard cap made the gesture feel broken ("放大到全宽就无法再放大") because it
 * was hit right after the artwork filled the frame — the point where a reader
 * most wants to keep going. The only limit is `MAX_SAFE_SCALE`, a guard against
 * numeric runaway from a stray event storm, far beyond any usable zoom.
 */
export const MIN_SCALE = 1
export const MAX_SAFE_SCALE = 64
/** Multiplier per wheel notch. */
const WHEEL_STEP = 1.15
/** Pointer travel (px) before a press becomes a pan rather than a click. */
const DRAG_THRESHOLD = 3

interface ZoomState {
    /** The figure currently being zoomed/ panned. */
    figure: HTMLElement
    /** The transformed media element (img or svg). */
    media: HTMLElement
    scale: number
    tx: number
    ty: number
    /** The svg's original `width` attribute, restored on deactivate. */
    savedSvgWidth?: string | null
    /** The author's inline `max-width` (mermaid writes one), restored on exit. */
    savedMaxWidth?: string
}

/** Only one figure can be zoomed at a time. */
let active: ZoomState | null = null
let installed = false
let removeListeners: (() => void) | null = null

/** True when the device has a precise pointer that can hover (mouse/trackpad). */
export function supportsInlineZoom(): boolean {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
    try {
        return window.matchMedia('(hover: hover) and (pointer: fine)').matches
    } catch {
        return false
    }
}

/**
 * The media element a figure can zoom. Mirrors `svgMediaFit.ts`'s content-media
 * selector (kept separate so the sizing module stays free of interaction code):
 * an `<img>` (raster or SVG file), a bare inline `<svg>`, or a rendered Mermaid
 * diagram.
 */
const CONTENT_MEDIA_SELECTOR =
    ':scope > .lightbox-img-wrap > img, ' +
    ':scope > .lightbox-svg-wrap > svg.lightbox-svg, ' +
    ':scope > .mermaid > svg'

/** Resolve the zoomable media of a figure, or null. */
export function zoomableMedia(figure: Element | null): HTMLElement | null {
    if (!figure) return null
    const media = figure.querySelector<HTMLElement>(CONTENT_MEDIA_SELECTOR)
    if (!media) return null
    // The toolbar's own icon SVGs live inside `.image-block-header`, never in a
    // content cell, so the `:scope >` anchors above are enough to exclude them.
    return media
}

/** The figure wrapping `target`, if it is zoomable. */
function figureFrom(target: EventTarget | null): HTMLElement | null {
    if (!(target instanceof Element)) return null
    const figure = target.closest<HTMLElement>('.image-block-wrapper')
    if (!figure) return null
    // Do not hijack interaction with the floating toolbar.
    if (target.closest('.image-block-header')) return null
    return zoomableMedia(figure) ? figure : null
}

/**
 * Restore the media to its resting layout size and drop the active marker.
 *
 * Every inline style the zoom wrote is cleared here, because the resting size
 * is whatever CSS says (contain-fit inside the frame) — the module must not
 * leave a single pixel of its own sizing behind, or "layout back to normal"
 * would be a lie after the first zoom.
 */
function deactivate(): void {
    if (!active) return
    const { figure, media, savedSvgWidth, savedMaxWidth } = active
    media.style.transform = ''
    media.style.transformOrigin = ''
    media.style.width = ''
    media.style.height = ''
    media.style.maxWidth = ''
    media.style.maxHeight = ''
    if (media.tagName.toLowerCase() === 'svg' && savedSvgWidth !== undefined) {
        if (savedSvgWidth === null) media.removeAttribute('width')
        else media.setAttribute('width', savedSvgWidth)
    }
    // Put the author's own sizing back BEFORE deriving the resting width, which
    // reads it.
    media.style.maxWidth = savedMaxWidth ?? ''
    // The resting aspect-ratio stays: it describes the artwork, not the zoom,
    // and dropping it would make the frame collapse for one frame on exit.
    figure.classList.remove(ZOOM_ACTIVE_CLASS)
    // Re-apply the resting width: the zoom wrote an inline width/height that was
    // just cleared, and the CSS fallback (`60dvh * ratio`) is only a rough
    // stand-in — without this the frame would stay at the active size until the
    // next render.
    applyRestingWidth(figure)
    active = null
}

/**
 * Natural (unzoomed) size of the media, used as the base for the zoom.
 *
 * Read from the element's intrinsic dimensions rather than `offsetWidth`,
 * because the resting size is the contain-fitted one — multiplying that by the
 * scale would compound the fit shrink and never reach 1:1 with the artwork.
 */
function naturalSize(state: ZoomState): { w: number; h: number } {
    const media = state.media
    if (media.tagName === 'IMG') {
        const img = media as HTMLImageElement
        if (img.naturalWidth > 0 && img.naturalHeight > 0) {
            return { w: img.naturalWidth, h: img.naturalHeight }
        }
    }
    // SVG (inline or mermaid): the viewBox is the authored coordinate space.
    const svg = media as unknown as SVGSVGElement
    const vb = svg.viewBox?.baseVal
    if (vb && vb.width > 0 && vb.height > 0) return { w: vb.width, h: vb.height }
    const r = media.getBoundingClientRect()
    return { w: r.width, h: r.height }
}

/**
 * Size of the media element at the current scale, in layout pixels.
 *
 * The media is scaled by writing WIDTH/HEIGHT, not by `transform: scale()`.
 * A CSS transform rasterises the element once at its layout size and then
 * stretches those pixels, so an SVG zoomed 4x looks blurry — the whole point of
 * zooming a diagram is to READ it. Setting the layout size makes the browser
 * re-rasterise at the new size, so text and strokes stay sharp at any scale.
 * (Panning still uses `translate`, which never resamples.)
 *
 * At rest (scale 1) the element is exactly the VIEWPORT box: the artwork is
 * letterboxed inside it by `object-fit: contain` (img) or the SVG's default
 * `preserveAspectRatio` (inline svg). The box is what grows when zooming, so
 * the visible artwork grows by the same factor on whichever axis is the
 * limiting one — the letterbox margins shrink to nothing first, which is
 * exactly the empty space the zoom is meant to reclaim.
 */
function scaledSize(state: ZoomState): { w: number; h: number } {
    const frame = state.figure.getBoundingClientRect()
    const cell = state.media.parentElement
    const box = cell ? cell.getBoundingClientRect() : frame
    const baseW = box.width || frame.width
    const baseH = box.height || frame.height
    // The element must be scaled on ITS OWN ratio, not the frame's. An `<svg>`
    // keeps `preserveAspectRatio` (`meet`), so a box whose ratio differs from
    // the viewBox gets its content letterboxed inside it — the element grows
    // but the artwork does not, which is exactly the "stops at full width"
    // symptom. Deriving the box from the artwork's ratio keeps box and artwork
    // in lockstep at every scale.
    const nat = naturalSize(state)
    if (!(nat.w > 0 && nat.h > 0)) {
        return { w: baseW * state.scale, h: baseH * state.scale }
    }
    // At scale 1 the artwork exactly covers the frame on its limiting axis
    // (contain). Growing from there keeps the same relationship.
    const fitScale = Math.min(baseW / nat.w, baseH / nat.h)
    return {
        w: nat.w * fitScale * state.scale,
        h: nat.h * fitScale * state.scale,
    }
}

/**
 * Clamp a pan offset so the media cannot be dragged out of the viewport.
 *
 * The media must always cover the frame on an axis once it is larger than the
 * frame there; when it is smaller (a wide diagram at low zoom), it stays
 * centred on that axis and cannot move at all. Without this the reader could
 * fling the diagram off-screen and be left staring at an empty box.
 */
function clampPan(state: ZoomState, tx: number, ty: number): { tx: number; ty: number } {
    const frame = state.figure.getBoundingClientRect()
    const { w, h } = scaledSize(state)
    // The box may be larger than the artwork inside it (letterboxing), so the
    // travel limit is derived from the ARTWORK's rendered size: it may move
    // until its edge meets the frame edge, and no further.
    const nat = naturalSize(state)
    const ratio = nat.w > 0 && nat.h > 0 ? nat.w / nat.h : 1
    // Rendered artwork size inside the scaled box, preserving the ratio.
    const boxRatio = h > 0 ? w / h : ratio
    const artW = ratio > boxRatio ? w : h * ratio
    const artH = ratio > boxRatio ? w / ratio : h
    const maxX = Math.max(0, (artW - frame.width) / 2)
    const maxY = Math.max(0, (artH - frame.height) / 2)
    return {
        tx: Math.min(maxX, Math.max(-maxX, tx)),
        ty: Math.min(maxY, Math.max(-maxY, ty)),
    }
}

/** Apply the state to the DOM (single write path). */
function apply(state: ZoomState): void {
    const { tx, ty } = clampPan(state, state.tx, state.ty)
    state.tx = tx
    state.ty = ty
    const { w, h } = scaledSize(state)
    // Layout size (sharp) + translate (no resampling). The element keeps
    // `object-fit: contain` from the stylesheet, so the artwork is letterboxed
    // inside this box at every scale.
    state.media.style.width = `${w}px`
    state.media.style.height = `${h}px`
    state.media.style.maxWidth = 'none'
    state.media.style.maxHeight = 'none'
    // Mermaid writes `width="100%"` as an ATTRIBUTE on the <svg>. A
    // presentation attribute loses to a stylesheet rule but is re-applied when
    // the inline style is cleared, and it kept the width pinned at the column
    // while the height grew — the diagram stretched instead of zooming. Drop
    // it while zooming; `deactivate` restores it.
    if (state.media.tagName.toLowerCase() === 'svg') {
        // `undefined` = not saved yet; `null` = the attribute was absent, which
        // is a real state to restore to.
        if (state.savedSvgWidth === undefined) {
            state.savedSvgWidth = state.media.getAttribute('width')
        }
        state.media.removeAttribute('width')
    }
    // The resting CSS centres the media with `translate(-50%, -50%)`; the pan
    // offset is added on top of that rather than replacing it, so the media
    // stays centred on the frame's centre axis while panning.
    state.media.style.transform = `translate(calc(-50% + ${tx}px), calc(-50% + ${ty}px))`
}

/** Begin zooming a figure at fit scale, centred. */
function activate(figure: HTMLElement): ZoomState | null {
    const media = zoomableMedia(figure)
    if (!media) return null
    if (active?.figure === figure) return active
    deactivate()

    active = {
        figure,
        media,
        scale: MIN_SCALE,
        tx: 0,
        ty: 0,
        // mermaid states the diagram's natural size as an inline max-width; the
        // zoom overwrites it, and the resting layout needs it back on exit.
        savedMaxWidth: media.style.maxWidth,
    }
    figure.classList.add(ZOOM_ACTIVE_CLASS)
    media.style.transformOrigin = 'center center'
    apply(active)
    return active
}

/** Reset the active figure to fit-scale and centre it (the layout-reset button). */
export function resetActiveZoom(): boolean {
    if (!active) return false
    active.scale = MIN_SCALE
    active.tx = 0
    active.ty = 0
    apply(active)
    return true
}

/**
 * Zoom around a client-space anchor point.
 *
 * The anchor stays under the cursor: we convert it to the media's local
 * coordinate space at the current scale, apply the new scale, then solve for
 * the translate that puts that same local point back under the cursor.
 */
function zoomAt(state: ZoomState, nextScale: number, clientX: number, clientY: number): void {
    const frame = state.figure.getBoundingClientRect()
    // Cursor position relative to the frame centre.
    const cx = clientX - (frame.left + frame.width / 2)
    const cy = clientY - (frame.top + frame.height / 2)
    // Keep the point under the cursor fixed: the ratio of scales is the amount
    // the point moves away from the centre, so the translate compensates for
    // the difference.
    const ratio = nextScale / state.scale
    state.tx = cx - (cx - state.tx) * ratio
    state.ty = cy - (cy - state.ty) * ratio
    state.scale = nextScale
    apply(state)
}

function onWheel(e: WheelEvent): void {
    // The wheel only belongs to a figure that has been explicitly entered
    // (click / focus). At rest EVERY notch — up or down, any delta — is the
    // page's. This is the whole point of click-to-enter: scrolling a document
    // with the pointer drifting over a diagram must never zoom it, and the
    // earlier hover-activation model failed exactly here.
    if (!active) return
    const figure = figureFrom(e.target)
    if (!figure || figure !== active.figure) return

    const state = active
    // Horizontal-only notches (deltaY === 0) are not a zoom gesture.
    if (e.deltaY === 0) return

    const next = Math.min(MAX_SAFE_SCALE, Math.max(MIN_SCALE, state.scale * (e.deltaY > 0 ? 1 / WHEEL_STEP : WHEEL_STEP)))

    // Zooming out stops AT fit scale; it does not leave the active state.
    // Leaving on the way out was wrong twice over: the ring and the reset
    // button vanished the moment the reader scrolled back to 1x, and the very
    // next notch went to the page instead of the figure — so the gesture they
    // were still making silently turned into a document scroll. Exiting is the
    // explicit act (outside click / Escape), never a side effect of scale.
    if (next === state.scale) {
        // Already at a bound: swallow the notch rather than letting the page
        // scroll out from under a figure the user is actively zooming.
        e.preventDefault()
        return
    }
    e.preventDefault()
    zoomAt(state, next, e.clientX, e.clientY)
}

let panning: { state: ZoomState; startX: number; startY: number; baseTx: number; baseTy: number; moved: boolean } | null = null

function onPointerDown(e: PointerEvent): void {
    if (e.pointerType === 'touch' || e.button !== 0) return
    const figure = figureFrom(e.target)
    if (!figure) return
    const state = active?.figure === figure ? active : null
    // Panning only makes sense once zoomed in.
    if (!state || state.scale <= MIN_SCALE) return

    e.preventDefault()
    // Capture the pointer so a drag that leaves the element still delivers
    // `pointerup` to us; without it the release can land on another element.
    try {
        ;(e.target as Element).setPointerCapture?.(e.pointerId)
    } catch { /* capture is best-effort; endStrandedPan covers the failure */ }
    panning = {
        state,
        startX: e.clientX,
        startY: e.clientY,
        baseTx: state.tx,
        baseTy: state.ty,
        moved: false,
    }
}

function onPointerMove(e: PointerEvent): void {
    if (!panning) return
    const dx = e.clientX - panning.startX
    const dy = e.clientY - panning.startY
    if (!panning.moved && Math.abs(dx) < DRAG_THRESHOLD && Math.abs(dy) < DRAG_THRESHOLD) return
    panning.moved = true
    e.preventDefault()
    panning.state.tx = panning.baseTx + dx
    panning.state.ty = panning.baseTy + dy
    apply(panning.state)
    panning.state.figure.classList.add('is-panning')
}

/**
 * Make a figure reachable by Tab the first time the pointer is over it.
 *
 * Figures are injected as `v-html`, so nothing in the template can carry
 * `tabindex`. Stamping it on first hover keeps the tab order free of every
 * diagram on the page until the reader shows interest in one; a figure reached
 * by Tab without ever being hovered is stamped by `onFocusIn` instead.
 *
 * No `role="img"` here on purpose: the figure CONTAINS the toolbar buttons, and
 * `role="img"` would flatten them out of the accessibility tree, making the
 * lightbox/reset controls unreachable for screen readers.
 */
function ensureFocusable(figure: HTMLElement): void {
    if (figure.hasAttribute('tabindex')) return
    figure.setAttribute('tabindex', '0')
}

function onPointerOver(e: PointerEvent): void {
    if (e.pointerType === 'touch') return
    const figure = figureFrom(e.target)
    if (!figure) return
    ensureFocusable(figure)
}


/**
 * Restore the media's resting width after the zoom cleared its inline sizes.
 *
 * The resting width is not a CSS constant — it is the artwork's own size,
 * clamped to the column — so it has to be re-derived here. `svgMediaFit.ts`
 * computes the same value when it stamps the figure; duplicating the few lines
 * keeps these two modules independent (importing it would be a cycle).
 */
function applyRestingWidth(figure: HTMLElement): void {
    const media = zoomableMedia(figure)
    const column = figure.parentElement
    if (!media || !column) return
    const avail = column.getBoundingClientRect().width
    if (!(avail > 0)) return
    const nat = naturalSize({ figure, media, scale: MIN_SCALE, tx: 0, ty: 0 })
    if (!(nat.w > 0 && nat.h > 0)) return
    const ar = nat.w / nat.h
    // The author's intended display width, when the markup states one.
    const stated = parseFloat(media.style.maxWidth) || parseFloat(media.getAttribute('width') ?? '') || nat.w
    const heightBudget = window.innerHeight * 0.6
    const width = Math.min(stated, avail, heightBudget * ar)
    if (!(width > 0)) return
    media.style.width = `${Math.round(width * 100) / 100}px`
    media.style.height = ''
    media.style.maxWidth = ''
    media.style.maxHeight = ''
}

/**
 * Click to enter the zoom state; click anywhere else to leave it.
 *
 * This is the explicit gesture that keeps the wheel unambiguous: until the user
 * clicks, the figure owns no wheel events at all. Once zoomed, a press on the
 * media starts a PAN (handled in `onPointerDown`), so clicking the figure again
 * does not toggle anything — leaving is done by clicking outside or Escape.
 */
function onClickEnter(e: MouseEvent): void {
    const target = e.target
    if (!(target instanceof Element)) return

    // The toolbar's own buttons (lightbox / reset / attach) keep their handlers.
    if (target.closest('.image-block-header')) return

    const figure = figureFrom(target)
    if (figure) {
        if (active?.figure === figure) return
        activate(figure)
        ensureFocusable(figure)
        return
    }

    // Click anywhere else leaves the zoom.
    deactivate()
}

function onPointerUp(): void {
    if (!panning) return
    panning.state.figure.classList.remove('is-panning')
    panning = null
}

/**
 * Safety net for a pan that never got its `pointerup`: releasing the button
 * outside the window (or the window losing focus mid-drag) would otherwise
 * leave `panning` set forever, so the media keeps following the cursor and the
 * figure can never be dragged again.
 */
function endStrandedPan(): void {
    if (!panning) return
    panning.state.figure.classList.remove('is-panning')
    panning = null
}

/**
 * The toolbar's layout-reset button. Handled here rather than in each surface
 * because the button is injected into `v-html` markup that no component owns.
 * `pointerdown` on the button must not start a pan, which `figureFrom` already
 * guarantees by rejecting anything inside `.image-block-header`.
 */
function onClick(e: MouseEvent): void {
    if (!(e.target instanceof Element)) return
    const reset = e.target.closest('.image-block-reset-btn')
    if (!reset) {
        onClickEnter(e)
        return
    }
    e.preventDefault()
    // Keep the zoom alive after the reset: the user is still working with this
    // figure, and re-entering on the next wheel notch would be jarring.
    resetActiveZoom()
}

/**
 * Keyboard path. A figure is not focusable by default (it is injected markup),
 * so activation-by-focus needs an explicit opt-in: figures get `tabindex="0"`
 * the first time the pointer passes over them, which is when we know the user
 * is on a pointer device. Once focusable, Tab reaches the figure and the same
 * wheel-equivalent keys zoom it.
 *
 * `+`/`=`/`-` are the conventional zoom keys; arrows pan. Escape returns to fit
 * and leaves the zoom, matching the lightbox's Escape-to-close muscle memory.
 */
/**
 * Keyboard activation. The user's model is "hover OR focus activates", so
 * focusing a figure must engage the zoom just like hovering it — otherwise the
 * keyboard path would be a dead end for anyone who never moved a pointer over
 * the figure (the `tabindex` is stamped on hover, so a keyboard-only user has
 * no way in).
 *
 * `focusin` bubbles, so one listener covers every figure. The figure is made
 * focusable here too: a diagram reached by Tab for the first time is stamped
 * on the spot, so the very first Tab stop works.
 */
function onFocusIn(e: FocusEvent): void {
    const target = e.target
    if (!(target instanceof Element)) return
    const figure = target.closest<HTMLElement>('.image-block-wrapper')
    if (!figure || !zoomableMedia(figure)) return
    ensureFocusable(figure)
    if (active?.figure === figure) return
    activate(figure)
}

/** Leaving a figure by keyboard ends the zoom, mirroring pointer-leave. */
function onFocusOut(e: FocusEvent): void {
    if (!active) return
    const next = e.relatedTarget
    if (next instanceof Node && active.figure.contains(next)) return
    deactivate()
}

function onKeyDown(e: KeyboardEvent): void {
    // Escape must work regardless of where focus sits: once the user is in the
    // active state the document body usually has focus (they clicked the media,
    // which is not focusable by default), so keying off `figureFrom(e.target)`
    // alone would make Escape a no-op exactly when it is needed.
    if (e.key === 'Escape' && active) {
        e.preventDefault()
        deactivate()
        return
    }

    const figure = figureFrom(e.target)
    if (!figure) return

    const state = active?.figure === figure ? active : null
    const zoomIn = e.key === '+' || e.key === '=' || e.key === 'Add'
    const zoomOut = e.key === '-' || e.key === '_' || e.key === 'Subtract'
    const panKeys: Record<string, [number, number]> = {
        ArrowLeft: [1, 0],
        ArrowRight: [-1, 0],
        ArrowUp: [0, 1],
        ArrowDown: [0, -1],
    }
    const panDir = panKeys[e.key]

    if (!zoomIn && !zoomOut && !panDir) return
    // At fit scale with nothing to zoom out of, let the key through.
    if (!state && !zoomIn) return

    e.preventDefault()

    const target = state ?? activate(figure)
    if (!target) return

    if (zoomIn || zoomOut) {
        const next = Math.min(MAX_SAFE_SCALE, Math.max(MIN_SCALE, target.scale * (zoomIn ? WHEEL_STEP : 1 / WHEEL_STEP)))
        if (next === target.scale) return
        // Keyboard zoom anchors on the frame centre — there is no cursor.
        const frame = figure.getBoundingClientRect()
        zoomAt(target, next, frame.left + frame.width / 2, frame.top + frame.height / 2)
        return
    }

    if (panDir) {
        // Arrow keys pan by a tenth of the frame, so holding one crosses the
        // frame in a couple of seconds.
        const step = Math.max(20, figure.getBoundingClientRect().width / 10)
        target.tx += panDir[0] * step
        target.ty += panDir[1] * step
        apply(target)
    }
}

/**
 * Install the delegated listeners. Idempotent; safe to call from every
 * bootstrap. Returns a teardown function (tests / HMR).
 */
export function installInlineZoom(): () => void {
    if (installed || typeof document === 'undefined') return () => {}
    // Touch-only devices never get inline zoom — see the module comment.
    if (!supportsInlineZoom()) return () => {}
    installed = true

    // `passive: false` on wheel is required: we call preventDefault to stop the
    // page from scrolling while zooming.
    const wheelOpts: AddEventListenerOptions = { passive: false }
    document.addEventListener('wheel', onWheel, wheelOpts)
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('pointermove', onPointerMove)
    document.addEventListener('pointerup', onPointerUp)
    document.addEventListener('pointercancel', onPointerUp)
    document.addEventListener('click', onClick)
    document.addEventListener('keydown', onKeyDown)
    // `pointerover` bubbles (unlike `pointerenter`), so one listener covers
    // every figure without per-element wiring.
    document.addEventListener('pointerover', onPointerOver)
    document.addEventListener('focusin', onFocusIn)
    document.addEventListener('focusout', onFocusOut)
    // A pan whose pointerup was never delivered (released outside the window,
    // window lost focus) would otherwise stick forever.
    window.addEventListener('blur', endStrandedPan)

    removeListeners = () => {
        document.removeEventListener('wheel', onWheel)
        document.removeEventListener('pointerdown', onPointerDown)
        document.removeEventListener('pointermove', onPointerMove)
        document.removeEventListener('pointerup', onPointerUp)
        document.removeEventListener('pointercancel', onPointerUp)
        document.removeEventListener('click', onClick)
        document.removeEventListener('keydown', onKeyDown)
        document.removeEventListener('pointerover', onPointerOver)
        document.removeEventListener('focusin', onFocusIn)
        document.removeEventListener('focusout', onFocusOut)
        window.removeEventListener('blur', endStrandedPan)
        deactivate()
        installed = false
        removeListeners = null
    }
    return removeListeners
}

/** Tear down the listeners (tests / HMR). */
export function uninstallInlineZoom(): void {
    if (removeListeners) removeListeners()
}

/** Test hook: current zoom scale of the active figure (0 when none). */
export function activeZoomScale(): number {
    return active?.scale ?? 0
}

/** Test hook: whether a figure is the active zoom target. */
export function isZoomActive(): boolean {
    return active !== null
}

