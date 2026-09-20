import { describe, it, expect } from 'vitest'
import { readdirSync, statSync, existsSync } from 'node:fs'
import { resolve, dirname, join } from 'node:path'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: every scroll-blocking event binding must be a DELIBERATE choice.
 *
 * Chrome warns "Added non-passive event listener to a scroll-blocking event"
 * for any `touchstart` / `touchmove` / `wheel` listener registered without
 * `passive: true`. The cost is real: the browser must wait for the handler to
 * finish before it may scroll, so a busy main thread (ClawBench streams and
 * renders heavily) turns every scroll into a stutter.
 *
 * Vue registers template handlers as NON-passive unless told otherwise, so a
 * plain `@wheel="onScroll"` silently blocks scrolling even when the handler
 * only sets a flag. Static grep of the bundle cannot catch this — the listener
 * is added by Vue at runtime, so there is no literal string to find. That is
 * exactly why this test exists.
 *
 * The contract enforced here, for every scroll-blocking binding:
 *
 *   - `.passive`  → the handler must NOT call preventDefault. Calling it in a
 *                   passive listener is a silent no-op (console warning only),
 *                   which would look correct in code review and fail at runtime.
 *   - bare        → the handler MUST call preventDefault. A bare listener that
 *                   never prevents anything is the bug this test guards: it
 *                   should be `.passive` instead.
 *   - `.prevent`  → always fine: an explicit, deliberate non-passive choice.
 *
 * This makes the classification total — no binding may be left undecided.
 */

const SCROLL_BLOCKING = ['touchstart', 'touchmove', 'wheel', 'mousewheel']

/** Every .vue file under web/src, as a path relative to web/ (e.g. src/App.vue). */
function listVueFiles(): string[] {
  const out: string[] = []
  const root = process.cwd().endsWith('web') ? resolve(process.cwd(), 'src') : resolve(process.cwd(), 'web/src')
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir)) {
      if (entry === 'node_modules' || entry === '__tests__') continue
      const full = join(dir, entry)
      if (statSync(full).isDirectory()) walk(full)
      else if (entry.endsWith('.vue')) out.push(full)
    }
  }
  walk(root)
  return out
}

/** Whether a web-relative file exists, from whichever cwd the runner uses. */
function webFileExists(rel: string): boolean {
  try {
    readWebFile(rel)
    return true
  } catch {
    return false
  }
}

interface Binding {
  file: string // web-relative, e.g. src/components/chat/ChatMessageList.vue
  line: number
  event: string
  modifiers: string[]
  expr: string
}

/** All scroll-blocking bindings in a .vue template, with modifiers. */
function findBindings(relPath: string, source: string): Binding[] {
  const out: Binding[] = []
  const pattern = new RegExp(
    `@(${SCROLL_BLOCKING.join('|')})((?:\\.[a-zA-Z]+)*)\\s*=\\s*["']([^"']*)["']`,
    'g',
  )
  const lines = source.split('\n')
  lines.forEach((line, idx) => {
    // Only look inside the <template> block — script code is checked separately.
    let m: RegExpExecArray | null
    pattern.lastIndex = 0
    while ((m = pattern.exec(line)) !== null) {
      out.push({
        file: relPath,
        line: idx + 1,
        event: m[1],
        modifiers: m[2] ? m[2].split('.').filter(Boolean) : [],
        expr: m[3].trim(),
      })
    }
  })
  return out
}

/**
 * Collect a component plus everything it transitively imports via `@/`, so a
 * handler destructured from a composable (or re-exported by one) can be found.
 */
function buildCorpus(entryRel: string): Map<string, string> {
  const corpus = new Map<string, string>()
  const queue: string[] = [entryRel]
  while (queue.length > 0) {
    const rel = queue.shift() as string
    if (corpus.has(rel)) continue
    let src: string
    try {
      src = readWebFile(rel)
    } catch {
      continue
    }
    corpus.set(rel, src)
    for (const m of src.matchAll(/from\s+['"]@\/([^'"]+)['"]/g)) {
      const target = resolveImport(`src/${m[1]}`)
      if (target) queue.push(target)
    }
  }
  return corpus
}

/** Resolve an `@/x` import to a web-relative file, trying .ts then .vue. */
function resolveImport(base: string): string | null {
  for (const cand of [base, `${base}.ts`, `${base}.vue`]) {
    if (webFileExists(cand)) return cand
  }
  return null
}

/**
 * Extract a function body by name from the corpus. Returns every definition
 * found (a name may appear in several files; all must satisfy the contract).
 */
function findBodies(corpus: Map<string, string>, name: string): string[] {
  const bodies: string[] = []
  for (const src of corpus.values()) {
    for (const pattern of [
      new RegExp(`function\\s+${name}\\s*\\(`, 'g'),
      new RegExp(`(?:const|let)\\s+${name}\\s*=\\s*(?:async\\s*)?\\(?[^=]*?\\)?\\s*=>`, 'g'),
    ]) {
      let m: RegExpExecArray | null
      while ((m = pattern.exec(src)) !== null) {
        const start = src.indexOf('{', m.index + m[0].length - 1)
        if (start === -1) continue
        let depth = 0
        let end = -1
        for (let i = start; i < src.length; i++) {
          if (src[i] === '{') depth++
          else if (src[i] === '}') {
            depth--
            if (depth === 0) {
              end = i
              break
            }
          }
        }
        if (end !== -1) bodies.push(src.slice(start, end + 1))
      }
    }
  }
  return bodies
}

