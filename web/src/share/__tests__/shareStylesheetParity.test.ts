/**
 * The share SPA and the main app render the SAME chat components
 * (ChatMessageItem, ContentBlocks, ToolDetailDrawer, CopyButton, SummaryToggle…),
 * so they need the same global component stylesheets. They do NOT get them the
 * same way:
 *
 *   - the app gets `css/*` from <link> tags in index.html, and the
 *     `assets/*.css` component sheets from imports in App.vue;
 *   - share.html has NO <link> tags at all — every sheet reaches the share entry
 *     through JS imports in share/main.ts.
 *
 * That asymmetry is the bug: a sheet added to App.vue (or index.html) and not to
 * share/main.ts compiles fine, ships fine, and leaves the component rendering
 * with raw UA styling. It is invisible to type checks and to any component test
 * (jsdom does not apply stylesheets), and it looks *almost* right in a
 * screenshot — the message-action row was the real instance: every button under
 * a shared message rendered as a default 2px-outset `<button>` because
 * `.chat-action-btn` (defined only in assets/chat-actions.css) never loaded.
 *
 * These tests pin the contract, not a fixed list, so adding a component sheet to
 * the app keeps working — as long as the share entry is updated too.
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/** Repo root: this file lives at web/src/share/__tests__/. */
const ROOT = join(__dirname, '..', '..', '..', '..')

function read(rel: string): string {
  return readFileSync(join(ROOT, rel), 'utf8')
}

/**
 * Normalise an import specifier to a comparable id.
 *
 * Three shapes arrive here:
 *   - relative (`../assets/chat-actions.css`) — resolve against the importer;
 *   - root-absolute (`/css/base.css`, from an index.html <link>) — the server
 *     serves `web/` at the root, so these map to `web/css/base.css`;
 *   - bare package (`highlight.js/styles/github.css`) — NOT a repo path. Joining
 *     it onto the importing directory would invent `web/src/highlight.js/...`,
 *     so it is kept verbatim: the same package specifier in both entries means
 *     the same stylesheet.
 */
function normalise(spec: string, fromDir: string): string {
  if (spec.startsWith('/')) spec = spec.slice(1)
  else if (!spec.startsWith('.')) return spec
  const base = spec.startsWith('web/') ? '' : fromDir
  const parts = join(base, spec).split(/[\\/]/)
  const out: string[] = []
  for (const p of parts) {
    if (p === '.' || p === '') continue
    if (p === '..') out.pop()
    else out.push(p)
  }
  return out.join('/')
}

/** Every `import '...css'` in a source file, as repo-relative paths. */
function cssImportsOf(relPath: string): string[] {
  const src = read(relPath)
  const dir = relPath.split('/').slice(0, -1).join('/')
  const found: string[] = []
  for (const m of src.matchAll(/^\s*import\s+['"]([^'"]+\.css)['"]/gm)) {
    found.push(normalise(m[1], dir))
  }
  return found
}

