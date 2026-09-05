import { afterEach, describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  buildFontStack,
  buildDualFontStack,
  readMonoFont,
  readUiFont,
  readMonoFallbackFont,
  readUiFallbackFont,
  resolveChoice,
  applyFontConfig,
  applyFontToDocument,
  ensureFontLoaded,
  isBundledChoice,
  isFontAvailable,
  filterAvailableFonts,
  preloadBundledFonts,
  setCustomFontChoices,
  getCustomFontChoices,
  isCustomFontId,
  quoteFamilyName,
  _resetAvailabilityCache,
  DEFAULT_UI_STACK,
  DEFAULT_MONO_STACK,
  DEFAULT_TERMINAL_MONO_STACK,
  DEFAULT_FONT_CHOICE,
  MONO_FONT_KEY,
  UI_FONT_KEY,
  MONO_FALLBACK_KEY,
  UI_FALLBACK_KEY,
  MONO_FONT_CHOICES,
  UI_FONT_CHOICES,
  MONO_FALLBACK_CHOICES,
} from '@/utils/fontConfig'

function mockStorage(initial: Record<string, string> = {}): Pick<Storage, 'getItem'> {
  return { getItem: (k: string) => (k in initial ? initial[k] : null) }
}

/** Minimal document stub recording --font-* custom property writes. */
function fakeDocument() {
  const styles: Record<string, string> = {}
  return {
    documentElement: {
      style: { setProperty: (prop: string, value: string) => { styles[prop] = value } },
    },
    get styles() { return styles },
  }
}

describe('buildFontStack', () => {
  it('returns default stack for default choice', () => {
    expect(buildFontStack(DEFAULT_FONT_CHOICE, DEFAULT_MONO_STACK)).toBe(DEFAULT_MONO_STACK)
  })

  it('returns default stack for null/undefined/empty', () => {
    expect(buildFontStack(null, DEFAULT_MONO_STACK)).toBe(DEFAULT_MONO_STACK)
    expect(buildFontStack(undefined, DEFAULT_MONO_STACK)).toBe(DEFAULT_MONO_STACK)
    expect(buildFontStack('', DEFAULT_MONO_STACK)).toBe(DEFAULT_MONO_STACK)
  })

  it('prepends a simple font name without quotes', () => {
    expect(buildFontStack('Hack', DEFAULT_MONO_STACK)).toBe(`Hack, ${DEFAULT_MONO_STACK}`)
  })

  it('quotes font names containing spaces', () => {
    expect(buildFontStack('JetBrains Mono', DEFAULT_MONO_STACK)).toBe(`'JetBrains Mono', ${DEFAULT_MONO_STACK}`)
    expect(buildFontStack('Sarasa Mono SC', DEFAULT_MONO_STACK)).toBe(`'Sarasa Mono SC', ${DEFAULT_MONO_STACK}`)
  })

  it('escapes embedded quotes and backslashes inside quoted names', () => {
    expect(quoteFamilyName("Ace 'Round")).toBe(`'Ace \\'Round'`)
    expect(quoteFamilyName('Weird\\Name')).toBe(`'Weird\\\\Name'`)
    expect(quoteFamilyName('Simple')).toBe('Simple')
  })

  it('keeps ui default stack for default ui choice', () => {
    expect(buildFontStack(DEFAULT_FONT_CHOICE, DEFAULT_UI_STACK)).toBe(DEFAULT_UI_STACK)
  })
})