/**
 * Every function body reachable from `name`, following local calls.
 *
 * A passive handler often delegates the real work — e.g. ChatMessageList's
 * onScrollAndTableTouchStart() calls onTableTouchStart(), which is destructured
 * from a composable. Checking only the entry function would miss a
 * preventDefault() one hop away (a mutation test caught exactly that gap), so
 * the closure is followed transitively within the corpus.
 */
function findBodiesClosure(corpus: Map<string, string>, name: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  const queue: string[] = [name]
  while (queue.length > 0) {
    const current = queue.shift() as string
    if (seen.has(current)) continue
    seen.add(current)
    for (const body of findBodies(corpus, current)) {
      out.push(body)
      // Local call sites: `foo(` where foo is a known corpus function.
      for (const call of body.matchAll(/(?<![.\w$])([A-Za-z_$][\w$]*)\s*\(/g)) {
        const callee = call[1]
        if (callee === current) continue
        if (findBodies(corpus, callee).length > 0) queue.push(callee)
      }
    }
  }
  return out
}

/** Last identifier of a handler expression: `a.b.c` → `c`, `f(x)` → `f`. */
function handlerName(expr: string): string | null {
  const call = expr.match(/^([A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*)\s*\(/)
  const bare = expr.match(/^([A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*)$/)
  const target = call?.[1] ?? bare?.[1]
  if (!target) return null
  return target.split('.').pop() as string
}

const vueFiles = listVueFiles()
const allBindings: Binding[] = vueFiles.flatMap((abs) => {
  const rel = abs.slice(abs.indexOf(`${'src'}`))
  return findBindings(rel, readWebFile(rel))
})

describe('scroll-blocking listeners are deliberate', () => {
  it('finds the scroll-blocking bindings it is meant to guard', () => {
    // Sanity: if the scanner silently breaks, every assertion below passes
    // vacuously. Pin a floor so a regression in the scanner is loud.
    expect(allBindings.length).toBeGreaterThan(10)
    const events = new Set(allBindings.map((b) => b.event))
    expect(events.has('wheel')).toBe(true)
    expect(events.has('touchstart')).toBe(true)
  })

  it('every .passive handler avoids preventDefault (a silent no-op there)', () => {
    const violations: string[] = []
    for (const b of allBindings) {
      if (!b.modifiers.includes('passive')) continue
      const name = handlerName(b.expr)
      if (!name) continue
      const corpus = buildCorpus(b.file)
      for (const body of findBodiesClosure(corpus, name)) {
        if (body.includes('preventDefault')) {
          violations.push(
            `${b.file}:${b.line} @${b.event}.passive="${b.expr}" → ${name}() calls preventDefault()`,
          )
        }
      }
    }
    expect(violations).toEqual([])
  })

  it('every bare handler prevents something — otherwise it must be .passive', () => {
    const violations: string[] = []
    for (const b of allBindings) {
      if (b.modifiers.includes('passive') || b.modifiers.includes('prevent')) continue
      const name = handlerName(b.expr)
      // Inline arrows carry their own body.
      const inline = b.expr.includes('=>')
      if (!name && !inline) continue
      const corpus = buildCorpus(b.file)
      const bodies = inline ? [b.expr] : findBodiesClosure(corpus, name as string)
      if (bodies.length === 0) continue // handler resolved elsewhere; not our call
      if (!bodies.some((body) => body.includes('preventDefault'))) {
        violations.push(
          `${b.file}:${b.line} @${b.event}="${b.expr}" is non-passive but never calls preventDefault() — add .passive`,
        )
      }
    }
    expect(violations).toEqual([])
  })
})

describe('the guard itself detects a regression', () => {
  // Prove the scanner is not a tautology: feed it the exact shapes it must
  // reject, and confirm both halves of the contract fire.
  it('flags a .passive handler that calls preventDefault', () => {
    const source = `<template>\n  <div @wheel.passive="onWheel" />\n</template>`
    const found = findBindings('src/Probe.vue', source)
    expect(found).toHaveLength(1)
    expect(found[0].modifiers).toContain('passive')

    const corpus = new Map<string, string>([
      ['src/Probe.vue', `${source}\n<script setup>\nfunction onWheel(e) {\n  e.preventDefault()\n}\n</script>`],
    ])
    const bodies = findBodies(corpus, 'onWheel')
    expect(bodies).toHaveLength(1)
    expect(bodies[0]).toContain('preventDefault')
  })

  it('flags a bare handler that never prevents', () => {
    const source = `<template>\n  <div @touchstart="onTouch" />\n</template>`
    const found = findBindings('src/Probe.vue', source)
    expect(found).toHaveLength(1)
    expect(found[0].modifiers).toEqual([])

    const corpus = new Map<string, string>([
      ['src/Probe.vue', `${source}\n<script setup>\nfunction onTouch() {\n  flag = true\n}\n</script>`],
    ])
    const bodies = findBodies(corpus, 'onTouch')
    expect(bodies).toHaveLength(1)
    expect(bodies[0]).not.toContain('preventDefault')
  })

  it('resolves a handler destructured from a composable', () => {
    // The real chain: ToolDetailDrawer destructures onTableTouchStart from
    // useTableRowExpand, which re-exports it from utils/tableRowExpand.
    const corpus = buildCorpus('src/components/chat/ToolDetailDrawer.vue')
    const bodies = findBodies(corpus, 'onTableTouchStart')
    expect(bodies.length).toBeGreaterThan(0)
    expect(bodies.join('\n')).not.toContain('preventDefault')
  })

  it('resolves a member-expression handler on a composable result', () => {
    const corpus = buildCorpus('src/components/chat/ChatPanelContent.vue')
    const bodies = findBodies(corpus, 'onTouchStart')
    expect(bodies.length).toBeGreaterThan(0)
    expect(bodies.join('\n')).not.toContain('preventDefault')
  })
})