/** Every `<link rel="stylesheet" href="...">` in an HTML entry. */
function linkedStylesheets(relPath: string): string[] {
  const src = read(relPath)
  const found: string[] = []
  for (const m of src.matchAll(/<link[^>]+rel=["']stylesheet["'][^>]*>/g)) {
    const href = /href=["']([^"']+)["']/.exec(m[0])?.[1]
    // Public-root hrefs ('/css/base.css') map to web/css/base.css.
    if (href) found.push(normalise(href, 'web'))
  }
  return found
}

const appSheets = new Set([...cssImportsOf('web/src/App.vue'), ...linkedStylesheets('web/index.html')])
const shareSheets = new Set(cssImportsOf('web/src/share/main.ts'))

/**
 * Sheets that are deliberately app-only.
 *
 * Each entry needs a reason: "the share page does not render this". Anything not
 * listed here and not shared is a bug — the component would render unstyle
 * there.
 */
const APP_ONLY = new Map<string, string>([
  // The share SPA has no file viewer / editor, so CodeMirror and the changed-line
  // overlay never render. (Diff ROWS — css/diff-rows.css — ARE shared: tool cards
  // show Edit/Write diffs.)
  ['web/src/assets/code-viewer.css', 'file viewer / CodeMirror only'],
  // Every refresh/rescan button lives in the app shell (sessions, tasks, skills).
  // The share page has none.
  ['web/src/assets/refresh-spin.css', 'no refresh buttons on the share page'],
])

/**
 * Sheets only the share entry needs.
 *
 * `share-chrome.css` is the share shell (topbar / body / TOC rail). The main app
 * does not render that shell — the SPA in web/src/share/ does, and the markdown
 * HTML export inlines the same file.
 */
const SHARE_ONLY = new Map<string, string>([
  ['web/css/share-chrome.css', 'share shell only; the app has its own layout'],
])

describe('share SPA loads the same component stylesheets as the app', () => {
  it('imports every shared component sheet', () => {
    const missing = [...appSheets].filter((s) => !shareSheets.has(s) && !APP_ONLY.has(s))
    expect(missing, `share/main.ts must import these (or APP_ONLY must explain why not): ${missing.join(', ')}`).toEqual([])
  })

  it('does not import sheets the app does not load', () => {
    // Guards the reverse drift: a sheet the share page invents would not exist in
    // the app build and would be dead weight (or a duplicate definition).
    const extra = [...shareSheets].filter((s) => !appSheets.has(s) && !SHARE_ONLY.has(s))
    expect(extra, `share/main.ts imports sheets the app never loads: ${extra.join(', ')}`).toEqual([])
  })

  it('lists only real app sheets in APP_ONLY', () => {
    // A stale exemption silently re-opens the hole it was written to document:
    // if the sheet is renamed or removed, the entry no longer exempts anything
    // but still looks like coverage.
    const stale = [...APP_ONLY.keys()].filter((s) => !appSheets.has(s))
    expect(stale, `APP_ONLY names sheets the app does not load: ${stale.join(', ')}`).toEqual([])
  })

  it('lists only real share sheets in SHARE_ONLY', () => {
    const stale = [...SHARE_ONLY.keys()].filter((s) => !shareSheets.has(s))
    expect(stale, `SHARE_ONLY names sheets the share entry does not import: ${stale.join(', ')}`).toEqual([])
  })

  it('keeps the sheets whose absence caused visible defects', () => {
    // Named explicitly (rather than relying on the set comparison above) so the
    // regression that motivated this file cannot come back through a wholesale
    // APP_ONLY exemption.
    for (const sheet of [
      'web/src/assets/chat-actions.css', // .chat-action-btn — meta bar buttons
      'web/src/assets/annotation-buttons.css', // .chat-file-path / .chat-open-btn
      'web/css/copy-button.css', // .copy-btn.is-copied feedback
      'highlight.js/styles/github.css', // syntax colours (light)
      'highlight.js/styles/github-dark.css', // syntax colours (dark)
      'web/src/assets/mono-icon-colors.css', // mono agent avatars on dark themes
    ]) {
      expect(shareSheets.has(sheet), `share/main.ts must import ${sheet}`).toBe(true)
    }
  })
})

describe('the classes those sheets define are actually used by shared components', () => {
  // The parity check above is necessary but not sufficient: it would also pass
  // if both entries loaded a sheet nobody needs. These assertions tie the
  // stylesheet to the markup, so deleting a sheet's only consumer also surfaces
  // here instead of leaving dead CSS behind.
  const cases: Array<[string, string, string]> = [
    ['web/src/assets/chat-actions.css', '.chat-action-btn', 'web/src/components/common/SummaryToggle.vue'],
    ['web/src/assets/annotation-buttons.css', '.chat-file-path', 'web/src/components/chat/ContentBlocks.vue'],
    ['web/css/copy-button.css', '.copy-btn', 'web/src/components/common/CopyButton.vue'],
  ]

  for (const [sheet, selector, consumer] of cases) {
    it(`${sheet} defines ${selector}, which ${consumer} renders`, () => {
      expect(read(sheet)).toContain(selector)
      expect(read(consumer)).toContain(selector.replace('.', ''))
    })
  }
})