describe('buildDualFontStack', () => {
  it('returns default stack when both primary and fallback are default/empty', () => {
    expect(buildDualFontStack('default', 'default', DEFAULT_MONO_STACK)).toBe(DEFAULT_MONO_STACK)
    expect(buildDualFontStack(null, undefined, DEFAULT_MONO_STACK)).toBe(DEFAULT_MONO_STACK)
    expect(buildDualFontStack('', 'default', DEFAULT_MONO_STACK)).toBe(DEFAULT_MONO_STACK)
  })

  it('behaves like single-stack when only primary is set', () => {
    expect(buildDualFontStack('Hack', 'default', DEFAULT_MONO_STACK)).toBe(`Hack, ${DEFAULT_MONO_STACK}`)
  })

  it('prepends fallback alone when only fallback is set', () => {
    expect(buildDualFontStack('default', 'SimSun', DEFAULT_MONO_STACK)).toBe(`SimSun, ${DEFAULT_MONO_STACK}`)
  })

  it('produces primary, fallback, default when both are set', () => {
    expect(buildDualFontStack('JetBrains Mono', 'Sarasa Mono SC', DEFAULT_MONO_STACK))
      .toBe(`'JetBrains Mono', 'Sarasa Mono SC', ${DEFAULT_MONO_STACK}`)
  })

  it('does not duplicate the fallback when it equals the primary', () => {
    expect(buildDualFontStack('Inter', 'Inter', DEFAULT_UI_STACK)).toBe(`Inter, ${DEFAULT_UI_STACK}`)
  })
})

describe('readMonoFallbackFont / readUiFallbackFont', () => {
  it('returns default when nothing stored', () => {
    expect(readMonoFallbackFont(mockStorage())).toBe(DEFAULT_FONT_CHOICE)
    expect(readUiFallbackFont(mockStorage())).toBe(DEFAULT_FONT_CHOICE)
  })

  it('reads stored values from the fallback keys', () => {
    const store = mockStorage({ [MONO_FALLBACK_KEY]: JSON.stringify('Sarasa Mono SC'), [UI_FALLBACK_KEY]: JSON.stringify('SimSun') })
    expect(readMonoFallbackFont(store)).toBe('Sarasa Mono SC')
    expect(readUiFallbackFont(store)).toBe('SimSun')
  })
})

describe('readMonoFont / readUiFont', () => {
  it('returns default when nothing stored', () => {
    expect(readMonoFont(mockStorage())).toBe(DEFAULT_FONT_CHOICE)
    expect(readUiFont(mockStorage())).toBe(DEFAULT_FONT_CHOICE)
  })

  it('reads a stored json-encoded value', () => {
    const store = mockStorage({ [MONO_FONT_KEY]: JSON.stringify('Fira Code') })
    expect(readMonoFont(store)).toBe('Fira Code')
  })

  it('returns default when stored value is not a string', () => {
    const store = mockStorage({ [MONO_FONT_KEY]: JSON.stringify(42) })
    expect(readMonoFont(store)).toBe(DEFAULT_FONT_CHOICE)
  })

  it('returns default when getItem throws', () => {
    const broken = { getItem: () => { throw new Error('denied') } }
    expect(readMonoFont(broken)).toBe(DEFAULT_FONT_CHOICE)
  })

  it('reads ui font from its own key', () => {
    const store = mockStorage({ [UI_FONT_KEY]: JSON.stringify('Inter') })
    expect(readUiFont(store)).toBe('Inter')
  })
})

describe('resolveChoice', () => {
  it('returns null for unknown id', () => {
    expect(resolveChoice('Comic Sans', MONO_FONT_CHOICES)).toBeNull()
    expect(resolveChoice(null, MONO_FONT_CHOICES)).toBeNull()
  })

  it('finds known mono font', () => {
    const c = resolveChoice('Fira Code', MONO_FONT_CHOICES)
    expect(c?.id).toBe('Fira Code')
  })

  it('finds the default sentinel', () => {
    const c = resolveChoice(DEFAULT_FONT_CHOICE, UI_FONT_CHOICES)
    expect(c?.id).toBe(DEFAULT_FONT_CHOICE)
  })
})

