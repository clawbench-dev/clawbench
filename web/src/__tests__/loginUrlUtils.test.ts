import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { readFileSync, existsSync } from 'node:fs'
import { resolve } from 'node:path'
import { createContext, runInContext } from 'node:vm'
import { parse as parseYaml } from 'yaml'

const ANDROID_UTILS = 'android/app/src/main/assets/url-utils.js'
const DESKTOP_UTILS = 'desktop/assets/url-utils.js'

/**
 * Locate a repo file from the vitest cwd. Mirrors the idiom in
 * coverageScriptPaths.test.ts: try the cwd, then its parent. No import.meta.url
 * — vitest runs with the repo root as cwd, so relative paths are repo-relative.
 */
function readRepoFile(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    try {
      return readFileSync(resolve(base, rel), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

/**
 * Resolve a repo-relative path to an absolute one, using the same cwd-then-parent
 * candidate search as readRepoFile so both helpers agree on the repo root.
 */
function resolveRepo(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const candidate = resolve(base, rel)
    if (existsSync(candidate)) return candidate
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

interface Parsed {
  protocol: 'http' | 'https' | null
  host: string
  port: string | null
}

/**
 * Evaluate a url-utils.js in a bare vm sandbox and return its global
 * parseServerInput. The bare sandbox is deliberate: load-time references to
 * document/window/ClawBenchNative throw, and any reference on an executed path
 * throws too. (A DOM reference inside an unreached branch is not detected —
 * the function is pure by design, so keep it that way.)
 */
function parserFor(rel: string): (text: string) => Parsed | null {
  const sandbox: Record<string, unknown> = {}
  createContext(sandbox)
  runInContext(readRepoFile(rel), sandbox, { filename: rel })
  const fn = sandbox.parseServerInput
  if (typeof fn !== 'function') throw new Error(`${rel} did not define parseServerInput`)
  return fn as (text: string) => Parsed | null
}

/**
 * One table, asserted against both copies. This is what keeps the two
 * url-utils.js files from drifting: a change to either alone fails here.
 */
const CASES: Array<[string, Parsed | null]> = [
  // Behavior table from the design doc.
  ['https://192.168.1.100:8443', { protocol: 'https', host: '192.168.1.100', port: '8443' }],
  ['http://example.com', { protocol: 'http', host: 'example.com', port: null }],
  ['https://example.com/chat?x=1', { protocol: 'https', host: 'example.com', port: null }],
  ['https://host:8443/api', { protocol: 'https', host: 'host', port: '8443' }],
  ['192.168.1.100:8080', { protocol: null, host: '192.168.1.100', port: '8080' }],
  ['192.168.1.100', { protocol: null, host: '192.168.1.100', port: null }],
  ['not a url', null],
  // Extra edges.
  ['', null],
  ['   ', null],
  // Whitespace around a valid URL — clipboard pastes often carry padding.
  ['  https://host:8443  ', { protocol: 'https', host: 'host', port: '8443' }],
  // An explicit port equal to the scheme default is still explicit.
  ['http://host:80', { protocol: 'http', host: 'host', port: '80' }],
  ['HTTPS://Host:8443', { protocol: 'https', host: 'Host', port: '8443' }],
  ['https://host:8443/', { protocol: 'https', host: 'host', port: '8443' }],
  // Garbage / unsupported forms.
  ['http://', null],
  ['ftp://host', null],
  ['https://user:pass@host:8443', null],
  ['[::1]:8080', null],
]

describe.each([
  ['android', ANDROID_UTILS],
  ['desktop', DESKTOP_UTILS],
])('parseServerInput (%s copy)', (_label, rel) => {
  for (const [input, expected] of CASES) {
    it(`${JSON.stringify(input)} -> ${JSON.stringify(expected)}`, () => {
      expect(parserFor(rel)(input)).toEqual(expected)
    })
  }
})

const ANDROID_LOGIN = 'android/app/src/main/assets/login.html'
const DESKTOP_LOGIN = 'desktop/assets/login.html'

/**
 * Static wiring guard. Robolectric cannot execute JS (ShadowWebView
 * .evaluateJavascript is a no-op), so the paste handler cannot be exercised
 * end-to-end in a unit test. These assertions catch the wiring being forgotten
 * — a missing <script> tag or an unwired listener — which would otherwise ship
 * silently (no CSP, no error, the feature just does nothing).
 */
describe('login.html wiring', () => {
  for (const [label, rel] of [
    ['android', ANDROID_LOGIN],
    ['desktop', DESKTOP_LOGIN],
  ] as const) {
    it(`${label} loads url-utils.js and wires a paste listener on #addHost`, () => {
      const html = readRepoFile(rel)
      expect(html).toContain('<script src="url-utils.js"></script>')
      expect(html).toMatch(/getElementById\('addHost'\)\.addEventListener\('paste'/)
      expect(html).toContain('parseServerInput')
    })

    /**
     * The functional test above builds its own DOM_FIXTURE, so renaming an id in
     * the real page (while leaving the JS untouched) would keep every test green
     * and still break the page at runtime: getElementById returns null and the
     * listener throws. Parse the real markup and assert the ids/values the
     * listener actually dereferences.
     */
    it(`${label} declares the element ids and radio values the listener needs`, () => {
      const doc = new DOMParser().parseFromString(readRepoFile(rel), 'text/html')

      const host = doc.getElementById('addHost')
      expect(host, '#addHost must exist (the paste listener is bound to it)').not.toBeNull()

      const port = doc.getElementById('addPort')
      expect(port, '#addPort must exist (the listener writes parsed.port here)').not.toBeNull()
      // The design's "default 20000" behaviour depends on this exact value: a
      // scheme-less paste leaves the field alone, so the default is what ships.
      expect(port!.getAttribute('value')).toBe('20000')

      // The listener flips these by value selector; a renamed value would make
      // the protocol silently fail to update.
      expect(
        doc.querySelector('input[name="addProtocol"][value="https"]'),
        'an addProtocol radio with value="https" must exist',
      ).not.toBeNull()
      expect(
        doc.querySelector('input[name="addProtocol"][value="http"]'),
        'an addProtocol radio with value="http" must exist',
      ).not.toBeNull()

      expect(
        doc.getElementById('addErrorMsg'),
        '#addErrorMsg must exist (the listener calls hideError(\'addErrorMsg\'))',
      ).not.toBeNull()
    })
  }
})

/**
 * Byte-equality guard for the duplicated url-utils.js. The plan accepts two
 * copies (no build-time single-source merge), so the only thing keeping them
 * honest is that they are identical. The behavioural table above catches drift
 * that changes output on one of its inputs; this catches the rest (a widened
 * regex on an unexercised shape, a renamed internal, a stray whitespace edit).
 */
describe('url-utils.js copies', () => {
  it('are byte-identical', () => {
    const android = readFileSync(resolveRepo(ANDROID_UTILS))
    const desktop = readFileSync(resolveRepo(DESKTOP_UTILS))
    expect(android.equals(desktop)).toBe(true)
  })
})

const ELECTRON_BUILDER_YML = 'desktop/electron-builder.yml'

/**
 * Packaging guard. desktop/assets/url-utils.js only reaches the packaged app if
 * electron-builder copies it via extraResources. If that entry is deleted (or
 * its `to:` is moved into a subdirectory), the page's <script src="url-utils.js">
 * 404s with no CSP violation and no console error surfaced anywhere — the
 * feature just silently does nothing, and every behavioural test above still
 * passes because they read the repo file directly. The yml is not shipped inside
 * app.asar, so the app cannot detect the drift at runtime; only a static test
 * can. Mirrors desktop/src/main/identity.test.ts, which guards appId the same
 * way. Parsed with `yaml` (a vite dependency) rather than regexed, so a
 * re-indent or an inline-map rewrite cannot fool it.
 */
describe('desktop packaging', () => {
  it('electron-builder ships url-utils.js as a sibling of login.html', () => {
    const yml = parseYaml(readRepoFile(ELECTRON_BUILDER_YML)) as {
      extraResources?: Array<{ from?: string; to?: string }>
    }
    const resources = yml.extraResources
    expect(Array.isArray(resources), 'electron-builder.yml must declare extraResources').toBe(true)

    const entry = resources!.find((r) => r && r.from === 'assets/url-utils.js')
    expect(
      entry,
      'extraResources must contain an entry with from: assets/url-utils.js',
    ).toBeDefined()
    // `to` must be the flat filename: login.html loads it as src="url-utils.js"
    // from the resources root, so a nested target like 'assets/url-utils.js'
    // would 404 at runtime while this entry still looks present.
    expect(entry!.to).toBe('url-utils.js')
  })
})

/**
 * The comment that opens the paste listener in both login.html files. The
 * extraction below slices from here to the listener's closing `});`, so if a
 * refactor renames or moves the block the test fails loudly instead of quietly
 * asserting nothing.
 */
const PASTE_COMMENT = '// Event: paste a full URL into the host field'

/**
 * Pull the paste listener's source out of a login.html. Robust anchors:
 *  - the comment marker must exist;
 *  - the first `});` after it must close the listener (no `// Event:` comment
 *    may intervene — the next handler starts with one);
 *  - the slice must contain the addEventListener('paste' call.
 * Throws with a specific reason when an anchor moved, so a future refactor is
 * caught rather than silently passing.
 */
function extractPasteListener(rel: string): string {
  const html = readRepoFile(rel)
  const markerIdx = html.indexOf(PASTE_COMMENT)
  if (markerIdx < 0) {
    throw new Error(`paste listener comment ${JSON.stringify(PASTE_COMMENT)} not found in ${rel}`)
  }
  const nextEventIdx = html.indexOf('// Event:', markerIdx + PASTE_COMMENT.length)
  const closeIdx = html.indexOf('});', markerIdx)
  if (closeIdx < 0) {
    throw new Error(`no closing '});' after the paste listener comment in ${rel}`)
  }
  if (nextEventIdx !== -1 && nextEventIdx < closeIdx) {
    throw new Error(
      `the paste listener block in ${rel} does not end before the next '// Event:' comment ` +
        `(close=${closeIdx}, nextEvent=${nextEventIdx}) — extraction anchors moved`,
    )
  }
  const src = html.slice(markerIdx, closeIdx + '});'.length)
  if (!src.includes("addEventListener('paste'")) {
    throw new Error(`extracted block from ${rel} is not the paste listener (no addEventListener('paste')`)
  }
  if (!src.endsWith('});')) {
    throw new Error(`extracted block from ${rel} does not end with '});'`)
  }
  return src
}

/**
 * The two login.html files legitimately differ (i18n slogans, the async bridge
 * handling, comments), so a whole-file equality check would be wrong. The paste
 * listener, however, is deliberately platform-agnostic — it touches no native
 * bridge, only the shared parseServerInput and plain DOM — so the two copies
 * must stay identical. Divergence means someone edited one page's listener and
 * forgot the other, which no behavioural test would catch (each page is tested
 * only against its own copy).
 */
describe('login.html paste listener copies', () => {
  it('are byte-identical', () => {
    expect(extractPasteListener(ANDROID_LOGIN)).toBe(extractPasteListener(DESKTOP_LOGIN))
  })
})

/**
 * Minimal DOM the paste listener touches. Values mirror the real login page:
 * #addPort defaults to 20000 and the https radio is checked by default.
 */
const DOM_FIXTURE = `
  <input type="text" id="addHost">
  <input type="number" id="addPort" value="20000">
  <input type="radio" name="addProtocol" value="https" checked>
  <input type="radio" name="addProtocol" value="http">
  <div id="addErrorMsg"></div>
`

/**
 * End-to-end wiring guard: actually executes the extracted listener against a
 * real jsdom document. Robolectric cannot run JS, so this is the only place the
 * paste handler's behaviour (not just its text) is exercised. Registered via
 * indirect eval so the listener's free identifiers (document, parseServerInput,
 * hideError) resolve in global scope exactly as they do in the page.
 */
describe.each([
  ['android', ANDROID_LOGIN, ANDROID_UTILS],
  ['desktop', DESKTOP_LOGIN, DESKTOP_UTILS],
])('paste listener behaviour (%s page)', (_label, loginRel, utilsRel) => {
  let host: HTMLInputElement

  beforeEach(() => {
    document.body.innerHTML = DOM_FIXTURE
    // The page defines hideError(id) as a global; mirror its real semantics
    // (see login.html): remove the 'visible' class from the error element.
    ;(globalThis as unknown as Record<string, unknown>).hideError = (id: string) => {
      document.getElementById(id)?.classList.remove('visible')
    }
    // url-utils.js defines parseServerInput as a global function.
    ;(0, eval)(readRepoFile(utilsRel))
    // Register the listener on the real #addHost element.
    ;(0, eval)(extractPasteListener(loginRel))
    host = document.getElementById('addHost') as HTMLInputElement
    if (!host) throw new Error(`${loginRel} fixture did not create #addHost`)
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  function paste(text: string): Event {
    const ev = new Event('paste', { bubbles: true, cancelable: true })
    // jsdom has no ClipboardEvent.clipboardData; the listener only reads
    // e.clipboardData.getData('text'), so a plain property suffices.
    Object.defineProperty(ev, 'clipboardData', { value: { getData: () => text } })
    host.dispatchEvent(ev)
    return ev
  }

  function portValue(): string {
    return (document.getElementById('addPort') as HTMLInputElement).value
  }

  function checkedProtocol(): string | null {
    const el = document.querySelector<HTMLInputElement>('input[name="addProtocol"]:checked')
    return el ? el.value : null
  }

  function setProtocol(value: 'http' | 'https'): void {
    const el = document.querySelector<HTMLInputElement>(`input[name="addProtocol"][value="${value}"]`)
    if (el) el.checked = true
  }

  it('fills host, port and protocol from a full https URL and intercepts the paste', () => {
    // Start on http so the flip to https is meaningful.
    setProtocol('http')
    const ev = paste('https://192.168.1.100:8443')
    expect(ev.defaultPrevented).toBe(true)
    expect(host.value).toBe('192.168.1.100')
    expect(portValue()).toBe('8443')
    expect(checkedProtocol()).toBe('https')
  })

  it('fills host from a scheme-only http URL and leaves the default port untouched', () => {
    const ev = paste('http://example.com')
    expect(ev.defaultPrevented).toBe(true)
    expect(host.value).toBe('example.com')
    expect(portValue()).toBe('20000')
    expect(checkedProtocol()).toBe('http')
  })

  it('fills host and port for a scheme-less URL but leaves the protocol radio alone', () => {
    const ev = paste('192.168.1.100:8080')
    expect(ev.defaultPrevented).toBe(true)
    expect(host.value).toBe('192.168.1.100')
    expect(portValue()).toBe('8080')
    // No scheme in the input -> radio must stay at its default (https).
    expect(checkedProtocol()).toBe('https')
  })

  it('does not intercept an unparseable paste', () => {
    host.value = 'ORIGINAL'
    const ev = paste('not a url')
    expect(ev.defaultPrevented).toBe(false)
    expect(host.value).toBe('ORIGINAL')
    expect(portValue()).toBe('20000')
  })

  it('hides the error message on a successful parse (proves hideError ran)', () => {
    const err = document.getElementById('addErrorMsg') as HTMLElement
    err.classList.add('visible')
    paste('https://host:8443')
    expect(err.classList.contains('visible')).toBe(false)
  })
})
