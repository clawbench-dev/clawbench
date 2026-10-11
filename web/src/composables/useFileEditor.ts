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
 *
 * A separate leave guard (`registerLeaveGuard`/`confirmLeave`) serves callers
 * that would leave the view WITHOUT the back gesture's untitled-buffer close
 * side effect — most notably a project switch, which unmounts the editor.
 */

const _editing = ref(false)
let _exitEditHandler: (() => void | Promise<void>) | null = null
let _dirtyGetter: (() => boolean) | null = null
let _leaveGuard: (() => boolean | Promise<boolean>) | null = null

/** @internal Reset all state — for tests only */
export function _resetForTesting() {
    _editing.value = false
    _exitEditHandler = null
    _dirtyGetter = null
    _leaveGuard = null
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

    /**
     * Register a guard consulted before an action that would leave the current
     * view (e.g. a project switch, which unmounts the editor and would otherwise
     * silently drop unsaved edits). Returns an unregister function. Only one
     * guard can be active at a time.
     *
     * Unlike `exitEdit()`, this runs the dirty-save confirmation WITHOUT the
     * untitled-buffer close side effect — the caller keeps control of whatever
     * navigation it was about to perform.
     */
    function registerLeaveGuard(fn: () => boolean | Promise<boolean>): () => void {
        _leaveGuard = fn
        return () => {
            if (_leaveGuard === fn) _leaveGuard = null
        }
    }

    /**
     * Ask the registered leave guard whether it is safe to leave. Returns true
     * when there is no guard (nothing to protect) or the guard allows leaving;
     * false when the user cancelled or a save failed.
     */
    async function confirmLeave(): Promise<boolean> {
        if (!_leaveGuard) return true
        return (await _leaveGuard()) === true
    }

    return { editing: _editing, setEditing, isEditing, registerExitEditHandler, exitEdit, registerDirtyGetter, isEditorDirty, registerLeaveGuard, confirmLeave }
}
