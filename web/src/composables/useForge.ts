import { ref, computed } from 'vue'
import {
    fetchForgeItems,
    fetchForgeItem,
    fetchForgeComments,
    fetchForgePipelines,
    fetchForgePipeline,
    fetchForgeUnreadItems,
    markForgeRead,
    type ForgeItem,
    type ForgeComment,
    type ForgePipelineRun,
    type ForgeUnreadItem,
    type ForgeActivityFilter,
    type ForgePipelineJob,
    type ForgePipelineStatus,
    ForgeApiError,
} from '@/utils/forgeApi'
import { appLog } from '@/utils/appLog'
import { setForgeBindingState, useForgeBinding } from '@/composables/useForgeBinding'
import { useForgeUnread } from '@/composables/useForgeUnread' 

const TAG = 'UseForge'

/**
 * The item key the server uses to identify an issue/PR row for read state.
 *
 * Must match forge.ItemKeyForNumber on the Go side: "<type>/<number>". Kept as a
 * named function so the shape lives in one place rather than being re-spelled at
 * each call site.
 */
export function forgeItemKey(item: { type: string; number: number }): string {
    return `${item.type}/${item.number}`
}

/** The item key for one CI run, matching forge.PipelineItemKey on the Go side. */
export function forgePipelineItemKey(runID: number): string {
    return `pipeline/run:${runID}`
}

export type ForgeFilter = 'all' | 'assigned' | 'created' | 'review'

/**
 * useForgeItems manages the issue/PR list for the currently selected project.
 *
 * It is a per-instance composable (not a module singleton): the panel is a
 * single component and there is no cross-tab state to share, unlike chat
 * context. Callers pass a getter for the active project so the composable can
 * reload when the project changes.
 */
