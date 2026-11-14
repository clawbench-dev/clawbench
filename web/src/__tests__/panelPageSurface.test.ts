import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: every left-panel tab paints the SAME page surface.
 *
 * The colour used to depend on whether a panel's root painted one: panels that
 * did used `--bg-primary`, panels that left the root transparent fell through
 * to `.tab-panel`'s `--bg-secondary`. Switching tabs therefore changed the page
 * colour by one step (white ↔ off-white in light themes, clearly different in
 * dark ones).
 *
 * Both paths now read `--panel-page-bg`. Source-level assertions because the
 * invariant is structural: one token, every page root, no raw background
 * tokens left behind.
 */
const PAGE_ROOTS: Array<[file: string, selector: string]> = [
  ['src/components/git/GitHistoryContent.vue', '.git-history-content'],
  ['src/components/file/FileOverlay.vue', '.file-overlay'],
  ['src/components/forge/ForgePanelContent.vue', '.forge-panel'],
  ['src/components/task/TaskListPage.vue', '.task-list-page'],
  ['src/components/task/TaskDetailPage.vue', '.task-detail-page'],
  ['src/components/task/TaskFormPage.vue', '.task-form-page'],
  ['src/components/task/TaskExecDetail.vue', '.exec-detail-page'],
  ['src/components/task/TaskOverviewTab.vue', '.task-overview'],
  ['src/components/proxy/ProxyPanelContent.vue', '.proxy-panel-content'],
  ['src/components/stats/StatsTabHost.vue', '.stats-tab-host'],
  ['src/components/notifications/NotificationsPanelContent.vue', '.notif-panel'],
  ['src/components/kb/KnowledgePanel.vue', '.kb-panel'],
  ['src/components/live/MeetingList.vue', '.meeting-panel'],
  ['src/components/live/MeetingCreate.vue', '.meeting-panel'],
  ['src/components/live/MeetingJoin.vue', '.meeting-panel'],
  ['src/components/live/MeetingDetail.vue', '.meeting-panel'],
  ['src/components/live/MeetingStage.vue', '.live-panel'],
  ['src/components/settings/SettingsIndex.vue', '.settings-index'],
  ['src/components/settings/SettingsCategory.vue', '.settings-category'],
  ['src/components/settings/SettingsAgentsIndex.vue', '.settings-agents-index'],
  ['src/components/settings/SettingsAgentDetail.vue', '.settings-agent-detail'],
  ['src/components/settings/DockPluginSetting.vue', '.dock-plugins'],
]

/** Card surfaces: the step above the page surface. One per tab that has cards. */
const CARD_SURFACES: Array<[file: string, selector: string]> = [
  ['src/components/settings/SettingsCard.vue', '.settings-card__body'],
  ['src/components/settings/SettingsGroupPanel.vue', '.group-panel__card'],
  ['src/components/settings/DockPluginSetting.vue', '.preset-card'],
  ['src/components/settings/SettingsAgentsIndex.vue', '.settings-agents-index__row'],
  ['src/components/share/ShareCenterPanel.vue', '.share-row'],
  ['src/components/notifications/NotificationsPanelContent.vue', '.nc-main'],
  ['src/components/task/TaskListPage.vue', '.task-item'],
  ['src/components/task/TaskFormPage.vue', '.form-section'],
  ['src/components/proxy/ProxyPortItem.vue', '.proxy-port-item'],
  ['src/components/stats/UsageStatsPanel.vue', '.stats-card-panel'],
  ['src/components/forge/ForgeDetail.vue', '.forge-comment'],
  ['src/components/forge/ForgePanelContent.vue', '.forge-remote-row'],
  ['src/components/live/MeetingList.vue', '.meeting-row'],
  ['src/components/live/MeetingResources.vue', '.res-share-card'],
]

