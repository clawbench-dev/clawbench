/**
 * "Mail me this file / folder" — the shared action behind the file header's
 * menu item and the file manager's context menu.
 *
 * The composer lives in another panel and its attachments have to be readable
 * by the server, so a path is not enough on its own: a folder has to become one
 * file first. That is `/api/mail/attachments`' job — it returns the same
 * `{filename, path, size}` shape an uploaded attachment has, so the composer
 * cannot tell the two apart.
 *
 * Why the server packs the folder instead of the browser: the zip is built from
 * files already on the server's disk. Streaming it down and uploading it back
 * would copy the whole tree through the client for no benefit, and a large tree
 * would have to fit in the tab's memory.
 */
import { apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { composeMail } from '@/composables/useMailCompose'
import { serverConfig } from '@/composables/useSettingsConfig'
import { builtInMailAvailable } from '@/utils/shareActions'
import type { MailDraftFile } from '@/utils/mailTypes'

const TAG = 'MailAttach'

/**
 * What happened, so the caller can word its message honestly:
 * `opened` — the composer is showing with the files attached;
 * `no-mailbox` — the built-in client is off or has no account;
 * `failed` — staging failed (missing path, oversized folder, server error).
 */
export type MailSendOutcome = 'opened' | 'no-mailbox' | 'failed'

export interface MailSendResult {
  outcome: MailSendOutcome
  /** Server's (already localized) explanation, when there is one. */
  error?: string
}

/** Ask the server to turn paths into composer attachments. */
export async function stageMailAttachments(paths: string[]): Promise<MailDraftFile[]> {
  const data = await apiPost<{ attachments?: MailDraftFile[] }>('/api/mail/attachments', { paths })
  return Array.isArray(data.attachments) ? data.attachments : []
}

/**
 * Stage `paths` and open the composer with them attached.
 *
 * The mail feature is checked before staging: packing a folder for a client
 * that will refuse to open would be wasted work, and the caller can then say
 * "configure a mailbox" instead of showing an empty tab.
 */
export async function sendPathsByMail(paths: string[], subject: string): Promise<MailSendResult> {
  if (paths.length === 0) return { outcome: 'failed' }
  if (!builtInMailAvailable(serverConfig.value as Record<string, unknown>)) return { outcome: 'no-mailbox' }

  let attachments: MailDraftFile[]
  try {
    attachments = await stageMailAttachments(paths)
  } catch (e) {
    appLog.w(TAG, 'stage attachments failed', e)
    return { outcome: 'failed', error: (e as Error)?.message }
  }
  if (attachments.length === 0) return { outcome: 'failed' }

  return { outcome: (await composeMail(subject, '', attachments)) ? 'opened' : 'no-mailbox' }
}