export function useForgeItems(getProjectPath: () => string) {
    const items = ref<ForgeItem[]>([])
    const loading = ref(false)
    const loadingMore = ref(false)
    const error = ref<{ message: string; code: string } | null>(null)

    const type = ref<'issue' | 'pr'>('issue')
    // "merged" is only meaningful for change requests; issues have no merged
    // lifecycle, so the chip is hidden on the issues tab (see stateOptions in
    // ForgePanelContent) rather than shown and always empty.
    const state = ref<'open' | 'closed' | 'merged' | 'all'>('open')
    const mineFilter = ref<ForgeFilter>('all')
    const query = ref('')

    const hasMore = ref(false)
    const nextPage = ref(1)
    let requestSeq = 0
    let abort: AbortController | null = null

    // The binding is a module-level singleton shared with the dock icon and the
    // task views. Reading it directly (rather than mirroring it into a local
    // ref) means the panel can never show a repository another consumer has
    // already refreshed away.
    const forge = useForgeBinding()
    const binding = forge.binding

    const isBound = computed(() => binding.value !== null)
    const isEmpty = computed(() => !loading.value && !error.value && isBound.value && items.value.length === 0)

    /**
     * Load the binding through the shared store.
     *
     * Pass `force` after a write (bind/unbind): the server state is known to
     * have changed, so the cached value would be stale.
     */
    async function loadBinding(force = false) {
        await forge.refresh(force)
    }

    /** Reload the list from page 1, honouring the current filters. */
    async function load() {
        const project = getProjectPath()
        if (!project) {
            items.value = []
            return
        }
        // Only the latest request may write state (seq guard).
        const seq = ++requestSeq
        abort?.abort()
        abort = new AbortController()

        loading.value = true
        error.value = null
        try {
            const res = await fetchForgeItems({
                type: type.value,
                state: state.value,
                page: 1,
                query: query.value,
                mine: mineFilter.value !== 'all',
                mineScope: mineFilter.value === 'all' ? undefined : mineFilter.value,
                signal: abort.signal,
            })
            if (seq !== requestSeq) return
            items.value = res.items
            // The list response carries the binding it was served for, so this
            // is a free confirmation — route it through the shared setter so
            // the resolved flag and cache timestamp stay consistent.
            //
            // Safety note: this runs only for the newest request (seq guard
            // above), and a bind/unbind triggers refresh(true) + load(), which
            // bumps requestSeq. So a list response that started before a manual
            // bind cannot land here afterwards and overwrite it.
            setForgeBindingState(res.binding)
            hasMore.value = res.hasMore
            nextPage.value = res.nextPage
        } catch (err) {
            if (seq !== requestSeq) return
            items.value = []
            if (err instanceof ForgeApiError) {
                error.value = { message: err.message, code: err.code }
            } else {
                error.value = { message: String(err), code: 'ForgeError' }
            }
        } finally {
            if (seq === requestSeq) loading.value = false
        }
    }

    /** Append the next page (infinite scroll). */
    async function loadMore() {
        if (!hasMore.value || loadingMore.value || loading.value) return
        const project = getProjectPath()
        if (!project) return
        loadingMore.value = true
        try {
            const res = await fetchForgeItems({
                type: type.value,
                state: state.value,
                page: nextPage.value,
                query: query.value,
                mine: mineFilter.value !== 'all',
                mineScope: mineFilter.value === 'all' ? undefined : mineFilter.value,
            })
            items.value = [...items.value, ...res.items]
            hasMore.value = res.hasMore
            nextPage.value = res.nextPage
        } catch (err) {
            appLog.w(TAG, 'loadMore failed', err)
        } finally {
            loadingMore.value = false
        }
    }

    function setType(t: 'issue' | 'pr') {
        if (type.value === t) return
        type.value = t
        // Switching away from change requests must not leave a filter the new
        // tab cannot express selected (issues have no merged state), or the
        // list would come back permanently empty with no visible cause.
        if (t === 'issue' && state.value === 'merged') state.value = 'open'
        void load()
    }
    function setState(s: 'open' | 'closed' | 'merged' | 'all') {
        if (state.value === s) return
        state.value = s
        void load()
    }
    function setMineFilter(f: ForgeFilter) {
        if (mineFilter.value === f) return
        mineFilter.value = f
        void load()
    }
    function setQuery(q: string) {
        query.value = q
        void load()
    }

    /**
     * Mark one item read (what opening a row does).
     *
     * The local flag is cleared optimistically so the dot disappears at once;
     * the badge is then re-derived from the server rather than decremented, so a
     * missed event cannot leave it permanently wrong.
     */
    async function markItemRead(item: ForgeItem): Promise<void> {
        if (!item.unread) return
        item.unread = false
        try {
            await markForgeRead(forgeItemKey(item))
            useForgeUnread().refresh()
        } catch (err) {
            appLog.w(TAG, 'markItemRead failed', err)
            // Restore the flag so the UI does not claim it was seen.
            item.unread = true
        }
    }

    /** Mark every item in the bound repository read. */
    async function markAllRead(): Promise<void> {
        // Snapshot so a failed write can be undone — otherwise the list claims
        // everything was seen while the server still holds it unread.
        const before = items.value.map(it => it.unread)
        for (const it of items.value) it.unread = false
        try {
            // Shared with the badge and the pipeline list: one repo-wide write,
            // not one per caller.
            await useForgeUnread().markAllRead()
        } catch (err) {
            appLog.w(TAG, 'markAllRead failed', err)
            items.value.forEach((it, i) => { it.unread = before[i] })
            return
        }
        // Re-derive rather than assume: the server is authoritative.
        void load()
    }

    return {
        items, binding, loading, loadingMore, error,
        type, state, mineFilter, query,
        hasMore, nextPage, isBound, isEmpty,
        loadBinding, load, loadMore, setType, setState, setMineFilter, setQuery,
        markItemRead, markAllRead,
    }
}

/**
 * useForgeDetail loads a single item and its comments, with upward paging for
 * long comment threads (newest page first, then load older pages upward).
 */
