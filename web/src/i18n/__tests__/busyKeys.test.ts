import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Keys for the long-action busy feedback (fork / ACP sync).
 *
 * A missing key renders the raw key string in the UI — and this text lands in a
 * sticky toast that stays on screen for the whole (slow) action, so a raw
 * `chat.busy.syncing` would be the most visible possible failure. vue-tsc and
 * the build both ignore it, so parity is asserted here instead of trusted.
 */
describe('busy feedback keys', () => {
  const KEYS = ['forking', 'syncing', 'elapsed'] as const

  it('defines every busy key in both locales', () => {
    for (const key of KEYS) {
      expect(en.chat.busy[key], `en missing chat.busy.${key}`).toBeTypeOf('string')
      expect(zh.chat.busy[key], `zh missing chat.busy.${key}`).toBeTypeOf('string')
      expect(en.chat.busy[key].length, `en chat.busy.${key} is empty`).toBeGreaterThan(0)
      expect(zh.chat.busy[key].length, `zh chat.busy.${key} is empty`).toBeGreaterThan(0)
    }
  })

  it('keeps the elapsed placeholder in both locales', () => {
    // The elapsed counter is interpolated by the caller; losing `{elapsed}`
    // silently degrades the toast to a static label and removes the only signal
    // distinguishing "working" from "hung".
    expect(en.chat.busy.elapsed).toContain('{elapsed}')
    expect(zh.chat.busy.elapsed).toContain('{elapsed}')
  })

  it('labels forking and syncing distinctly in both locales', () => {
    // Two different slow actions; an identical label would misreport which one
    // is running.
    expect(en.chat.busy.forking).not.toBe(en.chat.busy.syncing)
    expect(zh.chat.busy.forking).not.toBe(zh.chat.busy.syncing)
  })
})
