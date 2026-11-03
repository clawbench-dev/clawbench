/**
 * One glyph size for every empty state.
 *
 * The app had drifted to nine sizes (24 → 56) and three opacities across ~20
 * "nothing here yet" surfaces, so the size now lives in `--empty-icon-size` and
 * every call site opts into the same global `.empty-state__icon` rule. jsdom
 * has no layout engine, so the contract is read off the sources: a regression
 * here is "someone passed `:size` again", which no rendered test would notice.
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
  const m = css.match(new RegExp(`(?:^|[},])\\s*${escaped}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : null
}

/**
 * Every surface that draws an empty-state glyph. Adding one means adding it
 * here too — that is the point: the list is the inventory of the convention.
 */
const EMPTY_STATES = [
  'src/components/live/MeetingEmpty.vue',
  'src/components/live/MeetingStage.vue',
  'src/components/chat/ChatMessageList.vue',
  'src/components/chat/QuickSendDrawer.vue',
  'src/components/chat/UserMsgIndexDrawer.vue',
  'src/components/chat/MessageClustersDrawer.vue',
  'src/components/file/FileManagerContent.vue',
  'src/components/file/ContentSearchDialog.vue',
  'src/components/file/InertPathPicker.vue',
  'src/components/file/SharedFilesDrawer.vue',
  'src/components/session/SharedSessionsDrawer.vue',
  'src/components/git/GitCommitList.vue',
  'src/components/forge/ForgeOverviewList.vue',
  'src/components/forge/ForgePanelContent.vue',
  'src/components/task/TaskListPage.vue',
  'src/components/task/TaskHistoryTab.vue',
  'src/components/terminal/TerminalPanelContent.vue',
  'src/components/terminal/QuickCommandDrawer.vue',
  'src/components/proxy/ProxyPanelContent.vue',
  'src/components/notifications/NotificationsPanelContent.vue',
  'src/components/stats/GitCodeStatsPanel.vue',
  'src/components/stats/UsageStatsPanel.vue',
  'src/components/stats/ClocStatsPanel.vue',
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

  it('is opted into by every empty state', () => {
    // Either the file puts the class on its own glyph, or it renders the shared
    // `MeetingEmpty` — which does it for the caller. Both end at the same rule.
    const missing = EMPTY_STATES.filter((f) => {
      const src = read(f)
      return !src.includes('empty-state__icon') && !src.includes('<MeetingEmpty')
    })
    expect(missing, 'these surfaces still draw their own empty glyph').toEqual([])
  })

  it('puts the class on the glyph of each .empty-state container', () => {
    // The file-level check above is necessary but blind: FileManagerContent has
    // TWO empty states (search + directory) and only the search one carried the
    // class, so "the file mentions empty-state__icon" stayed true while the
    // directory glyph rendered at full opacity. Anchor on the container and
    // require the opt-in on the glyph that follows it.
    //
    // The window is bounded so a sibling empty state further down the file
    // cannot satisfy the check on this one's behalf; the glyph is the
    // container's first child, so 400 characters is ample.
    const WINDOW = 400
    const offenders: string[] = []
    let containers = 0
    for (const f of EMPTY_STATES) {
      const src = read(f)
      for (const m of src.matchAll(/class="empty-state(?:[\s"][^"]*)?"/g)) {
        containers++
        const block = src.slice(m.index, m.index + WINDOW)
        if (!/empty-state__icon|<MeetingEmpty/.test(block)) {
          const line = src.slice(0, m.index).split('\n').length
          offenders.push(`${f}:${line}: ${block.split('\n')[1]?.trim() ?? ''}`)
        }
      }
    }
    expect(containers, 'no .empty-state container was found — the anchors moved').toBeGreaterThan(0)
    expect(offenders, 'these glyphs are not covered by .empty-state__icon').toEqual([])
  })

  it('leaves no call site sizing its own glyph', () => {
    // A `:size` on the empty glyph is dead code (CSS wins) that reads like a
    // decision, and it is exactly how the nine sizes drifted apart. Checked
    // per file, on the element that carries the shared class.
    const offenders: string[] = []
    for (const f of EMPTY_STATES) {
      const src = read(f)
      const re = /<[^>]*empty-state__icon[^>]*>/g
      for (const tag of src.match(re) ?? []) {
        if (/:size=/.test(tag)) offenders.push(`${f}: ${tag.replace(/\s+/g, ' ').slice(0, 80)}`)
      }
    }
    expect(offenders).toEqual([])
  })
})
