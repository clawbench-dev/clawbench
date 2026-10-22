import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Ctrl+F must behave like a find bar, not a toggle:
 *   1. Pressing it while the search box is focused must keep focus in the box
 *      and keep the browser's native find bar out of the way.
 *   2. Pressing it again after the box lost focus must REFOCUS it (and reopen
 *      it if it was dismissed) instead of doing nothing.
 *
 * App.vue has no component test (its only tests read it as source text), and
 * CodeMirrorViewer is mounted through `defineAsyncComponent` so its ref is not
 * reachable from jsdom either. The behaviours themselves are covered in the
 * component suites; these guards pin the App-level routing that a refactor can
 * silently drop.
 */
describe('Ctrl+F routing in App.vue', () => {
  const app = readWebFile('src/App.vue')

  function ctrlFBody() {
    const body = app.slice(app.indexOf('function handleCtrlF'))
    return body.slice(0, body.indexOf('\n}'))
  }

  it('keeps the keystroke in-app when one of our own find boxes has focus', () => {
    // The generic INPUT bail-out below would otherwise let the browser's native
    // find bar open on top of ours.
    const head = ctrlFBody()
    expect(head).toContain('isAppSearchField(target)')
    expect(head).toContain('refocusSearchField(target)')
    // Must run BEFORE the INPUT/TEXTAREA bail-out, or the branch is dead.
    expect(head.indexOf('isAppSearchField(target)')).toBeLessThan(
      head.indexOf("tag === 'INPUT'"),
    )
    // And it must swallow the key. The `return` is asserted separately: slicing
    // to a missing `return` yields index -1, and `slice(0, -1)` would still
    // contain preventDefault() — so a dropped `return` (which falls through and
    // ALSO opens a drawer) would pass unnoticed.
    const branch = head.slice(head.indexOf('isAppSearchField(target)'))
    const branchHead = branch.slice(0, branch.indexOf('refocusSearchField'))
    expect(branchHead).toContain('e.preventDefault()')
    expect(branch).toMatch(/refocusSearchField\(target\)\s*\n\s*return\b/)
  })

  it('checks the dialog and terminal guards before the find-box branch', () => {
    // A search field inside a modal dialog must not be hijacked by the app's own
    // routing; the dialog owns the key there.
    const head = ctrlFBody()
    const findBox = head.indexOf('isAppSearchField(target)')
    expect(head.indexOf('_dlg.state.value.visible')).toBeLessThan(findBox)
    expect(head.indexOf("'.terminal-panel'")).toBeLessThan(findBox)
  })

  it('leaves ordinary inputs to the browser', () => {
    // The chat box, rename fields and the terminal must keep native Ctrl+F.
    //
    // Assert on the STATEMENTS, not on a substring of the condition: the same
    // `tag === 'INPUT' || tag === 'TEXTAREA'` text also appears elsewhere in the
    // file (the sibling copies in ChatMessageList/ChatPanelContent), so a bare
    // `toContain` on the whole body would still pass with the bail-out deleted
    // from this function. Anchoring on the `return` makes the guard depend on
    // the branch actually existing here.
    const head = ctrlFBody()
    expect(head).toMatch(/if\s*\(\s*tag === 'INPUT' \|\| tag === 'TEXTAREA'\s*\)\s*return\b/)
    expect(head).toMatch(/if\s*\(\s*target\?\.isContentEditable\s*\)\s*return\b/)
    expect(head).toContain("'.terminal-panel'")
  })

  it('runs the INPUT bail-out AFTER the find-box branch', () => {
    // Order is the whole point of the find-box branch: if the generic INPUT
    // bail-out came first, a focused find box would fall through to the
    // browser's native find bar and the branch would be dead code. Asserting
    // the two statements' relative position is what catches a reorder; a
    // `toContain` on each would pass either way round.
    const head = ctrlFBody()
    const findBox = head.indexOf('isAppSearchField(target)')
    const inputBail = head.search(/if\s*\(\s*tag === 'INPUT' \|\| tag === 'TEXTAREA'\s*\)\s*return\b/)
    expect(findBox).toBeGreaterThan(-1)
    expect(inputBail).toBeGreaterThan(-1)
    expect(findBox).toBeLessThan(inputBail)
  })

  it('closes the view search through an explicit close command, not by focusing', () => {
    // Focusing is now idempotent (repeat Ctrl+F refocuses), so it can no longer
    // double as the toggle's close half. Assert on the CALL, not the bare name:
    // a comment mentioning focusSearchInput must not fail this guard.
    const body = app.slice(app.indexOf('function toggleViewSearch'))
    const head = body.slice(0, body.indexOf('\n}'))
    expect(head).toContain('closeSearchInput()')
    expect(head).not.toMatch(/\.focusSearchInput\?*\.?\(/)
  })
})

describe('Ctrl+F forwarding hops', () => {
  const app = readWebFile('src/App.vue')
  const cm = readWebFile('src/components/file/CodeMirrorViewer.vue')
  const viewer = readWebFile('src/components/file/FileViewer.vue')
  const overlay = readWebFile('src/components/file/FileOverlay.vue')
  const preview = readWebFile('src/components/file/CodeLinkPreview.vue')

  /** Assert `name` is a member of the defineExpose object literal in `src`. */
  function expectExposed(src: string, name: string, label: string) {
    const expose = src.match(/defineExpose\(\{([\s\S]*?)\}\)/)
    expect(expose, `${label}: defineExpose not found`).toBeTruthy()
    expect(
      expose![1],
      `${label}: ${name} is not a member of defineExpose`,
    ).toMatch(new RegExp(`(?:^|,)\\s*${name}\\s*(?::[^,}]*)?(?:,|$)`))
  }

  it('CodeMirrorViewer exposes both open and close', () => {
    expectExposed(cm, 'openSearch', 'CodeMirrorViewer')
    expectExposed(cm, 'closeSearch', 'CodeMirrorViewer')
  })

  it('CodeMirrorViewer opens without toggling and focuses the input', () => {
    const body = cm.slice(cm.indexOf('function openSearch()'))
    const head = body.slice(0, body.indexOf('\nfunction '))
    // The panel must not be dismissed when it is already open.
    expect(head).not.toContain('searchPanelToggle.of(false)')
    expect(head).toContain('focusSearchInput(editor)')
    // CodeMirror's own Mod-f binding must route through the same command,
    // otherwise a focused editor still toggles the panel shut.
    expect(cm).toMatch(/key:\s*'Mod-f',\s*run:\s*\(\)\s*=>\s*\{\s*openSearch\(\)/)
  })

  it('FileViewer and FileOverlay forward closeSearchInput', () => {
    expect(viewer).toMatch(/cmEditorRef\.value\?\.closeSearch\?\.\(\)/)
    expectExposed(viewer, 'closeSearchInput', 'FileViewer')
    expect(overlay).toMatch(/fileViewerRef\.value\?\.closeSearchInput\?\.\(\)/)
    expectExposed(overlay, 'closeSearchInput', 'FileOverlay')
    expect(app).toMatch(/fileOverlayRef\.value\?\.closeSearchInput\(\)/)
  })

  it('the code-preview card refocuses on repeat Ctrl+F instead of toggling', () => {
    const body = preview.slice(preview.indexOf('const openSearch = '))
    const head = body.slice(0, body.indexOf('\nconst toggleSearch'))
    expect(head).toContain('isSearchOpen.value = true')
    expect(head).toContain('focusSearchField()')
    // The shortcut handler must call openSearch, not the toggling entry.
    const keyHandler = preview.slice(preview.indexOf('const onKeyDown'))
    const handlerHead = keyHandler.slice(0, keyHandler.indexOf('\n}'))
    expect(handlerHead).toContain('openSearch()')
    expect(handlerHead).not.toContain('toggleSearch()')
  })
})
