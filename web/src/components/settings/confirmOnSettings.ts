/**
 * Settings that must be explained before they can be switched ON.
 *
 * These are not merely dangerous — they are the ones whose consequence is not
 * visible from the row itself, and whose default exists for a reason the user
 * has to hear before overriding it. The dialog carries that reason; cancelling
 * leaves the switch where it was, so the value can never be changed by accident.
 *
 * Exported so the mapping and the off->on rule are testable without mounting the
 * whole settings panel (which pulls in every composable in the app).
 */
export const confirmOnKeys: Record<string, string> = {
  // Letting the AI's own writes re-trigger the AI is the one setting here that
  // can loop on its own.
  'forge.allow_self_authored_events': 'settings.items.forgeAllowSelfAuthoredConfirm',
}

/**
 * requiresConfirmBeforeEnabling reports whether changing `key` from `from` to
 * `to` needs an explanation first.
 *
 * Only the OFF -> ON transition is guarded. Turning a guarded setting back OFF
 * must stay one click: the warning exists to make an opt-in deliberate, not to
 * trap the user in it.
 */
export function requiresConfirmBeforeEnabling(key: string, from: unknown, to: unknown): boolean {
  if (!confirmOnKeys[key]) return false
  return from === false && to === true
}
