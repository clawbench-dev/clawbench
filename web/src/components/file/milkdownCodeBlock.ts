/**
 * Code-block behaviour for the visual markdown editor.
 *
 * Two gaps are filled here without pulling in Crepe's full CodeMirror block:
 *
 * 1. **Mermaid** — a ` ```mermaid ` fence keeps its editable source and gains a
 *    rendered diagram above it. Rendering reuses the preview's mermaid module
 *    (theme-aware, lazy 600KB chunk) so a diagram looks the same in both places.
 * 2. **Syntax colour** — a decoration pass runs the same highlight.js facade the
 *    preview uses and wraps token ranges with the existing `hljs-*` classes, so
 *    the editor picks up whatever colours the current theme already defines.
 *    Decorations never touch the document, so the markdown is unaffected.
 */
import { codeBlockSchema } from '@milkdown/kit/preset/commonmark'
import { $view } from '@milkdown/kit/utils'
import { Plugin, PluginKey } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as PMNode } from '@milkdown/kit/prose/model'
import { hljs, highlightCode, onLanguageLoaded } from '@/utils/highlight.ts'

/** Code block node view: optional mermaid preview + the ordinary editable <pre>. */
export function createCodeBlockView() {
    return $view(codeBlockSchema.node, () => (node, _view, _getPos) => {
        const wrapper = document.createElement('div')
        wrapper.className = 'md-code-block'
        const preview = document.createElement('div')
        preview.className = 'md-code-preview'
        preview.hidden = true
        const pre = document.createElement('pre')
        const code = document.createElement('code')
        pre.appendChild(code)
        wrapper.append(preview, pre)

        let token = 0
        let timer: ReturnType<typeof setTimeout> | null = null
        let lastText = ''

        const clearPreview = () => {
            token++
            if (timer) clearTimeout(timer)
            timer = null
            preview.hidden = true
            preview.replaceChildren()
            lastText = ''
        }

        const renderMermaid = (text: string) => {
            const mine = ++token
            if (timer) clearTimeout(timer)
            timer = setTimeout(async () => {
                if (mine !== token) return
                preview.hidden = false
                preview.replaceChildren()
                const source = document.createElement('pre')
                source.className = 'mermaid'
                source.textContent = text
                preview.appendChild(source)
                try {
                    const mermaid = await import('@/utils/mermaid.ts')
                    if (mine !== token) return
                    await mermaid.renderMermaidInElement(preview, 'md-editor-mermaid')
                } catch {
                    // The mermaid module paints its own error card on failure.
                }
            }, 250)
        }

        const sync = (n: PMNode) => {
            const language = (n.attrs.language || '').toLowerCase()
            pre.dataset.language = n.attrs.language || ''
            const text = n.textContent
            if (language !== 'mermaid' || !text.trim()) {
                clearPreview()
                return
            }
            if (text === lastText) return
            lastText = text
            renderMermaid(text)
        }

        sync(node)
        return {
            dom: wrapper,
            contentDOM: code,
            update: (next) => {
                if (next.type.name !== 'code_block') return false
                sync(next)
                return true
            },
            // Mutations inside the editable <code> belong to ProseMirror; the
            // preview we paint above it is ours and must not trigger a reload.
            ignoreMutation: (mutation) => !code.contains(mutation.target as Node),
            destroy: () => {
                token++
                if (timer) clearTimeout(timer)
            },
        }
    })
}

const highlightKey = new PluginKey('md-editor-highlight')

/** Build `hljs-*` inline decorations for every fenced code block. */
function buildHighlightDecorations(doc: PMNode): DecorationSet {
    const decorations: Decoration[] = []

    doc.descendants((node: PMNode, pos: number) => {
        if (node.type.name !== 'code_block') return
        const language = (node.attrs.language || '').toLowerCase()
        const text = node.textContent
        // Mermaid is drawn as a diagram; colouring its DSL adds noise.
        if (!text || !language || language === 'mermaid') return
        if (!hljs.getLanguage(language)) return

        let html: string
        try {
            html = highlightCode(text, language)
        } catch {
            return
        }
        const parsed = new DOMParser().parseFromString(`<div id="hljs-root">${html}</div>`, 'text/html')
        const root = parsed.getElementById('hljs-root')
        if (!root) return

        let offset = 0
        const walk = (el: Element) => {
            for (const child of Array.from(el.childNodes)) {
                if (child.nodeType === 3 /* TEXT_NODE */) {
                    const length = (child.textContent || '').length
                    let cls = ''
                    let parent: Element | null = child.parentElement
                    while (parent && parent !== root) {
                        const found = Array.from(parent.classList).filter((c) => c.startsWith('hljs-'))
                        if (found.length) cls = found.join(' ')
                        parent = parent.parentElement
                    }
                    if (cls && length) {
                        decorations.push(
                            Decoration.inline(pos + 1 + offset, pos + 1 + offset + length, { class: cls }),
                        )
                    }
                    offset += length
                } else if (child.nodeType === 1 /* ELEMENT_NODE */) {
                    walk(child as Element)
                }
            }
        }
        walk(root)
    })

    return DecorationSet.create(doc, decorations)
}

/**
 * Recompute highlighting after typing stops (180ms). Running highlight.js on
 * every keystroke would stall long documents; the document itself is never
 * modified, only the decoration set.
 */
export function createHighlightPlugin() {
    return new Plugin({
        key: highlightKey,
        state: {
            init: (_config, state) => buildHighlightDecorations(state.doc),
            apply: (tr, previous) => tr.getMeta(highlightKey) ?? previous,
        },
        props: {
            decorations: (state) => highlightKey.getState(state),
        },
        view: (editorView) => {
            let timer: ReturnType<typeof setTimeout> | null = null
            const schedule = () => {
                if (timer) clearTimeout(timer)
                timer = setTimeout(() => {
                    const decorations = buildHighlightDecorations(editorView.state.doc)
                    editorView.dispatch(
                        editorView.state.tr.setMeta(highlightKey, decorations).setMeta('addToHistory', false),
                    )
                }, 180)
            }
            // A lazily-loaded grammar arrives after the first paint; recompute
            // once it does so the block picks up colour without a keystroke.
            const off = onLanguageLoaded(schedule)
            return {
                update: (view, previous) => {
                    if (!view.state.doc.eq(previous.doc)) schedule()
                },
                destroy: () => {
                    if (timer) clearTimeout(timer)
                    off()
                },
            }
        },
    })
}
