/**
 * Whether the renderer has registered its notification-click listeners.
 *
 * This lives in its own module to keep `window.ts` (which resets the flag on
 * load) and `notification.ts` (which reads it when a notification is clicked)
 * from importing each other.
 *
 * Why a flag at all: the renderer only has a listener between App.vue's
 * onMounted and teardown, and the page's `did-finish-load` fires well before
 * onMounted completes (it awaits the project load and session bootstrap). A
 * notification clicked in that gap would be sent to a page that drops it.
 */
let ready = false

export function markRendererReady(): void {
  ready = true
}

export function markRendererLoading(): void {
  ready = false
}

export function isRendererReady(): boolean {
  return ready
}

/** Test helper — restores the module to its initial state. */
export function resetRendererReady(): void {
  ready = false
}
