import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast.ts'
import { markFileSaved } from '@/composables/useFileRefresh.ts'
import { useDialog } from '@/composables/useDialog.ts'
import { store } from '@/stores/app.ts'
import { joinPath } from '@/utils/path.ts'

/**
 * Characters that would make the entered name a path rather than a file name.
 * The backend resolves `dir + name` and rejects escapes, but a name like
 * `a/b.txt` would silently target a (probably missing) subdirectory instead of
 * failing loudly — so it is refused here with a clear message.
 */
const INVALID_NAME_CHARS = /[/\\]/

export function useCodeEditorSave() {
    const { show } = useToast()
    const { t } = useI18n()
    const dialog = useDialog()
    const saving = ref(false)

    async function saveFile(path: string, content: string): Promise<boolean> {
        // An empty path means the buffer is a new, not-yet-named file: the
        // filename is requested at save time (VSCode-style) instead of up front.
        if (!path) return await saveUntitled(content)
        return await writeFile(path, content)
    }

    /** Persist an already-named file. */
    async function writeFile(path: string, content: string): Promise<boolean> {
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
            // Suppress the fsnotify-triggered auto-refresh that our own write
            // causes; content is already synced, so it would just flash.
            markFileSaved(path)
            show(t('file.editor.saved'), { icon: '✅', type: 'success', duration: 2000 })
            return true
        } catch {
            show(t('file.editor.saveFailed'), { icon: '❌', type: 'error', duration: 2000 })
            return false
        } finally {
            saving.value = false
        }
    }

    /**
     * First save of an untitled buffer: ask for a filename, then create the file
     * with its content and re-point the open buffer at the real path.
     *
     * The name is validated for emptiness and path separators, and the target is
     * probed for an existing file/directory first — `/api/file/write` overwrites
     * silently, so without that check a typo would clobber an unrelated file.
     */
    async function saveUntitled(content: string): Promise<boolean> {
        const file = store.state.currentFile
        const dir = file?.targetDir ?? ''
        const name = (await dialog.prompt(t('file.prompt.fileName')))?.trim()
        if (!name) return false
        if (INVALID_NAME_CHARS.test(name) || name === '.' || name === '..') {
            show(t('file.prompt.invalidFileName'), { icon: '❌', type: 'error', duration: 2500 })
            return false
        }

        const path = joinPath(dir, name)
        saving.value = true
        try {
            const existsResp = await fetch('/api/file/batch-exists', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ paths: [path] }),
            })
            if (!existsResp.ok) throw new Error('existence check failed')
            const existsData = await existsResp.json() as { results?: Record<string, string> }
            const kind = existsData.results?.[path]
            if (kind === 'file' || kind === 'dir') {
                show(t('file.toast.fileExists'), { icon: '❌', type: 'error', duration: 2500 })
                return false
            }

            const resp = await fetch('/api/file/write', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path, content }),
            })
            if (!resp.ok) throw new Error('write failed')

            // Adopt the real path in place, then sync the saved content. Order
            // matters: markSaved matches on the current file's path.
            if (!store.adoptUntitledPath(path, content)) {
                // The buffer was replaced (file switched) while the prompt was
                // open — the write still succeeded, but there is nothing left to
                // re-point. Treat the save as done rather than showing an error.
                return true
            }
            store.markSaved(path, content)
            markFileSaved(path)
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
