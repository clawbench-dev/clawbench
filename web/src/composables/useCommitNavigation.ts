import { ref } from 'vue'
import { getCachedCommitInfo } from '@/composables/useCommitHashAnnotation.ts'

// Module-level pending SHA — set by chat click, consumed by Git history components
const pendingSha = ref<string | null>(null)

// Module-level pending manage navigation — set by branch badge click, consumed by Git history components
const pendingManageView = ref(false)

/**
 * Set a pending commit navigation request.
 * Called from App.vue's handleNavigateToCommit.
 */
export function setPendingCommitNavigation(sha: string) {
    pendingSha.value = sha
}

/**
 * Check if there's a pending commit navigation and consume it.
 * Returns the SHA or null.
 */
export function consumePendingCommitNavigation(): string | null {
    const sha = pendingSha.value
    pendingSha.value = null
    return sha
}

/**
 * Reset module-level state for testing.
 * @internal
 */
export function _resetPendingShaForTesting() {
    pendingSha.value = null
    pendingManageView.value = false
}

/**
 * Set a pending manage-view navigation request.
 * Called from AppHeader's branch badge click.
 */
export function setPendingManageNavigation() {
    pendingManageView.value = true
}

/**
 * Check if there's a pending manage-view navigation and consume it.
 * Returns true if pending, false otherwise.
 */
export function consumePendingManageNavigation(): boolean {
    const pending = pendingManageView.value
    pendingManageView.value = false
    return pending
}

/**
 * The reactive pending SHA ref. GitHistory components can watch this
 * to handle navigation even when already mounted and active.
 */
export { pendingSha }

/**
 * The reactive pending manage-view ref. GitHistory components can watch this
 * to handle navigation even when already mounted and active.
 */
export { pendingManageView }

/**
 * Shared commit navigation logic for GitHistory components.
 * Takes the component's reactive state and functions as parameters.
 */
export function useCommitNavigation(options: {
    commits: any              // ref([])
    selectedSHA: any          // ref(null)
    currentView: any          // ref('commits')
    loadCommitFiles: (sha: string) => Promise<void>
    loadProjectHistory?: () => Promise<void>
}) {
    const { commits, selectedSHA, currentView, loadCommitFiles, loadProjectHistory } = options

    /**
     * Fetch a single commit's info via verify-commits API.
     */
    async function fetchCommitInfo(sha: string): Promise<any | null> {
        try {
            const resp = await fetch('/api/git/verify-commits', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ shas: [sha] }),
            })
            if (!resp.ok) return null
            const data = await resp.json()
            const info = data.results?.[sha]
            return (info && info.sha) ? info : null
        } catch {
            return null
        }
    }

    /**
     * Navigate directly to a specific commit's files view.
     * Accepts short or full SHA. Resolves to full SHA for consistent state.
     * Ensures the commit info is in the commits array so breadcrumbs work.
     */
    async function navigateToCommit(sha: string) {
        // Resolve short SHA to full SHA and get commit info BEFORE switching view
        // to avoid showing an empty files view while the async fetch is in progress.
        let commitInfo = commits.value.find((c: any) => c.sha === sha || c.sha.startsWith(sha))
        if (!commitInfo) {
            // Try annotation cache first
            const cached = getCachedCommitInfo(sha)
            if (cached && cached.sha) {
                commitInfo = cached
                commits.value.unshift(cached)
            } else {
                // Fetch commit info via API (returns full SHA)
                const fetched = await fetchCommitInfo(sha)
                if (fetched && fetched.sha) {
                    commitInfo = fetched
                    if (!commits.value.find((c: any) => c.sha === fetched.sha)) {
                        commits.value.unshift(fetched)
                    }
                }
            }
        }

        // Use the full SHA from commit info for consistent state
        const fullSha = commitInfo?.sha || sha
        selectedSHA.value = fullSha

        // Switch to files view only after commit info is resolved
        currentView.value = 'files'

        loadCommitFiles(fullSha).catch(() => {})
    }

    /**
     * Handle drill-back to commits list when arriving from a deep-linked commit.
     * Loads the full project history if the commits array only has the one commit.
     */
    function handleDrillBackToCommits() {
        if (commits.value.length <= 1 && loadProjectHistory) {
            loadProjectHistory()
        }
    }

    return {
        navigateToCommit,
        handleDrillBackToCommits,
        fetchCommitInfo,
    }
}
