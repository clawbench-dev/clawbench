/**
 * Heading anchors for the visual markdown editor.
 *
 * The preview pipeline (markedConfig.ts) stamps every rendered heading with a
 * slug id, which is what the TOC panel scrolls to. ProseMirror renders its own
 * heading DOM without ids, so the TOC had nothing to jump to while the user was
 * in the visual editor. A decoration pass adds the same kind of id here:
 * decorations never touch the document, so the markdown written back on save is
 * unchanged.
 *
 * The TOC panel reads these ids straight from the editor DOM (the list is live
 * while typing), so the ids only have to be self-consistent. They still follow
 * the preview's convention (`slugify`, `-2`/`-3` deduplication) so both views
 * read the same way.
 */
import { Plugin, PluginKey } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as PMNode } from '@milkdown/kit/prose/model'
import { slugify } from '@/utils/toc.ts'

const headingAnchorKey = new PluginKey('md-editor-heading-anchors')

/** Build one `id` decoration per heading, in document order. */
export function buildHeadingDecorations(doc: PMNode): DecorationSet {
    const decorations: Decoration[] = []
    const counts: Record<string, number> = {}
    let untitled = 0

    doc.descendants((node: PMNode, pos: number) => {
        if (node.type.name !== 'heading') return
        const text = node.textContent.trim()
        // A heading made only of characters slugify drops (emoji, punctuation)
        // would get an empty id — an anchor nothing can select. Give it a
        // positional fallback instead of silently losing the TOC entry.
        const baseId = slugify(text) || `heading-${++untitled}`
        const count = (counts[baseId] || 0) + 1
        counts[baseId] = count
        const id = count > 1 ? `${baseId}-${count}` : baseId
        decorations.push(Decoration.node(pos, pos + node.nodeSize, { id }))
        // Headings only contain inline content; nothing below needs visiting.
        return false
    })

    return DecorationSet.create(doc, decorations)
}

export function createHeadingAnchorPlugin() {
    return new Plugin({
        key: headingAnchorKey,
        state: {
            init: (_config, state) => buildHeadingDecorations(state.doc),
            // Only edits can change the outline; selection/cursor transactions
            // reuse the existing set instead of walking the document again.
            apply: (tr, previous) => (tr.docChanged ? buildHeadingDecorations(tr.doc) : previous),
        },
        props: {
            decorations: (state) => headingAnchorKey.getState(state),
        },
    })
}