export function useForgeDetail() {
    const item = ref<ForgeItem | null>(null)
    const comments = ref<ForgeComment[]>([])
    const loading = ref(false)
    const loadingComments = ref(false)
    const error = ref<{ message: string; code: string } | null>(null)
    const hasMoreComments = ref(false)
    let commentPage = 1
    let abort: AbortController | null = null

    async function open(type: 'issue' | 'pr', number: number) {
        abort?.abort()
        abort = new AbortController()
        loading.value = true
        error.value = null
        item.value = null
        comments.value = []
        hasMoreComments.value = false
        commentPage = 1
        try {
            const res = await fetchForgeItem(type, number, abort.signal)
            item.value = res.item
            await loadComments(type, number, true)
        } catch (err) {
            if (err instanceof ForgeApiError) {
                error.value = { message: err.message, code: err.code }
            } else {
                error.value = { message: String(err), code: 'ForgeError' }
            }
        } finally {
            loading.value = false
        }
    }

    async function loadComments(type: 'issue' | 'pr', number: number, first: boolean) {
        loadingComments.value = true
        try {
            const res = await fetchForgeComments(type, number, commentPage, 30)
            // The provider returns comments oldest-first per page. The first
            // page shows the newest batch, so prepend it; later pages (older
            // comments) are prepended above what is already shown.
            comments.value = first ? res.comments : [...res.comments, ...comments.value]
            hasMoreComments.value = res.comments.length >= 30
            commentPage += 1
        } catch (err) {
            appLog.w(TAG, 'loadComments failed', err)
        } finally {
            loadingComments.value = false
        }
    }

    async function loadOlderComments() {
        if (!item.value || !hasMoreComments.value || loadingComments.value) return
        await loadComments(item.value.type, item.value.number, false)
    }

    function close() {
        abort?.abort()
        item.value = null
        comments.value = []
        error.value = null
    }

    return { item, comments, loading, loadingComments, error, hasMoreComments, open, loadOlderComments, close }
}

/**
 * Status filters offered on the Pipelines tab, in display order.
 *
 * "all" comes first because it is the default: the active filter should be the
 * leftmost chip, and the order reads as "everything → narrowing".
 */
export const FORGE_PIPELINE_FILTERS = ['all', 'failure', 'running'] as const
export type ForgePipelineFilter = typeof FORGE_PIPELINE_FILTERS[number]

/**
 * useForgePipelines lists CI runs for the project's bound repository.
 *
 * Mirrors useForgeItems' shape (paging, loading/error state, a seq guard so
 * only the latest request writes) but is a separate composable because the
 * filter vocabulary and the row data are entirely different from issues/PRs.
 *
 * The default filter is "all": the list is a history, and hiding runs by default
 * made the tab look empty (or stale) whenever nothing happened to be failing.
 * A user who wants only the breakage picks "failure" explicitly.
 */
