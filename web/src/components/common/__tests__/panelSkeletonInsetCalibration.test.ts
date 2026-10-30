import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Calibration guard: every panel skeleton must land on the same x as the rows it
 * previews.
 *
 * A placeholder that is inset differently from the real rows makes the panel
 * jump sideways the moment the data lands — the failure mode is invisible in
 * jsdom (no layout, and scoped styles are not applied), so it is pinned here by
 * resolving both sides out of the stylesheets:
 *
 *   skeleton x  = `--panel-skeleton-inset`
 *   real row x  = the horizontal component of that row rule's `padding`
 *                 (+ `extra` for container padding the rows get but the
 *                 placeholder does not)
 *
 * The row side is DERIVED, not restated, so changing a row's padding without
 * recalibrating its skeleton fails here. Each entry names the row it mirrors.
 */

const SPACE = (() => {
  const css = read('css/variables.css')
  const out: Record<string, number> = {}
  for (const m of css.matchAll(/--space-(\d+):\s*calc\(\s*([\d.]+)\s*\*\s*var\(--dp\)\s*\)/g)) {
    out[`--space-${m[1]}`] = Number(m[2])
  }
  return out
})()

function read(rel: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, rel), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

/** One design-px length token. */
function length(token: string): number {
  const t = token.trim()
  if (t === '0') return 0
  const calc = /^calc\(\s*(-?[\d.]+)\s*\*\s*var\(--dp\)\s*\)$/.exec(t)
  if (calc) return Number(calc[1])
  const space = /^var\((--space-\d+)\)$/.exec(t)
  if (space) {
    const v = SPACE[space[1]]
    if (v === undefined) throw new Error(`unknown spacing token ${t}`)
    return v
  }
  const px = /^(-?[\d.]+)px$/.exec(t)
  if (px) return Number(px[1])
  throw new Error(`cannot resolve length "${t}"`)
}

/** Split a shorthand on top-level whitespace (`calc(10 * var(--dp))` stays one). */
function values(shorthand: string): string[] {
  const out: string[] = []
  let depth = 0
  let cur = ''
  for (const ch of shorthand.trim()) {
    if (ch === '(') depth++
    if (ch === ')') depth--
    if (/\s/.test(ch) && depth === 0) {
      if (cur) out.push(cur)
      cur = ''
    } else {
      cur += ch
    }
  }
  if (cur) out.push(cur)
  return out
}

/** The horizontal component of a `padding` shorthand, in design px. */
function horizontalPadding(blockText: string): number {
  const value = blockText.match(/(?:^|[\s;])padding:\s*([^;]+);/)?.[1]
  if (!value) throw new Error('rule declares no padding')
  const v = values(value)
  // 1 value: all sides. 2/3: the second is horizontal. 4: the fourth (left) is
  // the leading edge, which is what the placeholder has to match.
  const token = v.length === 1 ? v[0] : v.length === 4 ? v[3] : v[1]
  return length(token)
}

