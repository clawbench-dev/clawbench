import { describe, expect, it } from 'vitest'
import { readdirSync, statSync } from 'node:fs'
import { resolve, join } from 'node:path'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: a ref must never be tested for truthiness in `<script setup>` code.
 *
 * A `ref` is an object, so `someRef || fallback` is a constant-true expression —
 * script code does NOT auto-unwrap the way a template does. The failure is
 * silent: the branch simply always runs (or never runs), with no error anywhere.
 *
 * Real incident (2026-09-24): App.vue's completion-notification guard read
 *
 *     const chatPanelActive = isWideScreen || activeTab.value === 'chat'
 *
 * `isWideScreen` comes from useWideScreenLayout(), so this was constant `true`
 * and the guard collapsed to "any event for the session you have open is
 * suppressed" — no matter which tab was showing. On Android the native
 * background service suppresses its own notification while the app is in the
 * foreground, so an approval request produced NO notification on either
 * channel.
 *
 * Why this test exists rather than relying on tooling:
 *   - `vue/no-ref-as-operand` (enabled in web/eslint.config.js) only recognises
 *     refs it can see declared with ref()/computed() in the SAME file. A ref
 *     destructured from a composable carries no such info — exactly the App.vue
 *     case, so the rule does not fire.
 *   - `vue-tsc` does not catch it either: TypeScript permits truthiness on
 *     object types, so there is no type error even with `lang="ts"`.
 *   - 67 of ~76 .vue files here omit `lang="ts"` anyway, so vue-tsc skips them.
 *
 * To cover the composable case this test reads every composable's `return {…}`
 * and learns which keys are refs, then checks each component's destructures
 * against that map. Verified at zero false positives on the current tree.
 */

/** Every file under a web-relative directory, filtered by extension. */
function listFiles(relDir: string, ext: string): string[] {
  const root = process.cwd().endsWith('web')
    ? resolve(process.cwd(), relDir)
    : resolve(process.cwd(), 'web', relDir)
  const out: string[] = []
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir)) {
      if (entry === 'node_modules' || entry === '__tests__') continue
      const full = join(dir, entry)
      if (statSync(full).isDirectory()) walk(full)
      else if (entry.endsWith(ext)) out.push(full)
    }
  }
  walk(root)
  return out
}

/** Path relative to web/, for readWebFile (cwd-agnostic). */
function webRel(full: string): string {
  return full.replace(/^.*?web\//, '')
}

/** The `<script>` block only — template code legitimately auto-unwraps refs. */
function scriptBlock(src: string): string {
  const m = src.match(/<script[^>]*>([\s\S]*?)<\/script>/)
  return m ? m[1] : ''
}

/** Strip comments so an assertion cannot be satisfied (or broken) by prose. */
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '')
}

const REF_FACTORY = /\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::[^=]*)?=\s*(?:ref|shallowRef|computed|toRef)\s*[<(]/g

/** Index of the `}` closing the `{` at `open`, or -1. */
function matchBrace(src: string, open: number): number {
  let depth = 0
  for (let i = open; i < src.length; i++) {
    if (src[i] === '{') depth++
    else if (src[i] === '}') {
      depth--
      if (depth === 0) return i
    }
  }
  return -1
}

/**
 * Map of composable function name -> keys whose returned value is a ref.
 *
 * Only keys that are literally refs matter: a composable returning a plain
 * value under the same name is not affected by this bug class.
 */
function buildRefReturnMap(): Map<string, Set<string>> {
  const map = new Map<string, Set<string>>()
  for (const full of listFiles('src/composables', '.ts')) {
    const src = readWebFile(webRel(full))
    const refNames = new Set([...src.matchAll(REF_FACTORY)].map((m) => m[1]))

    for (const m of src.matchAll(/export\s+function\s+([A-Za-z_$][\w$]*)\s*\([^)]*\)\s*\{/g)) {
      const close = matchBrace(src, m.index! + m[0].length - 1)
      if (close === -1) continue
      const body = src.slice(m.index!, close + 1)
      const keys = new Set<string>()
      for (const r of body.matchAll(/return\s*\{([^}]*)\}/g)) {
        for (const part of r[1].split(',')) {
          const local = part.trim().split(':').pop()?.trim()
          if (local && refNames.has(local)) keys.add(local)
        }
      }
      if (keys.size) {
        const existing = map.get(m[1]) ?? new Set<string>()
        for (const k of keys) existing.add(k)
        map.set(m[1], existing)
      }
    }
  }
  return map
}

