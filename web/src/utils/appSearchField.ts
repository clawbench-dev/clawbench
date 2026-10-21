/**
 * Ctrl+F routing for the app's own find boxes.
 *
 * The global shortcut bails out of any INPUT/TEXTAREA so typing in the chat
 * box, a rename field, etc. is never hijacked. That bail-out also swallowed the
 * keystroke when focus was already inside one of *our* find boxes, letting the
 * browser's native find bar open on top of ours ("press Ctrl+F again while the
 * search box is focused").
 *
 * These helpers let the handler recognise its own search fields and keep the
 * keystroke in-app: the box stays focused, its text gets selected, and the
 * native find bar never appears.
 */

/**
 * The find boxes Ctrl+F itself focuses. Each entry must be the exact container
 * the corresponding App route targets — anything broader would also capture
 * unrelated filter boxes (settings, git log, forge, project dialog), which must
 * keep the browser's native Ctrl+F.
 *
 *   - `.cm-search input` — the shared SearchBar, which backs the CodeMirror
 *     panel, the markdown preview bar and the HTML preview bar. Matches the
 *     replace input too; `refocusSearchField` routes back to the search field.
 *   - `.code-preview-search-input` — the code-preview card's inline bar.
 *   - `.fs-nav-bottom .search-pill input` — the file manager's resident filter
 *     (browse tab).
 *   - `.session-search-input-row .search-pill input` — the chat tab's session
 *     search drawer.
 *   - `.search-input-row .search-pill input` — the view tab's fallback
 *     SearchDrawer (PDF/media/unsupported files).
 *   - `.cs-input-row .search-pill input` — Ctrl+Shift+F content search.
 */
const SEARCH_FIELD_SELECTOR = [
    '.cm-search input',
    '.code-preview-search-input',
    '.fs-nav-bottom .search-pill input',
    '.session-search-input-row .search-pill input',
    '.search-input-row .search-pill input',
    '.cs-input-row .search-pill input',
].join(', ')

export function isAppSearchField(target: EventTarget | null): target is HTMLInputElement {
    return target instanceof HTMLElement && target.matches(SEARCH_FIELD_SELECTOR)
}

/**
 * Focus the find box that owns `field` and select its text.
 *
 * `field` may be the replace input (same bar), so focus is routed to the bar's
 * search input rather than to whichever input happened to have focus.
 */
export function refocusSearchField(field: HTMLInputElement): void {
    const input = field.closest('.cm-search')?.querySelector<HTMLInputElement>('input[name=search]') ?? field
    input.focus()
    input.select()
}
