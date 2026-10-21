/**
 * Custom search panel for the CodeMirror code viewer.
 *
 * Replaces the built-in @codemirror/search panel (whose flat flex DOM makes a
 * clean two-row layout impossible) with our own DOM, while still driving the
 * editor with @codemirror/search's state + commands (searchState, SearchQuery,
 * findNext/..., setSearchQuery). The panel renders into `view.dom` below the
 * scroller, mirroring where CodeMirror's own bottom panels live.
 *
 * The panel is rendered by mounting the shared SearchBar component (see
 * components/common/SearchBar.vue) — the exact same control the markdown
 * preview uses, so both search UIs are visually identical.
 *
 * The SearchBar is mounted imperatively with Vue's `h()` + `render()`. Each
 * update re-creates the vnode with fresh props (Vue patches in place, so the
 * input element and its focus survive); `onXxx` props receive the component's
 * emits (input / prev / next / close / …).
 */
import { EditorView, ViewPlugin, ViewUpdate, Decoration, DecorationSet } from '@codemirror/view'
import { EditorSelection, EditorState, Prec, RangeSetBuilder, StateEffect, StateField } from '@codemirror/state'
import {
    SearchQuery,
    setSearchQuery,
    getSearchQuery,
    replaceNext,
    replaceAll,
} from '@codemirror/search'
import { h, render, type VNode } from 'vue'
import SearchBar, { type SearchBarLabels } from '@/components/common/SearchBar.vue'

/** Toggle the custom search panel open/closed. */
export const searchPanelToggle = StateEffect.define<boolean>()

/** Whether the custom search panel is currently open. */
export const searchPanelField = StateField.define<boolean>({
    create: () => false,
    update(value, tr) {
        for (const e of tr.effects) {
            if (e.is(searchPanelToggle)) return e.value
        }
        return value
    },
})

/** Open the custom search panel programmatically (toolbar button / Mod-f). */
export function openSearchPanelCommand(view: EditorView): boolean {
    view.dispatch({ effects: searchPanelToggle.of(true) })
    return true
}

/**
 * Close the panel and clear the query.
 *
 * The query must be cleared (not just hidden): search state survives a panel
 * toggle, so a stale query would otherwise resurface when the panel reopens.
 * Clearing it here also makes the highlight (which would otherwise be gated off
 * anyway) come back fresh on the next search.
 */
export function closeSearchPanelCommand(view: EditorView): boolean {
    view.dispatch({
        effects: [
            searchPanelToggle.of(false),
            setSearchQuery.of(new SearchQuery({ search: '' })),
        ],
    })
    return true
}

export interface SearchPanelPhrases {
    find: string
    replace: string
    next: string
    previous: string
    matchCase: string
    regexp: string
    byWord: string
    replaceAction: string
    replaceAllAction: string
    close: string
}

export interface SearchPanelSpec {
    phrases: SearchPanelPhrases
    /** Hide the replace row for read-only editors. */
    readonly: boolean
}

// ── Match highlighting ──────────────────────────────────────────────────────
// @codemirror/search's own `searchHighlighter` only renders matches while its
// *built-in* panel is open (it checks `searchState.panel`), so it never fires
// with our custom panel (searchState.panel stays null). This plugin re-derives
// the decorations from searchState itself, so all matches get a persistent
// background highlight and the active (selected) match gets the flash class.
// Prec.high beats the library's Prec.low searchHighlighter.
//
// Panel-close gating: closing the panel must clear the highlight. The library's
// search state keeps the query around after the panel closes, so matching on
// the query alone would leave every match highlighted forever. Instead we also
// gate on searchPanelField — while the panel is closed no decorations render,
// regardless of what the query is.
const searchMatchDeco = Decoration.mark({ class: 'cm-searchMatch' })
const selectedMatchDeco = Decoration.mark({ class: 'cm-searchMatch cm-searchMatch-selected cm-searchMatch-flash' })

function buildSearchDecorations(view: EditorView): DecorationSet {
    // No highlight while the search panel is closed (the query may linger in
    // @codemirror/search state after close()).
    if (!view.state.field(searchPanelField, false)) return Decoration.none
    const spec = getSearchQuery(view.state)
    if (!spec || !spec.valid || !spec.search) return Decoration.none
    const query = new SearchQuery(spec)
    const state = view.state
    const sel = state.selection.main
    const builder = new RangeSetBuilder<Decoration>()
    const cursor = query.getCursor(state)
    let step = cursor.next()
    let count = 0
    while (!step.done) {
        const { from, to } = step.value
        const active = sel.from === from && sel.to === to
        builder.add(from, to, active ? selectedMatchDeco : searchMatchDeco)
        count++
        if (count > 100000) break
        step = cursor.next()
    }
    return builder.finish()
}

