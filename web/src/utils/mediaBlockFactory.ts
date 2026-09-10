/**
 * Shared factory producing the unified "bordered block figure + header bar"
 * wrapper for every inline media type in rendered markdown:
 *
 *   - raster <img>            (marked with `lightbox-img`)
 *   - bare inline <svg>       returned directly by the AI (marked `lightbox-svg`)
 *   - mermaid <div class="mermaid"> (DOM-level, see armMermaidFigure)
 *
 * All of them render as:
 *
 *   <div class="image-block-wrapper">            border:1px solid var(--border-color)
 *     <div class="image-block-header">
 *       <span class="image-block-header-actions">
 *         <button class="image-block-view-btn" ...>  ← always present
 *         [<button class="image-block-attach-btn">]  ← local file preview only
 *         [<button class="image-block-open-btn">]    ← local file preview only
 *       </span>
 *     </div>
 *     <span class="lightbox-img-wrap">…img / svg…</span>  OR  div.mermaid
 *   </div>
 *
 * Chat / file-preview / share / export all converge on this factory so the
 * figure markup and its CSS live in exactly one place. The wrapper class is
 * `.image-block-wrapper` for every media type (the old mermaid-specific
 * `.mermaid-block-wrapper` naming is gone); interactions reach content through
 * the preserved `lightbox-img` / `lightbox-svg` / `.mermaid` marker classes.
 */

import { gt } from '@/composables/useLocale'
import { isShareMode } from '@/share/shareMode'
import { ATTACH_BADGE_SVG } from '@/utils/attachSvg'
import { FILE_OPEN_ICON_SVG } from '@/composables/useFilePathAnnotation'

/** Feather "maximize" glyph for the header view button. */
export const IMAGE_VIEW_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3"/><path d="M21 8V5a2 2 0 0 0-2-2h-3"/><path d="M3 16v3a2 2 0 0 0 2 2h3"/><path d="M16 21h3a2 2 0 0 0 2-2v-3"/></svg>'

/**
 * Ensure the raster `<img>` carries the `lightbox-img` marker (no duplicates),
 * so the Lightbox collector and PC drag keep working.
 */
function stampLightboxImg(el: Element): void {
    const classes = el.getAttribute('class')
    const next = classes ? `${classes} lightbox-img` : 'lightbox-img'
    el.setAttribute('class', next.split(/\s+/).filter((c, i, arr) => c && arr.indexOf(c) === i).join(' '))
}

/**
 * Lift every media element in an HTML fragment into a block-level
 * `.image-block-wrapper` figure with a header row of action buttons:
 *   - view (`.image-block-view-btn`, data-action="view") — opens the lightbox
 *   - attach (`.image-block-attach-btn`) — only for LOCAL images (data-attach-src)
 *   - open  (`.image-block-open-btn`)  — only for LOCAL images; opens the file
 * Header buttons are skipped entirely in share mode (no chat / lightbox to reach).
 *
 * Targets every `<img>` (the chat pipeline has already stamped `lightbox-img` /
 * `chat-img` on it; the file-preview pipeline lets this factory stamp it) and
 * bare `svg.lightbox-svg` (marker class added by markInlineSvgs in the chat
 * pipeline; bare svg in file previews is left as-is, as before).
 * SVG elements still inside an interactive UI element injected by the pipeline
 * (a/button) are skipped — they are not content media.
 *
 * Media are inline-flow in the marked output; lifting them to block requires:
 *   - media alone in a <p> → the <p> is replaced by the figure;
 *   - media mid-paragraph → the paragraph is split (before text <p> + figure +
 *     after text <p>);
 *   - media in other containers (<li>, <td>, blockquote…) → the figure is
 *     inserted in place of the element.
 * The wrapped img keeps the `lightbox-img` class and `data-attach-src` so PC drag
 * (mdImageDrag) and Lightbox image collection keep working; svg keeps `lightbox-svg`.
 */
