import { contextBridge, ipcRenderer } from 'electron'

/**
 * Preload for the floating status capsule ONLY.
 *
 * Kept separate from `index.js` deliberately: the main window's bridge exposes
 * server credentials, tunnel control and download handles to the page the server
 * serves. The capsule needs a handful of verbs and nothing else, so it gets a
 * module with a fixed channel list rather than a filtered view of the big one.
 *
 * Sandboxed (the capsule window does not disable the sandbox), so this can only
 * require Electron builtins — same constraint documented in preload/index.ts.
 */
contextBridge.exposeInMainWorld('FloatingHost', {
  /** Main pushes {stats, colors, labels, logoUrl}; the capsule only renders. */
  onState(cb: (state: unknown) => void): void {
    ipcRenderer.on('floating:state', (_e, state) => cb(state))
  },
  /** Listeners are installed — ask for anything queued during document load. */
  ready(): void {
    ipcRenderer.send('floating:ready')
  },
  /** Right-click: native menu, built in the main process. */
  context(): void {
    ipcRenderer.send('floating:context')
  },
  /**
   * Grab offset inside the window (viewport CSS px). The main process then
   * follows the OS cursor and tells a drag from a click on release — see the
   * module comment in main/floating.ts for why this is not a CSS drag region.
   */
  dragStart(x: number, y: number): void {
    ipcRenderer.send('floating:drag-start', x, y)
  },
  dragEnd(): void {
    ipcRenderer.send('floating:drag-end')
  },
  hide(): void {
    ipcRenderer.send('floating:hide')
  },
  quit(): void {
    ipcRenderer.send('floating:quit')
  },
  /** Report the capsule's natural size; the main process clamps it on-screen. */
  resize(width: number, height: number): Promise<boolean> {
    return ipcRenderer.invoke('floating:resize', width, height)
  },
})