const searchMatchHighlight = ViewPlugin.fromClass(
    class {
        decorations: DecorationSet
        constructor(readonly view: EditorView) {
            this.decorations = buildSearchDecorations(view)
        }
        update(update: ViewUpdate) {
            if (
                update.docChanged ||
                update.selectionSet ||
                update.viewportChanged ||
                update.transactions.some((tr) => tr.effects.some((e) => e.is(setSearchQuery))) ||
                // Rebuild when the panel opens/closes — the close transition
                // has no setSearchQuery effect, so without this the highlight
                // would not be cleared on close.
                update.state.field(searchPanelField, false) !== update.startState.field(searchPanelField, false)
            ) {
                this.decorations = buildSearchDecorations(update.view)
            }
        }
    },
    { decorations: (v) => v.decorations },
)

/**
 * Build the custom search panel extension.
 */
export function searchPanel(spec: SearchPanelSpec) {
    return [
        searchPanelField,
        // All-match highlighting + active-match flash (see above).
        Prec.high(searchMatchHighlight),
        ViewPlugin.fromClass(
            class {
                private panel: HTMLElement
                private host: HTMLElement
                private vnode: VNode | null = null
                private wasOpen = false

                // Panel state, pushed into SearchBar props on each render.
                private open = false
                private canNav = false
                private matchText = ''
                private modelValue = ''
                private replaceValue = ''
                private caseChecked = false
                private regexpChecked = false
                private wordChecked = false

                constructor(readonly view: EditorView) {
                    this.panel = document.createElement('div')
                    this.panel.className = 'cm-panels'
                    this.panel.style.display = 'none'
                    view.dom.appendChild(this.panel)

                    // Mount point for the shared SearchBar component.
                    this.host = document.createElement('div')
                    this.host.style.display = 'contents'
                    this.panel.appendChild(this.host)

                    // Mount eagerly (hidden) so the DOM is always present;
                    // the SearchBar's open prop controls visibility.
                    this.renderBar()
                }

                private renderBar() {
                    const labels = toSearchBarLabels(spec.phrases)
                    this.vnode = h(SearchBar as never, {
                        open: this.open,
                        canNav: this.canNav,
                        matchText: this.matchText,
                        modelValue: this.modelValue,
                        replaceText: this.replaceValue,
                        caseSensitive: this.caseChecked,
                        regexp: this.regexpChecked,
                        wholeWord: this.wordChecked,
                        showReplace: !spec.readonly,
                        labels,
                        onInput: (value: string) => {
                            this.modelValue = value
                            this.commit()
                        },
                        'onReplace-input': (value: string) => {
                            this.replaceValue = value
                            this.commit()
                        },
                        onPrev: () => this.goPrev(),
                        onNext: () => this.goNext(),
                        onClose: () => this.close(),
                        onEnter: (shift: boolean) => {
                            if (shift) this.goPrev()
                            else this.goNext()
                        },
                        onEscape: () => this.close(),
                        'onCase-change': (checked: boolean) => {
                            this.caseChecked = checked
                            this.commit()
                        },
                        'onRegexp-change': (checked: boolean) => {
                            this.regexpChecked = checked
                            this.commit()
                        },
                        'onWord-change': (checked: boolean) => {
                            this.wordChecked = checked
                            this.commit()
                        },
                        onReplace: () => replaceNext(this.view),
                        'onReplace-all': () => replaceAll(this.view),
                    })
                    render(this.vnode, this.host)
                }

                // Move to the next match (with wrap-around), using the public
                // getCursor API: first search forward from the selection, then
                // wrap to the document start if nothing is found ahead.
                private goNext() {
                    const spec = getSearchQuery(this.view.state)
                    if (!spec || !spec.valid || !spec.search) return
                    const query = new SearchQuery(spec)
                    const state = this.view.state
                    const { to } = state.selection.main
                    const match = findMatchAhead(query, state, to)
                    if (match) goToMatch(this.view, match.from, match.to)
                }

                // Move to the previous match (with wrap-around): search backward
                // from the selection, wrapping to the document end if needed.
                private goPrev() {
                    const spec = getSearchQuery(this.view.state)
                    if (!spec || !spec.valid || !spec.search) return
                    const query = new SearchQuery(spec)
                    const state = this.view.state
                    const { from } = state.selection.main
                    const match = findMatchBehind(query, state, from)
                    if (match) goToMatch(this.view, match.from, match.to)
                }

                private commit() {
                    const query = new SearchQuery({
                        search: this.modelValue,
                        replace: this.replaceValue,
                        caseSensitive: this.caseChecked,
                        regexp: this.regexpChecked,
                        wholeWord: this.wordChecked,
                    })
                    this.view.dispatch({ effects: setSearchQuery.of(query) })
                }

                private close() {
                    // The shared close command clears the query too — see its
                    // doc comment for why hiding alone is not enough.
                    closeSearchPanelCommand(this.view)
                }

                update(update: ViewUpdate) {
                    const open = update.state.field(searchPanelField)
                    const wasOpen = this.wasOpen
                    this.wasOpen = open
                    if (open !== wasOpen) {
                        this.panel.style.display = open ? '' : 'none'
                        if (open) {
                            const q = getSearchQuery(update.state)
                            if (q) {
                                this.modelValue = q.search
                                this.replaceValue = q.replace ?? ''
                                this.caseChecked = q.caseSensitive
                                this.regexpChecked = q.regexp
                                this.wordChecked = q.wholeWord
                            }
                            this.open = true
                            this.renderBar()
                        } else {
                            this.open = false
                            this.renderBar()
                        }
                    }
                    if (
                        update.docChanged ||
                        update.selectionSet ||
                        update.transactions.some((tr) => tr.effects.some((e) => e.is(setSearchQuery)))
                    ) {
                        try {
                            this.updateMatchInfo()
                        } catch {
                            // Match counting must never break the editor.
                        }
                    }
                }

                private updateMatchInfo() {
                    const spec = getSearchQuery(this.view.state)
                    let text = ''
                    let canNav = false
                    if (spec && spec.valid && spec.search) {
                        // getSearchQuery returns the plain query *spec*; build a
                        // SearchQuery instance to count matches via its cursor.
                        const query = new SearchQuery(spec)
                        const state = this.view.state
                        const sel = state.selection.main
                        let matches = 0
                        let current = 0
                        const cursor = query.getCursor(state)
                        let step = cursor.next()
                        while (!step.done) {
                            matches++
                            if (step.value.from <= sel.from) current++
                            if (matches > 100000) break
                            step = cursor.next()
                        }
                        if (matches) {
                            text = `${current}/${matches}`
                            canNav = true
                        }
                    }
                    this.matchText = text
                    this.canNav = canNav
                    if (this.open) this.renderBar()
                }

                destroy() {
                    if (this.vnode) {
                        render(null, this.host)
                        this.vnode = null
                    }
                    this.panel.remove()
                }
            }
        ),
    ]
}