export function annotateMediaBlocks(html: string): string {
    if (!html) return html
    const doc = new DOMParser().parseFromString(html, 'text/html')

    // Iterate a snapshot: each pass moves the element, so a live NodeList would skip.
    const media = Array.from(doc.querySelectorAll('img, svg.lightbox-svg'))
    for (const el of media) {
        if (el.closest('.image-block-wrapper')) continue // idempotent guard
        const isSvg = el.tagName.toLowerCase() === 'svg'
        // A block <div> may not live inside an <a>: the HTML parser hoists it
        // out and leaves an empty link, so a linked image would lose its href.
        // Keep linked media inline and just stamp the lightbox marker so taps
        // still open it full-size.
        if (el.closest('a')) {
            if (!isSvg) stampLightboxImg(el)
            continue
        }
        if (isSvg && el.closest('button')) continue // UI icon, not content

        const isLocal = el.hasAttribute('data-attach-src')
        const shareMode = isShareMode()

        // Capture where the element currently sits BEFORE any move.
        const host = el.parentNode as HTMLElement | null
        const hostIsP = !!host && host.tagName === 'P'

        // 1. Build the wrapper shell at the element's old position.
        const wrapper = doc.createElement('div')
        wrapper.className = 'image-block-wrapper'
        // Preserve the source line the host block carried so the line-anchor
        // scroll (scrollRenderedToLine) can still locate media-containing
        // paragraphs after the <p> is dissolved into the figure.
        const hostLine = host?.getAttribute?.('data-source-line')
        if (hostLine) wrapper.setAttribute('data-source-line', hostLine)
        if (host) host.insertBefore(wrapper, el) // wrapper sits before the element

        // 2. Header row. The view (lightbox) button is always present — even on
        // the share SPA every media opens full-size in the mounted Lightbox.
        // Attach / open buttons are local-app only: they reference the open
        // file's chat attachment and in-manager file location, which a public
        // share viewer (read-only, token-scoped) cannot use.
        {
            const header = doc.createElement('div')
            header.className = 'image-block-header'
            const actions = doc.createElement('span')
            actions.className = 'image-block-header-actions'

            const viewBtn = doc.createElement('button')
            viewBtn.type = 'button'
            viewBtn.className = 'image-block-view-btn'
            viewBtn.dataset.action = 'view'
            const viewLabel = gt('imageBlock.view')
            viewBtn.title = viewLabel
            viewBtn.setAttribute('aria-label', viewLabel)
            viewBtn.innerHTML = IMAGE_VIEW_ICON_SVG
            actions.appendChild(viewBtn)

            if (isLocal && !shareMode) {
                actions.appendChild(makeHeaderButton(doc, 'image-block-attach-btn', 'attach', 'chat.attach.attachImageToChat', ATTACH_BADGE_SVG))
                actions.appendChild(makeHeaderButton(doc, 'image-block-open-btn', 'open', 'imageBlock.openFile', FILE_OPEN_ICON_SVG))
            }

            header.appendChild(actions)
            wrapper.appendChild(header)
        }

        // 3. Content cell — keeps lightbox/drag activation classes.
        const cell = doc.createElement('span')
        cell.className = isSvg ? 'lightbox-svg-wrap' : 'lightbox-img-wrap'
        if (!isSvg) stampLightboxImg(el)
        cell.appendChild(el) // detaches from host
        wrapper.appendChild(cell)

        // 4. Paragraph promotion: a <p> may not contain a block div. Split the
        // host <p> around the figure — text before becomes a leading <p>, text
        // after a trailing <p> — so multiple media in one paragraph keep their
        // original order.
        if (hostIsP && host) {
            const before: Node[] = []
            let n: Node | null = host.firstChild
            while (n && n !== wrapper) {
                before.push(n)
                n = n.nextSibling
            }
            const after: Node[] = []
            n = wrapper.nextSibling
            while (n) {
                const next: Node | null = n.nextSibling
                after.push(n)
                n = next
            }
            for (const b of before) b.parentNode?.removeChild(b)
            wrapper.parentNode?.removeChild(wrapper)

            // A node is meaningful if it is an element (e.g. another media) or
            // carries non-whitespace text. Pure text nodes of whitespace are
            // dropped so we do not emit empty <p> shells.
            const meaningful = (nodes: Node[]) =>
                nodes.some(x => x.nodeType === 1 || !!x.textContent?.trim())

            const frag = doc.createDocumentFragment()
            if (meaningful(before)) {
                const p = doc.createElement('p')
                for (const b of before) p.appendChild(b)
                frag.appendChild(p)
            }
            frag.appendChild(wrapper)
            if (meaningful(after)) {
                const p = doc.createElement('p')
                for (const a of after) p.appendChild(a)
                frag.appendChild(p)
            }
            host.replaceWith(frag)
        }
    }

    return doc.body.innerHTML
}

/**
 * DOM-level: wrap a rendered mermaid `div.mermaid` in the unified
 * `.image-block-wrapper` figure with a header bar. The diagram container stays
 * a DIRECT child of the wrapper (no content cell span — the `.mermaid` class is
 * itself the interaction marker).
 *
 * @param opts.attach  true in a file-preview context (non-share, has an md file
 *   ancestor): adds the range-reference attach button. The button carries BOTH
 *   `image-block-attach-btn` (unified styling / export cleanup) and
 *   `mermaid-block-attach-btn` (semantic class resolved by mdMermaidAttach).
 */
export function armMermaidFigure(container: HTMLElement, opts: { attach: boolean }): void {
    if (container.closest('.image-block-wrapper')) return // idempotent
    if (!container.parentNode) return

    const wrapper = document.createElement('div')
    wrapper.className = 'image-block-wrapper'

    const header = document.createElement('div')
    header.className = 'image-block-header'
    const actions = document.createElement('span')
    actions.className = 'image-block-header-actions'

    const viewBtn = document.createElement('button')
    viewBtn.type = 'button'
    viewBtn.className = 'image-block-view-btn'
    viewBtn.dataset.action = 'view'
    const viewLabel = gt('imageBlock.view')
    viewBtn.title = viewLabel
    viewBtn.setAttribute('aria-label', viewLabel)
    viewBtn.innerHTML = IMAGE_VIEW_ICON_SVG
    actions.appendChild(viewBtn)

    if (opts.attach) {
        const attachBtn = document.createElement('button')
        attachBtn.type = 'button'
        attachBtn.className = 'image-block-attach-btn mermaid-block-attach-btn'
        attachBtn.dataset.action = 'attach'
        const attachLabel = gt('chat.attach.attachDiagramToChat')
        attachBtn.title = attachLabel
        attachBtn.setAttribute('aria-label', attachLabel)
        attachBtn.innerHTML = ATTACH_BADGE_SVG
        actions.appendChild(attachBtn)
    }

    header.appendChild(actions)
    wrapper.appendChild(header)

    container.parentNode.insertBefore(wrapper, container)
    wrapper.appendChild(container)
}

/** Build a small header action button (paperclip / open-file). */
function makeHeaderButton(
    doc: Document,
    cls: string,
    action: string,
    i18nKey: string,
    svg: string
): HTMLButtonElement {
    const btn = doc.createElement('button')
    btn.type = 'button'
    btn.className = cls
    btn.dataset.action = action
    const label = gt(i18nKey)
    btn.title = label
    btn.setAttribute('aria-label', label)
    btn.innerHTML = svg
    return btn
}
