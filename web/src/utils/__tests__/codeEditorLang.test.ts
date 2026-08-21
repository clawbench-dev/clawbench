import { describe, it, expect } from 'vitest'
import { buildLangExtension, buildCompletionExtension, LANG_EXT } from '@/utils/codeEditorLang'

describe('buildLangExtension', () => {
  it('returns a truthy extension for static (high-frequency) languages', async () => {
    expect(await buildLangExtension('javascript')).toBeTruthy()
    expect(await buildLangExtension('typescript')).toBeTruthy()
    expect(await buildLangExtension('json')).toBeTruthy()
    expect(await buildLangExtension('css')).toBeTruthy()
    expect(await buildLangExtension('html')).toBeTruthy()
    expect(await buildLangExtension('go')).toBeTruthy()
    expect(await buildLangExtension('python')).toBeTruthy()
    expect(await buildLangExtension('yaml')).toBeTruthy()
    expect(await buildLangExtension('xml')).toBeTruthy()
    expect(await buildLangExtension('markdown')).toBeTruthy()
  })

  it('returns a truthy extension for lazy-loaded official additions', async () => {
    expect(await buildLangExtension('rust')).toBeTruthy()
    expect(await buildLangExtension('java')).toBeTruthy()
    expect(await buildLangExtension('c')).toBeTruthy()
    expect(await buildLangExtension('cpp')).toBeTruthy()
    expect(await buildLangExtension('sql')).toBeTruthy()
    expect(await buildLangExtension('php')).toBeTruthy()
    expect(await buildLangExtension('vue')).toBeTruthy()
    expect(await buildLangExtension('less')).toBeTruthy()
    expect(await buildLangExtension('sass')).toBeTruthy()
    expect(await buildLangExtension('liquid')).toBeTruthy()
    expect(await buildLangExtension('angular')).toBeTruthy()
    expect(await buildLangExtension('wast')).toBeTruthy()
  })

  it('returns a truthy extension for lazy-loaded community packages', async () => {
    expect(await buildLangExtension('bash')).toBeTruthy()
    expect(await buildLangExtension('shell')).toBeTruthy()
    expect(await buildLangExtension('lua')).toBeTruthy()
    expect(await buildLangExtension('swift')).toBeTruthy()
    expect(await buildLangExtension('kotlin')).toBeTruthy()
    expect(await buildLangExtension('scala')).toBeTruthy()
    expect(await buildLangExtension('ruby')).toBeTruthy()
    expect(await buildLangExtension('diff')).toBeTruthy()
    expect(await buildLangExtension('csharp')).toBeTruthy()
    expect(await buildLangExtension('perl')).toBeTruthy()
    expect(await buildLangExtension('makefile')).toBeTruthy()
    expect(await buildLangExtension('r')).toBeTruthy()
  })

  it('returns empty array for unknown languages (plain text fallback)', async () => {
    expect(await buildLangExtension('toml')).toEqual([])
    expect(await buildLangExtension('graphql')).toEqual([])
    expect(await buildLangExtension('nginx')).toEqual([])
  })

  it('returns empty array for empty string', async () => {
    expect(await buildLangExtension('')).toEqual([])
  })
})

describe('buildCompletionExtension', () => {
  it('returns a non-empty extension array for languages with completion sources', async () => {
    const ext = await buildCompletionExtension('javascript')
    expect(Array.isArray(ext)).toBe(true)
    expect(ext.length).toBeGreaterThan(0)
  })

  it('returns empty array for languages without completion sources', async () => {
    const ext = await buildCompletionExtension('yaml')
    expect(ext).toEqual([])
  })

  it('returns empty array for unknown languages', async () => {
    const ext = await buildCompletionExtension('brainfuck')
    expect(ext).toEqual([])
  })

  it('returns extension for markdown (built-in HTML tag completion)', async () => {
    const ext = await buildCompletionExtension('markdown')
    expect(Array.isArray(ext)).toBe(true)
    expect(ext.length).toBeGreaterThan(0)
  })

  it('returns completion extension for SQL', async () => {
    const ext = await buildCompletionExtension('sql')
    expect(Array.isArray(ext)).toBe(true)
    expect(ext.length).toBeGreaterThan(0)
  })

  it('returns completion extension for TypeScript', async () => {
    const ext = await buildCompletionExtension('typescript')
    expect(Array.isArray(ext)).toBe(true)
    expect(ext.length).toBeGreaterThan(0)
  })
})

describe('COMPLETION_LANGS coverage', () => {
  it('every completion language has a corresponding LANG_EXT entry', async () => {
    const completionLangs = ['javascript', 'typescript', 'html', 'css', 'python', 'sql', 'go', 'less', 'sass', 'liquid', 'markdown']
    for (const lang of completionLangs) {
      expect(lang in LANG_EXT).toBe(true)
      // Also verify the completion extension works for each
      const ext = await buildCompletionExtension(lang)
      expect(ext.length).toBeGreaterThan(0)
    }
  })
})
