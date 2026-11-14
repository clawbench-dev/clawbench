/**
 * Raw HTML in the visual markdown editor.
 *
 * Milkdown keeps every raw HTML chunk as an inline atom holding its own source
 * text, so a README-style `<p align="center"><img …></p>` block shows up as
 * literal tags. This node view renders such a chunk in place instead.
 *
 * Only *complete* elements are rendered. Inline HTML arrives as separate atoms
 * (`<b>` and `</b>` are two nodes with the text between them as ordinary text),
 * and rendering half a tag would paint nothing at all — so anything that is not
 * a whole element keeps showing its source, exactly as before.
 *
 * The atom stays the document's source of truth: rendering is presentation
 * only, so saving writes back the bytes the author typed (alignment and width
 * have no markdown spelling). The rendered block is not editable — a
 * double-click opens the source in a prompt, like formulas do.
 */
import { htmlSchema } from '@milkdown/kit/preset/commonmark'
import { $view } from '@milkdown/kit/utils'
import type { Node as PMNode } from '@milkdown/kit/prose/model'
import { resolveMarkdownImageSrc } from '@/utils/markdownImageSrc.ts'

/** `<tag …>…</tag>` — one element wrapping the whole chunk (greedy body). */
const PAIRED_TAG_RE = /^\s*<([a-zA-Z][\w:-]*)(?:\s[^<>]*)?>[\s\S]*<\/\1\s*>\s*$/
/** `<tag … />` — a self-closing element on its own. */
const SELF_CLOSING_RE = /^\s*<([a-zA-Z][\w:-]*)(?:\s[^<>]*)?\/>\s*$/
/** `<img …>` and friends: one void element, no closing tag. */
const VOID_TAG_RE = /^\s*<(?:img|hr|br|video|audio|source|iframe|picture|svg|table|input|canvas|embed|object)\b[^<>]*>\s*$/i

/**
 * How a raw HTML chunk should be presented:
 * - `block`  — a whole element; it owns its line(s) in the document
 * - `inline` — a single void element that belongs in the text flow
 * - `null`   — not a complete element; keep showing the source as text
 */
export type HtmlRenderMode = 'block' | 'inline' | null

export function htmlRenderMode(value: string): HtmlRenderMode {
    if (PAIRED_TAG_RE.test(value) || SELF_CLOSING_RE.test(value)) return 'block'
    if (VOID_TAG_RE.test(value)) return 'inline'
    return null
}

export interface HtmlBlockViewOptions {
    /** Markdown file path — relative image srcs resolve against its directory. */
    filePath: () => string | undefined
    /** Cache-busting stamp, stable for the editor session. */
    stamp: number
    /** Ask the user for new source; resolves null when they cancel. */
    edit: (value: string) => Promise<string | null>
}

/** Node view for the commonmark `html` atom: render complete elements. */
export function createHtmlBlockView(opts: HtmlBlockViewOptions) {
    return $view(htmlSchema.node, () => (node, view, getPos) => {
        const dom = document.createElement('span')
        dom.className = 'md-html-atom'
        /** Current source, so the double-click handler never reads the DOM. */
        let source = ''

        const apply = (n: PMNode) => {
            const value = String(n.attrs.value ?? '')
            source = value
            const mode = htmlRenderMode(value)
            dom.classList.toggle('md-html-block', mode === 'block')
            dom.classList.toggle('md-html-inline', mode === 'inline')
            if (!mode) {
                dom.textContent = value
                return
            }
            dom.innerHTML = value
            // The source addresses images relative to the markdown file; the
            // browser needs the served URL (same rule as the image node view).
            for (const img of Array.from(dom.querySelectorAll('img[src]'))) {
                const src = img.getAttribute('src') ?? ''
                const resolved = resolveMarkdownImageSrc(src, opts.filePath(), opts.stamp)
                if (resolved !== src) img.setAttribute('src', resolved)
            }
        }

        // Links inside rendered HTML would navigate away from the editor.
        const onClick = (e: MouseEvent) => {
            if ((e.target as Element | null)?.closest?.('a')) e.preventDefault()
        }

        const onDblClick = async (e: MouseEvent) => {
            if (htmlRenderMode(source) === null) return
            e.preventDefault()
            e.stopPropagation()
            const next = await opts.edit(source)
            if (next === null || next === source) return
            const pos = typeof getPos === 'function' ? getPos() : undefined
            if (typeof pos !== 'number') return
            view.dispatch(view.state.tr.setNodeMarkup(pos, undefined, { value: next }))
        }

        dom.addEventListener('click', onClick)
        dom.addEventListener('dblclick', onDblClick)
        apply(node)

        return {
            dom,
            update(next: PMNode) {
                if (next.type !== node.type) return false
                apply(next)
                return true
            },
            // Every DOM change here is our own rendering, never a document edit.
            ignoreMutation: () => true,
            stopEvent: (e: Event) => e.type === 'dblclick',
            destroy() {
                dom.removeEventListener('click', onClick)
                dom.removeEventListener('dblclick', onDblClick)
            },
        }
    })
}
