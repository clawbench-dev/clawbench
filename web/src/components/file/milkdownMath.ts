/**
 * LaTeX support for the visual markdown editor.
 *
 * Milkdown has no math plugin of its own outside the (much heavier) Crepe
 * preset, so this is the small version: `remark-math` reads `$…$` and `$$…$$`
 * into mdast `inlineMath` / `math` nodes, two atom schemas round-trip them back
 * to the same syntax, and a node view renders KaTeX (the same library the
 * preview uses) instead of showing the raw source.
 *
 * Editing: the rendered formula is an atom, so double-clicking it opens a
 * prompt. Typing LaTeX into a WYSIWYG surface would need a dedicated inline
 * editor; a prompt keeps the document model simple and never risks the
 * round-trip.
 */
import { $nodeSchema, $remark, $view } from '@milkdown/kit/utils'
import remarkMath from 'remark-math'
import katex from 'katex'

export type EditMath = (value: string) => Promise<string | null>

export function createMathPlugins(editMath: EditMath) {
    const remarkMathPlugin = $remark('remarkMath', () => remarkMath)

    /** Shared attrs/parse/serialize shape for the inline and block atoms. */
    function mathSchema(id: 'math_inline' | 'math_block', mdastType: 'inlineMath' | 'math', display: boolean) {
        return $nodeSchema(id, () => ({
            group: display ? 'block' : 'inline',
            inline: !display,
            atom: true,
            draggable: false,
            attrs: {
                value: { default: '' },
            },
            parseDOM: [
                {
                    tag: `${display ? 'div' : 'span'}[data-type="${id}"]`,
                    getAttrs: (dom: HTMLElement) => ({ value: dom.dataset.value ?? '' }),
                },
            ],
            toDOM: (node) => {
                const el = document.createElement(display ? 'div' : 'span')
                el.dataset.type = id
                el.dataset.value = node.attrs.value
                try {
                    katex.render(node.attrs.value || '', el, { displayMode: display, throwOnError: false })
                } catch {
                    el.textContent = node.attrs.value || ''
                }
                return el
            },
            parseMarkdown: {
                match: (node) => node.type === mdastType,
                runner: (state, node, type) => {
                    state.addNode(type, { value: node.value as string })
                },
            },
            toMarkdown: {
                match: (node) => node.type.name === id,
                runner: (state, node) => {
                    state.addNode(mdastType, undefined, node.attrs.value)
                },
            },
        }))
    }

    const mathInlineSchema = mathSchema('math_inline', 'inlineMath', false)
    const mathBlockSchema = mathSchema('math_block', 'math', true)

    /** Render with KaTeX; double-click opens the prompt and writes the attr back. */
    function mathView(display: boolean) {
        return $view((display ? mathBlockSchema : mathInlineSchema).node, () => (node, view, getPos) => {
            const dom = document.createElement(display ? 'div' : 'span')
            dom.className = display ? 'md-math-block' : 'md-math-inline'
            let current = node

            const render = (n: { attrs: { value?: string } }) => {
                dom.replaceChildren()
                try {
                    katex.render(n.attrs.value || '', dom, { displayMode: display, throwOnError: false })
                } catch {
                    dom.textContent = n.attrs.value || ''
                }
                dom.dataset.value = n.attrs.value || ''
            }
            render(node)

            dom.addEventListener('dblclick', (e) => {
                e.preventDefault()
                e.stopPropagation()
                const pos = getPos()
                if (typeof pos !== 'number') return
                void editMath(current.attrs.value || '').then((next) => {
                    if (next == null || next === current.attrs.value) return
                    // The prompt is async: the document may have changed while it
                    // was open. Only write when the node still sits at that
                    // position and is still the same kind of formula.
                    const nodeAtPos = view.state.doc.nodeAt(pos)
                    if (!nodeAtPos || nodeAtPos.type.name !== current.type.name) return
                    view.dispatch(view.state.tr.setNodeMarkup(pos, undefined, { ...current.attrs, value: next }))
                })
            })

            return {
                dom,
                update: (next) => {
                    if (next.type.name !== current.type.name) return false
                    current = next
                    render(next)
                    return true
                },
                ignoreMutation: () => true,
            }
        })
    }

    // `$nodeSchema` returns [ctxSlice, schemaPlugin]; `.use()` wants a flat list.
    return [remarkMathPlugin, ...mathInlineSchema, ...mathBlockSchema, mathView(false), mathView(true)]
}
