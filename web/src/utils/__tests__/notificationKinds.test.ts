import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { NOTIFICATION_KINDS, notificationKindSpec } from '@/utils/notificationKinds'

function lookup(messages: Record<string, unknown>, path: string): unknown {
  return path.split('.').reduce<unknown>((current, part) => {
    if (!current || typeof current !== 'object') return undefined
    return (current as Record<string, unknown>)[part]
  }, messages)
}

/**
 * The notification center's per-kind table is the frontend half of the
 * extension point. These guards make "add a kind" a complete operation: a
 * declared kind must be resolvable, have a destination, and carry label keys
 * that exist in both locales. A missing translation would otherwise render as
 * the raw key inside the chip.
 */
describe('notification kind table', () => {
  it('keeps the filter order and resolves every declared kind', () => {
    expect(NOTIFICATION_KINDS.map((kind) => kind.id)).toEqual([
      'session',
      'task',
      'forge',
      'usage',
      'meeting',
      'mail',
    ])
    for (const kind of NOTIFICATION_KINDS) {
      expect(notificationKindSpec(kind.id)).toBe(kind)
    }
  })

  it('gives every known kind a destination, and unknown kinds none', () => {
    for (const kind of NOTIFICATION_KINDS) {
      expect(typeof kind.open, `${kind.id} has no destination`).toBe('function')
    }
    expect(notificationKindSpec('plugin_new')).toBeUndefined()
  })

  it('has every label key in both locales', () => {
    for (const kind of NOTIFICATION_KINDS) {
      const keys = [kind.labelKey, ...Object.values(kind.eventLabels)]
      for (const key of keys) {
        expect(lookup(zh, key), `zh is missing ${key}`).toBeTypeOf('string')
        expect(lookup(en, key), `en is missing ${key}`).toBeTypeOf('string')
      }
    }
    // The unknown-kind fallback is part of the contract, not a stray string.
    expect(lookup(zh, 'notifications.kind.other')).toBeTypeOf('string')
    expect(lookup(en, 'notifications.kind.other')).toBeTypeOf('string')
  })
})
