import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * The collapsed selection toolbar's action labels (copy / discuss / quote).
 *
 * Two things are pinned here:
 *
 * 1. The zh wording is 讨论, not 评论. The product renamed this action — it opens
 *    a free-form reply to the AI, not a code-review comment — and the label had
 *    drifted back once already. Pinning it makes a revert fail here.
 * 2. The i18n KEY stays `comment` while the wording changes. The key is also the
 *    wire literal the Android native menu sends
 *    (`window.__clawbenchSelectionAction('comment')`), so renaming it for a copy
 *    change would break the Java↔JS contract for no user-visible gain. This test
 *    documents that the key is intentionally stale-looking.
 */
describe('quote bar action labels', () => {
  it('labels the discuss action 讨论 in Chinese, not 评论', () => {
    expect(zh.quoteBar.comment).toBe('讨论')
    expect(zh.quoteBar.comment).not.toBe('评论')
  })

  it('labels the discuss action "Discuss" in English, not "Comment"', () => {
    expect(en.quoteBar.comment).toBe('Discuss')
  })

  it('keeps the i18n key as `comment` (it is the Android wire literal)', () => {
    // Both locales must still expose the key under this name: App.vue's native
    // bridge and SelectionMenuPolicy.jsAction() send the string "comment".
    expect(typeof zh.quoteBar.comment).toBe('string')
    expect(typeof en.quoteBar.comment).toBe('string')
    // The quote action keeps its own wording in both locales.
    expect(zh.quoteBar.quoteSelection).toBe('引用')
    expect(en.quoteBar.quoteSelection).toBe('Quote')
  })
})
