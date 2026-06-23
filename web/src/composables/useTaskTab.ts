import { ref, type Ref } from 'vue'
import { store } from '@/stores/app'
import { playNotificationSound } from '@/composables/useNotificationSound'
import { showBrowserNotification } from '@/composables/useNotification'
import { useToast } from '@/composables/useToast'
import { gt } from '@/composables/useLocale'

// Module-level singleton refs (shared across all consumers)
const currentView = ref<'list' | 'settings' | 'history'>('list')
const selectedTaskId = ref<number | null>(null)
const selectedExecId = ref<string | null>(null)
const selectedExecData = ref<any>(null)
const execDetailOpen = ref(false)
const formViewOpen = ref(false)
const formMode = ref<'create' | 'edit'>('create')

// Module-level polling timer
let pollingTimer: ReturnType<typeof setInterval> | null = null

// AbortController for loadTasks — aborts previous in-flight request
let loadTasksAbortController: AbortController | null = null

// Debounce timer for onTaskEvent — coalesces rapid WS events into single loadTasks()
let onTaskEventDebounceTimer: ReturnType<typeof setTimeout> | null = null

// Guard: when markAllTasksRead is in progress, suppress loadTasks
// from overwriting taskUnreadCount back to non-zero (race condition fix)
let markingReadInProgress = false

// Track per-task running counts to detect completion transitions
const prevRunningCounts = new Map<number, number>()

// Dedup: track task IDs that have already fired completion notification
// to avoid repeated sound/notification on subsequent polls
const notifiedTaskCompletions = new Set<string>()

// Timer for clearing taskJustCompleted flag
let justCompletedTimer: ReturnType<typeof setTimeout> | null = null

// Callback registered by App.vue to switch the main tab
let switchTabCallback: ((tab: string) => void) | null = null

/** Register the switchTab callback from App.vue so notifications can navigate */
export function registerSwitchTab(cb: (tab: string) => void) {
    switchTabCallback = cb
}

/** Reset all task tab navigation state (for project switch) */
export function resetTaskTabState() {
    currentView.value = 'list'
    selectedTaskId.value = null
    selectedExecId.value = null
    selectedExecData.value = null
    execDetailOpen.value = false
    formViewOpen.value = false
    formMode.value = 'create'
}

/** Called when a task execution completes (runningCount drops to 0) */
function onTaskCompleted(task: any) {
    // Sound + haptic
    playNotificationSound()

    // Navigate to task history on click
    const navigateToHistory = () => {
        if (switchTabCallback) switchTabCallback('tasks')
    }

    // Browser push notification (only when page not focused)
    try {
        showBrowserNotification(task.name || gt('task.title'), {
            body: gt('task.exec.completed'),
            tag: `task-completed-${task.id}`,
            onClick: navigateToHistory,
        })
    } catch {
        // Non-critical
    }
    // Toast — include task name, icon, and click-to-navigate
    try {
        const taskName = task.name || gt('task.title')
        useToast().show(`${taskName} — ${gt('task.exec.completed')}`, {
            icon: '✅',
            type: 'success',
            duration: 5000,
            onClick: navigateToHistory,
        })
    } catch {
        // Non-critical
    }
    // Set just-completed flag for dock flash animation
    store.state.taskJustCompleted = true
    if (justCompletedTimer) clearTimeout(justCompletedTimer)
    justCompletedTimer = setTimeout(() => {
        store.state.taskJustCompleted = false
        justCompletedTimer = null
    }, 2000)
}

// --- Module-level data methods ---

