import i18n from '@/i18n'
import { useDialog } from '@/composables/useDialog'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { getEffectiveUIScale, setLocalConfig } from '@/composables/useSettingsConfig'
import {
  computeAutoUIScale,
  currentScreenHeight,
  shouldSuggestUIScale,
} from '@/utils/uiScale'
import { appLog } from '@/utils/appLog'

const TAG = 'StartupUIScale'

/**
 * Remembers the factor the user already turned down, so a high-resolution
 * screen asks once rather than every launch.
 *
 * Stores the SUGGESTED factor, not a bare boolean: the same machine can later
 * be attached to a different panel, and a different suggestion is a genuinely
 * new question. Declining 200% on a 4K monitor and then moving to a 1440p one
 * still offers 133%.
 */
const DECLINED_KEY = 'clawbench_ui_scale_prompt_declined'

/** Once per app session — both startup paths call in (see App.vue). */
let prompted = false

function readDeclined(): number | null {
  try {
    const raw = localStorage.getItem(DECLINED_KEY)
    if (raw === null) return null
    const n = Number(JSON.parse(raw))
    return Number.isFinite(n) && n > 0 ? n : null
  } catch {
    return null
  }
}

function writeDeclined(factor: number): void {
  try {
    localStorage.setItem(DECLINED_KEY, JSON.stringify(factor))
  } catch {
    // Private mode / quota: the prompt simply asks again next launch.
  }
}

/**
 * Offer to apply the screen-derived UI scale at startup.
 *
 * The scale is a plain stored value, and the settings "auto fit" button is the
 * manual way to set it. That leaves one gap the user hit in practice: a fresh
 * install on a 1440p/4K panel renders the 1080p-designed UI tiny, and nothing
 * points at the slider. So startup measures the screen once and, when the fit
 * is FAR from what is applied, asks before changing anything.
 *
 * Why a prompt rather than applying it: the applied factor is deliberately
 * stable (deriving it at startup is what used to make the UI jump), and it is
 * the user's setting — silently overwriting a deliberate 100% would be worse
 * than the small UI. Accepting goes through `setLocalConfig`, so the value is
 * persisted and the settings slider shows it immediately.
 *
 * Skips:
 *   - the Android shell, where the OS scales by density and the settings
 *     "auto fit" row is hidden for the same reason;
 *   - a screen whose fit is not at least `UI_SCALE_SUGGEST_THRESHOLD` larger
 *     than the applied factor (1080p and below never prompt at all);
 *   - a fit the user already declined on this device;
 *   - a launch where another dialog is already open (the dialog store is a
 *     singleton and would resolve that one as cancelled).
 */
export async function maybePromptStartupUIScale(): Promise<void> {
  if (prompted) return
  prompted = true

  try {
    if (usePlatformDetect().isAndroidApp.value) return

    const current = getEffectiveUIScale()
    const suggested = computeAutoUIScale(currentScreenHeight())
    if (!shouldSuggestUIScale(current, suggested)) return
    if (readDeclined() === suggested) return

    const dialog = useDialog()
    if (dialog.state.value.visible) return

    // Called as a method on `global` rather than detached into a local, so the
    // i18n instance is the receiver (the same shape every other non-setup
    // caller uses).
    const t = (key: string, params?: Record<string, unknown>) => i18n.global.t(key, params as never)
    const pct = (n: number) => Math.round(n * 100)
    const accepted = await dialog.confirm(
      t('uiScalePrompt.message', { suggested: pct(suggested), current: pct(current) }),
      {
        title: t('uiScalePrompt.title'),
        confirmText: t('uiScalePrompt.accept', { pct: pct(suggested) }),
        cancelText: t('uiScalePrompt.decline'),
      },
    )

    if (accepted) {
      setLocalConfig('uiScale', suggested)
    } else {
      // `null` means the dialog was replaced by another one — not a decision,
      // so leave no trace and let the next launch ask again.
      if (accepted === false) writeDeclined(suggested)
    }
  } catch (e) {
    // A failed measurement or a dialog problem must never break startup.
    appLog.w(TAG, 'startup UI scale prompt failed', e)
  }
}

/** Test hook: forget the once-per-session guard. */
export function _resetStartupUIScalePromptForTest(): void {
  prompted = false
}
