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
  // Whitespace around a valid URL — pasted/typed input often carries padding.
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

/**
 * buildServerUrl is the contract behind the single address field: it is the
 * ONLY thing that turns typed text into a saved URL, and it must reject
 * anything that does not carry both a scheme and a port explicitly. The old UI
 * filled those two in with defaults (https / 20000); the form no longer has
 * them, so "reject and tell the user" is the replacement for "guess".
 */
type UrlBuilder = (text: string) => string | null

function builderFor(rel: string): UrlBuilder {
  const sandbox: Record<string, unknown> = {}
  createContext(sandbox)
  runInContext(readRepoFile(rel), sandbox, { filename: rel })
  const fn = sandbox.buildServerUrl
  if (typeof fn !== 'function') throw new Error(`${rel} did not define buildServerUrl`)
  return fn as UrlBuilder
}

const BUILD_CASES: Array<[string, string | null]> = [
  // Complete addresses pass through, normalised.
  ['https://192.168.1.100:20000', 'https://192.168.1.100:20000'],
  ['http://example.com:8080', 'http://example.com:8080'],
  // Scheme is lowercased, path/query/fragment dropped, whitespace trimmed.
  ['HTTPS://Host:8443', 'https://Host:8443'],
  ['  https://example.com:20000/chat?x=1  ', 'https://example.com:20000'],
  ['https://host:8443/', 'https://host:8443'],
  // Missing scheme -> rejected (no default scheme any more).
  ['192.168.1.100:20000', null],
  ['example.com:20000', null],
  // Missing port -> rejected (no default port any more).
  ['https://192.168.1.100', null],
  ['http://example.com', null],
  ['192.168.1.100', null],
  // Garbage / unsupported forms.
  ['', null],
  ['   ', null],
  ['not a url', null],
  ['http://', null],
  ['ftp://host:21', null],
  ['https://user:pass@host:8443', null],
  ['[::1]:8080', null],
]

describe.each([
  ['android', ANDROID_UTILS],
  ['desktop', DESKTOP_UTILS],
])('buildServerUrl (%s copy)', (_label, rel) => {
  for (const [input, expected] of BUILD_CASES) {
    it(`${JSON.stringify(input)} -> ${JSON.stringify(expected)}`, () => {
      expect(builderFor(rel)(input)).toBe(expected)
    })
  }
})

const ANDROID_LOGIN = 'android/app/src/main/assets/login.html'
const DESKTOP_LOGIN = 'desktop/assets/login.html'

/**
 * Static wiring guard. Robolectric cannot execute JS (ShadowWebView
 * .evaluateJavascript is a no-op), so the blur handler cannot be exercised
 * end-to-end in a unit test. These assertions catch the wiring being forgotten
 * — a missing <script> tag or an unwired listener — which would otherwise ship
 * silently (no CSP, no error, the feature just does nothing).
 */
