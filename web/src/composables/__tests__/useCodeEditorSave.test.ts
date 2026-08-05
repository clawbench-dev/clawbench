import { describe, it, expect, vi, beforeEach } from 'vitest'

const { toastShow, selectFile } = vi.hoisted(() => ({
    toastShow: vi.fn(),
    selectFile: vi.fn(),
}))

vi.mock('@/composables/useToast.ts', () => ({
    useToast: () => ({ show: toastShow }),
}))
vi.mock('@/stores/app.ts', () => ({
    store: { state: {}, selectFile },
}))
vi.mock('vue-i18n', () => ({
    useI18n: () => ({ t: (k: string) => k }),
}))

import { useCodeEditorSave } from '@/composables/useCodeEditorSave'

describe('useCodeEditorSave', () => {
    beforeEach(() => {
        toastShow.mockReset()
        selectFile.mockReset()
        vi.unstubAllGlobals()
    })

    it('returns true and refreshes file on successful write', async () => {
        vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
        const { saveFile } = useCodeEditorSave()
        const ok = await saveFile('/tmp/a.go', 'package main')
        expect(ok).toBe(true)
        expect(globalThis.fetch).toHaveBeenCalledWith('/api/file/write', expect.objectContaining({
            method: 'POST',
            body: JSON.stringify({ path: '/tmp/a.go', content: 'package main' }),
        }))
        expect(selectFile).toHaveBeenCalledWith('/tmp/a.go', false, false, false)
        expect(toastShow).toHaveBeenCalled()
    })

    it('returns false and shows error toast when write fails', async () => {
        vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }))
        const { saveFile } = useCodeEditorSave()
        const ok = await saveFile('/tmp/a.go', 'package main')
        expect(ok).toBe(false)
        expect(selectFile).not.toHaveBeenCalled()
        expect(toastShow).toHaveBeenCalled()
    })

    it('returns false without fetch when path is empty', async () => {
        vi.stubGlobal('fetch', vi.fn())
        const { saveFile } = useCodeEditorSave()
        const ok = await saveFile('', 'x')
        expect(ok).toBe(false)
        expect(globalThis.fetch).not.toHaveBeenCalled()
    })
})
