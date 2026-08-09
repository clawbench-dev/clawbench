import { ref } from 'vue'

/**
 * Shared file-editor state bridging the file overlay's back gesture to the
 * in-editor exit flow.
 *
 * The `editing` flag lives here (module-level) instead of inside FileViewer so
 * that the global back handler (App.vue) can decide, when the user swipes in
 * from the screen edge, whether to exit edit mode first rather than navigating
 * back / closing the file.
 *
 * FileViewer registers an exit handler that runs the dirty-save confirmation
 * (CodeMirrorViewer.handleExit). The back handler calls `exitEdit()`; if no
 * handler is registered (edit never entered), it's a no-op.
 */

const _editing = ref(false)
let _exitEditHandler: (() => void | Promise<void>) | null = null
let _dirtyGetter: (() => boolean) | null = null

/** @internal Reset all state — for tests only */
export function _resetForTesting() {
    _editing.value = false
    _exitEditHandler = null
    _dirtyGetter = null
}

export function useFileEditor() {
    function setEditing(v: boolean) {
        _editing.value = v
    }

    function isEditing(): boolean {
        return _editing.value
    }

    /**
     * Register a getter for the editor's unsaved-changes (dirty) state.
     * Returns an unregister function. Only one getter can be active at a time.
     */
    function registerDirtyGetter(fn: () => boolean): () => void {
        _dirtyGetter = fn
        return () => {
            if (_dirtyGetter === fn) _dirtyGetter = null
        }
    }

    /** Whether the active editor currently has unsaved changes. */
    function isEditorDirty(): boolean {
        return _dirtyGetter?.() ?? false
    }

    /**
     * Register the callback that exits edit mode (with dirty confirmation).
     * Returns an unregister function. Only one handler can be active at a time.
     */
    function registerExitEditHandler(fn: () => void | Promise<void>): () => void {
        _exitEditHandler = fn
        return () => {
            if (_exitEditHandler === fn) _exitEditHandler = null
        }
    }

    /** Exit edit mode via the registered handler. Returns the (possibly async) result. */
    function exitEdit(): void | Promise<void> {
        return _exitEditHandler?.()
    }

    return { editing: _editing, setEditing, isEditing, registerExitEditHandler, exitEdit, registerDirtyGetter, isEditorDirty }
}
