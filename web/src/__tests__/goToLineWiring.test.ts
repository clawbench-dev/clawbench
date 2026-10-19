import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring guard for Ctrl+G "go to line" (issue #547).
 *
 * App.vue has no component test (its only tests read it as source text), so the
 * document-level binding is invisible to the behaviour suites: the CodeMirror
 * keymap covers the focused editor, but a freshly opened file has no focus
 * there. Without this handler Ctrl+G does nothing until the user clicks into
 * the code — which is exactly the reported bug.
 *
 * These assertions pin the parts that a refactor can silently drop: the
 * listener registration, its removal on unmount, and the guard that keeps the
 * key away from other surfaces.
 */
describe('Ctrl+G go-to-line wiring in App.vue', () => {
  const app = readWebFile('src/App.vue')

  it('registers the Ctrl+G handler on mount', () => {
    expect(app).toMatch(/document\.addEventListener\('keydown', handleCtrlG\)/)
  })

  it('removes the handler on unmount', () => {
    // A leaked listener would prompt for a line even after the view closed.
    expect(app).toMatch(/document\.removeEventListener\('keydown', handleCtrlG\)/)
  })

  it('only acts on Ctrl/Cmd+G, without other modifiers', () => {
    const body = app.slice(app.indexOf('function handleCtrlG'))
    const head = body.slice(0, body.indexOf('\n}'))
    expect(head).toMatch(/e\.ctrlKey \|\| e\.metaKey/)
    // Shift/Alt variants must not be swallowed: Ctrl+Shift+G and Alt+G belong to
    // the browser / OS.
    expect(head).toMatch(/e\.shiftKey/)
    expect(head).toMatch(/e\.altKey/)
    expect(head).toMatch(/toLowerCase\(\) !== 'g'/)
  })

  it('routes through FileOverlay.openGoToLine and preventDefaults only when handled', () => {
    const body = app.slice(app.indexOf('function handleCtrlG'))
    const head = body.slice(0, body.indexOf('\n}'))
    expect(head).toContain('fileOverlayRef.value?.openGoToLine?.()')
    // preventDefault must be conditional: rendered previews and media have no
    // line numbers, and the browser's own find-next should keep working there.
    expect(head).toMatch(/if \(!opened\) return\s*\n\s*e\.preventDefault\(\)/)
  })

  it('ignores the shortcut while a dialog or the terminal has focus', () => {
    const body = app.slice(app.indexOf('function handleCtrlG'))
    const head = body.slice(0, body.indexOf('\n}'))
    expect(head).toContain("tag === 'INPUT' || tag === 'TEXTAREA'")
    expect(head).toContain('isContentEditable')
    expect(head).toContain("'.terminal-panel'")
    expect(head).toContain('_dlg.state.value.visible')
  })

  it('defers to CodeMirror when the editor already handled the key', () => {
    // The editor contentDOM stays contenteditable="true" even in read-only mode
    // (CodeMirror uses EditorState.readOnly), so an isContentEditable guard
    // cannot dedupe: the editor's own Mod-g binding and this handler would both
    // open a prompt. `defaultPrevented` is the signal CodeMirror sets.
    const body = app.slice(app.indexOf('function handleCtrlG'))
    const head = body.slice(0, body.indexOf('\n}'))
    expect(head).toMatch(/if \(e\.defaultPrevented\) return/)
  })

  it('requires the file view to be the active panel', () => {
    const body = app.slice(app.indexOf('function handleCtrlG'))
    const head = body.slice(0, body.indexOf('\n}'))
    // Wide screen: the left pane must be active too, otherwise Ctrl+G in the
    // chat pane would hijack the browser shortcut.
    expect(head).toContain('PANE_LEFT')
    expect(head).toContain("panelIsActive('view')")
  })
})

/**
 * The two forwarding hops cannot be covered behaviourally:
 *   - CodeMirrorViewer is mounted through `defineAsyncComponent`, so in jsdom
 *     the parent's ref holds the AsyncComponentLoader placeholder, not the real
 *     instance (Object.keys(ref) === []).
 *   - The FileViewer tests stub CodeMirrorViewer, and a stub's methods are
 *     always visible on the instance.
 *
 * That combination is exactly how the first cut shipped with a dead App-level
 * shortcut: `openGoToLine` was defined but missing from `defineExpose`, so the
 * call resolved to undefined and Ctrl+G silently did nothing right after
 * opening a file. These guards read the real sources.
 */

/**
 * Assert `name` is a MEMBER of the defineExpose object literal in `src`.
 *
 * A plain `toMatch(/\bname\b/)` over the literal is not enough: a comment
 * mentioning the name, or `name: undefined`, would pass while the export stays
 * dead. Requiring a `,`/`{` before and a `,`/`}` after rejects both, and the
 * `undefined` check closes the remaining hole.
 */
function expectExposed(src: string, name: string, label: string) {
  const expose = src.match(/defineExpose\(\{([\s\S]*?)\}\)/)
  expect(expose, `${label}: defineExpose not found`).toBeTruthy()
  const body = expose![1]
  expect(
    body,
    `${label}: ${name} is not a member of defineExpose (a mention in a comment does not export it)`,
  ).toMatch(new RegExp(`(?:^|,)\\s*${name}\\s*(?::[^,}]*)?(?:,|$)`))
  expect(body, `${label}: ${name} must not be exported as undefined`).not.toMatch(
    new RegExp(`${name}\\s*:\\s*undefined`),
  )
}

describe('Ctrl+G forwarding hops', () => {
  const cm = readWebFile('src/components/file/CodeMirrorViewer.vue')
  const viewer = readWebFile('src/components/file/FileViewer.vue')
  const overlay = readWebFile('src/components/file/FileOverlay.vue')

  it('CodeMirrorViewer exposes openGoToLine', () => {
    // <script setup> exposes ONLY what defineExpose names.
    expectExposed(cm, 'openGoToLine', 'CodeMirrorViewer')
  })

  it('CodeMirrorViewer binds Mod-g to openGoToLine and no longer to findNext', () => {
    // Both halves matter: dropping the binding kills the focused path, and
    // re-adding Mod-g to the search keymap would shadow go-to-line.
    expect(cm).toMatch(/\{\s*key:\s*'Mod-g',\s*run:\s*openGoToLine/)
    expect(cm).not.toMatch(/key:\s*'Mod-g',\s*run:\s*\(\)\s*=>\s*findNextSafe/)
    // Match navigation moved to F3, which @codemirror/search also uses.
    expect(cm).toMatch(/key:\s*'F3',\s*run:\s*\(\)\s*=>\s*findNextSafe/)
  })

  it('FileViewer forwards openGoToLine to the editor ref and exposes it', () => {
    // FileViewer renders the viewer from four mutually exclusive branches
    // (source/raw, HTML, markdown raw, editing). EVERY branch needs the ref —
    // one missing branch means Ctrl+G dies on that file type — so compare
    // counts rather than merely "the ref appears somewhere".
    const editors = viewer.match(/<CodeMirrorViewer/g)?.length ?? 0
    const refs = viewer.match(/ref="cmEditorRef"/g)?.length ?? 0
    expect(editors).toBeGreaterThan(0)
    expect(refs).toBe(editors)
    expect(viewer).toMatch(/cmEditorRef\.value\?\.openGoToLine\?\.\(\)/)
    expectExposed(viewer, 'openGoToLine', 'FileViewer')
  })

  it('FileOverlay forwards openGoToLine and exposes it', () => {
    expect(overlay).toMatch(/ref="fileViewerRef"/)
    expect(overlay).toMatch(/fileViewerRef\.value\?\.openGoToLine\?\.\(\)/)
    expectExposed(overlay, 'openGoToLine', 'FileOverlay')
  })

  it('App holds the overlay ref it calls openGoToLine on', () => {
    expect(readWebFile('src/App.vue')).toMatch(/ref="fileOverlayRef"/)
  })
})
