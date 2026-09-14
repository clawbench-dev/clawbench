import { ref, computed } from 'vue'
import {
    fetchForgeItems,
    fetchForgeItem,
    fetchForgeComments,
    type ForgeItem,
    type ForgeComment,
    ForgeApiError,
} from '@/utils/forgeApi'
import { appLog } from '@/utils/appLog'
import { setForgeBindingState, useForgeBinding } from '@/composables/useForgeBinding'

const TAG = 'UseForge'

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
    const state = ref<'open' | 'closed' | 'all'>('open')
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

    return {
        items, binding, loading, loadingMore, error,
        type, state, mineFilter, query,
        hasMore, nextPage, isBound, isEmpty,
        loadBinding, load, loadMore, setType, setState, setMineFilter, setQuery,
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