export function useForgePipelines(getProjectPath: () => string) {
    const pipelines = ref<ForgePipelineRun[]>([])
    const loading = ref(false)
    const loadingMore = ref(false)
    const error = ref<{ message: string; code: string } | null>(null)
    const filter = ref<ForgePipelineFilter>('all')
    const hasMore = ref(false)
    const nextPage = ref(1)

    let requestSeq = 0
    let abort: AbortController | null = null

    /** The `status` query value; "all" means no filter. */
    function statusParam(): ForgePipelineStatus | undefined {
        return filter.value === 'all' ? undefined : filter.value
    }

    async function load() {
        const project = getProjectPath()
        if (!project) {
            pipelines.value = []
            return
        }
        const seq = ++requestSeq
        abort?.abort()
        abort = new AbortController()

        loading.value = true
        error.value = null
        // A reload supersedes any in-flight append: that append will bail on the
        // seq check without clearing its flag, so clear it here or the spinner
        // would stay on and loadMore would refuse to run again.
        loadingMore.value = false
        try {
            const res = await fetchForgePipelines({
                status: statusParam(),
                page: 1,
                signal: abort.signal,
            })
            if (seq !== requestSeq) return
            pipelines.value = res.pipelines
            hasMore.value = res.hasMore
            nextPage.value = res.nextPage
        } catch (err) {
            if (seq !== requestSeq) return
            pipelines.value = []
            if (err instanceof ForgeApiError) {
                error.value = { message: err.message, code: err.code }
            } else {
                error.value = { message: String(err), code: 'ForgeError' }
            }
        } finally {
            if (seq === requestSeq) loading.value = false
        }
    }

    /** Append the next page (infinite scroll). */
    async function loadMore() {
        if (!hasMore.value || loadingMore.value || loading.value) return
        if (!getProjectPath()) return

        // Capture the sequence BEFORE awaiting. A filter change (or any reload)
        // bumps requestSeq, and an in-flight append must not then land in the
        // new list — it was fetched for the previous filter, so its rows belong
        // to a result set the user has already navigated away from.
        const seq = requestSeq
        loadingMore.value = true
        try {
            const res = await fetchForgePipelines({
                status: statusParam(),
                page: nextPage.value,
            })
            if (seq !== requestSeq) return
            pipelines.value = [...pipelines.value, ...res.pipelines]
            hasMore.value = res.hasMore
            nextPage.value = res.nextPage
        } catch (err) {
            if (seq !== requestSeq) return
            appLog.w(TAG, 'loadMore pipelines failed', err)
        } finally {
            // Only clear the flag for the request that still owns the list;
            // otherwise a stale response would clear a newer request's spinner.
            if (seq === requestSeq) loadingMore.value = false
        }
    }

    function setFilter(f: ForgePipelineFilter) {
        if (filter.value === f) return
        filter.value = f
        void load()
    }

    /** Mark one run read (what opening a row does). */
    async function markItemRead(run: ForgePipelineRun): Promise<void> {
        if (!run.unread) return
        run.unread = false
        try {
            await markForgeRead(forgePipelineItemKey(run.id))
            useForgeUnread().refresh()
        } catch (err) {
            appLog.w(TAG, 'markPipelineRead failed', err)
            run.unread = true
        }
    }

    /** Mark every run in the bound repository read. */
    async function markAllRead(): Promise<void> {
        const before = pipelines.value.map(r => r.unread)
        for (const r of pipelines.value) r.unread = false
        try {
            await useForgeUnread().markAllRead()
        } catch (err) {
            appLog.w(TAG, 'markAllPipelinesRead failed', err)
            pipelines.value.forEach((r, i) => { r.unread = before[i] })
            return
        }
        void load()
    }

    return {
        pipelines, loading, loadingMore, error, filter, hasMore, nextPage,
        load, loadMore, setFilter,
        markItemRead, markAllRead,
    }
}

/**
 * useForgePipelineDetail loads one run and its jobs.
 *
 * Jobs are best-effort server-side, so an empty list is a normal outcome rather
 * than an error.
 */
export function useForgePipelineDetail() {
    const run = ref<ForgePipelineRun | null>(null)
    const jobs = ref<ForgePipelineJob[]>([])
    const loading = ref(false)
    const error = ref<{ message: string; code: string } | null>(null)
    let abort: AbortController | null = null

    async function open(id: number) {
        abort?.abort()
        abort = new AbortController()
        loading.value = true
        error.value = null
        run.value = null
        jobs.value = []
        try {
            const res = await fetchForgePipeline(id, abort.signal)
            run.value = res.pipeline
            jobs.value = res.jobs ?? []
        } catch (err) {
            if (err instanceof ForgeApiError) {
                error.value = { message: err.message, code: err.code }
            } else {
                error.value = { message: String(err), code: 'ForgeError' }
            }
        } finally {
            loading.value = false
        }
    }

    function close() {
        abort?.abort()
        run.value = null
        jobs.value = []
        error.value = null
    }

    return { run, jobs, loading, error, open, close }
}

/**
 * Read-state filters offered on the activity tab, in display order.
 *
 * "unread" comes first because it is the default: the active chip should be the
 * leftmost one, and the order reads as "what needs me → what I have seen".
 */
