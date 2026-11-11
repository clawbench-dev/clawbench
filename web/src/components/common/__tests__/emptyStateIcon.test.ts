/**
 * One empty state, one glyph size, one shape.
 *
 * The app used to draw "nothing here yet" ~20 times: nine glyph sizes (24 → 56),
 * three opacities, ten container class names, and seven blocks that hugged the
 * top of their pane because they had padding but no height to centre in. The
 * glyph size was unified first (tokens + a global rule); this file now also pins
 * the *shape*: every tab and sub-tab renders the shared `EmptyState`.
 *
 * jsdom has no layout engine, so the contract is read off the sources: a
 * regression here is "someone hand-rolled another empty block" or "someone
 * sized the glyph again", neither of which a rendered test would notice.
 */
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/** `web/` — this file lives at web/src/components/common/__tests__. */
const WEB = join(__dirname, '..', '..', '..', '..')

function read(rel: string): string {
  return readFileSync(join(WEB, rel), 'utf8')
}

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** Declarations of the first rule whose selector list is exactly `selector`. */
function ruleDecls(css: string, selector: string): string | null {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  // `m` so `^` also anchors at the start of a line: the first rule in a SFC's
  // <style> block is preceded by markup, not by `}`.
  const m = css.match(new RegExp(`(?:^|[},])\\s*${escaped}\\s*\\{([^}]*)\\}`, 'm'))
  return m ? m[1] : null
}

const COMPONENT = 'src/components/common/EmptyState.vue'

/**
 * Every tab / sub-tab surface that draws an empty state. Adding one means adding
 * it here too — that is the point: the list is the inventory of the convention.
 *
 * Drawers and dialogs are deliberately out of scope: they still render their own
 * centred block with the shared glyph, which is the same visual without the
 * pane-height contract.
 */
const EMPTY_STATES = [
  'src/App.vue',
  'src/components/chat/ChatMessageList.vue',
  'src/components/file/FileManagerContent.vue',
  'src/components/forge/ForgeOverviewList.vue',
  'src/components/forge/ForgePanelContent.vue',
  'src/components/git/GitBranchList.vue',
  'src/components/git/GitCommitList.vue',
  'src/components/git/GitHistoryContent.vue',
  'src/components/git/GitTagList.vue',
  'src/components/git/GitWorktreeList.vue',
  'src/components/live/MeetingDetail.vue',
  'src/components/live/MeetingList.vue',
  'src/components/live/MeetingResources.vue',
  'src/components/live/MeetingStage.vue',
  'src/components/live/MemberScreenSurface.vue',
  'src/components/mail/MailList.vue',
  'src/components/mail/MailPanel.vue',
  'src/components/mail/MailReader.vue',
  'src/components/notifications/NotificationsPanelContent.vue',
  'src/components/proxy/ProxyPanelContent.vue',
  'src/components/share/ShareCenterPanel.vue',
  'src/components/stats/ClocStatsPanel.vue',
  'src/components/stats/GitCodeStatsPanel.vue',
  'src/components/stats/UsageStatsPanel.vue',
  'src/components/task/TaskHistoryTab.vue',
  'src/components/task/TaskListPage.vue',
  'src/components/terminal/TerminalPanelContent.vue',
  'src/share/LiveChatPanel.vue',
  'src/share/LiveShareView.vue',
  'src/share/MeetingShareDetail.vue',
]

describe('the empty-state glyph is one size, declared once', () => {
  it('declares the size and the stroke as tokens', () => {
    const vars = stripComments(read('css/variables.css'))
    const root = ruleDecls(vars, ':root')
    expect(root, 'variables.css must declare :root').not.toBeNull()
    // 56 design px, expressed so it follows the UI scale.
    expect(root!).toMatch(/--empty-icon-size:\s*calc\(56 \* var\(--dp\)\)/)
    // Unitless: `stroke-width` is not a length.
    expect(root!).toMatch(/--empty-icon-stroke:\s*1\.25/)
  })

  it('applies them from one global rule', () => {
    const css = stripComments(read('css/components.css'))
    const decls = ruleDecls(css, '.empty-state__icon')
    expect(decls, '.empty-state__icon must live in the global stylesheet').not.toBeNull()
    expect(decls!).toMatch(/width:\s*var\(--empty-icon-size\)/)
    expect(decls!).toMatch(/height:\s*var\(--empty-icon-size\)/)
    expect(decls!).toMatch(/stroke-width:\s*var\(--empty-icon-stroke\)/)
  })

  it('is opted into by the shared component', () => {
    // The glyph comes either from the `icon` prop or the `#icon` slot; both are
    // the component's business now, so the opt-in is checked once, here.
    const src = read(COMPONENT)
    expect(src).toContain('class="empty-state__icon"')
  })

  it('sizes the chat welcome avatar from the same token', () => {
    // The single-chat welcome puts an agent avatar where the glyph would go.
    // It used to be a 40dp tile of its own, so the same surface drew two
    // different sizes depending on whether it was a chat or a group.
    const src = stripComments(read('src/components/chat/ChatMessageList.vue'))
    const box = ruleDecls(src, '.agent-welcome-icon')
    expect(box, '.agent-welcome-icon must exist').not.toBeNull()
    expect(box!).toMatch(/width:\s*var\(--empty-icon-size\)/)
    expect(box!).toMatch(/height:\s*var\(--empty-icon-size\)/)

    // The avatar is an AgentIcon, whose ladder is sized for text; `xxl` is the
    // step that resolves to the empty-state token rather than a second number.
    expect(src).toMatch(/<AgentIcon[^>]*\ssize="xxl"/)
    const agentIcon = stripComments(read('src/components/common/AgentIcon.vue'))
    const xxl = ruleDecls(agentIcon, '.agent-icon--xxl')
    expect(xxl, 'AgentIcon must declare the xxl step').not.toBeNull()
    expect(xxl!).toMatch(/--ai-size:\s*var\(--empty-icon-size\)/)
  })

  it('leaves no call site sizing its own glyph', () => {
    // A `:size` on the empty glyph is dead code (CSS wins) that reads like a
    // decision, and it is exactly how the nine sizes drifted apart. Still
    // checked app-wide: drawers render their own block with the shared class.
    const offenders: string[] = []
    for (const f of [...EMPTY_STATES, COMPONENT]) {
      const src = read(f)
      const re = /<[^>]*empty-state__icon[^>]*>/g
      for (const tag of src.match(re) ?? []) {
        if (/:size=/.test(tag)) offenders.push(`${f}: ${tag.replace(/\s+/g, ' ').slice(0, 80)}`)
      }
    }
    expect(offenders).toEqual([])
  })
})

