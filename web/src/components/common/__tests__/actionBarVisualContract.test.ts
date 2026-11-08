import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { readdirSync, existsSync } from 'node:fs'

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

describe('one toolbar row height', () => {
  const vars = stripComments(readWebFile('css/variables.css'))

  it('declares the row tokens once', () => {
    expect(vars, 'button size token').toMatch(/--toolbar-btn-size:\s*calc\(26 \* var\(--dp\)\)/)
    expect(vars, 'row height token').toMatch(/--toolbar-row-height:\s*calc\(32 \* var\(--dp\)\)/)
  })

  it('has the bar-size ActionBar take its button size from the token', () => {
    const decls = baseRule(bare, 'ab--bar')
    expect(decls).toMatch(/--ab-btn-size:\s*var\(--toolbar-btn-size\)/)
  })

  it('sizes every content tool strip from the row token', () => {
    // Regression: the file manager swaps its browse toolbar for the
    // multi-select one in the same slot, and the row used to be sized by its
    // tallest child — opening multi-select shrank the whole toolbar by 8px.
    const ROWS: Array<[string, string]> = [
      ['src/components/file/FileManagerContent.vue', 'dir-toolbar'],
      ['src/assets/code-link-preview.css', 'code-preview-meta'],
      ['src/components/file/DirPreviewBody.vue', 'dir-preview-meta'],
      ['src/components/mail/MailReader.vue', 'mail-reader__actions'],
    ]
    for (const [file, cls] of ROWS) {
      const decls = baseRule(stripComments(readWebFile(file)), cls)
      expect(decls, `.${cls} must exist in ${file}`).not.toBeNull()
      expect(decls, `.${cls} must take its height from --toolbar-row-height`).toMatch(
        /height:\s*var\(--toolbar-row-height\)/,
      )
    }
  })

  it('gives the multi-select buttons the same box as the ActionBar ones', () => {
    const src = stripComments(readWebFile('src/components/file/FileManagerContent.vue'))
    expect(src).toMatch(/\.ms-toolbar-btns \.toolbar-btn \{[^}]*height:\s*var\(--toolbar-btn-size\)/)
    expect(baseRule(src, 'ms-select-all-btn')).toMatch(/height:\s*var\(--toolbar-btn-size\)/)
  })

  /**
   * The bar owns the space BETWEEN its buttons (`gap`), so no button inside one
   * may also carry a left/right margin — the two stack.
   *
   * This is not hypothetical: the session-list header kept a
   * `margin-left: var(--space-3)` from before the bar took over spacing, and the
   * row measured 14px between buttons instead of 8. It read as WORSE once the
   * labels appeared, because a labelled button also gains 8px of side padding,
   * so the gap between two words was 14 + 16 = 30px.
   *
   * `margin-left: auto` is allowed and means something different (push to an
   * edge); so is a negative margin, which is an optical nudge, not spacing.
   */
  it('never lets a button inside a bar add its own horizontal margin', () => {
    const offenders: string[] = []
    const walk = (dir: string) => {
      for (const entry of readdirSync(dir, { withFileTypes: true })) {
        const full = join(dir, entry.name)
        if (entry.isDirectory()) {
          if (entry.name === '__tests__' || entry.name === 'node_modules') continue
          walk(full)
          continue
        }
        if (!entry.name.endsWith('.vue')) continue
        const src = stripComments(readFileSync(full, 'utf8'))
        if (!/<ActionBar\b/.test(src)) continue

        // Classes that appear on a tag INSIDE an <ActionBar>…</ActionBar> block.
        // Scoping to the bar is the whole point: this file's other buttons (a
        // search-hit row, a commit's quote button, the terminal key strip) sit
        // outside it and own their own spacing legitimately.
        const inBar = new Set<string>()
        for (const bar of src.matchAll(/<ActionBar\b[\s\S]*?<\/ActionBar>/g)) {
          for (const tag of bar[0].matchAll(/class="([^"]*)"/g)) {
            for (const cls of tag[1].split(/\s+/)) if (cls) inBar.add(cls)
          }
        }
        if (inBar.size === 0) continue

        for (const m of src.matchAll(/(?:^|\n)\s*([.\w][^{}]*?)\{([^}]*)\}/g)) {
          const selector = m[1].trim()
          const body = m[2]
          // Every class named in the selector must be one that lives in the bar.
          const classes = [...selector.matchAll(/\.([\w-]+)/g)].map((c) => c[1])
          if (classes.length === 0 || !classes.every((c) => inBar.has(c))) continue
          const decl = body.match(/margin-(left|right)\s*:\s*([^;]+)/)
          if (!decl) continue
          const value = decl[2].trim()
          // auto = alignment; a negative value = optical correction.
          if (/^auto$/i.test(value) || /^-|calc\(\s*-/.test(value)) continue
          // A margin that reserves room for a divider the rule draws itself is
          // separation, not spacing: the terminal key strip uses it to place a
          // 1px `::before` line between key groups.
          if (src.includes(`${selector}::before`)) continue
          offenders.push(`${entry.name}  ${selector}  -> margin-${decl[1]}: ${value}`)
        }
      }
    }
    // Same two candidate roots as readWebFile: cwd is `web/` for a bare vitest
    // run and the repo root under scripts/vitest-run.sh.
    const root = [process.cwd(), join(process.cwd(), 'web')]
      .map((base) => join(base, 'src', 'components'))
      .find((dir) => existsSync(dir))
    expect(root, 'web/src/components not found from cwd: ' + process.cwd()).toBeTruthy()
    walk(root!)
    expect(
      offenders,
      'The ActionBar already spaces its buttons with `gap`. A button that adds its own ' +
      'left/right margin doubles that space (14px instead of 8px) and looks worse once ' +
      'labels widen the buttons. Remove the margin; use `auto` only to push to an edge.',
    ).toEqual([])
  })
})