describe('applyFontToDocument / applyFontConfig', () => {
  it('sets --font-mono to default stack when choice is default', () => {
    const doc = fakeDocument() as unknown as Document
    applyFontToDocument(doc, '--font-mono', DEFAULT_FONT_CHOICE, 'default', DEFAULT_MONO_STACK)
    expect(doc.styles['--font-mono']).toBe(DEFAULT_MONO_STACK)
  })

  it('sets --font-mono with chosen font at head', () => {
    const doc = fakeDocument() as unknown as Document
    applyFontToDocument(doc, '--font-mono', 'Hack', 'default', DEFAULT_MONO_STACK)
    expect(doc.styles['--font-mono']).toBe(`Hack, ${DEFAULT_MONO_STACK}`)
  })

  it('sets --font-mono with primary + fallback', () => {
    const doc = fakeDocument() as unknown as Document
    applyFontToDocument(doc, '--font-mono', 'JetBrains Mono', 'Sarasa Mono SC', DEFAULT_MONO_STACK)
    expect(doc.styles['--font-mono']).toBe(`'JetBrains Mono', 'Sarasa Mono SC', ${DEFAULT_MONO_STACK}`)
  })

  it('applyFontConfig applies both variables from explicit choices', () => {
    const doc = fakeDocument() as unknown as Document
    applyFontConfig(doc, 'Cascadia Code', 'default', 'Inter', 'default')
    expect(doc.styles['--font-mono']).toBe(`'Cascadia Code', ${DEFAULT_MONO_STACK}`)
    expect(doc.styles['--font-ui']).toBe(`Inter, ${DEFAULT_UI_STACK}`)
  })

  it('applyFontConfig includes fallback stacks', () => {
    const doc = fakeDocument() as unknown as Document
    applyFontConfig(doc, 'JetBrains Mono', 'Menlo', 'Inter', 'SimSun')
    expect(doc.styles['--font-mono']).toBe(`'JetBrains Mono', Menlo, ${DEFAULT_MONO_STACK}`)
    expect(doc.styles['--font-ui']).toBe(`Inter, SimSun, ${DEFAULT_UI_STACK}`)
  })

  it('applyFontConfig falls back to default stack for unknown ids', () => {
    const doc = fakeDocument() as unknown as Document
    applyFontConfig(doc, 'NotARealFont', 'default', 'AlsoFake', 'default')
    expect(doc.styles['--font-mono']).toBe(DEFAULT_MONO_STACK)
    expect(doc.styles['--font-ui']).toBe(DEFAULT_UI_STACK)
  })

  it('applyFontConfig accepts default sentinel for either dimension', () => {
    const doc = fakeDocument() as unknown as Document
    applyFontConfig(doc, DEFAULT_FONT_CHOICE, 'default', 'Inter', 'default')
    expect(doc.styles['--font-mono']).toBe(DEFAULT_MONO_STACK)
    expect(doc.styles['--font-ui']).toBe(`Inter, ${DEFAULT_UI_STACK}`)
  })
})

describe('ensureFontLoaded / isBundledChoice', () => {
  it('isBundledChoice true for bundled ids, false otherwise', () => {
    expect(isBundledChoice('JetBrains Mono', MONO_FONT_CHOICES)).toBe(true)
    expect(isBundledChoice('Inter', UI_FONT_CHOICES)).toBe(true)
    expect(isBundledChoice('Consolas', MONO_FONT_CHOICES)).toBe(false)
    expect(isBundledChoice('Hack', MONO_FONT_CHOICES)).toBe(false)
    expect(isBundledChoice('default', MONO_FONT_CHOICES)).toBe(false)
    expect(isBundledChoice(null, MONO_FONT_CHOICES)).toBe(false)
  })

  it('ensureFontLoaded resolves without throwing when document.fonts is unavailable', async () => {
    // jsdom does not implement the Font Loading API — document.fonts is undefined.
    await expect(ensureFontLoaded('JetBrains Mono')).resolves.toBeUndefined()
  })
})

