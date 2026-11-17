import { describe, expect, it } from 'vitest'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, resolve } from 'node:path'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Guards the knowledge-base locale entries.
 *
 * The kb namespace used to be key-complete only by luck: the folder-import flow
 * (preview dialog, "all duplicates", move) called t('kb.…') for keys that were
 * only ever added to zh.ts, so English users saw the raw key path. Deriving the
 * used keys from the components instead of hand-listing them is what makes the
 * next missing key fail here instead of in production.
 */
const KB_DIRS = ['web/src/components/kb']
const KB_FILES = ['web/src/composables/useKnowledgeBase.ts', 'web/src/components/file/FileManagerContent.vue']

function walk(root: string, out: string[] = []): string[] {
  for (const entry of readdirSync(root)) {
    const full = join(root, entry)
    if (statSync(full).isDirectory()) {
      if (entry === '__tests__') continue
      walk(full, out)
    } else if (full.endsWith('.vue') || full.endsWith('.ts')) {
      out.push(full)
    }
  }
  return out
}

function usedKBKeys(): string[] {
  const repoRoot = resolve(__dirname, '../../../..')
  const files = [
    ...KB_DIRS.flatMap((d) => walk(join(repoRoot, d))),
    ...KB_FILES.map((f) => join(repoRoot, f)),
  ]
  const keys = new Set<string>()
  for (const file of files) {
    const src = readFileSync(file, 'utf8')
    // Only literal keys: a dynamic t(`kb.${x}`) cannot be checked here, and
    // there is none in the kb components.
    for (const m of src.matchAll(/['"`]kb\.([A-Za-z0-9_]+)['"`]/g)) keys.add(m[1])
  }
  return [...keys].sort()
}

type Dict = Record<string, unknown>
const enKb = en.kb as Dict
const zhKb = zh.kb as Dict

describe('kb locale keys', () => {
  it('every kb.* key used by the knowledge-base UI exists in both locales', () => {
    const used = usedKBKeys()
    expect(used.length).toBeGreaterThan(50)
    const missingEn = used.filter((k) => typeof enKb[k] !== 'string' || !enKb[k])
    const missingZh = used.filter((k) => typeof zhKb[k] !== 'string' || !zhKb[k])
    expect(missingEn, 'used by the UI but missing/different shape in en.ts').toEqual([])
    expect(missingZh, 'used by the UI but missing/different shape in zh.ts').toEqual([])
  })

  it('the folder-import and move keys have real English text, not just a key', () => {    // Spelled out because these were the keys that only ever landed in zh:
    // asserting presence alone would not catch an English value that is an
    // untranslated copy of the Chinese one.
    const expected = [
      'dirImportAllDuplicates',
      'importPreviewTitle',
      'importPreviewMessage',
      'importPreviewTruncated',
      'importContinue',
      'move',
      'moveDone',
      'pickEmpty',
    ]
    for (const key of expected) {
      expect(enKb[key], `en.kb.${key}`).toBeTruthy()
      expect(zhKb[key], `zh.kb.${key}`).toBeTruthy()
      expect(enKb[key]).not.toBe(zhKb[key])
      expect(String(enKb[key])).toMatch(/[A-Za-z]/)
    }
  })

  it('keeps the placeholder tokens of the import-preview messages', () => {
    // The panel passes {added, skipped, duplicates, failed}; a translation that
    // drops one silently prints nothing where the number belongs.
    for (const [name, dict] of [['en', enKb], ['zh', zhKb]] as const) {
      for (const token of ['{added}', '{skipped}', '{duplicates}', '{failed}']) {
        expect(String(dict.importPreviewMessage), `${name} ${token}`).toContain(token)
      }
      expect(String(dict.moveDone), `${name} {count}`).toContain('{count}')
      expect(String(dict.moveDone), `${name} {name}`).toContain('{name}')
    }
  })

  it('en and zh agree on the kb.reason codes the server can send', () => {
    const enReason = enKb.reason as Dict
    const zhReason = zhKb.reason as Dict
    expect(Object.keys(enReason).sort()).toEqual(Object.keys(zhReason).sort())
    // The PDF panic fix added this code; a missing entry renders the raw code.
    expect(enReason.pdf_malformed).toBeTruthy()
    expect(zhReason.pdf_malformed).toBeTruthy()
  })
})
