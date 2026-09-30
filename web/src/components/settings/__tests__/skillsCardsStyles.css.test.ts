import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Source guards for the skills settings cards.
 *
 * These exist because splitting one component into four silently dropped the
 * scoped styles: the new cards kept the class names in their templates
 * (`skills-input`, `sbtn`) but the definitions stayed behind in the old
 * component, so the fields and buttons rendered unstyled. A missing scoped rule
 * fails nothing — it just looks wrong — so it has to be pinned here.
 *
 * jsdom has no CSS engine, so these are source-sniffing checks. cwd differs
 * between a bare `vitest` run (web/) and scripts/vitest-run.sh (repo root), so
 * both roots are probed (same convention as the other CSS guards).
 */
function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`could not read ${relPath} from any known root`)
}

/** Every class name the template references. */
function templateClasses(src: string): Set<string> {
  const out = new Set<string>()
  for (const m of src.matchAll(/class="([^"]+)"/g)) {
    for (const c of m[1].split(/\s+/)) if (c) out.add(c)
  }
  return out
}

const CARDS = [
  'web/src/components/settings/SkillsDirsSetting.vue',
  'web/src/components/settings/SkillsReposSetting.vue',
]

describe('skills cards styles', () => {
  it.each(CARDS)('%s defines every skills- class its template uses', (rel) => {
    const src = readWebFile(rel)
    const used = [...templateClasses(src)].filter((c) => c.startsWith('skills-'))
    expect(used.length).toBeGreaterThan(0)

    // A `skills-*` class used in the template must appear as a rule in the same
    // file. Classes owned by a shared stylesheet are excluded by the prefix.
    const missing = used.filter((c) => !new RegExp(`\\.${c}[\\s,{:]`).test(src))
    expect(missing, `unstyled classes in ${rel}: ${missing.join(', ')}`).toEqual([])
  })

  // The design guide is explicit: buttons inside the settings panel reuse
  // `.fbtn`; no bespoke button class. `.sbtn` was a local invention that lost
  // its definition in the split (and would have diverged from every other
  // settings button even if it had survived).
  it.each(CARDS)('%s uses the shared .fbtn button, not a local one', (rel) => {
    const src = readWebFile(rel)
    const used = templateClasses(src)
    expect(used.has('fbtn')).toBe(true)
    expect([...used].filter((c) => /^(sbtn|btn)$/.test(c))).toEqual([])
    // Using .fbtn directly (rather than through ModalDialog) requires importing
    // its stylesheet; without it the button renders with UA defaults.
    expect(src).toMatch(/import '@\/assets\/modal-footer-btn\.css'/)
  })
})