describe('candidate tables', () => {
  it('contains expected mono candidates including default sentinel', () => {
    expect(MONO_FONT_CHOICES[0].id).toBe(DEFAULT_FONT_CHOICE)
    const ids = MONO_FONT_CHOICES.map(c => c.id)
    for (const f of ['JetBrains Mono', 'Fira Code', 'Cascadia Code', 'Source Code Pro', 'IBM Plex Mono']) {
      expect(ids).toContain(f)
    }
  })

  it('contains expected ui candidates', () => {
    const ids = UI_FONT_CHOICES.map(c => c.id)
    for (const f of ['Inter', 'Source Sans 3', 'IBM Plex Sans', 'Noto Sans SC']) {
      expect(ids).toContain(f)
    }
  })

  it('candidate ids are unique across each table', () => {
    expect(new Set(MONO_FONT_CHOICES.map(c => c.id)).size).toBe(MONO_FONT_CHOICES.length)
    expect(new Set(UI_FONT_CHOICES.map(c => c.id)).size).toBe(UI_FONT_CHOICES.length)
  })

  it('does not list open-source fonts that require manual install', () => {
    const monoIds = MONO_FONT_CHOICES.map(c => c.id)
    for (const f of ['Hack', 'Maple Mono', 'Iosevka', 'Sarasa Mono SC']) {
      expect(monoIds).not.toContain(f)
    }
    const uiIds = UI_FONT_CHOICES.map(c => c.id)
    expect(uiIds).not.toContain('LXGW WenKai')
  })

  it('contains system built-in font candidates', () => {
    const monoIds = MONO_FONT_CHOICES.map(c => c.id)
    for (const f of ['Menlo', 'Courier New', 'DejaVu Sans Mono', 'Consolas', 'Segoe UI Mono', 'Roboto Mono']) {
      expect(monoIds).toContain(f)
    }
    const uiIds = UI_FONT_CHOICES.map(c => c.id)
    for (const f of ['PingFang SC', 'Microsoft YaHei', 'Noto Sans SC', 'Segoe UI', 'Roboto', 'Arial']) {
      expect(uiIds).toContain(f)
    }
  })

  it('mono and ui candidate lists are grouped default → bundled → system', () => {
    const kinds = MONO_FONT_CHOICES.map(c => c.kind)
    const seen: string[] = []
    for (const k of kinds) {
      if (seen[seen.length - 1] !== k) seen.push(k)
    }
    expect(seen).toEqual(['default', 'bundled', 'system'])
    expect(UI_FONT_CHOICES.map(c => c.kind).filter((v, i, a) => a.indexOf(v) === i)).toEqual(['default', 'bundled', 'system'])
  })

  it('mono fallback candidates include CJK system fonts but the primary mono list does not', () => {
    const fbIds = MONO_FALLBACK_CHOICES.map(c => c.id)
    for (const f of ['SimSun', 'SimHei', 'PingFang SC', 'Microsoft YaHei', 'Noto Sans SC', 'KaiTi']) {
      expect(fbIds).toContain(f)
    }
    const monoIds = MONO_FONT_CHOICES.map(c => c.id)
    expect(monoIds).not.toContain('SimSun')
  })

  it('every bundled candidate id has a matching @font-face in self-hosted-fonts.css', () => {
    const css = readFileSync(resolve(__dirname, '../../assets/self-hosted-fonts.css'), 'utf8')
    const bundled = [...MONO_FONT_CHOICES, ...UI_FONT_CHOICES].filter(c => c.kind === 'bundled').map(c => c.id)
    for (const id of bundled) {
      // family names appear inside single quotes in the css.
      expect(css, `missing @font-face for bundled font ${id}`).toContain(`font-family: '${id}'`)
    }
  })
})

