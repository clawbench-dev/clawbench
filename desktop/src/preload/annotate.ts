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
})
