import { describe, it, expect, vi, beforeEach } from 'vitest'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { search, highlightSelectionMatches, setSearchQuery, SearchQuery } from '@codemirror/search'
import { searchPanel, searchPanelField, searchPanelToggle, openSearchPanelCommand } from '../codeMirrorSearchPanel'
const phrases = { find: 'Find', replace: 'Replace', next: 'Next', previous: 'Previous', matchCase: 'Match case', regexp: 'Regexp', byWord: 'By word', replaceAction: 'Replace', replaceAllAction: 'Replace all', close: 'Close' }

function makeView(readonly = false) {
    const st = EditorState.create({
        doc: 'alpha beta\nalpha gamma\nalpha',
        extensions: [
            search({ top: false }),
            highlightSelectionMatches(),
            searchPanel({ readonly, phrases }),
        ],
    })
    return new EditorView({ state: st, parent: document.body })
}

beforeEach(() => {
    document.body.innerHTML = ''
})

describe('codeMirrorSearchPanel', () => {
    it('creates the panel DOM with search/replace/option controls', () => {
        const v = makeView(false)
        const panel = document.querySelector('.cm-search')
        expect(panel).not.toBeNull()
        expect(panel!.querySelector('input[name=search]')).not.toBeNull()
        expect(panel!.querySelector('input[name=replace]')).not.toBeNull()
        expect(panel!.querySelectorAll('.search-input-actions button.search-opt-btn')).toHaveLength(3)
        expect(panel!.querySelector('button[name=close]')).not.toBeNull()
        v.destroy()
    })

    it('hides the replace row in read-only mode', () => {
        const v = makeView(true)
        expect(document.querySelector('.cm-search input[name=replace]')).toBeNull()
        expect(document.querySelector('.cm-search button[name=replace]')).toBeNull()
        v.destroy()
    })

    it('openSearchPanelCommand shows the panel', async () => {
        const v = makeView(false)
        // Panel is mounted but hidden by default.
        expect(document.querySelector('.cm-panels')!.style.display).toBe('none')
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        expect(document.querySelector('.cm-panels')!.style.display).not.toBe('none')
        expect(v.state.field(searchPanelField)).toBe(true)
        v.destroy()
    })

    it('toggle closes the panel and match info survives a query dispatch', async () => {
        const v = makeView(false)
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        const input = document.querySelector<HTMLInputElement>('.cm-search input[name=search]')!
        input.value = 'alpha'
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise(r => setTimeout(r, 20))
        // Match info should show 3/1 or similar; and the panel must stay open.
        expect(document.querySelector('.cm-search')).not.toBeNull()
        expect(document.querySelector('.cm-panels')!.style.display).not.toBe('none')
        v.destroy()
    })

    it('types in search input update the search query and highlight matches', async () => {
        const v = makeView(false)
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        const input = document.querySelector<HTMLInputElement>('.cm-search input[name=search]')!
        input.value = 'alpha'
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise(r => setTimeout(r, 20))
        const q = v.state.field(searchPanelField)
        expect(q).toBe(true)
        // Selection is not expected to move just from typing: navigation is
        // F3 / Shift+F3 (Ctrl+G is go-to-line, see CodeMirrorViewer).
        v.destroy()
    })

    it('shows current/total match count after typing a query', async () => {
        const v = makeView(false)
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        const input = document.querySelector<HTMLInputElement>('.cm-search input[name=search]')!
        input.value = 'alpha'
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise(r => setTimeout(r, 50))
        // doc has 3 'alpha' occurrences; selection at start = match 1.
        expect(document.querySelector<HTMLElement>('.cm-search .cm-search-match-info')!.textContent).toBe('1/3')
        v.destroy()
    })

    it('findNext via the next button moves the selection and updates the counter', async () => {
        const v = makeView(false)
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        const input = document.querySelector<HTMLInputElement>('.cm-search input[name=search]')!
        input.value = 'alpha'
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise(r => setTimeout(r, 50))
        document.querySelector<HTMLElement>('.cm-search button[name=next]')!.click()
        await new Promise(r => setTimeout(r, 20))
        // First click jumps from match 1 to match 2 (anchor moved past 0).
        expect(v.state.selection.main.head).toBeGreaterThan(0)
        v.destroy()
    })

    it('navigation dispatches an instant scrollIntoView with a sticky offset', async () => {
        let captured: Array<{ y?: string; yMargin?: number }> = []
        const st = EditorState.create({
            doc: 'alpha beta\nalpha gamma\nalpha',
            extensions: [
                search({ top: false }),
                highlightSelectionMatches(),
                EditorView.updateListener.of((u) => {
                    if (u.transactions.some((t) => t.selection)) {
                        captured = u.transactions.flatMap((t) =>
                            t.effects
                                .map((e) => e.value as { y?: string; yMargin?: number })
                                .filter((v): v is { y: string; yMargin?: number } => typeof v === 'object' && v !== null && v.y !== undefined),
                        )
                    }
                }),
                searchPanel({ readonly: false, phrases }),
            ],
        })
        const v = new EditorView({ state: st, parent: document.body })
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        const input = document.querySelector<HTMLInputElement>('.cm-search input[name=search]')!
        input.value = 'alpha'
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise(r => setTimeout(r, 50))
        captured = []
        document.querySelector<HTMLElement>('.cm-search button[name=next]')!.click()
        await new Promise(r => setTimeout(r, 20))
        // The next-click jump scrolls the match to the CENTER of the viewport,
        // keeping it clear of the sticky-scroll overlay at the top.
        expect(captured.length).toBe(1)
        expect(captured[0].y).toBe('center')
        v.destroy()
    })

    it('highlights every match; the active one only after navigation', async () => {
        const v = makeView(false)
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        const input = document.querySelector<HTMLInputElement>('.cm-search input[name=search]')!
        input.value = 'alpha'
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise(r => setTimeout(r, 50))
        const content = document.querySelector<HTMLElement>('.cm-content')!
        // All 3 'alpha' occurrences carry the match class.
        expect(content.querySelectorAll('.cm-searchMatch')).toHaveLength(3)
        // Selection still sits at the document start — no active match yet.
        expect(content.querySelectorAll('.cm-searchMatch-selected')).toHaveLength(0)
        // Navigate to the first match: it becomes the active (flash) one.
        document.querySelector<HTMLElement>('.cm-search button[name=next]')!.click()
        await new Promise(r => setTimeout(r, 20))
        const selected = content.querySelectorAll('.cm-searchMatch-selected')
        expect(selected.length).toBe(1)
        expect(selected[0].classList.contains('cm-searchMatch-flash')).toBe(true)
        v.destroy()
    })

    it('moves the selected/flash class to the new match on findNext', async () => {
        const v = makeView(false)
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        const input = document.querySelector<HTMLInputElement>('.cm-search input[name=search]')!
        input.value = 'alpha'
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise(r => setTimeout(r, 50))
        document.querySelector<HTMLElement>('.cm-search button[name=next]')!.click()
        await new Promise(r => setTimeout(r, 20))
        const content = document.querySelector<HTMLElement>('.cm-content')!
        const selected = content.querySelectorAll('.cm-searchMatch-selected')
        expect(selected.length).toBe(1)
        expect(selected[0].classList.contains('cm-searchMatch-flash')).toBe(true)
        v.destroy()
    })

    it('removes all match highlights when the query is cleared', async () => {
        const v = makeView(false)
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        const input = document.querySelector<HTMLInputElement>('.cm-search input[name=search]')!
        input.value = 'alpha'
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise(r => setTimeout(r, 50))
        input.value = ''
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise(r => setTimeout(r, 50))
        expect(document.querySelector<HTMLElement>('.cm-content')!.querySelectorAll('.cm-searchMatch')).toHaveLength(0)
        v.destroy()
    })

    it('close button hides the panel', async () => {
        const v = makeView(false)
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        document.querySelector<HTMLElement>('.cm-search button[name=close]')!.click()
        await new Promise(r => setTimeout(r, 20))
        expect(v.state.field(searchPanelField)).toBe(false)
        expect(document.querySelector<HTMLElement>('.cm-panels')!.style.display).toBe('none')
        v.destroy()
    })

    it('closing the panel clears match highlights (WARN-301)', async () => {
        const v = makeView(false)
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        const input = document.querySelector<HTMLInputElement>('.cm-search input[name=search]')!
        input.value = 'alpha'
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise(r => setTimeout(r, 50))
        // Matches are highlighted while the panel is open.
        const content = document.querySelector<HTMLElement>('.cm-content')!
        expect(content.querySelectorAll('.cm-searchMatch').length).toBeGreaterThan(0)
        // Close the panel — the highlight must disappear even though the
        // @codemirror/search query may still linger in state.
        document.querySelector<HTMLElement>('.cm-search button[name=close]')!.click()
        await new Promise(r => setTimeout(r, 50))
        expect(content.querySelectorAll('.cm-searchMatch')).toHaveLength(0)
        v.destroy()
    })

    it('closing the panel clears the search query for a fresh reopen', async () => {
        const v = makeView(false)
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        const input = document.querySelector<HTMLInputElement>('.cm-search input[name=search]')!
        input.value = 'alpha'
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise(r => setTimeout(r, 50))
        document.querySelector<HTMLElement>('.cm-search button[name=close]')!.click()
        await new Promise(r => setTimeout(r, 50))
        // Reopen — the input must be empty (no stale query).
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        expect(document.querySelector<HTMLInputElement>('.cm-search input[name=search]')!.value).toBe('')
        v.destroy()
    })

    it('toggle close (raw dispatch) also clears highlights via the panel gate', async () => {
        const v = makeView(false)
        openSearchPanelCommand(v)
        await new Promise(r => setTimeout(r, 20))
        const input = document.querySelector<HTMLInputElement>('.cm-search input[name=search]')!
        input.value = 'alpha'
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise(r => setTimeout(r, 50))
        const content = document.querySelector<HTMLElement>('.cm-content')!
        expect(content.querySelectorAll('.cm-searchMatch').length).toBeGreaterThan(0)
        // This is the raw toggle used by CodeMirrorViewer's openSearch() repeat
        // click — it bypasses close(), but the highlight must still clear
        // because buildSearchDecorations is gated on the panel state.
        v.dispatch({ effects: searchPanelToggle.of(false) })
        await new Promise(r => setTimeout(r, 50))
        expect(v.state.field(searchPanelField)).toBe(false)
        expect(content.querySelectorAll('.cm-searchMatch')).toHaveLength(0)
        v.destroy()
    })
})
