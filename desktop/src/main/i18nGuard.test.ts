import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative, resolve } from 'node:path'

/**
 * Guard: the Electron main process must not gain new hardcoded CJK.
 *
 * Every user-visible main-process string used to be Chinese-only (the
 * version-gate dialogs, the context menu), so an English user got Chinese
 * native UI. They now resolve through `labels.ts` / `contextMenuLabels`, keyed
 * off `resolveNativeLanguage()`. This test scans `desktop/src` for CJK and
 * fails on anything outside the two label tables — so a future dialog cannot
 * silently ship a hardcoded sentence.
 *
 * The scan is deliberately file-level (not line-level): the label tables are
 * the ONLY files allowed to contain CJK, and everything else must be free of
 * it. That keeps the rule simple and the failure message actionable.
 */

/** Files allowed to contain CJK, each with the reason. */
const ALLOWED: Record<string, string> = {
  'main/labels.ts': 'native dialog label table (en/zh), resolved by resolveNativeLanguage',
  'main/contextMenu.ts': 'context-menu label table (en/zh)',
}

const CJK = /[\u4e00-\u9fff]/

/**
 * Strip `//` and block comments and string-literal-free regions are NOT
 * needed: comments may legitimately name a Chinese UI concept ("界面缩放"), so
 * only comment text is removed and code (including string literals) is kept.
 * That is where a user-visible hardcoded string would live.
 */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/(^|[^:])\/\/[^\n]*/g, '$1')
}

function srcRoot(): string {
  for (const base of [process.cwd(), resolve(process.cwd(), 'desktop')]) {
    const candidate = resolve(base, 'src')
    try {
      if (statSync(candidate).isDirectory()) return candidate
    } catch {
      // try next
    }
  }
  throw new Error('desktop/src not found from cwd: ' + process.cwd())
}

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) {
      if (name === '__tests__') continue
      walk(p, out)
    } else if (name.endsWith('.ts') && !name.endsWith('.test.ts')) {
      out.push(p)
    }
  }
  return out
}

describe('desktop main-process CJK guard', () => {
  it('has no hardcoded CJK outside the label tables', () => {
    const root = srcRoot()
    const offenders: string[] = []
    for (const file of walk(root)) {
      const rel = relative(root, file).split(/[\\/]/).join('/')
      if (ALLOWED[rel]) continue
      if (CJK.test(stripComments(readFileSync(file, 'utf8')))) offenders.push(rel)
    }
    expect(
      offenders,
      `CJK found in desktop/src outside the allow list:\n  ${offenders.join('\n  ')}\n\n` +
        'Move user-visible text into main/labels.ts and resolve it via resolveNativeLanguage().',
    ).toEqual([])
  })

  it('keeps every allow-listed file actually present (no stale entries)', () => {
    // A renamed/deleted label table would otherwise silently stop being
    // guarded while the allow list still claims it is covered.
    const root = srcRoot()
    for (const rel of Object.keys(ALLOWED)) {
      expect(() => statSync(join(root, rel)), `${rel} is missing`).not.toThrow()
    }
  })
})

/**
 * Guard: the capsule document must carry no user-visible CJK.
 *
 * The scan above only walks `.ts` files under `desktop/src`, so
 * `desktop/assets/*.html` was a blind spot — exactly where the capsule's markup
 * lives. Its labels now arrive over IPC from `main/labels.ts` (see
 * `floatingCapsuleLabels`), so a literal here is either a dead default or,
 * worse, the string an English user sees whenever the first state push is late.
 *
 * `login.html` is deliberately NOT covered: it is a self-contained document
 * with its own in-file `I18N` table (the main process cannot reach it before
 * the page loads), so CJK there is the zh column, not a leak.
 */
describe('capsule document CJK guard', () => {
  const assetsRoot = resolve(srcRoot(), '..', 'assets')

  it('floating.html has no hardcoded CJK', () => {
    const file = join(assetsRoot, 'floating.html')
    const src = stripComments(readFileSync(file, 'utf8'))
    const offending = src
      .split('\n')
      .map((line, i) => [i + 1, line] as const)
      .filter(([, line]) => CJK.test(line))
      .map(([n, line]) => `  ${n}: ${line.trim()}`)
    expect(
      offending,
      `CJK found in desktop/assets/floating.html:\n${offending.join('\n')}\n\n` +
        'The capsule resolves its strings from main/labels.ts (floatingCapsuleLabels) ' +
        'and receives them over IPC — put the text there instead.',
    ).toEqual([])
  })

  it('floating.html still receives every label it renders', () => {
    // The guard above is satisfiable by deleting the text entirely, which would
    // leave the capsule rendering blanks. Pin that the four counter words plus
    // the body hint are actually applied from the pushed state.
    const src = readFileSync(join(assetsRoot, 'floating.html'), 'utf8')
    for (const field of ['running', 'pending', 'unread', 'idle', 'hint']) {
      expect(src, `floating.html must apply labels.${field}`).toContain(`l.${field}`)
    }
  })
})
