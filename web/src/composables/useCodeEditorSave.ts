import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast.ts'
import { store } from '@/stores/app.ts'

export function useCodeEditorSave() {
    const { show } = useToast()
    const { t } = useI18n()
    const saving = ref(false)

    async function saveFile(path: string, content: string): Promise<boolean> {
        if (!path) return false
        saving.value = true
        try {
            const resp = await fetch('/api/file/write', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path, content }),
            })
            if (!resp.ok) throw new Error('write failed')
            // Update the in-memory content in place — no re-fetch needed. The
            // text just written IS the on-disk state, so reloading would only
            // add a network round-trip and scroll flash with no benefit.
            store.markSaved(path, content)
            show(t('file.editor.saved'), { icon: '✅', type: 'success', duration: 2000 })
            return true
        } catch {
            show(t('file.editor.saveFailed'), { icon: '❌', type: 'error', duration: 2000 })
            return false
        } finally {
            saving.value = false
        }
    }

    return { saving, saveFile }
}
