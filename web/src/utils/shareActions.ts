/**
 * "Send this link somewhere else" — one entry point per intent, per host.
 *
 * WHY THIS IS NOT JUST `navigator.share`
 * --------------------------------------
 * The three hosts disagree about what sharing a LINK even means, and every
 * wrong guess fails silently (a tap that does nothing), so the differences are
 * resolved here once instead of at each call site:
 *
 *   - Android / HarmonyOS shells: the system share sheet is the whole point of
 *     a phone — it is how a link reaches WeChat, DingTalk, a note app. Both
 *     expose `shareText` on the bridge.
 *   - Desktop (Electron): there is NO system share sheet. `shareText` on that
 *     bridge copies to the clipboard and returns, which is a real behaviour but
 *     must be LABELLED as copying, not dressed up as a share sheet. Sharing a
 *     FILE is different and genuinely works — the shell writes it to a temp
 *     file and hands it to the OS default handler — so file shares keep that
 *     path.
 *   - Plain browser: `navigator.share` when available (mobile browsers, some
 *     desktops), otherwise the clipboard.
 *
 * `shareText`'s return value is deliberately not trusted to tell these apart:
 * the bridge method exists on every host and always resolves.
 */
import { getNative, getNativeHostPlatform } from '@/utils/clawbenchNative'
import { copyText } from '@/utils/clipboard'
import { openExternalUrl } from '@/utils/externalLink'
import { appLog } from '@/utils/appLog'

const TAG = 'ShareActions'

/** What the "share" button will actually do on this host. */
export type ShareCapability = 'sheet' | 'copy'

/**
 * Whether a real system share sheet exists on this host.
 *
 * Electron is excluded by name rather than by probing `shareText`, because the
 * desktop bridge implements that method by writing to the clipboard — probing
 * would report "sheet" on the one host that has none.
 */
export function shareCapability(): ShareCapability {
  const host = getNativeHostPlatform()
  if (host === 'android' || host === 'harmonyos') return 'sheet'
  if (host === 'electron') return 'copy'
  // Plain browser: the Web Share API is the sheet. It requires a secure
  // context, which an http install on the LAN does not have — hence the check
  // rather than a bare `typeof navigator.share === 'function'`.
  if (typeof navigator !== 'undefined' && typeof navigator.share === 'function' && window.isSecureContext) {
    return 'sheet'
  }
  return 'copy'
}

/**
 * Hand a link to another app.
 *
 * Returns what actually happened so the caller can word its toast honestly:
 * `'shared'` (a sheet was shown), `'copied'` (no sheet on this host, so the
 * link went to the clipboard).
 */
export async function shareLink(url: string, title: string): Promise<'shared' | 'copied'> {
  if (!url) return 'copied'

  const native = getNative()
  const host = getNativeHostPlatform()

  if ((host === 'android' || host === 'harmonyos') && native?.shareText) {
    try {
      await native.shareText(url)
      return 'shared'
    } catch (e) {
      // A shell that cannot show the chooser is not a reason to lose the link.
      appLog.w(TAG, 'native shareText failed, copying instead', e)
      copyText(url)
      return 'copied'
    }
  }

  if (host === 'web' && shareCapability() === 'sheet') {
    try {
      await navigator.share({ title, url })
      return 'shared'
    } catch (e) {
      // AbortError is the user closing the sheet — that is not a failure and
      // must NOT be answered by silently copying something they declined.
      if ((e as { name?: string })?.name === 'AbortError') return 'shared'
      appLog.w(TAG, 'navigator.share failed, copying instead', e)
      copyText(url)
      return 'copied'
    }
  }

  copyText(url)
  return 'copied'
}

/**
 * Whether the built-in mail client can be used for "send by email".
 *
 * Read from the live server config rather than a cached flag: the switch can be
 * turned off in Settings while this panel is open, and sending someone into a
 * tab that refuses to open is worse than falling back to `mailto:`.
 */
export function builtInMailAvailable(serverConfig: Record<string, unknown>): boolean {
  const mail = serverConfig.mail as Record<string, unknown> | undefined
  return mail?.enabled !== false
}

/** Build a `mailto:` URL with the link already in the body. */
export function mailtoUrl(subject: string, body: string): string {
  // `encodeURIComponent` on both parts: a subject containing "&" or a body with
  // a "#" would otherwise truncate the message at that character.
  return `mailto:?subject=${encodeURIComponent(subject)}&body=${encodeURIComponent(body)}`
}

/**
 * Open the system mail app with a prefilled message.
 *
 * Goes through `openExternalUrl` because the Android WebView swallows
 * `window.open` entirely (no `setSupportMultipleWindows`) — a bare anchor would
 * be a dead tap on the one host where the system mail app is the fallback.
 * Electron's URL policy allows `mailto:` and hands it to the OS.
 */
export function openSystemMail(subject: string, body: string): void {
  openExternalUrl(mailtoUrl(subject, body))
}