describe('default-stack source consistency (drift guard)', () => {
  // The default stacks are defined in three places that cannot share code:
  //  1. fontConfig.ts constants (read by JS renderers / applyFontConfig)
  //  2. variables.css :root custom properties (read by the whole CSS layer)
  //  3. index.html inline pre-CSS injection script (runs before any module)
  // This spec reads the raw sources and asserts they stay identical, so a
  // future edit cannot silently drift one channel.
  const repoRoot = resolve(__dirname, '../../..')
  const norm = (s: string) => s.replace(/,\s+/g, ',').replace(/\s+/g, ' ').replace(/^var\([^,]*,/, '')
  const declValue = (source: string, name: string): string => {
    const m = source.match(new RegExp(`${name}:\\s*([^;]+);`))
    return m ? norm(m[1].trim()) : ''
  }

  it('variables.css :root matches DEFAULT_MONO_STACK / DEFAULT_UI_STACK', () => {
    const css = readFileSync(resolve(repoRoot, 'css/variables.css'), 'utf8')
    expect(declValue(css, '--font-mono')).toBe(norm(DEFAULT_MONO_STACK))
    expect(declValue(css, '--font-ui')).toBe(norm(DEFAULT_UI_STACK))
  })

  it('index.html inline injection mirrors both default stacks', () => {
    const html = readFileSync(resolve(repoRoot, 'index.html'), 'utf8')
    // Both fontConfig.ts and index.html write families with single quotes;
    // index.html additionally wraps the JS string in double quotes.
    // Normalize whitespace then drop the surrounding double quotes so both
    // sides reduce to the same bare stack text.
    const stripWs = (s: string) => s.replace(/\s+/g, ' ')
    const htmlNorm = stripWs(html)
    const want = (varName: string, stack: string) =>
      `${varName} = ${JSON.stringify(stack)}`.replace(/\s+/g, ' ')
    expect(htmlNorm).toContain(want('var MONO_DEFAULT', DEFAULT_MONO_STACK))
    expect(htmlNorm).toContain(want('var UI_DEFAULT', DEFAULT_UI_STACK))
  })

  it('DEFAULT_TERMINAL_MONO_STACK is exported and non-empty', () => {
    expect(DEFAULT_TERMINAL_MONO_STACK.length).toBeGreaterThan(10)
  })
})

describe('custom font registry (runtime-scanned)', () => {
  afterEach(() => {
    setCustomFontChoices([])
  })

  it('registry starts empty and is replaceable', () => {
    expect(getCustomFontChoices()).toEqual([])
    expect(isCustomFontId('Sarasa Mono SC')).toBe(false)

    setCustomFontChoices([{ id: 'Sarasa Mono SC', kind: 'custom' }])
    expect(getCustomFontChoices()).toEqual([{ id: 'Sarasa Mono SC', kind: 'custom' }])
    expect(isCustomFontId('Sarasa Mono SC')).toBe(true)
    expect(isCustomFontId('JetBrains Mono')).toBe(false)
    expect(isCustomFontId(null)).toBe(false)
  })

  it('isFontAvailable always true for custom kind', () => {
    expect(isFontAvailable('Anything', 'custom')).toBe(true)
  })

  it('filterAvailableFonts keeps custom candidates without a canvas', async () => {
    setCustomFontChoices([{ id: 'My Custom', kind: 'custom' }])
    const candidates = [...MONO_FONT_CHOICES, { id: 'My Custom', kind: 'custom' as const }]
    const out = await filterAvailableFonts(candidates, 'default')
    const ids = out.map(c => c.id)
    expect(ids).toContain('My Custom')
  })

  it('applyFontConfig accepts a registered custom id', () => {
    const doc = fakeDocument() as unknown as Document
    setCustomFontChoices([{ id: 'Sarasa Mono SC', kind: 'custom' }])
    applyFontConfig(doc, 'Sarasa Mono SC', 'default', 'default', 'default')
    expect(doc.styles['--font-mono']).toBe(`'Sarasa Mono SC', ${DEFAULT_MONO_STACK}`)
    expect(doc.styles['--font-ui']).toBe(DEFAULT_UI_STACK)
  })

  it('applyFontConfig falls back to default stack when the registry is empty', () => {
    // Mirrors cold start: a stored custom selection cannot be validated until
    // the first scan fills the registry — it safely degrades to the default.
    const doc = fakeDocument() as unknown as Document
    setCustomFontChoices([])
    applyFontConfig(doc, 'Some Custom', 'default', 'default', 'default')
    expect(doc.styles['--font-mono']).toBe(DEFAULT_MONO_STACK)
  })
})

describe('availability detection', () => {
  afterEach(() => {
    _resetAvailabilityCache()
  })

  // Stub canvas measureText to emulate the real browser: the candidate width
  // differs from the sans-serif baseline only when the family is INSTALLED
  // AND actually covers the probe text's script (Latin probe text vs CJK probe
  // text). A CJK-only face therefore only "registers" on the CJK probe.
  function stubCanvas(meta: { [font: string]: { installed: boolean; latin?: boolean; cjk?: boolean } }) {
    const originalCreate = document.createElement.bind(document)
    vi.spyOn(document, 'createElement').mockImplementation(((tag: string) => {
      const el = originalCreate(tag)
      if (tag === 'canvas') {
        const state: { font: string } = { font: '' }
        Object.defineProperty(el, 'getContext', {
          configurable: true,
          value: () => ({
            set font(v: string) { state.font = v },
            get font() { return state.font },
            measureText: (text: string) => {
              const m = state.font.match(/"([^"]+)"/)
              const family = m?.[1]
              const info = family ? meta[family] : undefined
              const covers = info?.installed && (/[\u4e00-\u9fff]/.test(text) ? !!info.cjk : info.latin !== false)
              // width proportional to length: 8px/char when the face actually
              // provides the glyphs, otherwise the 6px generic fallback.
              return { width: covers ? text.length * 8 : text.length * 6 }
            },
          }),
        })
      }
      return el
    }) as typeof document.createElement)
  }

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('isFontAvailable always true for bundled fonts', () => {
    expect(isFontAvailable('JetBrains Mono', 'bundled')).toBe(true)
    expect(isFontAvailable('Inter', 'bundled')).toBe(true)
  })

  it('isFontAvailable is optimistic (true) without a canvas 2D context', () => {
    // jsdom baseline: canvas.getContext('2d') returns null → cannot probe.
    expect(isFontAvailable('SimSun', 'system')).toBe(true)
  })

  it('isFontAvailable probes system fonts through canvas measureText', () => {
    // SimSun covers both scripts; Menlo covers Latin only; PingFang SC missing.
    stubCanvas({
      SimSun: { installed: true, latin: true, cjk: true },
      Menlo: { installed: true, latin: true, cjk: false },
      'PingFang SC': { installed: false },
    })
    _resetAvailabilityCache()
    expect(isFontAvailable('SimSun', 'system')).toBe(true)
    _resetAvailabilityCache()
    expect(isFontAvailable('Menlo', 'system')).toBe(true)
    _resetAvailabilityCache()
    expect(isFontAvailable('PingFang SC', 'system')).toBe(false)
  })

  it('detects a CJK-only face via the CJK probe even though it lacks Latin glyphs', () => {
    // Emulates Android: 'Noto Sans SC' exists and covers CJK but NOT Latin
    // (Latin falls back to Roboto) → the Latin-only probe would miss it.
    stubCanvas({
      'Noto Sans SC': { installed: true, latin: false, cjk: true },
      Menlo: { installed: true, latin: true, cjk: false },
    })
    _resetAvailabilityCache()
    expect(isFontAvailable('Noto Sans SC', 'system')).toBe(true)
  })

  it('filterAvailableFonts keeps default + bundled, drops missing system, keeps selection', async () => {
    stubCanvas({ Menlo: { installed: true, latin: true, cjk: false } }) // only Menlo among system mono
    _resetAvailabilityCache()
    const out = await filterAvailableFonts(MONO_FONT_CHOICES, 'Consolas')
    const ids = out.map(c => c.id)
    expect(ids).toContain(DEFAULT_FONT_CHOICE)
    expect(ids).toContain('JetBrains Mono') // bundled
    expect(ids).toContain('Menlo')          // detected system
    expect(ids).toContain('Consolas')       // selected kept even if not detected
    expect(ids).not.toContain('DejaVu Sans Mono') // system not detected
    expect(ids).not.toContain('Courier New')      // system not detected
  })

  it('filterAvailableFonts without a canvas keeps every candidate', async () => {
    const out = await filterAvailableFonts(UI_FONT_CHOICES)
    expect(out.map(c => c.id)).toEqual(UI_FONT_CHOICES.map(c => c.id))
  })

  it('preloadBundledFonts resolves without throwing', async () => {
    Object.defineProperty(document, 'fonts', { configurable: true, value: undefined })
    await expect(preloadBundledFonts(MONO_FONT_CHOICES)).resolves.toBeUndefined()
  })
})