describe('the empty-state shape is declared once', () => {
  it('owns the container rule and the vertical-fill contract', () => {
    const src = stripComments(read(COMPONENT))
    const container = ruleDecls(src, '.empty-state')
    expect(container, 'EmptyState must declare .empty-state').not.toBeNull()
    expect(container!).toMatch(/display:\s*flex/)
    expect(container!).toMatch(/flex-direction:\s*column/)
    expect(container!).toMatch(/align-items:\s*center/)
    expect(container!).toMatch(/justify-content:\s*center/)
    expect(container!).toMatch(/text-align:\s*center/)

    // `fill` is what makes a pane-level block sit in the middle instead of
    // hugging the header.
    const fill = ruleDecls(src, '.empty-state.is-fill')
    expect(fill, 'EmptyState must declare .empty-state.is-fill').not.toBeNull()
    expect(fill!).toMatch(/flex:\s*1/)
  })

  it('is the only declaration of the container class', () => {
    // A second `.empty-state { … }` in a component's scoped block would silently
    // win for that component and re-open the drift this file exists to stop.
    const offenders: string[] = []
    const roots = [
      'src/App.vue',
      ...EMPTY_STATES,
      COMPONENT,
    ]
    for (const f of roots) {
      const src = stripComments(read(f))
      const matches = src.match(/\.empty-state\s*\{/g) ?? []
      // The component itself is allowed exactly one.
      const allowed = f === COMPONENT ? 1 : 0
      if (matches.length > allowed) offenders.push(`${f}: ${matches.length}`)
    }
    expect(offenders).toEqual([])
  })

  it('is used by every tab and sub-tab surface', () => {
    const missing = EMPTY_STATES.filter((f) => !read(f).includes('<EmptyState'))
    expect(missing, 'these surfaces still hand-roll their empty state').toEqual([])
  })

  it('gives every empty-state action the shared .fbtn pill', () => {
    // The buttons had drifted the same way the glyph did: three hand-rolled
    // variants (terminal, chat, mail) with their own height, radius and colour
    // next to the shared pill everyone else used. The pill is the convention,
    // `fbtn-primary` marks the one main action.
    const offenders: string[] = []
    // `[^/]>` skips self-closing `<EmptyState … />` — otherwise the match would
    // run on to the NEXT component's closing tag and inspect its buttons.
    const blockRe = /<EmptyState\b[^>]*[^/]>([\s\S]*?)<\/EmptyState>/g
    for (const f of EMPTY_STATES) {
      const src = read(f)
      for (const m of src.matchAll(blockRe)) {
        const body = m[1]
        const at = body.indexOf('#action')
        if (at === -1) continue
        for (const b of body.slice(at).matchAll(/<button\b[^>]*>/g)) {
          if (!/class="[^"]*\bfbtn\b/.test(b[0])) {
            offenders.push(`${f}: ${b[0].replace(/\s+/g, ' ').slice(0, 90)}`)
          }
        }
      }
    }
    expect(offenders).toEqual([])
  })

  it('keeps the tab empty states from sizing or colouring their own block', () => {
    // The type scale, gap and padding all belong to the component. A local
    // `font-size` on the empty block is how the copy drifted to four scales.
    // (`.share-hint` and `.notif-state` are excluded on purpose: they survived
    // as the *error* blocks, which are not empty states.)
    const offenders: string[] = []
    for (const f of EMPTY_STATES) {
      const src = read(f)
      for (const m of src.matchAll(/\.(?:chat-empty|task-empty|history-empty|stats-empty|proxy-empty|forge-empty-card|section-empty|empty-state-card)\s*\{/g)) {
        offenders.push(`${f}: ${m[0]}`)
      }
    }
    expect(offenders).toEqual([])
  })
})
