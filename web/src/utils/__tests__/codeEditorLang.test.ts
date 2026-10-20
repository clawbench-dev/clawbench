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

  it('returns a truthy extension for legacy stream modes', async () => {
    // These have no Lezer grammar; they are CodeMirror 5 stream parsers wrapped
    // by StreamLanguage. They must still colour the file, which is the whole
    // point of depending on @codemirror/legacy-modes.
    expect(await buildLangExtension('dockerfile')).toBeTruthy()
    expect(await buildLangExtension('nginx')).toBeTruthy()
    expect(await buildLangExtension('powershell')).toBeTruthy()
    expect(await buildLangExtension('toml')).toBeTruthy()
    expect(await buildLangExtension('protobuf')).toBeTruthy()
    expect(await buildLangExtension('cmake')).toBeTruthy()
    expect(await buildLangExtension('verilog')).toBeTruthy()
    expect(await buildLangExtension('julia')).toBeTruthy()
  })

  it('returns a truthy extension for graphql without pulling in the graphql runtime', async () => {
    // cm6-graphql would drag in the full `graphql` package (~6.5MB unpacked);
    // this is a hand-written simpleMode, so the check is that it parses at all.
    expect(await buildLangExtension('graphql')).toBeTruthy()
  })

  it('returns empty array for unknown languages (plain text fallback)', async () => {
    expect(await buildLangExtension('not_a_real_language')).toEqual([])
    // `regex` is the one file type neither engine has a grammar for.
    expect(await buildLangExtension('regex')).toEqual([])
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

describe('LANG_EXT coverage', () => {
  it('every declared language builds without throwing', async () => {
    // A typo in a dynamic import path only surfaces when that language is
    // opened, which is exactly when a user is looking at a file. Build them all.
    const failures: string[] = []
    for (const lang of Object.keys(LANG_EXT)) {
      try {
        const ext = await buildLangExtension(lang)
        if (Array.isArray(ext) && ext.length === 0) failures.push(lang)
      } catch (e) {
        failures.push(`${lang}: ${(e as Error).message}`)
      }
    }
    expect(failures).toEqual([])
  })

  it('covers every text file type that has a grammar on either engine', async () => {
    // Guards the seam between the file-type table and the editor: a language
    // listed in `fileType.ts` but absent here silently renders as plain text.
    const { getFileType } = await import('@/utils/fileType')
    const { hljs, ensureLanguage } = await import('@/utils/highlight')
    const textLangs = [
      '.proto', '.dart', '.ex', '.erl', '.hs', '.clj', '.ml', '.fs', '.groovy',
      '.glsl', '.cmake', '.gradle', '.m', '.v', '.vhd', '.vim', '.hbs', '.scss',
      '.nix', '.nim', '.cr', '.jl', '.elm', '.scm', '.tcl', '.f90', '.coffee',
      '.http', '.feature', '.thrift', '.capnp', '.d', '.vala', '.haxe', '.lisp',
      '.styl', '.twig', '.erb', '.adb', '.applescript', '.ahk', '.cob', '.zone',
      '.gcode', '.openscad', '.pde', '.qml', '.re', '.sml', '.sqf', '.stan',
      '.stata', '.textile', '.vb', '.vbs', '.webidl', '.wren', '.xq', '.zep',
      '.pgsql', '.sparql', '.ttl', '.toml', '.dockerfile', '.nginx', '.ps1',
      '.tex', '.graphql',
    ].map((ext) => getFileType('f' + ext).lang)

    // "Supported" means at least one render path can colour it: the editor has
    // a CodeMirror mode, or highlight.js can supply a grammar — eagerly or via
    // the lazy tier (which `ensureLanguage` resolves).
    const missing: string[] = []
    for (const lang of [...new Set(textLangs)]) {
      if (lang in LANG_EXT) continue
      if (await ensureLanguage(lang)) continue
      missing.push(lang)
    }
    expect(missing).toEqual([])
    // The eager registry must stay small; the lazy tier is what makes 170+
    // file types affordable.
    expect(hljs.listLanguages().length).toBeLessThan(80)
  })
})
