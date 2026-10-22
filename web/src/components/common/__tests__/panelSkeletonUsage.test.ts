import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Guards for the panel first-load placeholder.
 *
 * Every dock panel used to show a lone centred ring while its data was in
 * flight: it says "waiting" but nothing about *what* is coming, and the layout
 * collapses to a single glyph. The panels now render <PanelSkeleton> instead.
 *
 * This cannot be pinned behaviourally — both shapes render, and a mounted panel
 * usually stubs the network away so the loading branch never runs. It is pinned
 * at the source level: these panels must not regress to <LoadingIndicator> for
 * their first-load state.
 *
 * `LoadingIndicator` legitimately remains in several of these files for
 * *inline* work (a "load more" footer, a save button, a per-item refresh), so
 * the assertion is narrow: no bare `size="md"` indicator, which is the shape a
 * panel-level placeholder takes.
 */

function readSource(relPath: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), 'web')]) {
    try {
      return readFileSync(resolve(base, relPath), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`${relPath} not found from cwd: ${process.cwd()}`)
}

/** Panels that own a first-load state, with the skeleton variant they use. */
const PANELS: Array<[file: string, why: string]> = [
  ['web/src/components/file/FileManagerContent.vue', 'file listing (list + grid)'],
  ['web/src/components/file/FileViewer.vue', 'file preview'],
  ['web/src/components/file/DirPreviewBody.vue', 'directory preview'],
  ['web/src/components/file/ContentSearchDialog.vue', 'content search results'],
  ['web/src/components/file/InertPathPicker.vue', 'inert path results'],
  ['web/src/components/file/OpenApiPreview.vue', 'openapi docs'],
  ['web/src/components/file/SharedFilesDrawer.vue', 'shared files'],
  ['web/src/components/git/GitHistoryContent.vue', 'git history'],
  ['web/src/components/git/GitHistoryDrawer.vue', 'git history drawer'],
  ['web/src/components/git/GitCommitList.vue', 'commit list'],
  ['web/src/components/git/GitDiffView.vue', 'diff view'],
  ['web/src/components/forge/ForgePanelContent.vue', 'forge lists'],
  ['web/src/components/forge/ForgeOverviewList.vue', 'forge overview'],
  ['web/src/components/forge/ForgeDetail.vue', 'forge detail'],
  ['web/src/components/forge/ForgePipelineDetail.vue', 'pipeline detail'],
  ['web/src/components/task/TaskTab.vue', 'task tab'],
  ['web/src/components/task/TaskListPage.vue', 'task list'],
  ['web/src/components/task/TaskHistoryTab.vue', 'task execution history'],
  ['web/src/components/notifications/NotificationsPanelContent.vue', 'notification log'],
  ['web/src/components/proxy/ProxyPanelContent.vue', 'port forwarding'],
  ['web/src/components/session/SessionList.vue', 'session list'],
  ['web/src/components/session/SessionSearchDrawer.vue', 'session search'],
  ['web/src/components/session/SharedSessionsDrawer.vue', 'shared sessions'],
  ['web/src/components/TocPanel.vue', 'outline'],
  ['web/src/components/chat/MessageClustersDrawer.vue', 'message clusters'],
  ['web/src/components/chat/AttachDrawer.vue', 'attachment drawer'],
  ['web/src/components/chat/UserMsgIndexDrawer.vue', 'user message index'],
  ['web/src/components/chat/AcpSessionDrawer.vue', 'ACP session list'],
  ['web/src/components/common/AgentSelectorDrawer.vue', 'agent picker'],
  ['web/src/components/common/SessionPickerDialog.vue', 'session picker'],
  ['web/src/components/ProjectDialog.vue', 'project picker'],
  ['web/src/components/settings/ProjectsSetting.vue', 'project registry'],
  ['web/src/components/settings/ProjectDetailSetting.vue', 'project detail'],
  ['web/src/components/settings/AgentAvatarPicker.vue', 'avatar tile grid'],
  ['web/src/components/session/SessionTagDialog.vue', 'tag candidates'],
  ['web/src/components/session/SessionShareDialog.vue', 'session share summary'],
  ['web/src/components/file/ShareLinkDialog.vue', 'file share summary'],
  ['web/src/components/git/GitBranchList.vue', 'branch section'],
  ['web/src/components/git/GitTagList.vue', 'tag section'],
  ['web/src/components/git/GitWorktreeList.vue', 'worktree section'],
  ['web/src/components/media/OfficePreview.vue', 'office document'],
  ['web/src/components/media/PdfPreview.vue', 'pdf document'],
  ['web/src/share/ShareRoot.vue', 'share dispatcher'],
  ['web/src/share/ShareView.vue', 'shared file'],
  ['web/src/share/SessionShareView.vue', 'shared conversation'],
]

describe('panel first-load placeholder', () => {
  for (const [file, why] of PANELS) {
    it(`${file} previews its shape (${why})`, () => {
      const src = readSource(file)
      expect(src, `${file} must render <PanelSkeleton>`).toContain('<PanelSkeleton')
      expect(
        src,
        `${file} must import PanelSkeleton`,
      ).toMatch(/import PanelSkeleton from '@\/components\/common\/PanelSkeleton\.vue'|import PanelSkeleton from '\.\/common\/PanelSkeleton\.vue'/)
    })
  }

  it('leaves no bare panel-level LoadingIndicator behind', () => {
    // `size="md"` with no `inline` is the shape of a panel-level placeholder
    // (the ring is centred in an empty area). Inline indicators inside buttons
    // and "load more" footers carry `inline`, so they are not matched.
    //
    // A few panel-level rings remain on purpose, and each is listed here with
    // the reason it is NOT a first-load placeholder. Anything new fails.
    const ALLOWED: Record<string, string[]> = {
      // Refreshing a directory that already has rows on screen: the old rows
      // must stay put, so this is a mask over content, not a placeholder.
      'web/src/components/file/FileManagerContent.vue': ['dirLoading && displayEntries.length > 0'],
      // "Load more" footers — appending, not replacing.
      'web/src/components/forge/ForgePanelContent.vue': ['<LoadingIndicator size="sm" />'],
      'web/src/components/session/SessionSearchDrawer.vue': ['searchState.loadingMore'],
      // A sub-section (the CI list inside an already-rendered detail page).
      'web/src/components/forge/ForgeDetail.vue': ["ci.loading.value"],
      // Jumping to an anchor is an action in progress, not a panel load.
      'web/src/components/chat/UserMsgIndexDrawer.vue': ['jumping'],
      // Resuming an ACP session blocks the whole app: the label ("resuming")
      // is the payload and the list behind it is already populated.
      'web/src/components/chat/AcpSessionDrawer.vue': ['acpResuming'],
    }

    const offenders: string[] = []
    for (const [file] of PANELS) {
      const src = readSource(file)
      // `[\s\S]*?/>` rather than `[^>]*>`: conditions like
      // `v-if="dirLoading && displayEntries.length > 0"` contain a `>`, which
      // would truncate the tag and hide the condition from the allowlist.
      const re = /<LoadingIndicator[\s\S]*?\/>/g
      let m: RegExpExecArray | null
      while ((m = re.exec(src)) !== null) {
        const tag = m[0]
        if (/\binline\b/.test(tag)) continue
        // Allowances name the *condition*, which usually sits in the attribute
        // value or on an enclosing element a few lines above.
        const context = src.slice(Math.max(0, m.index - 400), m.index) + tag
        const allowed = ALLOWED[file] || []
        if (allowed.some(a => context.includes(a))) continue
        offenders.push(`${file} → ${tag.replace(/\s+/g, ' ')}`)
      }
    }
    expect(
      offenders,
      'a panel-level first-load state must be a PanelSkeleton; add the site to ALLOWED with a reason if it is a mask / load-more / in-progress action',
    ).toEqual([])
  })

  it('keeps the shimmer primitive global', () => {
    // The blocks are painted from two different style scopes plus the panels'
    // own scoped blocks, so `.skeleton-block` must stay in the global sheet
    // (design-guide red line 2). A scoped copy would silently miss the others.
    const css = readSource('web/css/components.css')
    expect(css).toContain('.skeleton-block {')
    expect(css).toContain('@keyframes skeleton-shimmer')
    // The shimmer carries no information — freezing it is the intended
    // reduced-motion behaviour, unlike BusyBar.
    expect(css).toMatch(/@media \(prefers-reduced-motion: reduce\) \{\s*\.skeleton-block \{\s*animation: none;/)
  })

  it('does not re-declare the primitive in the component', () => {
    const src = readSource('web/src/components/common/PanelSkeleton.vue')
    const scoped = src.slice(src.indexOf('<style scoped>'), src.lastIndexOf('</style>'))
    // Strip comments: the block legitimately mentions the primitive in prose.
    const bare = scoped.replace(/\/\*[\s\S]*?\*\//g, '')
    expect(
      bare,
      'PanelSkeleton must use the global .skeleton-block, not fork its fill/shimmer',
    ).not.toMatch(/^\.skeleton-block\s*\{/m)
    expect(bare).not.toContain('@keyframes skeleton-shimmer')
  })
})
