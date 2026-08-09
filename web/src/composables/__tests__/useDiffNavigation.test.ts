import { describe, expect, it, vi } from 'vitest'
import { ref, nextTick } from 'vue'
import { useDiffNavigation } from '@/composables/useDiffNavigation.ts'

function makeFile(path: string) {
    return { path, type: 'M' }
}

describe('useDiffNavigation', () => {
    it('exposes total as the flat file count for a regular commit', () => {
        const files = ref([makeFile('a.ts'), makeFile('b.ts'), makeFile('c.ts')])
        const mergeGroups = ref([])
        const selectedFilePath = ref('b.ts')
        const { total } = useDiffNavigation({ files, mergeGroups, selectedFilePath, loadDiff: vi.fn() })
        expect(total.value).toBe(3)
    })

    it('flattens merge-commit groups into the navigation sequence', () => {
        const files = ref([])
        const mergeGroups = ref([
            { label: 'main', files: [makeFile('a.ts'), makeFile('b.ts')] },
            { label: 'feature', files: [makeFile('c.ts')] },
        ])
        const selectedFilePath = ref('a.ts')
        const { navigableFiles, total } = useDiffNavigation({ files, mergeGroups, selectedFilePath, loadDiff: vi.fn() })
        expect(total.value).toBe(3)
        expect(navigableFiles.value.map(f => f.path)).toEqual(['a.ts', 'b.ts', 'c.ts'])
    })

    it('computes index of the currently selected file', () => {
        const files = ref([makeFile('a.ts'), makeFile('b.ts'), makeFile('c.ts')])
        const mergeGroups = ref([])
        const selectedFilePath = ref('c.ts')
        const { index } = useDiffNavigation({ files, mergeGroups, selectedFilePath, loadDiff: vi.fn() })
        expect(index.value).toBe(2)
    })

    it('reports -1 index when no file is selected', () => {
        const files = ref([makeFile('a.ts')])
        const mergeGroups = ref([])
        const selectedFilePath = ref(null)
        const { index } = useDiffNavigation({ files, mergeGroups, selectedFilePath, loadDiff: vi.fn() })
        expect(index.value).toBe(-1)
    })

    it('prev() moves to the previous file and reloads the diff', async () => {
        const files = ref([makeFile('a.ts'), makeFile('b.ts'), makeFile('c.ts')])
        const mergeGroups = ref([])
        const selectedFilePath = ref('b.ts')
        const loadDiff = vi.fn()
        const nav = useDiffNavigation({ files, mergeGroups, selectedFilePath, loadDiff })
        nav.prev()
        await nextTick()
        expect(selectedFilePath.value).toBe('a.ts')
        expect(loadDiff).toHaveBeenCalledTimes(1)
    })

    it('next() moves to the following file and reloads the diff', async () => {
        const files = ref([makeFile('a.ts'), makeFile('b.ts'), makeFile('c.ts')])
        const mergeGroups = ref([])
        const selectedFilePath = ref('a.ts')
        const loadDiff = vi.fn()
        const nav = useDiffNavigation({ files, mergeGroups, selectedFilePath, loadDiff })
        nav.next()
        await nextTick()
        expect(selectedFilePath.value).toBe('b.ts')
        expect(loadDiff).toHaveBeenCalledTimes(1)
    })

    it('prev() is a no-op at the first file (does not reload)', async () => {
        const files = ref([makeFile('a.ts'), makeFile('b.ts')])
        const mergeGroups = ref([])
        const selectedFilePath = ref('a.ts')
        const loadDiff = vi.fn()
        const nav = useDiffNavigation({ files, mergeGroups, selectedFilePath, loadDiff })
        nav.prev()
        await nextTick()
        expect(selectedFilePath.value).toBe('a.ts')
        expect(loadDiff).not.toHaveBeenCalled()
    })

    it('next() is a no-op at the last file (does not reload)', async () => {
        const files = ref([makeFile('a.ts'), makeFile('b.ts')])
        const mergeGroups = ref([])
        const selectedFilePath = ref('b.ts')
        const loadDiff = vi.fn()
        const nav = useDiffNavigation({ files, mergeGroups, selectedFilePath, loadDiff })
        nav.next()
        await nextTick()
        expect(selectedFilePath.value).toBe('b.ts')
        expect(loadDiff).not.toHaveBeenCalled()
    })

    it('next() across a merge group boundary selects the next group file', async () => {
        const files = ref([])
        const mergeGroups = ref([
            { label: 'main', files: [makeFile('a.ts')] },
            { label: 'feature', files: [makeFile('b.ts')] },
        ])
        const selectedFilePath = ref('a.ts')
        const loadDiff = vi.fn()
        const nav = useDiffNavigation({ files, mergeGroups, selectedFilePath, loadDiff })
        nav.next()
        await nextTick()
        expect(selectedFilePath.value).toBe('b.ts')
        expect(loadDiff).toHaveBeenCalledTimes(1)
    })
})
