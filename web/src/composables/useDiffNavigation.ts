import { computed, type Ref } from 'vue'

interface NavFile {
    path: string
    [key: string]: unknown
}

interface MergeGroup {
    files: NavFile[]
    [key: string]: unknown
}

/**
 * Diff prev/next navigation for the project-history drill-down.
 *
 * Given the current commit's file list, produces a flat, ordered navigation
 * sequence (merge commits flatten their per-branch groups in the same order
 * the file list shows) and exposes prev/next helpers that update the selected
 * path and reload the diff — so the user can hop between file diffs without
 * returning to the file list.
 */
export function useDiffNavigation(options: {
    files: Ref<NavFile[]>
    mergeGroups: Ref<MergeGroup[]>
    selectedFilePath: Ref<string | null>
    loadDiff: () => void
}) {
    const { files, mergeGroups, selectedFilePath, loadDiff } = options

    const navigableFiles = computed<NavFile[]>(() => {
        if (mergeGroups.value.length > 0) {
            return mergeGroups.value.flatMap(g => g.files)
        }
        return files.value
    })

    const total = computed(() => navigableFiles.value.length)

    const index = computed(() => navigableFiles.value.findIndex(f => f.path === selectedFilePath.value))

    function goToFile(target: number) {
        const list = navigableFiles.value
        if (target < 0 || target >= list.length) return
        selectedFilePath.value = list[target].path
        loadDiff()
    }

    function prev() {
        goToFile(index.value - 1)
    }

    function next() {
        goToFile(index.value + 1)
    }

    return {
        navigableFiles,
        total,
        index,
        goToFile,
        prev,
        next,
    }
}
