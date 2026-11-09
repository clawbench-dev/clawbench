/**
 * "Open a new mail with this already filled in" — from anywhere in the app.
 *
 * WHY A MODULE-LEVEL PARKED REQUEST
 * ---------------------------------
 * The composer lives inside the mail panel, which is lazily mounted by its
 * TabPanel and only exists once the user has visited the tab. A caller in
 * another panel therefore cannot call into it directly: the component may not
 * be mounted yet, and a prop would have to be threaded through App.vue for one
 * screen's convenience.
 *
 * So the request is parked here, the tab switch is requested, and the panel
 * consumes the request once it mounts/activates. This mirrors how settings
 * deep-links work (`setPendingSettingsCategory`) — same problem, same shape.
 *
 * `compose()` resolves to whether the built-in client will handle it, so the
 * caller can fall back to the system mail app instead of sending the user to a
 * tab that cannot open a composer (no mailbox configured, feature switched off).
 */
import { ref } from 'vue'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import type { MailDraftFile } from '@/utils/mailTypes'

const TAG = 'MailCompose'

/** A parked "start a new mail" request. */
export interface MailComposeRequest {
  subject: string
  body: string
  /** Attachments already staged by the caller (e.g. a file the user mailed
   *  from the file manager). Optional — most requests start empty. */
  attachments: MailDraftFile[]
  /** Set by the panel once it has taken the request (guards double-consume). */
  consumed: boolean
}

const pending = ref<MailComposeRequest | null>(null)

/**
 * Ask the mail panel to open a new message.
 *
 * Returns false — WITHOUT parking anything — when there is no mailbox to send
 * from, so the caller can hand the message to the system mail app instead. The
 * probe is one small GET, and it is deliberately run before the tab switch:
 * parking first and discovering the problem afterwards would strand the user on
 * an empty mail tab with no message.
 *
 * `attachments` are already-staged files (see `utils/mailAttach.ts`), not paths:
 * the panel only knows how to send what the composer can display.
 */
export async function composeMail(subject: string, body: string, attachments: MailDraftFile[] = []): Promise<boolean> {
  try {
    const data = await apiGet<{ accounts?: unknown[] }>('/api/mail/accounts')
    if (!Array.isArray(data.accounts) || data.accounts.length === 0) return false
  } catch (e) {
    // A server without the mail feature answers 404 here; treat every failure
    // as "cannot compose" rather than guessing.
    appLog.w(TAG, 'mail account probe failed', e)
    return false
  }

  pending.value = { subject, body, attachments, consumed: false }
  // App.vue listens for this and switches to the mail tab; the panel listens
  // too and opens the composer once it is active.
  window.dispatchEvent(new CustomEvent('clawbench-compose-mail'))
  return true
}

/** Take the parked request, if any (consumed exactly once). */
export function takePendingMailCompose(): MailComposeRequest | null {
  const req = pending.value
  if (!req || req.consumed) return null
  req.consumed = true
  pending.value = null
  return req
}

/** Test seam: forget any parked request. */
export function _resetMailComposeForTest(): void {
  pending.value = null
}

export function useMailCompose() {
  return { compose: composeMail }
}
