/**
 * Escape layers: one window listener, and only the newest open layer reacts.
 *
 * A dialog cannot rely on focus alone. Opening one from a popup menu can leave
 * focus on the trigger behind the overlay, so the old `keydown.escape` handler
 * on the overlay never fired (the reported "Esc does nothing" bug). A window
 * listener has no such precondition.
 *
 * The stack is what makes nesting correct: without it, every open overlay
 * handled the same keypress and one Escape closed the whole stack.
 *
 * Layers that consume Escape for something narrower (a search box clearing its
 * query) must call `preventDefault()`; this listener then leaves the dialog
 * alone for that keypress.
 */
type EscapeLayer = { close: () => void }

const layers: EscapeLayer[] = []
let installed = false

function onKeydown(event: KeyboardEvent): void {
  if (event.key !== 'Escape' || event.defaultPrevented) return
  const top = layers[layers.length - 1]
  if (!top) return
  // Claim the keypress so an outer shell (drawer, page) does not also react.
  event.preventDefault()
  top.close()
}

function install(): void {
  if (installed || typeof window === 'undefined') return
  window.addEventListener('keydown', onKeydown)
  installed = true
}

function uninstall(): void {
  if (!installed || typeof window === 'undefined') return
  window.removeEventListener('keydown', onKeydown)
  installed = false
}

/**
 * Pushes a layer and returns its disposer. Push on open, dispose on close and
 * on unmount; the disposer is idempotent.
 */
export function pushEscapeLayer(close: () => void): () => void {
  const layer: EscapeLayer = { close }
  layers.push(layer)
  install()
  return () => {
    const index = layers.indexOf(layer)
    if (index >= 0) layers.splice(index, 1)
    if (layers.length === 0) uninstall()
  }
}

/** Test-only: drop every layer and the listener. */
export function resetEscapeLayersForTest(): void {
  layers.length = 0
  uninstall()
}
