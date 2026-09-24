import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Ensures the conversation-share keys exist in both locales.
 *
 * The share dialog and the read-only share page are only ever exercised in
 * whichever locale the viewer happens to have, so a key missing from one
 * locale ships as a raw key string in production rather than failing a build.
 */
describe('i18n sessionShare keys completeness', () => {
  it('en and zh have the same sessionShare keys', () => {
    const enKeys = Object.keys(en.sessionShare)
    const zhKeys = Object.keys(zh.sessionShare)
    expect(enKeys.filter(k => !zhKeys.includes(k)), 'keys only in en').toEqual([])
    expect(zhKeys.filter(k => !enKeys.includes(k)), 'keys only in zh').toEqual([])
  })

  it('no sessionShare values are empty strings', () => {
    for (const key of Object.keys(en.sessionShare)) {
      expect((en.sessionShare as Record<string, string>)[key], `en.sessionShare.${key}`).not.toBe('')
    }
    for (const key of Object.keys(zh.sessionShare)) {
      expect((zh.sessionShare as Record<string, string>)[key], `zh.sessionShare.${key}`).not.toBe('')
    }
  })

  // The viewer-side keys live in the shared `share` namespace alongside the
  // file-share ones.
  it('the share namespace has the session-viewer keys in both locales', () => {
    for (const key of ['sharedConversation', 'messageCount']) {
      expect((en.share as Record<string, string>)[key], `en.share.${key}`).toBeTruthy()
      expect((zh.share as Record<string, string>)[key], `zh.share.${key}`).toBeTruthy()
    }
  })

  // Every key the share UI references must exist. The parity check above only
  // compares en against zh — if BOTH are missing a key, it still passes while
  // the UI renders the raw key string. That happened with roleUser/
  // roleAssistant/active, caught only by an end-to-end browser test.
  it('every key the share UI references exists', () => {
    const requiredSessionShare = [
      'button', 'buttonActive', 'title', 'explain', 'securityHint',
      'selectAll', 'deselectAll', 'selectedCount',
      'viewFullInLink', 'generatingCannotShare', 'empty',
      'generate', 'regenerate', 'regenerateTip', 'copyTip', 'copied',
      'revoke', 'revoked', 'openPage',
      'confirmRegenerate', 'confirmRevoke', 'loadFailed',
      'active', 'roleUser', 'roleAssistant',
    ]
    for (const key of requiredSessionShare) {
      expect((en.sessionShare as Record<string, string>)[key], `en.sessionShare.${key}`).toBeTruthy()
      expect((zh.sessionShare as Record<string, string>)[key], `zh.sessionShare.${key}`).toBeTruthy()
    }

    for (const key of ['sharedConversation', 'messageCount', 'loading', 'notFound', 'invalidTitle', 'invalidUrl']) {
      expect((en.share as Record<string, string>)[key], `en.share.${key}`).toBeTruthy()
      expect((zh.share as Record<string, string>)[key], `zh.share.${key}`).toBeTruthy()
    }
  })

  // The shared-conversations drawer. Parity is not enough on its own — a key
  // missing from BOTH locales would still pass that check while rendering the
  // raw key, so every key the component references is listed here explicitly.
  it('every sharedSessions key the drawer references exists in both locales', () => {
    const required = [
      'button', 'title', 'empty',
      'openConversation', 'openInNewTab', 'copyLink', 'copied',
      'revoke', 'revoked', 'clearAll', 'clear',
      'confirmClearAll', 'confirmRevoke',
      'archived', 'archivedHint', 'messageCount',
    ]
    for (const key of required) {
      expect((en.sharedSessions as Record<string, string>)[key], `en.sharedSessions.${key}`).toBeTruthy()
      expect((zh.sharedSessions as Record<string, string>)[key], `zh.sharedSessions.${key}`).toBeTruthy()
    }
  })

  // ── Terminology ──
  //
  // The feature must not mix 对话 and 会话 for the same object. It did: the
  // button said 分享对话 while the dialog body said 该会话, and the drawer
  // pointed at a button name that did not match the button. Parity and
  // non-emptiness checks all pass on inconsistent wording, so this asserts the
  // wording itself.
  //
  // 会话 is the project term (151 uses vs 42) and matches the session list the
  // share entry lives in. English is deliberately NOT asserted: it says
  // "conversation" consistently, which is the natural choice there.
  it('the zh share copy uses one term (会话), never 对话', () => {
    const shareNamespaces = [zh.sessionShare, zh.sharedSessions] as Record<string, string>[]
    for (const ns of shareNamespaces) {
      for (const [key, value] of Object.entries(ns)) {
        expect(value, `zh ${key} mixes 对话 into the share copy`).not.toContain('对话')
      }
    }
    expect((zh.share as Record<string, string>).sharedConversation).not.toContain('对话')
  })

  // The drawer instructs the user to right-click and pick a named menu item,
  // so that quoted name must equal the actual button label. They disagreed
  // when the button was renamed and this string was not.
  it('the drawer instruction quotes the real button label', () => {
    const label = (zh.sessionShare as Record<string, string>).button
    expect((zh.sharedSessions as Record<string, string>).empty).toContain(label)
  })

  // The revoke confirmations interpolate the conversation title and the count.
  it('sharedSessions interpolated keys keep their placeholders', () => {
    for (const key of ['confirmRevoke'] as const) {
      expect((en.sharedSessions as Record<string, string>)[key]).toContain('{')
      expect((zh.sharedSessions as Record<string, string>)[key]).toContain('{')
    }
    expect((en.sharedSessions as Record<string, string>).messageCount).toContain('{count}')
    expect((zh.sharedSessions as Record<string, string>).messageCount).toContain('{count}')
  })
  // Keys the dialog interpolates must keep their placeholders in both locales,
  // otherwise the count renders as a bare literal.
  it('interpolated keys keep their placeholders', () => {
    for (const key of ['selectedCount'] as const) {
      expect((en.sessionShare as Record<string, string>)[key]).toContain('{')
      expect((zh.sessionShare as Record<string, string>)[key]).toContain('{')
    }
    expect((en.share as Record<string, string>).messageCount).toContain('{count}')
    expect((zh.share as Record<string, string>).messageCount).toContain('{count}')
  })
})
