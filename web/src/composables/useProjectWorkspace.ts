// Workspace restore shared by app startup and project switch.
//
// restoreProjectWorkspace() restores the current project's last browsed
// directory and last opened file. It is the single source of truth used by
// both initializeApp (cold start) and hotSwitchProject (SPA project switch),
// keeping the two paths from diverging.
//
// Which PANEL to show is deliberately NOT decided here. It used to be, via an
// `activateView` option that switched to the file-view tab — but the two callers
// wanted opposite things, and neither answer was right:
//   - Cold start passed false, because activating the viewer made startup jump
//     away from chat whenever the project had a last-opened file (commit
//     e1bb5fb55).
//   - Project switch passed true, because without it the restored file stayed in
//     state (header badge only) and the viewer was never brought back.
// The panel is now remembered PER PROJECT (composables/useProjectPanel.ts) and
// applied by the caller AFTER this function returns, so the landing panel is a
// property of the project rather than of the code path that reached it.
import { useFileNavStack } from '@/composables/useFileNavStack'
import { useToast } from '@/composables/useToast'
import { gt } from '@/composables/useLocale'
import { store, loadBrowseDir, loadOpenFile, clearStaleOpenFile } from '@/stores/app'

export async function restoreProjectWorkspace(): Promise<void> {
  const fileNav = useFileNavStack()
  const toast = useToast()

  // Restore last browsed directory, falling back to the project root if the
  // saved directory no longer exists.
  const savedDir = loadBrowseDir()
  if (savedDir) {
    try {
      await store.loadFiles(savedDir, true)
    } catch {
      try { await store.loadFiles('') } catch { /* ignore */ }
    }
  } else {
    try { await store.loadFiles('') } catch {
      toast.show(gt('toast.fileListLoadFailed'), { icon: '⚠️', type: 'error', duration: 6000 })
    }
  }

  // Restore last opened file (per-project). Opening it populates `currentFile`,
  // which is what makes a remembered 'view' panel safe to apply afterwards.
  const savedFile = loadOpenFile()
  if (savedFile) {
    const ok = await store.selectFile(savedFile)
    if (ok) {
      fileNav.openFile(savedFile)
    } else {
      // File no longer exists — clear the stale record to avoid repeated failures.
      clearStaleOpenFile()
    }
  }
}
