import { gt } from '@/composables/useLocale'
import { useToast } from '@/composables/useToast'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { generateSessionTitle } from '@/composables/useSessionIdentity'
import { isSummaryModelConfigured } from '@/utils/aiSummaryModel'

/**
 * Build the "auto-generate" options for the session-rename prompt dialog.
 *
 * Single source for BOTH rename entry points (the chat header title and the
 * session list's ⋮ menu). They are the same action reached two ways, so a
 * second copy of this logic would drift: a changed config path or i18n key
 * would silently apply to only one of them. See `sessionRename.test.ts`.
 *
 * The button is offered only when the user has configured the shared AI summary
 * model (`ai_summary.api.base_url`) — the only LLM this feature can call.
 * Returns an empty object otherwise, which the dialog treats as "no button".
 *
 * Errors surface as a toast (the dialog's generic generate handler only logs),
 * so a misconfigured or unreachable model is not a silent no-op.
 */
export function buildRenameGenerateOptions(sessionId: string) {
  const { serverConfig } = useSettingsConfig()
  if (!isSummaryModelConfigured(serverConfig.value)) return {}

  const toast = useToast()
  return {
    generateText: gt('chat.sessionRename.generate'),
    onGenerate: async () => {
      const title = await generateSessionTitle(sessionId)
      if (!title) {
        toast.show(gt('chat.sessionRename.generateFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
        return null
      }
      return title
    },
  }
}
