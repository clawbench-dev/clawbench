import { describe, it, expect, beforeEach } from 'vitest'
import { isAppSearchField, refocusSearchField } from '../appSearchField'

/**
 * The global Ctrl+F handler bails out of any INPUT/TEXTAREA so typing in the
 * chat box is never hijacked. These helpers carve out the app's own find boxes
 * so that a second Ctrl+F keeps focus in them instead of falling through to the
 * browser's native find bar.
 */
describe('appSearchField', () => {
    beforeEach(() => {
        document.body.innerHTML = ''
    })

    it('recognises the shared SearchBar input (CodeMirror panel + markdown bar)', () => {
        document.body.innerHTML = `
            <div class="search-bar"><div class="cm-search">
                <input name="search" />
            </div></div>`
        expect(isAppSearchField(document.querySelector('input[name=search]'))).toBe(true)
    })

    it('recognises the code-preview inline search input', () => {
        document.body.innerHTML = '<input class="code-preview-search-input" />'
        expect(isAppSearchField(document.querySelector('.code-preview-search-input'))).toBe(true)
    })

    it('recognises the file manager filter, but only inside its dock', () => {
        document.body.innerHTML = `
            <div class="fs-nav-bottom"><div class="search-pill"><input /></div></div>`
        expect(isAppSearchField(document.querySelector('.fs-nav-bottom input'))).toBe(true)
    })

    it('recognises the find boxes Ctrl+F itself focuses in each tab', () => {
        // Each of these is where an App Ctrl+F route puts focus; missing one
        // leaves the browser's native find bar openable on that surface.
        const cases: Array<[string, string]> = [
            ['chat tab: session search drawer', '<div class="session-search-input-row"><div class="search-pill"><input /></div></div>'],
            ['view tab fallback: SearchDrawer', '<div class="search-input-row"><div class="search-pill"><input /></div></div>'],
            ['Ctrl+Shift+F: content search', '<div class="cs-input-row"><div class="search-pill"><input /></div></div>'],
        ]
        for (const [label, html] of cases) {
            document.body.innerHTML = html
            expect(isAppSearchField(document.querySelector('.search-pill input')), label).toBe(true)
        }
    })

    it('leaves unrelated filter boxes to the browser', () => {
        // Many panels have a SearchInput; only the ones Ctrl+F focuses are ours.
        // Hijacking the rest would suppress the browser's find bar on surfaces
        // we do not search.
        const unrelated = [
            '<div class="settings-panel"><div class="search-pill"><input /></div></div>',
            '<div class="toc-panel"><div class="search-pill"><input /></div></div>',
            '<div class="git-commit-list"><div class="search-pill"><input /></div></div>',
            '<div class="project-dialog"><div class="search-pill"><input /></div></div>',
        ]
        for (const html of unrelated) {
            document.body.innerHTML = html
            expect(isAppSearchField(document.querySelector('.search-pill input')), html).toBe(false)
        }
    })

    it('rejects ordinary inputs so the shortcut stays out of the chat box', () => {
        document.body.innerHTML = '<input class="chat-input" /><textarea></textarea>'
        expect(isAppSearchField(document.querySelector('.chat-input'))).toBe(false)
        expect(isAppSearchField(document.querySelector('textarea'))).toBe(false)
        // A keydown dispatched on document has a non-Element target.
        expect(isAppSearchField(document)).toBe(false)
        expect(isAppSearchField(null)).toBe(false)
    })

    it('refocuses the bar search input and selects its text', () => {
        document.body.innerHTML = `
            <div class="search-bar"><div class="cm-search">
                <input name="search" value="alpha" />
                <input name="replace" />
            </div></div>`
        const search = document.querySelector<HTMLInputElement>('input[name=search]')!
        const replace = document.querySelector<HTMLInputElement>('input[name=replace]')!

        // Focus in the replace field still belongs to the same bar; the caret
        // must land back in the search field, not stay on replace.
        refocusSearchField(replace)

        expect(document.activeElement).toBe(search)
        expect(search.selectionStart).toBe(0)
        expect(search.selectionEnd).toBe('alpha'.length)
    })

    it('handles a bare preview input without a surrounding bar', () => {
        document.body.innerHTML = '<input class="code-preview-search-input" value="beta" />'
        const input = document.querySelector<HTMLInputElement>('.code-preview-search-input')!
        refocusSearchField(input)
        expect(document.activeElement).toBe(input)
        expect(input.selectionEnd).toBe('beta'.length)
    })
})
