import { describe, expect, it } from 'vitest'
import { getFileType, formatFileSize } from '@/utils/fileType.ts'
import { isThumbableExt } from '@/utils/fileManager.ts'

describe('getFileType', () => {
  it('detects TypeScript', () => {
    const ft = getFileType('app.ts')
    expect(ft.lang).toBe('typescript')
    expect(ft.label).toBe('TS')
  })

  it('detects Python', () => {
    const ft = getFileType('main.py')
    expect(ft.lang).toBe('python')
  })

  it('detects Go', () => {
    const ft = getFileType('server.go')
    expect(ft.lang).toBe('go')
  })

  it('detects Markdown', () => {
    const ft = getFileType('README.md')
    expect(ft.lang).toBe('markdown')
    expect(ft.isMarkdown).toBe(true)
  })

  it('detects JSON', () => {
    const ft = getFileType('package.json')
    expect(ft.lang).toBe('json')
  })

  it('detects HTML', () => {
    const ft = getFileType('index.html')
    expect(ft.isHtml).toBe(true)
  })

  it('detects PNG images', () => {
    const ft = getFileType('photo.png')
    expect(ft.isImage).toBe(true)
  })

  it('detects PDF', () => {
    const ft = getFileType('doc.pdf')
    expect(ft.isPdf).toBe(true)
  })

  it('detects MP3 audio', () => {
    const ft = getFileType('song.mp3')
    expect(ft.isAudio).toBe(true)
  })

  it('detects MP4 video', () => {
    const ft = getFileType('movie.mp4')
    expect(ft.isVideo).toBe(true)
  })

  it('detects Shell', () => {
    const ft = getFileType('script.sh')
    expect(ft.lang).toBe('bash')
  })

  it('returns plaintext for unknown extensions', () => {
    const ft = getFileType('data.xyz')
    expect(ft.lang).toBe('plaintext')
  })

  it('handles case-insensitive matching', () => {
    const ft = getFileType('APP.TS')
    expect(ft.lang).toBe('typescript')
  })

  it('detects YAML', () => {
    expect(getFileType('config.yaml').lang).toBe('yaml')
    expect(getFileType('config.yml').lang).toBe('yaml')
  })

  it('detects Dockerfile', () => {
    expect(getFileType('Dockerfile').lang).toBe('dockerfile')
  })

  it('detects CSS', () => {
    expect(getFileType('style.css').lang).toBe('css')
  })

  it('detects Vue', () => {
    expect(getFileType('App.vue').lang).toBe('vue')
  })

  it('detects Rust', () => {
    expect(getFileType('main.rs').lang).toBe('rust')
  })

  it('detects SQL', () => {
    expect(getFileType('query.sql').lang).toBe('sql')
  })

  it('detects PDF by extension', () => {
    const ft = getFileType('document.pdf')
    expect(ft.isPdf).toBe(true)
    expect(ft.lang).toBe('pdf')
  })

  it('detects Excalidraw', () => {
    const ft = getFileType('diagram.excalidraw')
    expect(ft.isExcalidraw).toBe(true)
    expect(ft.lang).toBe('plaintext')
  })

  it('detects Excalidraw case-insensitively', () => {
    const ft = getFileType('DIAGRAM.EXCALIDRAW')
    expect(ft.isExcalidraw).toBe(true)
  })

  it('detects the .xdraw short alias', () => {
    // .xdraw must map to the same FileType entry as .excalidraw, so it gets the
    // DRAW label, the Excalidraw colour and the canvas editor rather than
    // falling through to the generic plaintext default.
    const ft = getFileType('diagram.xdraw')
    expect(ft.isExcalidraw).toBe(true)
    expect(ft.lang).toBe('plaintext')
    expect(ft.label).toBe('DRAW')
    expect(ft).toEqual(getFileType('diagram.excalidraw'))
  })

  it('detects the .xdraw alias case-insensitively', () => {
    const ft = getFileType('DIAGRAM.XDRAW')
    expect(ft.isExcalidraw).toBe(true)
  })

  it('does not flag .json as Excalidraw', () => {
    const ft = getFileType('data.json')
    expect(ft.isExcalidraw).toBeUndefined()
  })
})

describe('formatFileSize', () => {
  it('formats bytes', () => {
    expect(formatFileSize(500)).toBe('500 B')
  })

  it('formats kilobytes', () => {
    expect(formatFileSize(2048)).toBe('2.0 KB')
  })

  it('formats megabytes', () => {
    expect(formatFileSize(2 * 1024 * 1024)).toBe('2.0 MB')
  })
})

describe('isThumbableExt', () => {
  it('returns true for PNG', () => {
    expect(isThumbableExt('photo.png')).toBe(true)
  })

  it('returns true for JPG/JPEG', () => {
    expect(isThumbableExt('photo.jpg')).toBe(true)
    expect(isThumbableExt('photo.jpeg')).toBe(true)
  })

  it('returns true for GIF', () => {
    expect(isThumbableExt('anim.gif')).toBe(true)
  })

  it('returns false for WebP (not thumbable)', () => {
    expect(isThumbableExt('photo.webp')).toBe(false)
  })

  it('returns false for non-image extensions', () => {
    expect(isThumbableExt('doc.pdf')).toBe(false)
    expect(isThumbableExt('main.go')).toBe(false)
  })

  it('handles full paths', () => {
    expect(isThumbableExt('/home/user/photo.png')).toBe(true)
    expect(isThumbableExt('.clawbench/uploads/img.jpg')).toBe(true)
  })

  it('is case-insensitive', () => {
    expect(isThumbableExt('photo.PNG')).toBe(true)
    expect(isThumbableExt('photo.Jpg')).toBe(true)
  })
})