describe('the bar holds icon buttons only', () => {
  it('keeps context switchers beside the bar, not inside it', () => {
    // ActionBar's three behaviours (labels, folding into "more", scrolling) are
    // all built around icon buttons. A text control inside one is measured as a
    // fixed item, squashed by the icon-button box, and makes the fold decision
    // depend on how long its text happens to be. The git history toolbar's
    // branch picker is the reference: the picker sits outside the bar.
    const forge = readWebFile('src/components/forge/ForgePanelContent.vue')
    expect(forge).toContain('<div class="forge-header">')
    expect(forge).toContain('class="forge-repo-badge"')
    expect(forge).toMatch(/<ActionBar class="forge-header-actions"/)

    const mail = readWebFile('src/components/mail/MailFolderBar.vue')
    expect(mail).toContain('<div class="mail-bar__top">')
    expect(mail).toMatch(/<ActionBar class="mail-bar__actions"/)
  })

  it('has no escape hatch for text controls left over', () => {
    expect(bare).not.toContain('ab-auto')
    expect(readWebFile('src/components/forge/ForgePanelContent.vue')).not.toContain('ab-auto')
    expect(readWebFile('src/components/mail/MailFolderBar.vue')).not.toContain('ab-auto')
  })
})

/* --------------------------------------------------------------------------
   Structural audit: every ActionBar in the app.

   The bar manages icon buttons — labels, folding into "more", scrolling — and
   pins the odd inert text item (a title, a breadcrumb, a count) to its natural
   width. Anything else inside it is a bug:
     - a text control gets squashed by the icon-button box and makes the fold
       decision depend on its own content;
     - a clickable fixed item is a control the bar does not manage;
     - a wrapper element hides its buttons from the bar entirely (they never
       fold and are clipped on a narrow row).
   `<template>` / `<Teleport>` / `<slot>` render no box of their own, so their
   contents still count as row children.
   -------------------------------------------------------------------------- */

/** Files allowed to keep a wrapper element around buttons, with the reason. */
const WRAPPER_EXEMPT: Record<string, string> = {
  'src/components/terminal/TerminalPanelContent.vue':
    'virtual keyboard: keys are grouped into .key-group for the layout (dividers between groups), not to manage the row',
}

const BUTTONISH = /^(button|RefreshButton|CopyButton)$/
const TRANSPARENT = /^(template|slot)$/
const CONTROL = /^(MenuSelect|input|textarea|select)$/
const VOID_TAGS = new Set(['input', 'br', 'hr', 'img', 'path', 'circle', 'rect', 'line', 'polyline', 'polygon'])

interface Tag { name: string; attrs: string; closing: boolean; selfClosing: boolean }

