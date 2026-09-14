import { ref, computed } from 'vue'
import {
    fetchForgeItems,
    fetchForgeItem,
    fetchForgeComments,
    fetchForgeBinding,
    fetchForgePipelines,
    fetchForgePipeline,
    markForgeRead,
    type ForgeItem,
    type ForgeComment,
    type ForgeBinding,
    type ForgePipelineRun,
    type ForgePipelineJob,
    type ForgePipelineStatus,
    ForgeApiError,
} from '@/utils/forgeApi'
import { appLog } from '@/utils/appLog'
import { setForgeBindingState } from '@/composables/useForgeBinding'
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
    const binding = ref<ForgeBinding | null>(null)
    const suggested = ref<Record<string, string> | null>(null)
    const loading = ref(false)
    const loadingMore = ref(false)
    const error = ref<{ message: string; code: string } | null>(null)

    const type = ref<'issue' | 'pr'>('issue')
    const state = ref<'open' | 'closed' | 'all'>('open')
    const mineFilter = ref<ForgeFilter>('all')
    const query = ref('')

    const hasMore = ref(false)
    const nextPage = ref(1)
    let requestSeq = 0
    let abort: AbortController | null = null

    const isBound = computed(() => binding.value !== null)
    const isEmpty = computed(() => !loading.value && !error.value && isBound.value && items.value.length === 0)

    /** Load the binding (and a suggested binding when unbound). */
    async function loadBinding() {
        try {
            const res = await fetchForgeBinding()
            binding.value = res.binding
            suggested.value = res.suggested ?? null
        } catch (err) {
            appLog.w(TAG, 'loadBinding failed', err)
            binding.value = null
        }
        // Keep the shared binding in sync so the dock icon follows the platform
        // (the dock lives in App.vue, outside this composable's scope).
        setForgeBindingState(binding.value)
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
            binding.value = res.binding
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
        void load()
    }
    function setState(s: 'open' | 'closed' | 'all') {
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

    function reset() {
        items.value = []
        binding.value = null
        error.value = null
        hasMore.value = false
        nextPage.value = 1
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
        for (const it of items.value) it.unread = false
        try {
            await markForgeRead()
        } catch (err) {
            appLog.w(TAG, 'markAllRead failed', err)
        }
        // Re-derive rather than assume: the server is authoritative.
        useForgeUnread().refresh()
        void load()
    }

    return {
        items, binding, suggested, loading, loadingMore, error,
        type, state, mineFilter, query,
        hasMore, nextPage, isBound, isEmpty,
        loadBinding, load, loadMore, setType, setState, setMineFilter, setQuery, reset,
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

/** Status filters offered on the Pipelines tab, in display order. */
export const FORGE_PIPELINE_FILTERS = ['failure', 'running', 'all'] as const
export type ForgePipelineFilter = typeof FORGE_PIPELINE_FILTERS[number]

/**
 * useForgePipelines lists CI runs for the project's bound repository.
 *
 * Mirrors useForgeItems' shape (paging, loading/error state, a seq guard so
 * only the latest request writes) but is a separate composable because the
 * filter vocabulary and the row data are entirely different from issues/PRs.
 *
 * The default filter is "failure": a busy repository produces far more green
 * runs than anyone wants to scroll through, and the reason to open this tab is
 * almost always to find out what broke.
 */
export function useForgePipelines(getProjectPath: () => string) {
    const pipelines = ref<ForgePipelineRun[]>([])
    const loading = ref(false)
    const loadingMore = ref(false)
    const error = ref<{ message: string; code: string } | null>(null)
    const filter = ref<ForgePipelineFilter>('failure')
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
        for (const r of pipelines.value) r.unread = false
        try {
            await markForgeRead()
        } catch (err) {
            appLog.w(TAG, 'markAllPipelinesRead failed', err)
        }
        useForgeUnread().refresh()
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