describe('extended language coverage', () => {
  it('detects protobuf', () => {
    expect(getFileType('user.proto').lang).toBe('protobuf')
  })

  it('detects languages that were previously unmapped', () => {
    const cases: Array<[string, string]> = [
      ['main.dart', 'dart'],
      ['app.ex', 'elixir'],
      ['app.exs', 'elixir'],
      ['app.erl', 'erlang'],
      ['Main.hs', 'haskell'],
      ['core.clj', 'clojure'],
      ['lib.ml', 'ocaml'],
      ['lib.fs', 'fsharp'],
      ['build.groovy', 'groovy'],
      ['shader.glsl', 'glsl'],
      ['CMakeLists.txt', 'cmake'],
      ['build.gradle', 'gradle'],
      ['flake.nix', 'nix'],
      ['main.nim', 'nim'],
      ['main.cr', 'crystal'],
      ['main.jl', 'julia'],
      ['Main.elm', 'elm'],
      ['main.scm', 'scheme'],
      ['main.tcl', 'tcl'],
      ['main.f90', 'fortran'],
      ['app.coffee', 'coffeescript'],
      ['api.http', 'http'],
      ['login.feature', 'gherkin'],
      ['svc.thrift', 'thrift'],
      ['schema.capnp', 'capnproto'],
      ['main.d', 'd'],
      ['main.vala', 'vala'],
      ['Main.hx', 'haxe'],
      ['init.lisp', 'lisp'],
      ['app.styl', 'stylus'],
      ['page.twig', 'twig'],
      ['view.erb', 'erb'],
      ['main.adb', 'ada'],
      ['run.applescript', 'applescript'],
      ['hotkey.ahk', 'autohotkey'],
      ['prog.cob', 'cobol'],
      ['db.zone', 'dns'],
      ['part.gcode', 'gcode'],
      ['part.scad', 'openscad'],
      ['sketch.pde', 'processing'],
      ['ui.qml', 'qml'],
      ['main.re', 'reasonml'],
      ['main.sml', 'sml'],
      ['mission.sqf', 'sqf'],
      ['model.stan', 'stan'],
      ['analysis.do', 'stata'],
      ['doc.textile', 'textile'],
      ['Main.vb', 'vbnet'],
      ['api.webidl', 'webidl'],
      ['main.wren', 'wren'],
      ['q.xq', 'xquery'],
      ['x.zep', 'zephir'],
      ['fn.pgsql', 'pgsql'],
      ['q.rq', 'sparql'],
      ['ont.ttl', 'turtle'],
    ]
    const wrong = cases.filter(([file, lang]) => getFileType(file).lang !== lang)
    expect(wrong).toEqual([])
  })

  it('resolves ambiguous extensions by the mainstream reading', () => {
    // `.m` is Objective-C here, not MATLAB; `.v` is Verilog, not Coq;
    // `.cls` is LaTeX, not Apex/VB.
    expect(getFileType('view.m').lang).toBe('objectivec')
    expect(getFileType('view.mm').lang).toBe('objectivec')
    expect(getFileType('counter.v').lang).toBe('verilog')
    expect(getFileType('paper.cls').lang).toBe('latex')
    expect(getFileType('build.sbt').lang).toBe('plaintext')
  })

  it('keeps the languages that previously shared an entry distinct', () => {
    // `.scss` and `.sass` are different syntaxes and now different languages,
    // so the editor can pick the indented grammar for `.sass`.
    expect(getFileType('a.scss').lang).toBe('scss')
    expect(getFileType('a.sass').lang).toBe('sass')
    // `.svelte` is HTML-shaped; `.vue` keeps its own grammar.
    expect(getFileType('A.svelte').lang).toBe('xml')
    expect(getFileType('A.vue').lang).toBe('vue')
  })

  it('never assigns the same extension to two different languages', async () => {
    // getFileType returns the FIRST match, so a duplicate would silently
    // shadow the later entry — the file would open with the wrong grammar and
    // nothing would fail. Probing through getFileType cannot detect this (it
    // always returns the winner), so read the table itself.
    const { readFileSync } = await import('node:fs')
    const { resolve } = await import('node:path')
    const src = readFileSync(resolve(__dirname, '../fileType.ts'), 'utf-8')

    const owner = new Map<string, string>()
    const clashes: string[] = []
    // Each entry is `{ exts: [...], lang: '...'` on one line.
    const entryRe = /\{\s*exts:\s*\[([^\]]*)\],\s*lang:\s*'([^']+)'/g
    for (const m of src.matchAll(entryRe)) {
      const exts = [...m[1].matchAll(/'([^']+)'/g)].map((e) => e[1])
      const lang = m[2]
      for (const ext of exts) {
        const prev = owner.get(ext)
        if (prev !== undefined && prev !== lang) clashes.push(`${ext}: ${prev} vs ${lang}`)
        owner.set(ext, lang)
      }
    }
    expect(clashes).toEqual([])
    // Guard the guard: a regex that silently stopped matching would make the
    // assertion above vacuously true.
    expect(owner.size).toBeGreaterThan(300)
  })
})

describe('notebook files', () => {
  it('keeps .ipynb plaintext so cell-source diffs are not mis-highlighted', () => {
    // `.ipynb` is a JSON container, but every renderer that shows its contents
    // (NotebookEdit's cell diff) displays CELL SOURCE. Mapping the file to
    // `json` or `python` makes the diff renderer colour tokens inside the cell
    // code, which corrupted `z = 3` into `z = <span class="hljs-number">3</span>`
    // and broke the NotebookEdit renderer contract.
    const ft = getFileType('nb.ipynb')
    expect(ft.lang).toBe('plaintext')
    expect(ft.label).toBe('IPYNB')
  })
})
