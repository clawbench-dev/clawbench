import { apiPatch } from '@/utils/api'
import { appLog } from '@/utils/appLog'

// serverLanguage.ts owns "tell the Go server which language the UI is in".
//
// It exists because the server persists some user-visible text from background
// goroutines that have no HTTP request in scope — the auto-continue message
// being the first — and so cannot read the per-request X-Locale header. The
// browser is the source of truth for the language, so it pushes its choice to
// the server's `language` config.
//
// It lives in its own module (rather than inside useSettingsConfig) because two
// unrelated call sites need it — the settings-panel locale row and the header
// toggle in useLocale — and importing the settings composable from useLocale
// would close a cycle through stores/app.

const TAG = 'ServerLanguage'

/**
 * Push the current UI language to the server so text the server persists on its
 * own (auto-continue, and any future background string) matches the UI.
 *
 * Best-effort by design: a failure only means background strings fall back to
 * the server default, which must never surface as an error or block the UI.
 */
export async function syncServerLanguage(lang: string): Promise<void> {
  if (!lang) return
  try {
    await apiPatch('/api/config', { language: lang })
  } catch (err) {
    appLog.w(TAG, `failed to sync UI language to server: ${String(err)}`)
  }
}
