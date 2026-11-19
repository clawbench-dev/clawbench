import { beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { categoryItems } from '@/components/settings/settingsFieldMap'
import { highlightParts, searchSettings, settingsHits, groupHitsByCategory } from '@/components/settings/settingsSearch'

/**
 * The search index behind the settings home page's box (issue #691).
 *
 * Runs against the REAL locale files, because the contract of the feature is
 * about the words the user actually sees: a label that exists only in
 * `categoryItems` is findable only if its translation does too.
 */
describe('settings search index', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'zh'
  })

  it('indexes every category and a large number of rows', () => {
    const hits = settingsHits()
    const categoryHits = hits.filter(h => h.kind === 'category')
    // `tts_engine` is a sub-page's panel, not an index row (SettingsIndex.test
    // pins the same exclusion for the rendered list).
    const declared = Object.keys(categoryItems).filter(id => id !== 'tts_engine')
    expect(categoryHits.map(h => h.categoryId).sort()).toEqual(declared.sort())
    expect(hits.filter(h => h.kind === 'item').length).toBeGreaterThan(200)
  })

  it('finds a row by its Chinese label', () => {
    const hits = searchSettings('自动继续')
    expect(hits.some(h => h.id === 'chat.auto_continue_enabled')).toBe(true)
  })

  it('finds the same row by its English label', () => {
    i18n.global.locale.value = 'en'
    const byEnglish = searchSettings('auto continue')
    expect(byEnglish.some(h => h.id === 'chat.auto_continue_enabled')).toBe(true)
  })

  it('finds a row by English text while the UI is Chinese', () => {
    // The issue asks for both names to be searchable regardless of the
    // language the UI is currently in.
    i18n.global.locale.value = 'en'
    const englishLabel = settingsHits().find(h => h.id === 'chat.auto_continue_enabled')!.label
    expect(englishLabel.toLowerCase()).toContain('continue')

    i18n.global.locale.value = 'zh'
    const hits = searchSettings('continue')
    expect(hits.some(h => h.id === 'chat.auto_continue_enabled')).toBe(true)
  })

  it('finds a row by a word from its description', () => {
    const hits = searchSettings('追踪像素')
    expect(hits.some(h => h.id === 'mailLoadRemoteImages')).toBe(true)
  })

  it('indexes fields inside group panels, including per-mode credentials', () => {
    const hits = searchSettings('AppSecret')
    expect(hits.some(h => h.id === 'dingtalk.app_secret' || h.id === 'feishu.app_secret')).toBe(true)
  })

  it('ranks a label match above a description-only match', () => {
    const hits = searchSettings('主题')
    expect(hits[0].id).toBe('theme')
  })

  it('treats several words as AND', () => {
    const both = searchSettings('邮件 通知')
    expect(both.length).toBeGreaterThan(0)
    expect(both.every(h => h.haystack.includes('邮件') && h.haystack.includes('通知'))).toBe(true)
    // Narrower than either word alone.
    expect(both.length).toBeLessThan(searchSettings('邮件').length)
    expect(both.length).toBeLessThan(searchSettings('通知').length)
  })

  it('returns nothing for an empty query or an unknown one', () => {
    expect(searchSettings('')).toEqual([])
    expect(searchSettings('   ')).toEqual([])
    expect(searchSettings('zzzz-not-a-setting')).toEqual([])
  })

  it('is case-insensitive', () => {
    expect(searchSettings('IMAP').map(h => h.id)).toEqual(searchSettings('imap').map(h => h.id))
  })

  it('groups hits by category, keeping the category hit with its rows', () => {
    const groups = groupHitsByCategory(searchSettings('邮件'))
    const mail = groups.get('mail') ?? []
    expect(mail.length).toBeGreaterThan(1)
    expect(mail.some(h => h.kind === 'item')).toBe(true)
  })

  it('caches one index per locale but follows a locale switch', () => {
    i18n.global.locale.value = 'zh'
    const zhHits = settingsHits()
    expect(settingsHits()).toBe(zhHits)
    i18n.global.locale.value = 'en'
    expect(settingsHits()).not.toBe(zhHits)
    expect(settingsHits()[0].categoryLabel).not.toBe(zhHits[0].categoryLabel)
  })
})

// ── Highlighting (issue #691: 命中结果高亮展示) ──
describe('highlightParts', () => {
  const hitText = (parts: ReturnType<typeof highlightParts>) => parts.filter(p => p.hit).map(p => p.text)

  it('marks the typed word and leaves the rest alone', () => {
    // 「收信」is not contiguous in 每多少分钟收一次信 (收一次信), so this uses a
    // sentence where it appears twice, as the mail interval description does.
    const parts = highlightParts('收信越频繁，收信越费流量', '收信')
    expect(hitText(parts)).toEqual(['收信', '收信'])
    expect(parts.map(p => p.text).join('')).toBe('收信越频繁，收信越费流量')
  })

  it('matches case-insensitively but keeps the original casing', () => {
    expect(hitText(highlightParts('IMAP 收信', 'imap'))).toEqual(['IMAP'])
  })

  it('marks every occurrence and every query word', () => {
    const parts = highlightParts('邮件与邮件通知', '邮件 通知')
    expect(hitText(parts)).toEqual(['邮件', '邮件', '通知'])
  })

  it('marks nothing when the text does not contain the query', () => {
    expect(hitText(highlightParts('主题', '邮件'))).toEqual([])
  })

  it('returns the text untouched for an empty query or empty text', () => {
    expect(highlightParts('主题', '  ')).toEqual([{ text: '主题', hit: false }])
    expect(highlightParts('', '主题')).toEqual([{ text: '', hit: false }])
  })

  it('never splits a word: the earliest match wins, longest on a tie', () => {
    // '邮件' starts before '件通', so it is taken whole; the remaining '通知'
    // holds no second word. The other order would have split 邮件 in half.
    expect(hitText(highlightParts('邮件通知', '件通 邮件'))).toEqual(['邮件'])
  })

  it('does not loop forever on adjacent matches', () => {
    expect(hitText(highlightParts('aaa', 'a')).length).toBe(3)
  })
})