/**
 * Names in this script that are (very likely) refs: declared locally with
 * ref()/computed(), destructured from a ref-returning composable, or injected
 * as a non-optional Ref.
 *
 * `inject<Ref<T> | undefined>` is excluded — an explicitly optional injection
 * is meant to be tested for undefined before use.
 */
function refNamesIn(body: string, refReturns: Map<string, Set<string>>): Set<string> {
  const names = new Set<string>([...body.matchAll(REF_FACTORY)].map((m) => m[1]))

  for (const m of body.matchAll(/const\s*\{([^}]*)\}\s*=\s*([A-Za-z_$][\w$]*)\s*\(/g)) {
    const returned = refReturns.get(m[2])
    if (!returned) continue
    for (const part of m[1].split(',')) {
      const k = part.trim()
      if (!k) continue
      const [orig, local] = k.includes(':')
        ? [k.split(':')[0].trim(), k.split(':')[1].trim()]
        : [k, k]
      if (returned.has(orig)) names.add(local)
    }
  }

  // Only `inject<Ref<...>>` is a ref. A plain `inject<(path) => void>('x')`
  // is a function that may legitimately be absent, and testing it before use
  // is the correct pattern — not this bug.
  for (const m of body.matchAll(/\b(?:const|let)\s+([A-Za-z_$][\w$]*)\s*=\s*inject\s*<([\s\S]*?)>\s*\(([^)]*)\)/g)) {
    if (!/\bRef\s*</.test(m[2])) continue
    if (m[2].includes('undefined') || /\bundefined\b/.test(m[3])) continue
    names.add(m[1])
  }

  return names
}

const esc = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

