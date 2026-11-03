import { contextBridge, ipcRenderer } from 'electron'

/**
 * Preload for the annotation overlay ONLY.
 *
 * Same reasoning as the floating capsule's preload: the overlay document is a
 * local page, but it runs with the same sandbox rules, so it gets a fixed verb
 * list instead of a view of the main bridge. Two verbs are enough — report a
 * stroke frame, and close the window.
 */
contextBridge.exposeInMainWorld('AnnotateHost', {
  /** One control frame, already JSON-encoded by the page. */
  ink(msg: string): void {
    ipcRenderer.send('annotate:ink', msg)
  },
  close(): void {
    ipcRenderer.send('annotate:close')
  },
  /**
   * The host asked for the toolbar again (the meeting footer's 标注画面 button
   * while the overlay is already open). The page cannot see that click — it
   * happens in another window — so the main process relays it here.
   */
  onWake(cb: () => void): void {
    ipcRenderer.on('annotate:wake', () => cb())
  },
})
