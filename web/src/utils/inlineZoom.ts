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

/** Scale bounds. `FIT` is the resting state (no transform). */
export const MIN_SCALE = 1
export const MAX_SCALE = 8
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
    const { figure, media } = active
    media.style.transform = ''
    media.style.transformOrigin = ''
    media.style.width = ''
    media.style.height = ''
    media.style.maxWidth = ''
    media.style.maxHeight = ''
    figure.classList.remove(ZOOM_ACTIVE_CLASS)
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
    return { w: baseW * state.scale, h: baseH * state.scale }
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

    active = { figure, media, scale: MIN_SCALE, tx: 0, ty: 0 }
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

    const next = Math.min(MAX_SCALE, Math.max(MIN_SCALE, state.scale * (e.deltaY > 0 ? 1 / WHEEL_STEP : WHEEL_STEP)))

    // Zooming back out to fit scale ends the gesture in this same notch: the
    // figure returns to its resting size and the page takes over again. Leaving
    // the zoom engaged at 1x would show a grab cursor over an unzoomed figure
    // and cost the user one extra notch to escape.
    if (e.deltaY > 0 && next <= MIN_SCALE) {
        e.preventDefault()
        deactivate()
        return
    }

    if (next === state.scale) {
        // Already at the bound: swallow the notch rather than letting the page
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
 * leave `panning` set forever, so the media keeps following the cursor and
 * `onPointerLeave` early-returns instead of ending the zoom — a permanently
 * stuck figure. Both signals are cheap and cover different platforms.
 */
function endStrandedPan(): void {
    if (!panning) return
    panning.state.figure.classList.remove('is-panning')
    panning = null
}

function onPointerLeave(e: PointerEvent): void {
    if (!active || e.pointerType === 'touch') return
    if (panning) return

    // `pointerout` fires when moving from the media onto the figure's own
    // toolbar too — and `figureFrom` deliberately rejects toolbar targets, so
    // asking it would read "left the figure" and hide the reset button just as
    // the pointer arrives on it. `relatedTarget` is the authoritative answer
    // when the browser provides one.
    const next = e.relatedTarget
    if (next instanceof Node && active.figure.contains(next)) return

    // `relatedTarget === null` is ambiguous: it means "left to a non-node" OR
    // "released outside the window mid-drag". A geometry check disambiguates —
    // if the pointer is still inside the figure's box, the zoom stays. This
    // also covers a drag released just outside: the user was mid-gesture, and
    // dropping the zoom there would undo their pan.
    if (e.clientX || e.clientY) {
        const box = active.figure.getBoundingClientRect()
        const inside = e.clientX >= box.left && e.clientX <= box.right
            && e.clientY >= box.top && e.clientY <= box.bottom
        if (inside) return
    }

    // Leaving the figure entirely: drop the zoom.
    deactivate()
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
        const next = Math.min(MAX_SCALE, Math.max(MIN_SCALE, target.scale * (zoomIn ? WHEEL_STEP : 1 / WHEEL_STEP)))
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
    document.addEventListener('pointerout', onPointerLeave)
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
        document.removeEventListener('pointerout', onPointerLeave)
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