/** Bare (non-.value) truthiness uses of refs, with the offending line number. */
function scan(
  src: string,
  refReturns: Map<string, Set<string>>,
): Array<{ line: number; name: string; text: string }> {
  const body = stripComments(scriptBlock(src))
  if (!body) return []

  const findings: Array<{ line: number; name: string; text: string }> = []
  for (const name of [...refNamesIn(body, refReturns)].sort()) {
    const n = esc(name)
    body.split('\n').forEach((line, idx) => {
      // Skip the declaration/destructure line itself.
      if (/const\s*\{/.test(line)) return
      if (new RegExp(`\\b(?:const|let|var)\\s+${n}\\b`).test(line)) return
      // Must be used bare (not `x.value`, not a call `x()`, not a property).
      if (!new RegExp(`(?<![.\\w$])${n}(?![.\\w$(])`).test(line)) return

      const bare = new RegExp(`(?<![.\\w$])${n}\\s*(?:\\|\\||&&)`).test(line)
      const negated = new RegExp(`(?<![.\\w$])!\\s*${n}\\b(?!\\.)`).test(line)
      const ifGuard = new RegExp(`if\\s*\\(\\s*${n}\\s*(?:\\)|\\|\\||&&)`).test(line)
      if (bare || negated || ifGuard) {
        findings.push({ line: idx + 1, name, text: line.trim() })
      }
    })
  }
  return findings
}

const REF_RETURNS = buildRefReturnMap()

describe('refs are not used bare in boolean context (<script setup>)', () => {
  it('parsed the composables (guard is actually armed)', () => {
    // If the composable parser silently stopped matching (e.g. a formatting
    // change), the destructure half of this guard would go quiet without
    // failing. Pin a floor so that cannot happen unnoticed.
    expect(REF_RETURNS.size).toBeGreaterThan(40)
    expect(REF_RETURNS.get('useWideScreenLayout')).toContain('isWideScreen')
  })

  it('finds .vue files to check', () => {
    expect(listFiles('src', '.vue').length).toBeGreaterThan(50)
  })

  it('no ref is tested for truthiness without .value', () => {
    const offenders: string[] = []
    for (const full of listFiles('src', '.vue')) {
      const rel = webRel(full)
      for (const f of scan(readWebFile(rel), REF_RETURNS)) {
        offenders.push(`${rel}:${f.line}  [${f.name}]  ${f.text}`)
      }
    }
    expect(
      offenders,
      'A ref used bare in a boolean context is a constant-true/false expression '
      + '(script code does not auto-unwrap). Read it with .value, or pass the '
      + 'unwrapped value into a typed helper so a missing .value is a type error.',
    ).toEqual([])
  })
})

/**
 * The scanner is the thing under test as much as the tree is: a guard that
 * cannot see the bug it was written for is worse than no guard, because it
 * looks like coverage. These cases pin its behaviour.
 */
describe('scanner catches the bug class it was written for', () => {
  const empty = new Map<string, Set<string>>()
  const withLayout = new Map([['useWideScreenLayout', new Set(['isWideScreen', 'chatCollapsed'])]])

  it('flags a composable-destructured ref used with ||', () => {
    const src = `<script setup>
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
const { isWideScreen } = useWideScreenLayout()
const activeTab = { value: 'chat' }
function check() {
  const bad = isWideScreen || activeTab.value === 'chat'
  return bad
}
</script>
<template><div>{{ check() }}</div></template>`
    expect(scan(src, withLayout).map((f) => f.name)).toContain('isWideScreen')
  })

  it('flags a locally declared ref used as an if-guard', () => {
    const src = `<script setup>
import { ref } from 'vue'
const visible = ref(false)
if (visible) { console.log('x') }
</script>`
    expect(scan(src, empty).map((f) => f.name)).toContain('visible')
  })

  it('flags a negated ref', () => {
    const src = `<script setup>
import { ref } from 'vue'
const ready = ref(false)
const out = !ready
</script>`
    expect(scan(src, empty).map((f) => f.name)).toContain('ready')
  })

  it('flags a composable ref used only bare (never with .value)', () => {
    // The worst case: nothing in the file reveals it is a ref, so only the
    // composable's return map can tell.
    const src = `<script setup>
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
const { chatCollapsed } = useWideScreenLayout()
if (chatCollapsed) { console.log('hidden') }
</script>`
    expect(scan(src, withLayout).map((f) => f.name)).toContain('chatCollapsed')
  })

  it('does NOT flag the same ref read with .value', () => {
    const src = `<script setup>
import { ref } from 'vue'
const visible = ref(false)
const out = visible.value || 'fallback'
</script>`
    expect(scan(src, empty)).toEqual([])
  })

  it('does NOT flag an explicitly optional inject', () => {
    const src = `<script setup lang="ts">
import { inject, type Ref } from 'vue'
const activeTab = inject<Ref<string> | undefined>('activeTab', undefined)
if (activeTab) { activeTab.value }
</script>`
    expect(scan(src, empty)).toEqual([])
  })

  it('does NOT look at template code (refs auto-unwrap there)', () => {
    const src = `<script setup>
import { ref } from 'vue'
const visible = ref(false)
</script>
<template><div v-if="visible">hi</div></template>`
    expect(scan(src, empty)).toEqual([])
  })

  it('does NOT flag a plain object that is never declared as a ref', () => {
    const src = `<script setup>
const props = defineProps({ title: String })
if (props.title) { console.log('x') }
</script>`
    expect(scan(src, empty)).toEqual([])
  })

  it('does NOT flag a composable key that is not a ref', () => {
    const src = `<script setup>
import { useFoo } from '@/composables/useFoo'
const { loading } = useFoo()
if (loading) { console.log('x') }
</script>`
    // useFoo returns `loading` as a plain boolean, not a ref.
    const map = new Map([['useFoo', new Set(['other'])]])
    expect(scan(src, map)).toEqual([])
  })
})
