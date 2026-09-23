import { describe, it, expect, vi, beforeEach } from 'vitest'

const { toastShow, markSaved, markFileSaved, prompt, adoptUntitledPath, storeState } = vi.hoisted(() => ({
    toastShow: vi.fn(),
    markSaved: vi.fn(),
    markFileSaved: vi.fn(),
    prompt: vi.fn(),
    adoptUntitledPath: vi.fn(),
    storeState: { currentFile: null as null | { untitled?: boolean; targetDir?: string; path?: string } },
}))

vi.mock('@/composables/useToast.ts', () => ({
    useToast: () => ({ show: toastShow }),
}))
vi.mock('@/composables/useFileRefresh.ts', () => ({
    markFileSaved,
}))
vi.mock('@/composables/useDialog.ts', () => ({
    useDialog: () => ({ prompt }),
}))
vi.mock('@/stores/app.ts', () => ({
    store: { state: storeState, markSaved, adoptUntitledPath },
}))
vi.mock('vue-i18n', () => ({
    useI18n: () => ({ t: (k: string) => k }),
}))

import { useCodeEditorSave } from '@/composables/useCodeEditorSave'

describe('useCodeEditorSave', () => {
    beforeEach(() => {
        toastShow.mockReset()
        markSaved.mockReset()
        markFileSaved.mockReset()
        prompt.mockReset()
        adoptUntitledPath.mockReset()
        storeState.currentFile = null
        vi.unstubAllGlobals()
    })

    it('returns true and updates file in memory on successful write', async () => {
        vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
        const { saveFile } = useCodeEditorSave()
        const ok = await saveFile('/tmp/a.go', 'package main')
        expect(ok).toBe(true)
        expect(globalThis.fetch).toHaveBeenCalledWith('/api/file/write', expect.objectContaining({
            method: 'POST',
            body: JSON.stringify({ path: '/tmp/a.go', content: 'package main' }),
        }))
        expect(markSaved).toHaveBeenCalledWith('/tmp/a.go', 'package main')
        expect(markFileSaved).toHaveBeenCalledWith('/tmp/a.go')
        expect(toastShow).toHaveBeenCalled()
    })

    it('returns false and shows error toast when write fails', async () => {
        vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }))
        const { saveFile } = useCodeEditorSave()
        const ok = await saveFile('/tmp/a.go', 'package main')
        expect(ok).toBe(false)
        expect(markSaved).not.toHaveBeenCalled()
        expect(markFileSaved).not.toHaveBeenCalled()
        expect(toastShow).toHaveBeenCalled()
    })

    // An empty path no longer means "nothing to save" — it means the buffer is a
    // new, not-yet-named file, so the filename is requested at save time.
    describe('untitled buffer (empty path)', () => {
        beforeEach(() => {
            storeState.currentFile = { untitled: true, targetDir: 'docs' }
        })

        it('prompts for a name, writes the file, then adopts the real path', async () => {
            prompt.mockResolvedValue('notes.md')
            adoptUntitledPath.mockReturnValue(true)
            const fetchSpy = vi.fn()
                .mockResolvedValueOnce({ ok: true, json: async () => ({ results: { 'docs/notes.md': 'none' } }) })
                .mockResolvedValueOnce({ ok: true })
            vi.stubGlobal('fetch', fetchSpy)

            const { saveFile } = useCodeEditorSave()
            const ok = await saveFile('', 'hello')

            expect(ok).toBe(true)
            expect(prompt).toHaveBeenCalled()
            expect(fetchSpy).toHaveBeenNthCalledWith(1, '/api/file/batch-exists', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ paths: ['docs/notes.md'] }),
            }))
            expect(fetchSpy).toHaveBeenNthCalledWith(2, '/api/file/write', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ path: 'docs/notes.md', content: 'hello' }),
            }))
            expect(adoptUntitledPath).toHaveBeenCalledWith('docs/notes.md', 'hello')
            expect(markSaved).toHaveBeenCalledWith('docs/notes.md', 'hello')
        })

        it('returns false without writing when the prompt is cancelled', async () => {
            prompt.mockResolvedValue(null)
            const fetchSpy = vi.fn()
            vi.stubGlobal('fetch', fetchSpy)

            const { saveFile } = useCodeEditorSave()
            const ok = await saveFile('', 'hello')

            expect(ok).toBe(false)
            expect(fetchSpy).not.toHaveBeenCalled()
            expect(adoptUntitledPath).not.toHaveBeenCalled()
        })

        it('refuses a name containing a path separator', async () => {
            prompt.mockResolvedValue('sub/notes.md')
            const fetchSpy = vi.fn()
            vi.stubGlobal('fetch', fetchSpy)

            const { saveFile } = useCodeEditorSave()
            const ok = await saveFile('', 'hello')

            expect(ok).toBe(false)
            expect(fetchSpy).not.toHaveBeenCalled()
            expect(toastShow).toHaveBeenCalled()
        })

        // /api/file/write overwrites silently, so an existing target must be
        // rejected before the write rather than clobbering an unrelated file.
        it('refuses to overwrite an existing file or directory', async () => {
            prompt.mockResolvedValue('notes.md')
            const fetchSpy = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ results: { 'docs/notes.md': 'file' } }) })
            vi.stubGlobal('fetch', fetchSpy)

            const { saveFile } = useCodeEditorSave()
            const ok = await saveFile('', 'hello')

            expect(ok).toBe(false)
            expect(fetchSpy).toHaveBeenCalledTimes(1) // existence check only
            expect(adoptUntitledPath).not.toHaveBeenCalled()
            expect(toastShow).toHaveBeenCalled()
        })

        it('treats a save as done when the buffer was replaced while prompting', async () => {
            prompt.mockResolvedValue('notes.md')
            adoptUntitledPath.mockReturnValue(false) // switched files mid-prompt
            const fetchSpy = vi.fn()
                .mockResolvedValueOnce({ ok: true, json: async () => ({ results: {} }) })
                .mockResolvedValueOnce({ ok: true })
            vi.stubGlobal('fetch', fetchSpy)

            const { saveFile } = useCodeEditorSave()
            const ok = await saveFile('', 'hello')

            expect(ok).toBe(true)
            expect(markSaved).not.toHaveBeenCalled()
        })
    })
})
