import { ref, computed, type Ref, type ComputedRef } from 'vue'
import { renderDiff } from '@/utils/diff.ts'
import { gitFetch, GitTimeoutError, createSeqGuard, type SeqToken } from '@/utils/gitApi'
import { shouldShowFullLoading, splitGitFilePath, buildFileHistoryCommits } from '@/utils/gitFileHistory'
import { useCommitNavigation } from '@/composables/useCommitNavigation.ts'
import { appLog } from '@/utils/appLog'

// ─── Types ──────────────────────────────────────────────────────────────────

export interface GitCommit {
  sha: string
  msg: string
  date: string
  author: string
  /** Frontend-only synthetic row for the working tree. */
  isWT?: boolean
  refs?: string[]
}

export interface GitFile {
  path: string
  /** Git status letter: A/M/D/R/? */
  type: string
  staged?: boolean
}

export interface MergeGroup {
  label: string
  files: GitFile[]
}

export interface DiffState {
  loading: boolean
  empty: boolean
  html: string
}

/** 'commits' | 'files' | 'diff' (GitHistoryContent additionally uses 'manage'). */
export type GitHistoryView = string

export interface UseGitHistoryViewOptions {
  /** i18n translate function (injected so callers keep their i18n mock). */
  t: (key: string, named?: Record<string, unknown>) => string
  /** Reads the host's `mode` prop: 'project' | 'file'. */
  mode: () => string
  /** Reads the host's `file.path` (only meaningful when mode === 'file'). */
  filePath?: () => string | undefined
  /**
   * Whether to pass an AbortSignal to gitFetch. GitHistoryContent does (tab
   * re-activation can supersede an in-flight load); GitHistoryDrawer does not.
   */
  useAbortSignal?: boolean
  /** Called after a successful project-history load (host records git state). */
  onProjectHistoryLoaded?: () => void
}

export interface UseGitHistoryViewReturn {
  // ── state ──
  loading: Ref<boolean>
  fullReloading: Ref<boolean>
  error: Ref<string>
  commits: Ref<GitCommit[]>
  hasMore: Ref<boolean>
  searchLoading: Ref<boolean>
  loadingMore: Ref<boolean>
  isGit: Ref<boolean>
  untracked: Ref<boolean>
  currentView: Ref<GitHistoryView>
  selectedSHA: Ref<string | null>
  filesLoading: Ref<boolean>
  filesRefreshing: Ref<boolean>
  files: Ref<GitFile[]>
  mergeGroups: Ref<MergeGroup[]>
  selectedFilePath: Ref<string | null>
  diffState: Ref<DiffState>
  wtFiles: Ref<GitFile[]>
  commitSearch: Ref<string>
  /** Host-owned: whether the user paginated past the first page. */
  hasLoadedMore: Ref<boolean>
  /** Host-owned: stale-data pulse on the refresh button. */
  refreshHint: Ref<boolean>
  /** Template ref for the GitCommitList child. */
  commitListRef: Ref<unknown>

  // ── computed ──
  selectedCommit: ComputedRef<GitCommit | null>
  isWorkingTree: ComputedRef<boolean>
  mode: ComputedRef<string>
  sortedFiles: ComputedRef<GitFile[]>
  stagedFiles: ComputedRef<GitFile[]>
  unstagedFiles: ComputedRef<GitFile[]>
  hasStaged: ComputedRef<boolean>
  hasUnstaged: ComputedRef<boolean>
  totalFileCount: ComputedRef<number>
  /** Changed-file count shown as a badge on the working-tree commit row. */
  workingTreeFileCount: ComputedRef<number>

  // ── helpers ──
  fileTypeLabel: (type: string, staged?: boolean) => string
  fileSplit: (f: GitFile | undefined) => { name: string; dir: string }
  badgeClass: (f: GitFile) => string
  resetListState: () => void

  // ── loading ──
  loadProjectHistory: () => Promise<void>
  loadFileHistory: (filePath: string) => Promise<void>
  loadMoreCommits: () => Promise<void>
  onSearch: (q: string) => Promise<void>
  onRefresh: () => Promise<void>
  loadCommitFiles: (sha: string) => Promise<void>
  loadDiff: () => Promise<void>
  loadWorkingTreeFiles: (opts?: { inPlace?: boolean }) => Promise<void>
  onFilesRefresh: () => void
  /** Reload history while restoring (and re-freshening) the current drill-down. */
  reloadPreservingDrillDown: () => Promise<void>