describe('login.html wiring', () => {
  for (const [label, rel] of [
    ['android', ANDROID_LOGIN],
    ['desktop', DESKTOP_LOGIN],
  ] as const) {
    it(`${label} loads url-utils.js, wires the address field, and defines hideError`, () => {
      const html = readRepoFile(rel)
      expect(html).toContain('<script src="url-utils.js"></script>')
      // The blur listener is bound to the single address field.
      expect(html).toMatch(/getElementById\('addAddress'\)\.addEventListener\('blur'/)
      expect(html).toContain('buildServerUrl')
      // The listener calls hideError('addErrorMsg') *before* it writes the field,
      // so deleting or renaming the page's real top-level hideError makes that
      // call throw and aborts the whole normalization. The behaviour tests below
      // inject their own stub, so only this static check can catch that.
      expect(html).toMatch(/function hideError\s*\(/)
    })

    it(`${label} declares the element ids the handlers dereference`, () => {
      const doc = new DOMParser().parseFromString(readRepoFile(rel), 'text/html')

      const address = doc.getElementById('addAddress')
      expect(address, '#addAddress must exist (the blur listener is bound to it)').not.toBeNull()
      // The sample placeholder is the affordance that teaches the strict format
      // now that the protocol radio and port field are gone.
      expect(address!.getAttribute('placeholder')).toBe('https://192.168.1.100:20000')

      expect(
        doc.getElementById('addPassword'),
        '#addPassword must exist (edit prefills it, submit reads it)',
      ).not.toBeNull()
      expect(
        doc.getElementById('addErrorMsg'),
        '#addErrorMsg must exist (the listener calls hideError(\'addErrorMsg\'))',
      ).not.toBeNull()
      // Title element reused to show "Edit Server" when editing.
      expect(doc.getElementById('addFormTitle'), '#addFormTitle must exist').not.toBeNull()
    })

    /**
     * The old UI had a protocol radio and a port field; the whole point of the
     * rework is that those are gone and the address is one string. If a refactor
     * reintroduces them (or leaves them behind), the form silently goes back to
     * two sources of truth for the URL.
     */
    it(`${label} has no protocol radio or port field`, () => {
      const doc = new DOMParser().parseFromString(readRepoFile(rel), 'text/html')
      expect(doc.querySelector('input[name="addProtocol"]')).toBeNull()
      expect(doc.getElementById('addPort')).toBeNull()
      expect(doc.getElementById('addHost')).toBeNull()
    })

    /**
     * Every server row must offer BOTH actions. The pencil is wired through an
     * inline onclick (like the existing delete button), so a missing SVG or a
     * renamed handler ships silently — the button renders but does nothing.
     */
    it(`${label} renders an edit button on each server row`, () => {
      const html = readRepoFile(rel)
      // Match the render site, not the stylesheet: the CSS rule
      // `.server-edit-btn` would satisfy a bare toContain() even after the
      // button's class was changed in the markup. The quoted class + onclick
      // pair only appears where the row is built.
      expect(html).toMatch(/class="server-edit-btn"[^>]*onclick="event\.stopPropagation\(\); editServer\(/)
      expect(html).toContain('SVG_PENCIL')
      expect(html).toMatch(/function editServer\s*\(/)
    })

    /**
     * Saving must not connect. Before this rework the add form called
     * connectToServer(); now both add and edit only persist. Asserting the
     * submit path's own text (sliced between its comment and the next handler)
     * keeps this from being satisfied by the unrelated connect form's call.
     */
    it(`${label} saves without connecting`, () => {
      const submitSrc = extractSubmitHandler(rel)
      expect(submitSrc).toContain('buildServerUrl')
      expect(submitSrc).toContain('saveServer')
      expect(submitSrc).not.toContain('connectToServer')
      // Renaming must retire the old URL, but only when it actually changed.
      expect(submitSrc).toMatch(/editingUrl\s*!==\s*url/)
      expect(submitSrc).toContain('removeServer')
      // The busy state must not reuse setConnecting: saving does not
      // authenticate, and "Authenticating..." would misdescribe it.
      expect(submitSrc).toContain('setSaving')
      expect(submitSrc).not.toContain('setConnecting')
      expect(readRepoFile(rel)).toMatch(/function setSaving\s*\(/)
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
 * The comment that opens the blur listener in both login.html files. The
 * extraction below slices from here to the listener's closing `});`, so if a
 * refactor renames or moves the block the test fails loudly instead of quietly
 * asserting nothing.
 */
const BLUR_COMMENT = '// Event: blur on the address field'

/**
 * Pull one handler's source out of a login.html, anchored on its `// Event:`
 * comment. Robust anchors:
 *  - the comment marker must exist;
 *  - the first `});` after it must close the handler (no further `// Event:`
 *    comment may intervene — the next handler starts with one).
 * Throws with a specific reason when an anchor moved, so a future refactor is
 * caught rather than silently passing.
 */
function extractHandler(rel: string, comment: string): string {
  const html = readRepoFile(rel)
  const markerIdx = html.indexOf(comment)
  if (markerIdx < 0) {
    throw new Error(`handler comment ${JSON.stringify(comment)} not found in ${rel}`)
  }
  const nextEventIdx = html.indexOf('// Event:', markerIdx + comment.length)
  const closeIdx = html.indexOf('});', markerIdx)
  if (closeIdx < 0) {
    throw new Error(`no closing '});' after ${JSON.stringify(comment)} in ${rel}`)
  }
  if (nextEventIdx !== -1 && nextEventIdx < closeIdx) {
    throw new Error(
      `the handler block in ${rel} does not end before the next '// Event:' comment ` +
        `(close=${closeIdx}, nextEvent=${nextEventIdx}) — extraction anchors moved`,
    )
  }
  const src = html.slice(markerIdx, closeIdx + '});'.length)
  if (!src.endsWith('});')) {
    throw new Error(`extracted block from ${rel} does not end with '});'`)
  }
  return src
}

/** The blur listener specifically, with an extra shape assertion. */
function extractBlurListener(rel: string): string {
  const src = extractHandler(rel, BLUR_COMMENT)
  if (!src.includes("addEventListener('blur'")) {
    throw new Error(`extracted block from ${rel} is not the blur listener (no addEventListener('blur')`)
  }
  return src
}

const SUBMIT_COMMENT = '// Event: save the add/edit form'

/**
 * Pull the add/edit submit handler out of a login.html.
 *
 * This one cannot use extractHandler's "first `});` closes the handler"
 * shortcut: its body contains a nested `savedServers.some(function(){...})`,
 * whose own `});` would end the slice early and hide the rest of the handler
 * from the assertions. Instead, count braces from the `function(e) {` opening
 * until they balance.
 */
function extractSubmitHandler(rel: string): string {
  const html = readRepoFile(rel)
  const markerIdx = html.indexOf(SUBMIT_COMMENT)
  if (markerIdx < 0) {
    throw new Error(`handler comment ${JSON.stringify(SUBMIT_COMMENT)} not found in ${rel}`)
  }
  const openIdx = html.indexOf('function(e) {', markerIdx)
  if (openIdx < 0) {
    throw new Error(`no 'function(e) {' after ${JSON.stringify(SUBMIT_COMMENT)} in ${rel}`)
  }
  let depth = 0
  for (let i = openIdx + 'function(e) {'.length - 1; i < html.length; i++) {
    const ch = html[i]
    if (ch === '{') depth++
    else if (ch === '}') {
      depth--
      if (depth === 0) {
        // Include the trailing `);` that closes addEventListener(...).
        const end = html.indexOf(');', i)
        if (end < 0) throw new Error(`unterminated addEventListener in ${rel}`)
        return html.slice(markerIdx, end + 2)
      }
    }
  }
  throw new Error(`unbalanced braces in the submit handler of ${rel}`)
}

/**
 * The two login.html files legitimately differ (i18n slogans, the async bridge
 * handling, comments), so a whole-file equality check would be wrong. The blur
 * listener, however, is deliberately platform-agnostic — it touches no native
 * bridge, only the shared parseServerInput and plain DOM — so the two copies
 * must stay identical. Divergence means someone edited one page's listener and
 * forgot the other, which no behavioural test would catch (each page is tested
 * only against its own copy).
 */
describe('login.html blur listener copies', () => {
  it('are byte-identical', () => {
    expect(extractBlurListener(ANDROID_LOGIN)).toBe(extractBlurListener(DESKTOP_LOGIN))
  })
})

/**
 * Minimal DOM the blur listener touches: just the single address field and the
 * error container. Mirrors the real login page.
 */
const DOM_FIXTURE = `
  <input type="text" id="addAddress">
  <div id="addErrorMsg"></div>
`

/**
 * End-to-end wiring guard: actually executes the extracted listener against a
 * real jsdom document. Robolectric cannot run JS, so this is the only place the
 * blur handler's behaviour (not just its text) is exercised. Registered via
 * indirect eval so the listener's free identifiers (document, buildServerUrl,
 * hideError) resolve in global scope exactly as they do in the page.
 */
describe.each([
  ['android', ANDROID_LOGIN, ANDROID_UTILS],
  ['desktop', DESKTOP_LOGIN, DESKTOP_UTILS],
])('blur listener behaviour (%s page)', (_label, loginRel, utilsRel) => {
  let address: HTMLInputElement

  beforeEach(() => {
    document.body.innerHTML = DOM_FIXTURE
    // The page defines hideError(id) as a global; mirror its real semantics
    // (see login.html): remove the 'visible' class from the error element.
    ;(globalThis as unknown as Record<string, unknown>).hideError = (id: string) => {
      document.getElementById(id)?.classList.remove('visible')
    }
    // url-utils.js defines buildServerUrl as a global function.
    ;(0, eval)(readRepoFile(utilsRel))
    // Register the listener on the real #addAddress element.
    ;(0, eval)(extractBlurListener(loginRel))
    address = document.getElementById('addAddress') as HTMLInputElement
    if (!address) throw new Error(`${loginRel} fixture did not create #addAddress`)
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  function blur(text: string): void {
    address.value = text
    // blur does not bubble, but the listener is bound directly on #addAddress,
    // so a non-bubbling event dispatched on the element itself still reaches it.
    address.dispatchEvent(new Event('blur'))
  }

  it('normalizes a full address in place', () => {
    blur('HTTPS://Host:8443/chat?x=1')
    // Scheme lowercased, path/query dropped.
    expect(address.value).toBe('https://Host:8443')
  })

  it('leaves an incomplete address exactly as typed and keeps the error visible', () => {
    const err = document.getElementById('addErrorMsg') as HTMLElement
    err.classList.add('visible')
    // Missing port -> not a complete address.
    blur('https://192.168.1.100')
    expect(address.value).toBe('https://192.168.1.100')
    // Incomplete -> the listener returns before hideError, so a shown error
    // must stay visible.
    expect(err.classList.contains('visible')).toBe(true)
  })

  it('leaves a scheme-less address untouched (no default scheme is guessed)', () => {
    blur('192.168.1.100:20000')
    expect(address.value).toBe('192.168.1.100:20000')
  })

  it('is idempotent across repeated blurs', () => {
    blur('https://192.168.1.100:20000')
    expect(address.value).toBe('https://192.168.1.100:20000')
    // Re-blurring (which happens often, e.g. when the form is hidden) must be
    // harmless.
    blur(address.value)
    expect(address.value).toBe('https://192.168.1.100:20000')
  })

  it('hides the error message on a successful parse (proves hideError ran)', () => {
    const err = document.getElementById('addErrorMsg') as HTMLElement
    err.classList.add('visible')
    blur('https://host:8443')
    expect(err.classList.contains('visible')).toBe(false)
  })
})