function toSearchBarLabels(p: SearchPanelPhrases): SearchBarLabels {
    return {
        find: p.find,
        replace: p.replace,
        previous: p.previous,
        next: p.next,
        matchCase: p.matchCase,
        regexp: p.regexp,
        byWord: p.byWord,
        replaceAction: p.replaceAction,
        replaceAll: p.replaceAllAction,
        close: p.close,
    }
}

// ── Match navigation ────────────────────────────────────────────────────────
// Jump to a match: set the selection AND scroll it into view instantly,
// vertically CENTERED in the viewport. Centering keeps the selected text clear
// of the sticky-scroll overlay (which only occupies the top strip), so the
// library's default y:'nearest' (which can park a match at the very top,
// under the sticky lines) is replaced.
function goToMatch(view: EditorView, from: number, to: number) {
    const selection = EditorSelection.single(from, to)
    view.dispatch({
        selection,
        effects: EditorView.scrollIntoView(from, { y: 'center' }),
        userEvent: 'select.search',
    })
}

// First match at or after `from`, wrapping to the document start when nothing
// is found ahead (mirrors @codemirror/search's findNext wrap-around).
function findMatchAhead(query: SearchQuery, state: EditorState, from: number): { from: number; to: number } | null {
    const doc = state.doc
    const range = query.getCursor(state, from, doc.length)
    let step = range.next()
    if (!step.done) return step.value
    // Wrap: search from the document start up to `from`.
    const wrap = query.getCursor(state, 0, Math.max(0, from))
    step = wrap.next()
    return step.done ? null : step.value
}

// Last match strictly before `from`, wrapping to the document end when nothing
// is found behind (mirrors @codemirror/search's findPrevious wrap-around).
function findMatchBehind(query: SearchQuery, state: EditorState, from: number): { from: number; to: number } | null {
    const doc = state.doc
    const range = query.getCursor(state, 0, from)
    let last: { from: number; to: number } | null = null
    let step = range.next()
    while (!step.done) {
        last = step.value
        step = range.next()
    }
    if (last) return last
    // Wrap: search from `from` to the end.
    const wrap = query.getCursor(state, from, doc.length)
    step = wrap.next()
    return step.done ? null : step.value
}