async function loadTasks() {
    // Abort previous in-flight request to prevent stale response overwriting fresh data
    if (loadTasksAbortController) {
        loadTasksAbortController.abort()
    }
    const controller = new AbortController()
    loadTasksAbortController = controller

    try {
        const resp = await fetch('/api/tasks', { signal: controller.signal })
        if (!resp.ok) return
        const data = await resp.json()
        const newTasks = data.tasks || []
        // Race condition guard: if markAllTasksRead is in progress,
        // don't let a stale response flip taskUnreadCount back to non-zero
        if (!markingReadInProgress) {
            store.state.taskUnreadCount = newTasks.reduce((sum: number, t: any) => sum + (t.unreadCount || 0), 0)
        }
        // Derive running state from runningCount
        const hasRunning = newTasks.some((t: any) => t.runningCount > 0)
        store.state.taskRunning = hasRunning

        // ── Detect task completion (runningCount dropped to 0) ──
        for (const task of newTasks) {
            const id: number = task.id
            const prevCount = prevRunningCounts.get(id) || 0
            const currCount = task.runningCount || 0
            // Completion: was running, now stopped, and has new unread results
            if (prevCount > 0 && currCount === 0) {
                const dedupKey = `${id}-${task.runCount}`
                if (!notifiedTaskCompletions.has(dedupKey)) {
                    notifiedTaskCompletions.add(dedupKey)
                    // Trigger completion effects
                    onTaskCompleted(task)
                }
            }
            prevRunningCounts.set(id, currCount)
        }
        // Clean up dedup set for deleted tasks
        const currentIds = new Set(newTasks.map((t: any) => t.id))
        for (const key of prevRunningCounts.keys()) {
            if (!currentIds.has(key)) prevRunningCounts.delete(key)
        }
        // Clean up notifiedTaskCompletions for tasks no longer running
        // (they will be re-added if the task runs again)
        for (const key of [...notifiedTaskCompletions]) {
            const taskId = parseInt(key.split('-')[0])
            const task = newTasks.find((t: any) => t.id === taskId)
            if (task && task.runningCount === 0) {
                notifiedTaskCompletions.delete(key)
            }
        }

        // Diff-check to avoid unnecessary watcher triggers
        if (
            store.state.tasks.length !== newTasks.length ||
            newTasks.some(
                (t: any, i: number) =>
                    t.id !== store.state.tasks[i]?.id ||
                    t.status !== store.state.tasks[i]?.status ||
                    t.runCount !== store.state.tasks[i]?.runCount ||
                    t.unreadCount !== store.state.tasks[i]?.unreadCount ||
                    t.runningCount !== store.state.tasks[i]?.runningCount
            )
        ) {
            store.state.tasks = newTasks
        }
    } catch (e: any) {
        // AbortError is expected when a newer request supersedes this one
        if (e?.name === 'AbortError') return
        // Silently ignore fetch errors (network down, server restart, etc.)
    }
}

async function markAllTasksRead() {
    const unreadTasks = store.state.tasks.filter((t: any) => t.unreadCount > 0)
    if (unreadTasks.length === 0) return
    markingReadInProgress = true
    try {
        await Promise.all(
            unreadTasks.map((t: any) =>
                fetch(`/api/tasks/${t.id}`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ action: 'read' }),
                }).then(r => {
                    if (!r.ok) throw new Error(`mark read failed: ${r.status}`)
                })
            )
        )
        // Optimistically clear unread counts in local store
        for (const t of store.state.tasks) {
            if ((t as any).unreadCount > 0) {
                (t as any).unreadCount = 0
            }
        }
        store.state.taskUnreadCount = 0
    } catch {
        // Mark-read failed — don't clear badge, next poll will correct
    } finally {
        markingReadInProgress = false
    }
}

