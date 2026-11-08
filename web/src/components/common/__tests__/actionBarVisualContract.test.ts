import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Visual contract for every ActionBar (the one icon-row control).
 *
 * Why this exists: the row's SHAPE, its five colour states and its background
 * were each re-decided per call site. The result was visible in the product:
 * the same toggled button was a round dot in the task header and a rounded
 * square in the meeting header, the "delete" action was grey in the mail
 * reader (its own red lost to the shared rule) and three different reds
 * elsewhere, press feedback was a 0.9 scale in some bars and nothing in the
 * rest, and two headers painted themselves a second background colour.
 *
 * The rules now live once, in web/css/components.css. This spec pins them by
 * reading the sources (jsdom has no CSS engine, so these are source checks —
 * the same pattern as gitHistoryChrome.css.test.ts).
 */

function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

/** Strip comments so a commented-out rule can never satisfy an assertion. */
function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** The declaration block of a rule that starts a line with exactly `.cls {`. */
function baseRule(css: string, cls: string): string | null {
  const m = css.match(new RegExp(`(?:^|\\n)\\s*\\.${cls}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : null
}

/** Flat `selector { body }` pairs. At-rule wrappers are skipped, inner rules kept. */
function rules(css: string): Array<{ selector: string; body: string }> {
  const out: Array<{ selector: string; body: string }> = []
  const re = /([^{}]+)\{([^{}]*)\}/g
  let m: RegExpExecArray | null
  while ((m = re.exec(css)) !== null) out.push({ selector: m[1].trim(), body: m[2] })
  return out
}

/**
 * Only rules that style a BUTTON INSIDE A BAR. Other buttons live in the same
 * files (the composer's round send/stop key, a row's inline delete) and are
 * not part of this contract.
 */
const BAR_BTN =
  /\.(header-btn|header-action-btn|mail-icon-btn|meeting-icon-btn|chat-action-btn|file-header-btn|toolbar-btn|forge-header-btn|create-btn|code-preview-btn)\b/

const buttonRules = (css: string) => rules(css).filter(r => BAR_BTN.test(r.selector))

const componentsCss = readWebFile('css/components.css')
const bare = stripComments(componentsCss)

describe('one shape: rounded square everywhere', () => {
  it('gives both size rows the same radius token', () => {
    for (const cls of ['ab--header', 'ab--bar']) {
      const decls = baseRule(bare, cls)
      expect(decls, `.${cls} must declare its button geometry`).not.toBeNull()
      expect(decls, `.${cls} must use the shared button radius`).toMatch(
        /--ab-btn-radius:\s*var\(--radius-sm\)/,
      )
    }
  })

  it('never falls back to a pill/circle radius', () => {
    // The header row used to be `--radius-full`, which made an icon-only
    // button a dot and a labelled one a pill.
    const start = bare.indexOf('.ab--header {')
    const end = bare.indexOf('/* No transitions while measuring.')
    expect(start, 'the ActionBar block must exist').toBeGreaterThan(-1)
    const actionBarBlock = bare.slice(start, end > start ? end : undefined)
    expect(actionBarBlock).not.toMatch(/--ab-btn-radius:\s*var\(--radius-full\)/)
  })

  it('keeps the chat composer on the same radius', () => {
    const chat = stripComments(readWebFile('src/components/chat/ChatInputBar.vue'))
    const decls = baseRule(chat, 'chat-action-btn')
    expect(decls, '.chat-action-btn must exist').not.toBeNull()
    expect(decls).toMatch(/border-radius:\s*var\(--radius-sm\)/)
  })
})

describe('one press feedback: tint only, never a scale', () => {
  const BARS = [
    'src/components/chat/ChatInputBar.vue',
    'src/components/task/TaskListPage.vue',
    'src/components/notifications/NotificationsPanelContent.vue',
    'src/components/proxy/ProxyPanelContent.vue',
    'src/components/live/MeetingList.vue',
    'src/components/live/MeetingDetail.vue',
    'src/components/file/FileHeader.vue',
    'src/components/file/FileManagerContent.vue',
    'src/components/forge/ForgePanelContent.vue',
    'src/components/git/GitCommitList.vue',
    'src/components/mail/MailReader.vue',
    'src/components/mail/MailFolderBar.vue',
    'src/components/session/SessionListHeader.vue',
  ]

  it('has no press-time scale in any bar', () => {
    const offenders: string[] = []
    for (const file of BARS) {
      const src = stripComments(readWebFile(file))
      // `:active` (pressed), not `.active` (toggled on); buttons only, so a
      // row's delete affordance elsewhere in the file is not swept in.
      for (const r of buttonRules(src)) {
        if (!/:active/.test(r.selector)) continue
        if (/transform:\s*scale\(/.test(r.body)) offenders.push(`${file}: ${r.selector}`)
      }
    }
    expect(offenders, 'press feedback must be the shared accent tint, not a scale').toEqual([])
  })

  it('keeps the shared press recipe at a 14% accent tint', () => {
    const rule = bare.match(
      /\.ab--header button:not\(\[data-ab-more\]\):active:not\(:disabled\),\s*\.ab--bar button:not\(\[data-ab-more\]\):active:not\(:disabled\)\s*\{([^}]*)\}/,
    )
    expect(rule, 'the shared pressed rule must exist').not.toBeNull()
    expect(rule![1]).toMatch(/color-mix\(in srgb, var\(--accent-color\) 14%/)
  })

  it('keeps the chat composer on that same recipe', () => {
    // size="custom", so the shared rule does not reach it — the values are
    // restated there and must not drift.
    const chat = stripComments(readWebFile('src/components/chat/ChatInputBar.vue'))
    const decls = baseRule(chat, 'chat-action-btn:active')
    expect(decls, '.chat-action-btn:active must exist').not.toBeNull()
    expect(decls).toMatch(/color-mix\(in srgb, var\(--accent-color\) 14%/)
    expect(decls).not.toMatch(/transform:/)
    expect(decls).not.toMatch(/(^|;)\s*color:/)
  })
})

describe('one danger colour', () => {
  it('declares the danger recipe once, off --color-red', () => {
    const decls = baseRule(bare, 'is-danger')
    expect(decls, '.is-danger must exist in the global sheet').not.toBeNull()
    expect(decls).toMatch(/color:\s*var\(--color-red\)/)
    expect(bare).toMatch(/\.ab button\.is-danger:not\(\[data-ab-more\]\):hover:not\(:disabled\)/)
  })

  it('leaves no hard-coded red in a bar', () => {
    const FILES = [
      'src/components/file/FileManagerContent.vue',
      'src/components/file/FileHeader.vue',
      'src/components/mail/MailReader.vue',
    ]
    const offenders: string[] = []
    for (const file of FILES) {
      const src = stripComments(readWebFile(file))
      // #ef4444 / #dc2626 / #fef2f2 / #2d1b1b were the three drifting reds.
      for (const r of buttonRules(src)) {
        const m = r.body.match(/#(ef4444|dc2626|fef2f2|2d1b1b)\b/i)
        if (m) offenders.push(`${file} ${r.selector}: ${m[0]}`)
      }
    }
    expect(offenders, 'red must come from --color-red, not a literal hex').toEqual([])
  })

  it('uses is-danger on the destructive buttons', () => {
    const expectDanger = (file: string, marker: string) => {
      const src = stripComments(readWebFile(file))
      expect(src, `${file} must mark the destructive button as danger`).toContain(marker)
    }
    expectDanger('src/components/mail/MailReader.vue', 'mail-icon-btn is-danger')
    expectDanger('src/components/file/FileManagerContent.vue', 'ms-toolbar-danger is-danger')
  })
})

describe('one bar background: follow the surface', () => {
  it('does not paint a bar with a second surface colour', () => {
    const BARS: Array<[string, string]> = [
      ['src/components/file/FileHeader.vue', 'file-header-bar'],
      ['src/assets/code-link-preview.css', 'code-preview-meta'],
    ]
    for (const [file, cls] of BARS) {
      const decls = baseRule(stripComments(readWebFile(file)), cls)
      expect(decls, `.${cls} must exist in ${file}`).not.toBeNull()
      expect(decls, `.${cls} must inherit the surface, not paint --bg-secondary`).not.toMatch(
        /background:\s*var\(--bg-secondary/,
      )
    }
  })
})

describe('no dead overrides left behind', () => {
  it('keeps the task header button free of geometry/colour it cannot win', () => {
    const src = stripComments(readWebFile('src/components/task/TaskListPage.vue'))
    const decls = baseRule(src, 'header-btn')
    expect(decls, '.header-btn must exist').not.toBeNull()
    for (const prop of ['width', 'height', 'border-radius', 'color', 'background']) {
      expect(decls, `.header-btn must not restate ${prop} (the shared rule wins)`).not.toMatch(
        new RegExp(`(^|;)\\s*${prop}:`),
      )
    }
  })

  it('hands the code preview toggles to the shared active class', () => {
    const src = stripComments(readWebFile('src/components/file/CodePreviewToolbar.vue'))
    expect(src).not.toMatch(/is-active/)
    expect(src).toMatch(/active:\s*isWordWrap/)
  })
})