  // ── navigation ──
  onCommitSelect: (c: GitCommit) => void
  drillBack: (view: GitHistoryView) => void
  drillToFile: (f: GitFile) => void
  navigateToCommit: (sha: string) => Promise<void>
}

/**
 * Shared git-history view logic for the two history hosts.
 *
 * `GitHistoryContent` (left dock, project mode, driven by `active`) and
 * `GitHistoryDrawer` (file-history bottom sheet, driven by `open`) render
 * nearly identical UI and previously carried duplicate copies of this logic —
 * which meant every fix had to be applied twice. This composable owns the
 * state, loading and drill-down navigation; each host keeps only its own
 * trigger (tab activation vs drawer open) and its own chrome (manage view,
 * diff prev/next, search, BottomSheet header).
 *
 * Host-specific knobs are injected via options: `mode`/`filePath` read the
 * host's props, `useAbortSignal` matches the host's existing gitFetch call
 * shape, and `onProjectHistoryLoaded` lets GitHistoryContent record the git
 * state it uses to decide whether a tab re-entry needs a reload.
 */
export function useGitHistoryView(options: UseGitHistoryViewOptions): UseGitHistoryViewReturn {
  const { t, mode: readMode, filePath, useAbortSignal = false, onProjectHistoryLoaded } = options

  // Sequence guard to suppress stale concurrent loads. A refresh, re-entry, or
  // load-more can overlap an in-flight request; only the latest call may write
  // data and reset the loading flag.
  const historySeq = createSeqGuard()

  /** Attach the guard's signal only for hosts that pass one. */
  function req(url: string, seq?: SeqToken): Promise<Response> {
    if (useAbortSignal && seq) return gitFetch(url, { signal: seq.signal })
    return gitFetch(url)
  }

  // ─── Unified state ───────────────────────────────────────────────────────

  const loading = ref(false)
  // True while a full reload is in flight WITHOUT the full-screen spinner (i.e.
  // a background refresh that keeps the existing list). loadMore must not run
  // concurrently with it — it would paginate the old commits with stale counts.
  const fullReloading = ref(false)
  const error = ref('')
  const commits = ref<GitCommit[]>([])
  const hasMore = ref(false)
  const searchLoading = ref(false)
  const loadingMore = ref(false)
  const isGit = ref(false)
  const untracked = ref(false)

  const currentView = ref<GitHistoryView>('commits')
  const selectedSHA = ref<string | null>(null)

  // Files view (project mode only)
  const filesLoading = ref(false)
  // Separate from filesLoading: drives the refresh button's spin for an
  // in-place working-tree reload without tearing down the list (no flicker).
  const filesRefreshing = ref(false)
  const files = ref<GitFile[]>([])
  const mergeGroups = ref<MergeGroup[]>([])
  const selectedFilePath = ref<string | null>(null)

  // Unified diff state
  const diffState = ref<DiffState>({ loading: false, empty: false, html: '' })

  // Working tree
  const wtFiles = ref<GitFile[]>([])

  // Whether the user has paginated past the first page. Hosts use it to decide
  // whether a re-entry may safely auto-reload (reloading would discard the
  // extra pages the user loaded).
  const hasLoadedMore = ref(false)
  // Drives the stale-data pulse on the refresh button.
  const refreshHint = ref(false)

  const commitListRef = ref<unknown>(null)

  // ─── Computed ────────────────────────────────────────────────────────────

  const selectedCommit = computed<GitCommit | null>(() => {
    return commits.value.find(c => c.sha === selectedSHA.value) || null
  })
  const isWorkingTree = computed(() => selectedSHA.value === 'HEAD')

  const mode = computed(() => readMode())

  const sortedFiles = computed(() => {
    const order: Record<string, number> = { M: 0, A: 1, D: 2, R: 3, '?': 4 }
    return [...files.value].sort((a, b) => (order[a.type] ?? 5) - (order[b.type] ?? 5))
  })
  const stagedFiles = computed(() => sortedFiles.value.filter(f => f.staged))
  const unstagedFiles = computed(() => sortedFiles.value.filter(f => !f.staged))
  const hasStaged = computed(() => stagedFiles.value.length > 0)
  const hasUnstaged = computed(() => unstagedFiles.value.length > 0)

  const totalFileCount = computed(() => {
    if (mergeGroups.value.length > 0) {
      return mergeGroups.value.reduce((sum, g) => sum + g.files.length, 0)
    }
    return files.value.length
  })

  /**
   * Badge count on the working-tree commit row.
   *
   * In project mode it is the working-tree change list itself (`wtFiles`), so
   * the number always matches the files the user sees after drilling in. In
   * file mode the working tree holds exactly one entry — the file whose
   * history is being viewed.
   */
  const workingTreeFileCount = computed(() => {
    if (readMode() === 'file') return 1
    return wtFiles.value.length
  })

  // ─── Helpers ─────────────────────────────────────────────────────────────

  function fileTypeLabel(type: string, staged?: boolean): string {
    const keys: Record<string, string> = {
      A: 'git.fileType.added', M: 'git.fileType.modified', D: 'git.fileType.deleted',
      R: 'git.fileType.renamed', '?': 'git.fileType.untracked',
    }
    const base = t(keys[type] || type)
    return staged ? t('git.fileType.stagedPrefix') + base : base
  }

  function fileSplit(f: GitFile | undefined): { name: string; dir: string } {
    return splitGitFilePath(f?.path || '')
  }

  function badgeClass(f: GitFile): string {
    const typeMap: Record<string, string> = { A: 'A', M: 'M', D: 'D', R: 'R', '?': 'U' }
    const cls = typeMap[f.type] || 'M'
    return 'badge-' + cls + (f.staged ? ' badge-staged' : '')
  }

  /**
   * Clear the list/loading state back to its initial values.
   *
   * Hosts wrap this in their own `resetState()` to also clear the identity refs
   * they own (lastProjectRoot / lastFilePath), which is what makes a project or
   * file switch start from a clean slate.
   */
  function resetListState() {
    commits.value = []
    files.value = []
    mergeGroups.value = []
    hasMore.value = false
    selectedSHA.value = null
    selectedFilePath.value = null
    diffState.value = { loading: false, empty: false, html: '' }
    currentView.value = 'commits'
    error.value = ''
    commitSearch.value = ''
    isGit.value = false
    untracked.value = false
    wtFiles.value = []
    hasLoadedMore.value = false
    refreshHint.value = false
  }

  // Exposed for the search watcher / GitCommitList two-way binding
  const commitSearch = ref('')

  // ─── Data loading ────────────────────────────────────────────────────────

  async function loadProjectHistory(): Promise<void> {
    const seq = historySeq.token()
    // Keep the existing list visible during background refreshes — only show the
    // full-screen spinner when there is nothing to render yet (first load/empty),
    // so the refresh button stays mounted and its spin feedback is visible.
    const isFirstLoad = shouldShowFullLoading(commits.value, error.value)
    loading.value = isFirstLoad
    fullReloading.value = !isFirstLoad
    error.value = ''
    if (isFirstLoad) commits.value = []
    hasMore.value = false
    selectedSHA.value = null
    files.value = []
    mergeGroups.value = []
    selectedFilePath.value = null
    wtFiles.value = []
    isGit.value = true

    try {
      const resp = await req('/api/git/project-history', seq)
      if (!historySeq.isCurrent(seq)) return // superseded by a newer load
      if (!resp.ok) {
        const data = await resp.json()
        commits.value = []
        error.value = data.error || t('git.history.loadError')
        return
      }
      const data = await resp.json()

      if (!data.isGit) {
        commits.value = []
        isGit.value = false
        return
      }

      isGit.value = true

      // Check working tree changes
      const wtResp = await req('/api/git/working-tree', seq)
      let loadedWtFiles: GitFile[] = []
      if (wtResp.ok) {
        const wt = await wtResp.json()
        loadedWtFiles = wt.files || []
        wtFiles.value = loadedWtFiles
      }

      if (!historySeq.isCurrent(seq)) return // superseded while working-tree was in flight

      const histCommits: GitCommit[] = data.commits || []

      // Prepend working tree entry if there are uncommitted changes
      if (loadedWtFiles.length > 0) {
        commits.value = [
          { sha: 'HEAD', msg: t('git.history.workingTreeChanges'), date: '', author: '', isWT: true },
          ...histCommits,
        ]
      } else {
        commits.value = histCommits
      }
      hasMore.value = data.hasMore
      // Let the host record whatever it uses to decide if a re-entry needs a
      // reload (GitHistoryContent snapshots the git branch/dirty state here).
      onProjectHistoryLoaded?.()
    } catch (err) {
      // A superseded request's abort must not surface as an error.
      if (err instanceof Error && err.name === 'AbortError') return
      if (!historySeq.isCurrent(seq)) return
      // Timeout: the request is not coming back — surface a distinct message
      // so the user can retry instead of staring at an endless spinner.
      commits.value = []
      if (err instanceof GitTimeoutError) {
        appLog.w('GitHistory', err.message)
        error.value = t('git.history.loadTimeout')
        return
      }
      error.value = t('git.history.loadError')
    } finally {
      // Only the latest request may clear the loading flag.
      if (historySeq.isCurrent(seq)) {
        loading.value = false
        fullReloading.value = false
      }
    }
  }

  async function loadFileHistory(path: string): Promise<void> {
    const seq = historySeq.token()
    // Keep the existing list visible during background refreshes (see
    // loadProjectHistory) so the refresh button's spin stays visible.
    const isFirstLoad = shouldShowFullLoading(commits.value, error.value)
    loading.value = isFirstLoad
    fullReloading.value = !isFirstLoad
    error.value = ''
    if (isFirstLoad) commits.value = []
    selectedSHA.value = null
    isGit.value = true
    untracked.value = false

    try {
      const resp = await req(`/api/git/history?path=${encodeURIComponent(path)}`, seq)
      if (!historySeq.isCurrent(seq)) return
      if (!resp.ok) {
        const data = await resp.json()
        commits.value = []
        error.value = data.error || t('git.history.loadError')
        return
      }
      const hist = await resp.json()
      if (!historySeq.isCurrent(seq)) return
      if (!hist.isGit) {
        commits.value = []
        isGit.value = false
        return
      }
      isGit.value = true
      untracked.value = !!hist.untracked

      // Prepend a working-tree entry only when this specific file has
      // uncommitted changes. Otherwise file history shows commits only.
      let hasUncommitted = false
      const wtResp = await req(`/api/git/working-tree?path=${encodeURIComponent(path)}`, seq)
      if (wtResp.ok) {
        const wt = await wtResp.json()
        hasUncommitted = !!wt.hasUncommitted
      }

      if (!historySeq.isCurrent(seq)) return // superseded while working-tree was in flight

      const histCommits: GitCommit[] = hist.commits || []
      commits.value = buildFileHistoryCommits(histCommits, hasUncommitted, t('git.history.workingTreeChanges'))
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      if (!historySeq.isCurrent(seq)) return
      commits.value = []
      if (err instanceof GitTimeoutError) {
        appLog.w('GitHistory', err.message)
        error.value = t('git.history.loadTimeout')
        return
      }
      error.value = t('git.history.loadError')
    } finally {
      if (historySeq.isCurrent(seq)) {
        loading.value = false
        fullReloading.value = false
      }
    }
  }

  async function loadMoreCommits(): Promise<void> {
    // Skip while a full reload is in flight: loading replaces the commit list
    // and loadMore would paginate the OLD commits with stale skip counts.
    if (loading.value || fullReloading.value || loadingMore.value || !hasMore.value || !isGit.value) return
    loadingMore.value = true
    hasLoadedMore.value = true
    try {
      // Count only git commits (exclude WT node) for the skip parameter,
      // since WT is a frontend-only entry not present in git log output.
      const gitCount = commits.value.filter(c => !c.isWT).length
      const resp = await req(`/api/git/project-history?skip=${gitCount}`)
      if (!resp.ok) return
      const data = await resp.json()
      commits.value.push(...(data.commits || []))
      hasMore.value = data.hasMore
    } catch {
      // ignore
    } finally {
      loadingMore.value = false
    }
  }

  // When searching, auto-load all commits so filtering covers the full history
  async function onSearch(q: string): Promise<void> {
    if (!q.trim() || !isGit.value || readMode() === 'file') return
    const seq = historySeq.token()
    searchLoading.value = true
    if (hasMore.value) hasLoadedMore.value = true
    try {
      while (hasMore.value) {
        if (!historySeq.isCurrent(seq)) return // superseded by a refresh/load
        const gitCount = commits.value.filter(c => !c.isWT).length
        const resp = await req(`/api/git/project-history?skip=${gitCount}`, seq)
        if (!resp.ok) break
        const data = await resp.json()
        commits.value.push(...(data.commits || []))
        hasMore.value = data.hasMore
      }
    } catch (err) {
      if (err instanceof GitTimeoutError) {
        appLog.w('GitHistory', err.message)
      }
      // Search is best-effort: ignore failures, the already-loaded commits remain visible.
    } finally {
      searchLoading.value = false
    }
  }

  async function onRefresh(): Promise<void> {
    commitSearch.value = ''
    hasLoadedMore.value = false
    refreshHint.value = false
    const list = commitListRef.value as { commitSearch?: string; observeList?: () => void } | null
    if (list) list.commitSearch = ''
    const path = filePath?.()
    if (readMode() === 'file' && path) {
      await loadFileHistory(path)
    } else {
      await loadProjectHistory()
    }
  }

  // ─── Drill-down navigation ───────────────────────────────────────────────

  function onCommitSelect(c: GitCommit) {
    selectedSHA.value = c.sha

    if (readMode() === 'project') {
      // Project mode: commit → files list
      currentView.value = 'files'
      if (c.sha === 'HEAD') {
        // Always re-fetch instead of reading the wtFiles snapshot: the snapshot
        // is only refreshed by loadProjectHistory, so entering the working-tree
        // view after the workspace changed would otherwise render stale (or
        // empty, when a background refresh just cleared it).
        files.value = wtFiles.value // show the cached list immediately (no flash)
        mergeGroups.value = []
        loadWorkingTreeFiles()
      } else {
        loadCommitFiles(c.sha).catch(() => {})
      }
    } else {
      // File mode: commit → diff
      currentView.value = 'diff'
      loadDiff()
    }
  }

  function drillBack(view: GitHistoryView) {
    if (view === 'commits') {
      selectedSHA.value = null
      files.value = []
      mergeGroups.value = []
      selectedFilePath.value = null
      diffState.value = { loading: false, empty: false, html: '' }
      handleDrillBackToCommits()
    } else if (view === 'files') {
      selectedFilePath.value = null
      diffState.value = { loading: false, empty: false, html: '' }
    }
    currentView.value = view
  }

  function drillToFile(f: GitFile) {
    selectedFilePath.value = f.path
    currentView.value = 'diff'
    loadDiff()
  }

  // ─── Diff loading ────────────────────────────────────────────────────────

  async function loadCommitFiles(sha: string): Promise<void> {
    filesLoading.value = true
    files.value = []
    mergeGroups.value = []
    try {
      const resp = await gitFetch(`/api/git/commit-files?sha=${encodeURIComponent(sha)}`)
      if (!resp.ok) { files.value = []; return }
      const data = await resp.json()
      if (data && data.merge === true && Array.isArray(data.groups)) {
        mergeGroups.value = data.groups
        files.value = []
      } else if (Array.isArray(data)) {
        files.value = data
        mergeGroups.value = []
      } else {
        files.value = []
        mergeGroups.value = []
      }
    } catch {
      files.value = []
      mergeGroups.value = []
    } finally {
      filesLoading.value = false
    }
  }

  async function loadDiff(): Promise<void> {
    diffState.value = { loading: true, empty: false, html: '' }

    try {
      let resp: Response
      if (readMode() === 'project') {
        resp = await gitFetch(
          `/api/git/file-diff?sha=${encodeURIComponent(selectedSHA.value as string)}&path=${encodeURIComponent(selectedFilePath.value as string)}`
        )
      } else {
        resp = await gitFetch(
          `/api/git/diff?path=${encodeURIComponent(filePath?.() as string)}&commit=${encodeURIComponent(selectedSHA.value as string)}`
        )
      }
      if (!resp.ok) {
        diffState.value = { loading: false, empty: true, html: '' }
        return
      }
      const data = await resp.json()
      if (data.empty) {
        diffState.value = { loading: false, empty: true, html: '' }
      } else {
        const path = readMode() === 'project' ? (selectedFilePath.value as string) : (filePath?.() as string)
        diffState.value = { loading: false, empty: false, html: renderDiff(data.diff || '', path) }
      }
    } catch {
      diffState.value = { loading: false, empty: true, html: '' }
    }
  }

  /**
   * Fetch the working tree change list into the files view.
   *
   * The full-screen spinner is only used when there is no cached list to keep
   * on screen — otherwise the refresh happens in place so the list does not
   * flash.
   */
  async function loadWorkingTreeFiles({ inPlace = false }: { inPlace?: boolean } = {}): Promise<void> {
    const seq = historySeq.token()
    const showSpinner = !inPlace && files.value.length === 0
    filesLoading.value = showSpinner
    filesRefreshing.value = !showSpinner
    try {
      const resp = await req('/api/git/working-tree', seq)
      if (!historySeq.isCurrent(seq)) return
      if (!resp.ok) {
        if (showSpinner) files.value = []
        return
      }
      const data = await resp.json()
      if (!historySeq.isCurrent(seq)) return
      const loaded: GitFile[] = data.files || []
      wtFiles.value = loaded
      files.value = loaded
      mergeGroups.value = []
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      if (!historySeq.isCurrent(seq)) return
      appLog.w('GitHistory', `working-tree load failed: ${err instanceof Error ? err.message : err}`)
    } finally {
      // This request holds the latest token, so it also owns the flags it may
      // have superseded (an aborted loadProjectHistory cannot clear them itself).
      if (historySeq.isCurrent(seq)) {
        loading.value = false
        fullReloading.value = false
        filesLoading.value = false
        filesRefreshing.value = false
      }
    }
  }

  // Refresh button in the files-view header. Only the working-tree view is
  // re-fetchable in place — a historical commit's file list is immutable, so
  // for those the button re-runs the commit-files load.
  function onFilesRefresh() {
    if (isWorkingTree.value) {
      loadWorkingTreeFiles({ inPlace: true })
    } else {
      loadCommitFiles(selectedSHA.value as string).catch(() => {})
    }
  }

  /**
   * Reload history while preserving the current drill-down.
   *
   * loadProjectHistory()/loadFileHistory() clear selectedSHA/files/mergeGroups
   * but leave currentView untouched, so a host that reloads while the user is
   * drilled into a commit (or into the working-tree file list) would render the
   * emptied state as a blank list/diff. Snapshot the drill-down, reload, then
   * restore it and re-fetch the mutable (working-tree) part.
   */
  async function reloadPreservingDrillDown(): Promise<void> {
    const prev = {
      view: currentView.value,
      sha: selectedSHA.value,
      files: files.value,
      groups: mergeGroups.value,
      path: selectedFilePath.value,
    }
    const wasDrilled = prev.view === 'files' || prev.view === 'diff'

    const path = filePath?.()
    if (readMode() === 'file' && path) {
      await loadFileHistory(path)
    } else {
      await loadProjectHistory()
    }

    if (!wasDrilled) return

    if (!commits.value.some(c => c.sha === prev.sha)) {
      // The drilled-into entry is gone (workspace became clean, or the commit is
      // no longer reachable) — fall back to the commit list. Set the view
      // directly rather than via drillBack(): the load already cleared
      // selectedSHA/files, and drillBack would fire a redundant history request
      // for single-commit repos.
      currentView.value = 'commits'
      return
    }

    // Restore the snapshot so the list stays on screen while the working-tree
    // re-fetch runs (no empty-state flash).
    selectedSHA.value = prev.sha
    selectedFilePath.value = prev.path
    files.value = prev.files
    mergeGroups.value = prev.groups
    currentView.value = prev.view
    if (prev.sha === 'HEAD' && prev.view === 'files') {
      await loadWorkingTreeFiles({ inPlace: true })
    } else if (prev.view === 'diff') {
      loadDiff()
    }
  }

  // ─── Shared commit navigation ────────────────────────────────────────────
  // Lives here (not in the hosts) because its callbacks are exactly this
  // composable's own loaders.

  const { navigateToCommit, handleDrillBackToCommits } = useCommitNavigation({
    commits,
    selectedSHA,
    currentView,
    loadCommitFiles,
    loadProjectHistory,
  })

  return {
    loading, fullReloading, error, commits, hasMore, searchLoading, loadingMore, isGit, untracked,
    currentView, selectedSHA, filesLoading, filesRefreshing, files, mergeGroups, selectedFilePath,
    diffState, wtFiles, commitSearch, hasLoadedMore, refreshHint, commitListRef,
    selectedCommit, isWorkingTree, mode, sortedFiles, stagedFiles, unstagedFiles,
    hasStaged, hasUnstaged, totalFileCount, workingTreeFileCount,
    fileTypeLabel, fileSplit, badgeClass, resetListState,
    loadProjectHistory, loadFileHistory, loadMoreCommits, onSearch, onRefresh,
    loadCommitFiles, loadDiff, loadWorkingTreeFiles, onFilesRefresh, reloadPreservingDrillDown,
    onCommitSelect, drillBack, drillToFile, navigateToCommit,
  }
}
