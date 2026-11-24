import { describe, expect, it } from 'vitest'
import { confirmOnKeys, requiresConfirmBeforeEnabling } from '@/components/settings/confirmOnSettings'

/**
 * Settings that cannot be switched ON without an explanation.
 *
 * The rule is deliberately narrow: confirm only on the OFF→ON transition. A
 * switch that is already on must be turnable OFF without a dialog — otherwise
 * the warning becomes a trap, and the user cannot undo what it just warned
 * about.
 */
describe('requiresConfirmBeforeEnabling', () => {
  it('asks for confirmation when a guarded setting goes off -> on', () => {
    expect(requiresConfirmBeforeEnabling('forge.allow_self_authored_events', false, true)).toBe(true)
  })

  it('does not ask when a guarded setting is turned OFF', () => {
    // Undoing must stay one click; a dialog here would block the safe direction.
    expect(requiresConfirmBeforeEnabling('forge.allow_self_authored_events', true, false)).toBe(false)
  })

  it('does not re-ask when the value is not actually changing', () => {
    expect(requiresConfirmBeforeEnabling('forge.allow_self_authored_events', true, true)).toBe(false)
    expect(requiresConfirmBeforeEnabling('forge.allow_self_authored_events', false, false)).toBe(false)
  })

  it('never asks for settings outside the guarded set', () => {
    expect(requiresConfirmBeforeEnabling('forge.pause_event_tasks', false, true)).toBe(false)
    expect(requiresConfirmBeforeEnabling('forge.notify.opened', false, true)).toBe(false)
  })

  it('ignores non-boolean values', () => {
    // A stale string ("on") from a hand-edited config must not be read as true.
    expect(requiresConfirmBeforeEnabling('forge.allow_self_authored_events', false, 'on')).toBe(false)
  })

  it('guards the self-authored switch with an explanation key', () => {
    // The dialog's body is a translation of the risk; a missing key would render
    // the raw key name and explain nothing.
    expect(confirmOnKeys['forge.allow_self_authored_events'])
      .toBe('settings.items.forgeAllowSelfAuthoredConfirm')
  })
})
