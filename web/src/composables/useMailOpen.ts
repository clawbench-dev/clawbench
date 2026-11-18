/**
 * "Open this message in the mail panel" — a parked request, for the same reason
 * as `useMailCompose`: the panel is lazily mounted by its TabPanel, so a click
 * that arrives before the user has ever visited the 邮件 tab has no listener to
 * open the reader.
 *
 * The deeplink handler (plugins/mail/plugin.ts) switches the tab; it parks the
 * message id here ONLY while no panel is mounted. A mounted panel handles the
 * event itself, so parking there as well would leave a stale id that the next
 * mount would open as if the user had just clicked it.
 */
import { ref } from 'vue'

/** Message the panel should open once it exists. */
const pending = ref<number | null>(null)

/**
 * How many mail panels are mounted. A panel that is on screen handles the
 * deeplink directly, so parking is only for the cold case.
 */
let mountedPanels = 0

export function mailPanelMounted(): void {
  mountedPanels++
}

export function mailPanelUnmounted(): void {
  if (mountedPanels > 0) mountedPanels--
}

/** Park a message to open on the next mount — only when nobody is listening. */
export function parkPendingMailOpen(messageId: number): void {
  if (!messageId || mountedPanels > 0) return
  pending.value = messageId
}

/** Take the parked message, if any (consumed exactly once). */
export function takePendingMailOpen(): number | null {
  const id = pending.value
  pending.value = null
  return id
}

/** Test seam: forget any parked message. */
export function _resetMailOpenForTest(): void {
  pending.value = null
}
