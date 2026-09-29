import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Contract for the shared `.wrap-check` class in css/components.css.
 *
 * Why this exists: the trailing "this option is currently ON" check started
 * life inside FileHeader.vue (word wrap / line numbers / sticky scroll) and was
 * declared twice there — once scoped, once in the Teleported-menu block. When
 * the session list's "Share conversation" menu item needed the same indicator,
 * a third copy would have been needed (a scoped rule only styles the component
 * that declares it, and the menu is Teleported out of both components' trees).
 * The base rule now lives in components.css and every call site keeps none.
 *
 * The trap this guards: re-adding the base rule to a component silently
 * "works" for that one component while the other menu keeps its own copy —
 * and the two drift. jsdom has no CSS engine, so these are source-sniffing
 * checks (same pattern as countBadge.css.test.ts).
 */

function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

const css = readWebFile('css/components.css')

/** Declarations of the first `<selector> {` rule. */
function declsOf(selector: string): string {
  const m = css.match(
    new RegExp(selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([^}]*)\\}'),
  )
  expect(m, `${selector} rule must exist in components.css`).not.toBeNull()
  return m![1]
}

describe('.wrap-check is declared globally', () => {
  it('exists in components.css as a base rule', () => {
    expect(css).toMatch(/(?:^|\n)\.wrap-check\s*\{/)
  })

  it('owns the geometry that makes it a right-edge check', () => {
    // `margin-left: auto` is what pushes it to the far edge of a flex menu row;
    // without it the check sits immediately after the label.
    const decls = declsOf('.wrap-check')
    expect(decls).toMatch(/margin-left:\s*auto/)
    expect(decls).toMatch(/color:\s*var\(--accent-color\)/)
    expect(decls).toMatch(/font-weight:\s*var\(--font-weight-bold\)/)
  })
})

describe('no component re-declares .wrap-check', () => {
  /** Every .vue under src/, with its path relative to web/. */
  function collectVueSources(): { path: string; text: string }[] {
    let root = ''
    for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
      try {
        readFileSync(join(base, 'css/components.css'))
        root = base
        break
      } catch {
        // next
      }
    }
    expect(root, 'web root must be discoverable').not.toBe('')

    const out: { path: string; text: string }[] = []
    const walk = (dir: string) => {
      for (const e of readdirSync(dir, { withFileTypes: true })) {
        const full = join(dir, e.name)
        if (e.isDirectory()) {
          if (['node_modules', 'dist', '__tests__', 'vendor-build'].includes(e.name)) continue
          walk(full)
        } else if (e.name.endsWith('.vue')) {
          out.push({ path: full.slice(root.length + 1), text: readFileSync(full, 'utf8') })
        }
      }
    }
    walk(join(root, 'src'))
    return out
  }

  const sources = collectVueSources()

  it('finds the sources it is meant to guard', () => {
    expect(sources.length).toBeGreaterThan(50)
  })

  it('no .vue declares .wrap-check in any of its style blocks', () => {
    // A .vue can carry more than one <style> block (FileHeader has both a
    // scoped and an unscoped one), so the whole file is scanned rather than a
    // single block — the class must not reappear in EITHER.
    const offenders: string[] = []
    for (const { path, text } of sources) {
      for (const m of text.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)) {
        if (/\.wrap-check\s*[,{]/.test(m[1])) {
          offenders.push(`${path}: .wrap-check is declared locally`)
        }
      }
    }
    expect(
      offenders,
      `these must use the global rule in css/components.css:\n${offenders.join('\n')}`,
    ).toEqual([])
  })

  it('both menus render the check from the same class', () => {
    // The two call sites are the file header's dropdown and the session list's
    // context menu; losing the class on either makes that menu's "on" state
    // invisible while the other keeps working.
    const fileHeader = readWebFile('src/components/file/FileHeader.vue')
    const sessionList = readWebFile('src/components/session/SessionList.vue')
    expect(fileHeader).toMatch(/class="wrap-check"/)
    expect(sessionList).toMatch(/class="wrap-check"/)
  })
})