export const FORGE_ACTIVITY_FILTERS: readonly ForgeActivityFilter[] = ['unread', 'read', 'all']

/**
 * useForgeUnreadItems lists the activity items for the project's bound
 * repository, for the activity tab.
 *
 * Kept apart from useForgeUnread (which owns the dock badge): the badge is a
 * single number refreshed on every live event, so it must stay a cheap count.
 * These rows are only fetched while the activity view is on screen.
 *
 * The filter is applied SERVER-side rather than by trimming the returned rows.
 * The read/unread split is an aggregate over each item's events ("all of them
 * read"), which the client cannot recompute from a page of rows — and filtering
 * client-side would silently show an empty "read" view whenever the unread set
 * happened to fill the page.
 */
export function useForgeUnreadItems(getProjectPath: () => string) {
    const items = ref<ForgeUnreadItem[]>([])
    const loading = ref(false)
    const error = ref<{ message: string; code: string } | null>(null)
    /** False until the first load settles, so the panel can tell "empty" from
     *  "not fetched yet" and avoid flashing the empty state. */
    const loaded = ref(false)
    /** Which read state is shown. Defaults to unread — the reason to look. */
    const filter = ref<ForgeActivityFilter>('unread')

    let requestSeq = 0
    let abort: AbortController | null = null

    async function load() {
        const project = getProjectPath()
        if (!project) {
            items.value = []
            loaded.value = true
            return
        }
        const seq = ++requestSeq
        abort?.abort()
        abort = new AbortController()

        loading.value = true
        error.value = null
        try {
            const res = await fetchForgeUnreadItems(filter.value, abort.signal)
            if (seq !== requestSeq) return
            items.value = res.items ?? []
            loaded.value = true
        } catch (err) {
            if (seq !== requestSeq) return
            items.value = []
            // A failed load must not look like "nothing unread".
            loaded.value = false
            if (err instanceof ForgeApiError) {
                error.value = { message: err.message, code: err.code }
            } else {
                error.value = { message: String(err), code: 'ForgeError' }
            }
        } finally {
            if (seq === requestSeq) loading.value = false
        }
    }

    /** Switch the read-state filter and reload. */
    function setFilter(f: ForgeActivityFilter) {
        if (filter.value === f) return
        filter.value = f
        // Rows from the previous view would otherwise linger until the response
        // lands, showing e.g. read rows under the "unread" chip.
        items.value = []
        loaded.value = false
        void load()
    }

    /**
     * Mark every item read and clear the list.
     *
     * Delegates to the shared badge composable so the list and the dock badge
     * settle from the same server response and cannot disagree.
     */
    async function markAllRead(): Promise<void> {
        await useForgeUnread().markAllRead()
        items.value = []
    }

    /**
     * Mark one row read, server-side, without removing it.
     *
     * The key is passed through VERBATIM: a pipeline's number is 0, so rebuilding
     * it from type+number would send "pipeline/0" and mark nothing.
     *
     * The row is greyed out rather than spliced: removing it would shift every
     * row below the cursor mid-click. It drops on the next load().
     *
     * Only `locallyRead` is touched. Writing the server field `read` here would
     * make a failed write indistinguishable from a successful one — the
     * rollback below relies on being able to tell what the server actually said.
     */
    async function markRowRead(itemKey: string) {
        const row = items.value.find(r => r.itemKey === itemKey)
        if (row) row.locallyRead = true
        try {
            await markForgeRead(itemKey)
            // Re-derive the badge rather than decrementing, so a missed event
            // cannot leave it permanently wrong.
            useForgeUnread().refresh()
        } catch (err) {
            appLog.w(TAG, 'markRowRead failed', err)
            // Restore, so the row does not claim it was seen.
            if (row) row.locallyRead = false
        }
    }

    return { items, loading, loaded, error, filter, load, setFilter, markAllRead, markRowRead }
}