/** Mark a single task as read — clears unread badge for that task only */
async function markTaskRead(taskId: number) {
    const task = store.state.tasks.find((t: any) => t.id === taskId)
    if (!task || (task as any).unreadCount <= 0) return
    try {
        const resp = await fetch(`/api/tasks/${taskId}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'read' }),
        })
        if (!resp.ok) return
        // Optimistically clear unread count for this task
        ;(task as any).unreadCount = 0
        // Re-derive taskUnreadCount from remaining unread tasks
        store.state.taskUnreadCount = store.state.tasks.reduce((sum: number, t: any) => sum + (t.unreadCount || 0), 0)
    } catch {
        // Silently ignore — next poll will correct
    }
}

// --- WS event handler ---

// Called from WS task_update event
export function onTaskEvent(data: { task_id?: string; status?: string; execution_id?: string } | undefined) {
    if (!data) return
    // Debounce: coalesce rapid WS events into a single loadTasks() call
    if (onTaskEventDebounceTimer) clearTimeout(onTaskEventDebounceTimer)
    onTaskEventDebounceTimer = setTimeout(() => {
        onTaskEventDebounceTimer = null
        loadTasks()
    }, 200)
}

export function useTaskTab() {
    // --- Navigation methods ---

    function navigateToTaskSettings(taskId: number) {
        selectedTaskId.value = taskId
        currentView.value = 'settings'
        execDetailOpen.value = false
        formViewOpen.value = false
    }

    function navigateToTaskHistory(taskId: number) {
        selectedTaskId.value = taskId
        currentView.value = 'history'
        execDetailOpen.value = false
        formViewOpen.value = false
        // Clear unread badge for this task — user is viewing its execution history
        markTaskRead(taskId)
    }

    function goBack() {
        if (formViewOpen.value) {
            formViewOpen.value = false
        } else if (execDetailOpen.value) {
            execDetailOpen.value = false
            selectedExecId.value = null
        } else if (currentView.value === 'history') {
            currentView.value = 'settings'
        } else {
            currentView.value = 'list'
            selectedTaskId.value = null
        }
    }

    function navigateToList() {
        formViewOpen.value = false
        execDetailOpen.value = false
        selectedExecId.value = null
        currentView.value = 'list'
        selectedTaskId.value = null
    }

    function openExecDetail(execId: string, execData?: any) {
        selectedExecId.value = execId
        selectedExecData.value = execData || null
        execDetailOpen.value = true
        // If no execData provided, auto-fetch from API (e.g. from push notification deep link)
        if (!execData) {
            refreshExecDetail()
        }
    }

    function closeExecDetail() {
        execDetailOpen.value = false
        selectedExecId.value = null
        selectedExecData.value = null
    }

    /** Refresh the currently-viewed execution detail by re-fetching from API */
    async function refreshExecDetail() {
        if (!selectedTaskId.value || !selectedExecId.value) return
        try {
            const resp = await fetch(`/api/tasks/${selectedTaskId.value}/executions?limit=50`)
            if (!resp.ok) return
            const data = await resp.json()
            const exec = (data.executions || []).find((e: any) => String(e.id) === String(selectedExecId.value) || String(e.sessionId) === String(selectedExecId.value))
            if (exec) {
                // Preserve existing content/blocks/metadata/preview if API returns null
                // (LEFT JOIN may return null content when chat_history has no matching row)
                const { content: _apiContent, blocks: _apiBlocks, metadata: _apiMetadata, preview: _apiPreview, ...safeFields } = exec
                const merged = { ...selectedExecData.value, ...safeFields }
                // Only overwrite content if API returned a non-null value
                if (exec.content != null) merged.content = exec.content
                selectedExecData.value = merged
            }
        } catch {
            // Silently ignore
        }
    }

    function openCreateForm() {
        formMode.value = 'create'
        formViewOpen.value = true
    }

    function openEditForm() {
        formMode.value = 'edit'
        formViewOpen.value = true
    }

    function closeForm() {
        formViewOpen.value = false
    }

    // --- Polling ---

    function startTaskPolling() {
        if (pollingTimer !== null) return // guard against double-start
        loadTasks()
        pollingTimer = setInterval(loadTasks, 2000)
    }

    function stopTaskPolling() {
        if (pollingTimer !== null) {
            clearInterval(pollingTimer)
            pollingTimer = null
        }
    }

    return {
        // Navigation state
        currentView: currentView as Ref<'list' | 'settings' | 'history'>,
        selectedTaskId,
        selectedExecId,
        selectedExecData,
        execDetailOpen,
        formViewOpen,
        formMode: formMode as Ref<'create' | 'edit'>,

        // Navigation methods
        navigateToTaskSettings,
        navigateToTaskHistory,
        navigateToList,
        goBack,
        openExecDetail,
        closeExecDetail,
        refreshExecDetail,
        openCreateForm,
        openEditForm,
        closeForm,

        // Data methods
        loadTasks,
        markAllTasksRead,
        markTaskRead,

        // Polling
        startTaskPolling,
        stopTaskPolling,
    }
}