function parseTags(block: string): Tag[] {
  const out: Tag[] = []
  const re = /<(\/?)([A-Za-z][\w.-]*)((?:[^>"']|"[^"]*"|'[^']*')*?)(\/?)>/g
  let m: RegExpExecArray | null
  while ((m = re.exec(block)) !== null) {
    out.push({ closing: m[1] === '/', name: m[2], attrs: m[3], selfClosing: m[4] === '/' })
  }
  return out
}

/** Problems inside one `<ActionBar>…</ActionBar>` block. */
function barProblems(block: string, file: string): string[] {
  const problems: string[] = []
  const inner = block
    .replace(/^<(?:ActionBar|action-bar)[^>]*>/, '')
    .replace(/<\/(?:ActionBar|action-bar)>$/, '')
  const stack: boolean[] = [] // true = renders no box of its own
  let depth = 0
  let teleported = 0 // <Teleport> content is not in the row at all

  for (const tag of parseTags(inner)) {
    if (teleported > 0) {
      // Only balance the Teleport subtree; nothing inside it is a row child.
      if (!tag.selfClosing && !VOID_TAGS.has(tag.name)) teleported += tag.closing ? -1 : 1
      continue
    }
    if (!tag.closing && tag.name === 'Teleport') {
      teleported = 1
      continue
    }
    if (tag.closing) {
      if (stack.pop() === false) depth--
      continue
    }
    const transparent = TRANSPARENT.test(tag.name)
    const insideTransparent = stack.some(Boolean)

    if (!insideTransparent) {
      if (CONTROL.test(tag.name)) {
        problems.push(`${file}: <${tag.name}> is a control — it belongs beside the bar, not in it`)
      }
      if (depth === 0) {
        const interactive =
          !BUTTONISH.test(tag.name) &&
          (tag.name === 'a' || /@click|@pointerdown|role="button"/.test(tag.attrs))
        if (interactive) {
          problems.push(`${file}: <${tag.name}> is clickable — only icon buttons may be interactive inside the bar`)
        }
      } else if (BUTTONISH.test(tag.name) && !WRAPPER_EXEMPT[file]) {
        problems.push(`${file}: <${tag.name}> is nested inside another element — the bar only manages its direct children, so it never folds`)
      }
    }

    if (VOID_TAGS.has(tag.name) || tag.selfClosing) continue
    stack.push(transparent)
    if (!transparent) depth++
  }
  return problems
}

function vueFilesWithBars(): string[] {
  const root = process.cwd().endsWith('/web') ? process.cwd() : join(process.cwd(), 'web')
  const out: string[] = []
  const walk = (rel: string) => {
    for (const entry of readdirSync(join(root, rel), { withFileTypes: true })) {
      const next = rel ? `${rel}/${entry.name}` : entry.name
      if (entry.isDirectory()) {
        if (entry.name === '__tests__' || entry.name === 'node_modules') continue
        walk(next)
      } else if (entry.name.endsWith('.vue')) {
        if (/<(ActionBar|action-bar)\b/.test(readFileSync(join(root, next), 'utf8'))) out.push(next)
      }
    }
  }
  walk('src')
  return out
}

describe('every ActionBar holds icon buttons and inert text only', () => {
  const files = vueFilesWithBars()

  it('finds the bars to audit', () => {
    expect(files.length, 'the audit must actually walk the source tree').toBeGreaterThan(10)
  })

  it('has no control, clickable fixed item or hidden button in any bar', () => {
    const problems: string[] = []
    for (const file of files) {
      const src = readWebFile(file).replace(/<!--[\s\S]*?-->/g, '')
      for (const block of src.match(/<(?:ActionBar|action-bar)\b[\s\S]*?<\/(?:ActionBar|action-bar)>/g) ?? []) {
        problems.push(...barProblems(block, file))
      }
    }
    expect(problems, problems.join('\n')).toEqual([])
  })
})

describe('a scroll row can always reach its content', () => {
  it('starts at the scroll origin, ignoring align', () => {
    // Measured in Chromium: `justify-content: flex-end` on a scroll container
    // puts the overflow on the START side, where the scrollport cannot reach
    // it — the element even reports scrollWidth === clientWidth, so the bar
    // concludes "no overflow" and its first buttons are clipped for good.
    const src = stripComments(readWebFile('src/components/common/ActionBar.vue'))
    const block = src.match(/(?:^|\n)\s*\.ab--scroll\s*\{([^}]*)\}/)
    expect(block, '.ab--scroll must exist').not.toBeNull()
    expect(block![1]).toMatch(/justify-content:\s*flex-start/)
  })
})

describe('a bar cannot be squeezed below one button + the more button', () => {
  it('declares the floor on both size rows', () => {
    // Below this width the row shows nothing at all: the "more" button itself
    // is clipped, and the buttons it would have listed are unreachable.
    for (const cls of ['ab--header', 'ab--bar']) {
      expect(baseRule(bare, cls), `.${cls} must declare a floor`).toMatch(
        /min-width:\s*calc\(2 \* var\(--ab-btn-size\) \+ 8 \* var\(--dp\)\)/,
      )
    }
  })
})

describe('a bar that sits beside a title can still grow', () => {
  it('gives the task header icon row the leftover width', () => {
    // `flex: 0 1 auto` pins the row to its content width, so the labels can
    // never be turned on (the bar measures exactly what it needs) — a silent
    // regression that no jsdom test can see.
    const src = stripComments(readWebFile('src/components/task/TaskListPage.vue'))
    expect(baseRule(src, 'list-header-actions')).toMatch(/flex:\s*1 1 auto/)
  })

  it('gives the forge header icon row the leftover width', () => {
    const src = stripComments(readWebFile('src/components/forge/ForgePanelContent.vue'))
    expect(baseRule(src, 'forge-header-actions')).toMatch(/flex:\s*1 1 auto/)
  })

  it('gives the mail bar icon row the leftover width', () => {
    const src = stripComments(readWebFile('src/components/mail/MailFolderBar.vue'))
    expect(baseRule(src, 'mail-bar__actions')).toMatch(/flex:\s*1 1 auto/)
  })
})

/* --------------------------------------------------------------------------
   One primary action per row.

   The create/new entry is the one button a header may paint in the accent
   colour. It used to exist in exactly one panel (the task list), so the same
   action read as "the main thing to do here" in one tab and as an ordinary
   utility one tab over. The marking is now applied to every row that has a
   single create entry, and to nothing else — two accents in one row would put
   us back where we started, only in the other direction.
   -------------------------------------------------------------------------- */
describe('one primary action per row', () => {
  it('declares the primary recipe once, off --accent-color', () => {
    const rule = bare.match(
      /\.ab--header button\.is-primary:not\(:disabled\),\s*\.ab--bar button\.is-primary:not\(:disabled\)\s*\{([^}]*)\}/,
    )
    expect(rule, 'the shared primary rule must exist').not.toBeNull()
    expect(rule![1]).toMatch(/color:\s*var\(--accent-color\)/)
  })

  it('marks the create entry of every header row that has one', () => {
    // Source checks: jsdom has no CSS engine, and the marking is what these
    // assertions can actually see. The colour itself is pinned by the rule
    // above, so a row that carries the class is a row that renders it.
    const MARKED: Array<[string, string, string]> = [
      ['src/components/task/TaskListPage.vue', 'class="create-btn is-primary"', '新建任务'],
      ['src/components/proxy/ProxyPanelContent.vue', 'class="create-btn is-primary"', '添加端口'],
      ['src/components/session/SessionListHeader.vue', 'class="header-action-btn is-primary"', '新会话'],
      ['src/components/mail/MailFolderBar.vue', 'class="mail-icon-btn is-primary"', '写邮件'],
    ]
    for (const [file, marker, what] of MARKED) {
      expect(readWebFile(file), `${file}: the ${what} entry must be the primary action`).toContain(marker)
    }
  })

  it('never puts two of them in the same bar', () => {
    const offenders: string[] = []
    for (const file of vueFilesWithBars()) {
      // Comments must not count: a bar's own explanation of why the marking
      // exists names it more than once.
      const src = readWebFile(file).replace(/<!--[\s\S]*?-->/g, '')
      for (const block of src.match(/<(?:ActionBar|action-bar)\b[\s\S]*?<\/(?:ActionBar|action-bar)>/g) ?? []) {
        const count = (block.match(/\bis-primary\b/g) ?? []).length
        if (count > 1) offenders.push(`${file}: ${count} primary buttons in one bar`)
      }
    }
    expect(offenders, 'a row has ONE stand-out action; two accents cancel out').toEqual([])
  })

  it('leaves the utility buttons of a marked row neutral', () => {
    // The session header's search / refresh / pin must stay plain: the accent
    // is what makes the "+" readable as "start here".
    const src = readWebFile('src/components/session/SessionListHeader.vue')
    expect(src).not.toMatch(/class="header-action-btn is-primary"[^>]*data-action="search"/)
    expect(src).not.toMatch(/class="header-action-btn is-primary"[^>]*data-action="refresh"/)
  })
})