function ruleBody(source: string, selector: string): string | null {
  // Selector anchored at the start of a line so `.meeting-panel` cannot match
  // `.meeting-panel__header` or a `:deep(...)` variant.
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = source.match(new RegExp(`(?:^|\\n)${escaped}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : null
}

describe('one page surface for every left-panel tab', () => {
  it('declares the token once, on top of the primary background', () => {
    const vars = readWebFile('css/variables.css')
    expect(vars, '--panel-page-bg must be declared once').toMatch(
      /--panel-page-bg:\s*var\(--bg-primary\);/,
    )
  })

  it('makes the shared tab-panel surface the token (left column only)', () => {
    // Scoped to `.col-left`: the chat pane also renders a `.tab-panel` and
    // keeps its own --bg-secondary surface (it is a pane, not a tab).
    const css = readWebFile('css/components.css')
    const rule = css.match(/(?:^|\n)\.col-left \.tab-panel\s*\{([^}]*)\}/)
    expect(rule, '.col-left .tab-panel rule must exist').not.toBeNull()
    expect(rule![1]).toMatch(/background:\s*var\(--panel-page-bg\)/)
  })

  it('paints every page root with the token', () => {
    const offenders: string[] = []
    for (const [file, selector] of PAGE_ROOTS) {
      const body = ruleBody(readWebFile(file), selector)
      if (!body) {
        offenders.push(`${file}: ${selector} rule not found`)
        continue
      }
      if (!/background:\s*var\(--panel-page-bg\)/.test(body)) {
        offenders.push(`${file}: ${selector} does not use --panel-page-bg`)
      }
    }
    expect(offenders, `page roots not on the shared surface:\n${offenders.join('\n')}`).toEqual([])
  })

  it('keeps card surfaces on --bg-secondary, not the page token', () => {
    // The page is the base; cards are the step above it. Painting a card with
    // the page token makes it disappear into the background (the share
    // centre's cards did exactly that).
    const offenders: string[] = []
    for (const [file, selector] of CARD_SURFACES) {
      const body = ruleBody(readWebFile(file), selector)
      if (!body || !/background:\s*var\(--bg-secondary\b[^)]*\)/.test(body)) {
        offenders.push(`${file}: ${selector}`)
      }
    }
    expect(offenders, `cards must sit on --bg-secondary:\n${offenders.join('\n')}`).toEqual([])
  })

  it('finds every file it is meant to guard', () => {
    // Guard against a rename silently turning the checks above into no-ops.
    for (const [file] of [...PAGE_ROOTS, ...CARD_SURFACES]) {
      expect(() => readWebFile(file), `${file} must exist`).not.toThrow()
    }
  })
})

describe('the card step exists in every theme', () => {
  /** Max channel distance between two `#rrggbb` colours. */
  function delta(a: string, b: string): number {
    const rgb = (h: string) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16))
    const [ra, rb] = [rgb(a), rgb(b)]
    return Math.max(...ra.map((v, i) => Math.abs(v - rb[i])))
  }

  it('never sets --bg-secondary equal to --bg-primary', () => {
    // Cards are --bg-secondary on a --bg-primary page. A theme where the two
    // are identical (or one step apart) makes every card in the app vanish
    // into the page — the exact bug the share-centre cards had, but
    // theme-wide. The tightest shipped theme is 3 apart, so 2 is a floor.
    const css = readWebFile('css/variables.css')
    const blocks = css.split(/(?=\[data-theme=")/)
    const offenders: string[] = []
    let checked = 0
    for (const block of blocks) {
      const theme = block.match(/^\[data-theme="([^"]+)"\]/)?.[1]
      if (!theme) continue
      const primary = block.match(/--bg-primary:\s*(#[0-9a-fA-F]{6})\s*;/)?.[1]
      const secondary = block.match(/--bg-secondary:\s*(#[0-9a-fA-F]{6})\s*;/)?.[1]
      if (!primary || !secondary) continue
      checked++
      if (delta(primary, secondary) < 2) offenders.push(`${theme}: ${primary} / ${secondary}`)
    }
    // If the block parsing silently stops matching, the check above is vacuous.
    expect(checked, 'themes parsed').toBeGreaterThanOrEqual(30)
    expect(offenders, `card step too close to the page:\n${offenders.join('\n')}`).toEqual([])
  })
})