/** The declaration block for `selector`, comments stripped. */
function block(file: string, selector: string): string {
  const src = read(file).replace(/\/\*[\s\S]*?\*\//g, '')
  const at = src.indexOf(`${selector} {`)
  if (at === -1) throw new Error(`${file}: no rule for "${selector}"`)
  return src.slice(at, src.indexOf('}', at))
}

/** The inset the skeleton declares, in design px. */
function inset(file: string, selector: string): number {
  const decl = block(file, selector).match(/--panel-skeleton-inset:\s*([^;]+);/)?.[1]
  if (!decl) throw new Error(`${file}: "${selector}" does not declare --panel-skeleton-inset`)
  return length(decl)
}

interface Surface {
  /** Human label, used in the failure message. */
  name: string
  /** Component that hosts the skeleton. */
  file: string
  /** Selector carrying `--panel-skeleton-inset`. */
  skeleton: string
  /** File declaring the real row (often the same one). */
  rowFile?: string
  /** The real row whose text edge the placeholder must match. */
  row: string
  /**
   * Design px of extra container padding the ROWS get but the placeholder does
   * not (0 when both sit in the same container, which then cancels out).
   */
  extra?: number
}

const SURFACES: Surface[] = [
  {
    name: 'projects list',
    file: 'src/components/settings/ProjectsSetting.vue',
    skeleton: '.projects-skeleton',
    row: '.projects-row',
  },
  {
    name: 'session search results',
    file: 'src/components/session/SessionSearchDrawer.vue',
    skeleton: '.session-search-skeleton',
    row: '.session-search-item',
  },
  {
    name: 'session search preview',
    file: 'src/components/session/SessionSearchDrawer.vue',
    skeleton: '.detail-skeleton',
    row: '.detail-chunk',
  },
  {
    name: 'session list',
    file: 'src/components/session/SessionList.vue',
    skeleton: '.session-list-skeleton',
    row: '.session-item',
  },
  {
    name: 'content search results',
    file: 'src/components/file/ContentSearchDialog.vue',
    skeleton: '.cs-loading-skeleton',
    row: '.cs-file-head',
  },
  {
    name: 'inert path picker',
    file: 'src/components/file/InertPathPicker.vue',
    skeleton: '.ip-loading',
    row: '.ip-item',
  },
  {
    name: 'file viewer preview',
    file: 'src/components/file/FileViewer.vue',
    skeleton: '.file-viewer-skeleton',
    rowFile: 'css/content.css',
    row: '.markdown-body',
  },
  {
    name: 'file overlay mask',
    file: 'src/components/file/FileOverlay.vue',
    skeleton: '.file-loading-mask > .panel-skeleton',
    rowFile: 'css/content.css',
    row: '.markdown-body',
  },
  {
    name: 'git history drawer',
    file: 'src/components/git/GitHistoryDrawer.vue',
    skeleton: '.git-history-loading--skeleton',
    row: '.drilldown-item',
  },
  {
    name: 'git commit list',
    file: 'src/components/git/GitCommitList.vue',
    skeleton: '.git-history-loading--skeleton',
    row: '.drilldown-item',
  },
  {
    name: 'git history content',
    file: 'src/components/git/GitHistoryContent.vue',
    skeleton: '.git-history-loading--skeleton',
    row: '.drilldown-item',
  },
  {
    name: 'git worktree list',
    file: 'src/components/git/GitWorktreeList.vue',
    skeleton: '.section-skeleton',
    rowFile: 'src/components/git/GitWorktreeCard.vue',
    row: '.git-worktree-row',
  },
  {
    name: 'git branch list',
    file: 'src/components/git/GitBranchList.vue',
    skeleton: '.section-skeleton',
    rowFile: 'src/components/git/GitBranchRow.vue',
    row: '.git-branch-row',
  },
  {
    name: 'git tag list',
    file: 'src/components/git/GitTagList.vue',
    skeleton: '.section-skeleton',
    row: '.tag-row',
  },
  {
    name: 'proxy registered ports',
    file: 'src/components/proxy/ProxyPanelContent.vue',
    skeleton: '.proxy-skeleton',
    rowFile: 'src/components/proxy/ProxyPortItem.vue',
    row: '.proxy-port-item',
  },
  {
    name: 'proxy port scan',
    file: 'src/components/proxy/ProxyPanelContent.vue',
    skeleton: '.port-scan-loading--skeleton',
    row: '.port-scan-item',
    // The rows sit inside `.port-scan-results` (2dp) which the placeholder does
    // not; `.port-scan-content` (10dp) is shared and cancels out.
    extra: 2,
  },
  {
    name: 'task tab fallback',
    file: 'src/components/task/TaskTab.vue',
    skeleton: '.task-pending--skeleton',
    rowFile: 'src/components/task/TaskListPage.vue',
    row: '.task-item',
    // The list this placeholder stands in for is wrapped in `.task-list-body`.
    extra: 8,
  },
  {
    name: 'task list',
    file: 'src/components/task/TaskListPage.vue',
    skeleton: '.task-loading--skeleton',
    row: '.task-item',
  },
  {
    name: 'task history',
    file: 'src/components/task/TaskHistoryTab.vue',
    skeleton: '.history-skeleton',
    row: '.execution-row',
  },
  {
    name: 'notifications',
    file: 'src/components/notifications/NotificationsPanelContent.vue',
    skeleton: '.notif-skeleton',
    row: '.nc-main',
  },
  {
    name: 'session picker',
    file: 'src/components/common/SessionPickerDialog.vue',
    skeleton: '.session-picker-list',
    row: '.sp-row',
  },
  {
    name: 'message index',
    file: 'src/components/chat/UserMsgIndexDrawer.vue',
    skeleton: '.msg-index-skeleton',
    rowFile: 'src/components/chat/MessageIndexRow.vue',
    row: '.msg-item',
  },
  {
    name: 'attachment drawer',
    file: 'src/components/chat/AttachDrawer.vue',
    skeleton: '.ad-content',
    row: '.ad-file-row',
  },
  {
    name: 'acp session drawer',
    file: 'src/components/chat/AcpSessionDrawer.vue',
    skeleton: '.acp-session-skeleton',
    row: '.acp-session-item',
  },
  {
    name: 'message clusters',
    file: 'src/components/chat/MessageClustersDrawer.vue',
    skeleton: '.mc-content',
    row: '.mc-cluster-item',
  },
]

describe('panel skeletons sit on their rows’ inset', () => {
  for (const s of SURFACES) {
    it(`${s.name}: placeholder lands where ${s.row} does`, () => {
      const skeleton = inset(s.file, s.skeleton)
      const row = horizontalPadding(block(s.rowFile ?? s.file, s.row)) + (s.extra ?? 0)
      expect(
        skeleton,
        `${s.name}: the placeholder is inset ${skeleton}dp but ${s.row} is inset ${row}dp — ` +
          `the panel will jump sideways when the data arrives`,
      ).toBe(row)
    })
  }
})

/**
 * Surfaces that need no override because their rows already sit on the
 * primitive's own 8dp row padding. Listed so that moving one of these rows is
 * not silently un-calibrated: the failure says to add the surface to SURFACES.
 */
const DEFAULT_INSET_SURFACES: Array<{ name: string; file: string; row: string }> = [
  { name: 'table of contents', file: 'src/components/TocPanel.vue', row: '.toc-item' },
  { name: 'directory preview', file: 'src/components/file/DirPreviewBody.vue', row: '.dir-preview-item' },
  { name: 'project dialog', file: 'src/components/ProjectDialog.vue', row: '.dialog-item' },
  { name: 'shared sessions', file: 'src/components/session/SharedSessionsDrawer.vue', row: '.shared-session-row' },
  { name: 'shared files', file: 'src/components/file/SharedFilesDrawer.vue', row: '.shared-file-row' },
  { name: 'agent selector', file: 'src/components/common/AgentSelectorDrawer.vue', row: '.agent-option' },
]

describe('surfaces relying on the default 8dp row inset', () => {
  for (const s of DEFAULT_INSET_SURFACES) {
    it(`${s.name}: ${s.row} still sits on the primitive's default`, () => {
      const row = horizontalPadding(block(s.file, s.row))
      expect(
        row,
        `${s.name}: ${s.row} moved to ${row}dp — give its skeleton an explicit ` +
          `--panel-skeleton-inset and move this entry into SURFACES`,
      ).toBe(8)
    })
  }
})
