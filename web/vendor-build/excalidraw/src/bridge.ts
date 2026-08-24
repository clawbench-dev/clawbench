/**
 * postMessage bridge between the Vue host (parent window) and the Excalidraw
 * iframe (this window).
 *
 * Protocol (all messages are JSON strings):
 *
 *   parent → iframe:
 *     { event: 'load', data: { content: string } }   // .excalidraw JSON text
 *     { event: 'saveRequest' }                       // ask for current XML
 *
 *   iframe → parent:
 *     { event: 'ready' }                             // editor mounted
 *     { event: 'changed' }                           // scene modified (dirty)
 *     { event: 'save', data: { content: string } }   // serialized .excalidraw JSON
 *     { event: 'exit', data: { modified: boolean } } // user leaving the editor
 */

export interface BridgeMessage {
  event: 'load' | 'saveRequest'
  data?: { content: string }
}

export function postToParent(msg: BridgeMessage): void {
  if (!window.parent) return
  window.parent.postMessage(JSON.stringify(msg), '*')
}

export function emitReady(): void {
  postToParent({ event: 'ready' })
}

export function emitChanged(): void {
  postToParent({ event: 'changed' })
}

export function emitSave(content: string): void {
  postToParent({ event: 'save', data: { content } })
}

export function emitExit(modified: boolean): void {
  postToParent({ event: 'exit', data: { modified } })
}
