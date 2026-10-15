import { ref, computed } from 'vue'
import {
    fetchNotifications,
    clearNotifications,
    type NotificationItem,
    type NotificationKind,
    type NotificationRange,
} from '@/utils/notificationsApi'
import { appLog } from '@/utils/appLog'

const TAG = 'UseNotifications'

/** Page size for the list. Matches the server's default. */
const PAGE_SIZE = 50

/** The "no filter" sentinel for the project dropdown (all projects). */
export const ALL_PROJECTS = ''

/**
 * useNotifications drives the notification-center panel.
 *
 * State is module-level so the panel and any future consumer share one list.
 * There is no unread state and no badge to keep in sync — the log has neither
 * (see the design doc), which is what keeps this composable small.
 *
 * Scope is GLOBAL by default: the log spans all projects, and the project
 * dropdown narrows it. That is deliberate — the notification center answers
 * "what happened" across the whole app, unlike the per-project forge/task
 * panels. The active project is offered as one of the options, not as the
 * implicit default.
 */
const items = ref<NotificationItem[]>([])
const total = ref(0)
const hasMore = ref(false)
const loading = ref(false)
const loadingMore = ref(false)
const error = ref('')

// Active filters. Held here (not in the component) so they survive the panel
// being hidden by TabPanel's v-show and so a reload keeps the user's view.
const filterKind = ref<NotificationKind | ''>('')
const filterRange = ref<NotificationRange | ''>('')
const filterProject = ref<string>(ALL_PROJECTS)

// Project options for the dropdown: [{id, path}]. Loaded once on first use.
const projectOptions = ref<{ id: number; path: string }[]>([])
let projectsLoaded = false

// Monotonic request sequence: only the newest load may write state, so a slow
// earlier response cannot clobber a newer filter change.
let loadSeq = 0

function currentQuery() {
    return {
        project: filterProject.value || undefined,
        kind: (filterKind.value || undefined) as NotificationKind | undefined,
        range: (filterRange.value || undefined) as NotificationRange | undefined,
        limit: PAGE_SIZE,
    }
}

export function useNotifications() {
    /** Load the first page for the current filters. */
    async function load() {
        const seq = ++loadSeq
        loading.value = true
        error.value = ''
        try {
            const page = await fetchNotifications(currentQuery())
            if (seq !== loadSeq) return // superseded
            items.value = page.items ?? []
            total.value = page.total ?? 0
            hasMore.value = page.hasMore ?? false
        } catch (err) {
            if (seq !== loadSeq) return
            appLog.w(TAG, 'load failed', err)
            error.value = err instanceof Error ? err.message : String(err)
            items.value = []
            total.value = 0
            hasMore.value = false
        } finally {
            if (seq === loadSeq) loading.value = false
        }
    }

    /** Append the next page, using the last row as the keyset cursor. */
    async function loadMore() {
        if (loadingMore.value || !hasMore.value || items.value.length === 0) return
        const seq = loadSeq // do not bump: a filter change must invalidate this
        loadingMore.value = true
        try {
            const last = items.value[items.value.length - 1]
            const page = await fetchNotifications({
                ...currentQuery(),
                beforeAt: last.createdAt,
                beforeId: last.id,
            })
            if (seq !== loadSeq) return // a reload happened; drop this page
            items.value = [...items.value, ...(page.items ?? [])]
            hasMore.value = page.hasMore ?? false
        } catch (err) {
            appLog.w(TAG, 'loadMore failed', err)
        } finally {
            loadingMore.value = false
        }
    }

    /**
     * Load the project dropdown options once.
     *
     * A failure is non-fatal: the dropdown simply stays empty (only "All
     * projects"), and the list itself is unaffected.
     */
    async function loadProjects() {
        if (projectsLoaded) return
        projectsLoaded = true
        try {
            const res = await fetch('/api/projects/list')
            if (!res.ok) return
            const data = await res.json()
            projectOptions.value = (data.projects ?? [])
                .filter((p: { path?: string }) => !!p.path)
                .map((p: { id: number; path: string }) => ({ id: p.id, path: p.path }))
        } catch (err) {
            appLog.w(TAG, 'loadProjects failed', err)
            projectsLoaded = false // allow a retry on the next open
        }
    }

    /** Change a filter and reload. */
    async function setKind(kind: NotificationKind | '') {
        filterKind.value = kind
        await load()
    }

    async function setRange(range: NotificationRange | '') {
        filterRange.value = range
        await load()
    }

    async function setProject(path: string) {
        filterProject.value = path
        await load()
    }

    /** Delete the whole log, then reload (it will come back empty). */
    async function clear() {
        try {
            await clearNotifications()
        } catch (err) {
            appLog.w(TAG, 'clear failed', err)
            throw err
        }
        await load()
    }

    /** Dropdown options: "All projects" first, then the registry, current project marked. */
    return {
        items,
        total,
        hasMore,
        loading,
        loadingMore,
        error,
        filterKind,
        filterRange,
        filterProject,
        projectOptions,
        isEmpty: computed(() => !loading.value && !error.value && items.value.length === 0),
        load,
        loadMore,
        loadProjects,
        setKind,
        setRange,
        setProject,
        clear,
    }
}
