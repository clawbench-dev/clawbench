import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Source guard: every PascalCase component/icon used in a template must be
 * imported.
 *
 * Why this exists: a `<SearchCode :size="16" />` was added to the file
 * manager's toolbar but its `import { SearchCode } from 'lucide-vue-next'`
 * never landed (an Edit reported success without writing). Vue treats an
 * unresolved tag as a native custom element, so the button rendered with an
 * EMPTY icon and nothing failed: no build error, no test failure, and
 * `vue-tsc` does not check template tag resolution. The bug was only visible by
 * looking at the running UI.
 *
 * This catches that whole class of mistake for the files it covers.
 */

/** Vue built-ins and globally-registered tags that need no import. */
const BUILTIN_TAGS = new Set([
  'Teleport', 'Transition', 'TransitionGroup', 'KeepAlive', 'Component',
  'Suspense', 'RouterView', 'RouterLink', 'template', 'slot',
])

/**
 * Extract imported binding names from a `<script setup>` block, covering
 * `import X from`, `import { A, B as C } from`, and `import * as ns`.
 */
function importedNames(script: string): Set<string> {
  const names = new Set<string>()
  for (const m of script.matchAll(/import\s+([^'"]+?)\s+from\s+['"][^'"]+['"]/g)) {
    const clause = m[1]
    // `{ A, B as C }` → A, C
    for (const braced of clause.matchAll(/\{([^}]*)\}/g)) {
      for (const part of braced[1].split(',')) {
        const name = part.trim().split(/\s+as\s+/).pop()?.trim()
        if (name) names.add(name)
      }
    }
    // default import and `* as ns`
    const withoutBraces = clause.replace(/\{[^}]*\}/g, '')
    for (const part of withoutBraces.split(',')) {
      const name = part.trim().split(/\s+as\s+/).pop()?.replace(/^\*\s*/, '').trim()
      if (name && /^[A-Za-z_$][\w$]*$/.test(name)) names.add(name)
    }
  }
  return names
}

/**
 * PascalCase tags used in the template. Only the opening form is matched; a
 * closing `</Foo>` is covered by its opening tag. Self-closing and paired forms
 * both match `<Foo` followed by whitespace, `/`, or `>`.
 */
function usedComponents(template: string): Set<string> {
  const out = new Set<string>()
  for (const m of template.matchAll(/<([A-Z][A-Za-z0-9]*)(?=[\s/>])/g)) {
    out.add(m[1])
  }
  return out
}

function splitBlocks(src: string): { template: string; script: string } {
  // The template is everything before the first <script> block.
  const scriptStart = src.search(/<script\b/)
  const template = scriptStart === -1 ? src : src.slice(0, scriptStart)
  const script = scriptStart === -1 ? '' : src.slice(scriptStart)
  return { template, script }
}

/** Files this guard covers — the components touched by the search feature. */
const GUARDED_FILES = [
  'src/components/file/FileManagerContent.vue',
  'src/components/file/ContentSearchDialog.vue',
]

describe('template component imports', () => {
  for (const file of GUARDED_FILES) {
    it(`${file} imports every component it uses in the template`, () => {
      const src = readWebFile(file)
      const { template, script } = splitBlocks(src)

      const imported = importedNames(script)
      const used = usedComponents(template)

      const unresolved = [...used].filter(
        (tag) => !imported.has(tag) && !BUILTIN_TAGS.has(tag),
      )

      expect(
        unresolved,
        `these tags are used in ${file} but never imported — Vue renders them ` +
          `as empty native elements (a silently missing icon/component): ${unresolved.join(', ')}`,
      ).toEqual([])
    })
  }
})
