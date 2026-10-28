import { describe, expect, it } from 'vitest'
import { execFileSync } from 'node:child_process'
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, readFileSync, symlinkSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { parse as parseSfc } from '@vue/compiler-sfc'

/**
 * Guard: no TypeScript-only syntax in a `<script setup>` block that is plain JS.
 *
 * Why this needs its own guard rather than an existing one:
 *   - `vue-tsc` never type-checks these blocks (verified: a deliberately broken
 *     block produces zero errors), so `npm run typecheck` cannot see them.
 *   - It is not a syntax error either. `f<T>(x)` is a legal JS expression
 *     (`f < T > x`), so the bundler compiles it and the failure surfaces only at
 *     RUNTIME as `ReferenceError: T is not defined` thrown from setup(), which
 *     unmounts that component's entire subtree.
 *
 * Real incident (2026-10-23): FileManagerContent.vue got
 * `readPanelState<boolean>('fmPreviewOpen')`; `boolean` became a free variable,
 * setup() threw, and the file manager stopped rendering. Build, typecheck and
 * the whole unit suite were green.
 *
 * The script's own logic is exercised by importing nothing — it is a CLI — so
 * these tests drive it as a subprocess against fixture trees. That also proves
 * the exit code, which is the part a caller (build.sh / CI) actually depends on.
 */

const REPO_ROOT = resolve(__dirname, '../../..')
const SCRIPT = join(REPO_ROOT, 'scripts/check-js-script-blocks.mjs')

/** Run the checker against a throwaway tree containing `files`. */
function runChecker(files: Record<string, string>): { code: number; stdout: string; stderr: string } {
  const dir = mkdtempSync(join(tmpdir(), 'jsblock-'))
  try {
    // The script resolves `web/src` relative to its OWN location, so the
    // fixture tree has to be laid out as a miniature repo next to a copy of it.
    mkdirSync(join(dir, 'scripts'), { recursive: true })
    mkdirSync(join(dir, 'web/src'), { recursive: true })
    writeFileSync(join(dir, 'scripts/check-js-script-blocks.mjs'), readFileSync(SCRIPT, 'utf8'))
    for (const [rel, content] of Object.entries(files)) {
      const full = join(dir, 'web/src', rel)
      mkdirSync(join(full, '..'), { recursive: true })
      writeFileSync(full, content)
    }
    // The script imports `typescript` and `@vue/compiler-sfc`. ESM ignores
    // NODE_PATH, so link the real node_modules in rather than copying it.
    symlinkSync(join(REPO_ROOT, 'node_modules'), join(dir, 'node_modules'), 'dir')
    return runNode(join(dir, 'scripts/check-js-script-blocks.mjs'), dir)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
}

function runNode(script: string, cwd: string) {
  try {
    const stdout = execFileSync(process.execPath, [script], {
      cwd,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
      env: process.env,
    })
    return { code: 0, stdout, stderr: '' }
  } catch (e) {
    const err = e as { status?: number; stdout?: string; stderr?: string }
    return { code: err.status ?? -1, stdout: err.stdout ?? '', stderr: err.stderr ?? '' }
  }
}

describe('check-js-script-blocks', () => {
  it('accepts a plain-JS block with no TS syntax', () => {
    const r = runChecker({
      'A.vue': '<template><div/></template>\n<script setup>\nconst x = ref(1)\n</script>\n',
    })
    expect(r.code).toBe(0)
  })

  it('accepts a lang="ts" block containing generics', () => {
    // The whole point: the same syntax is fine once the block declares TS.
    const r = runChecker({
      'A.vue': '<template><div/></template>\n<script setup lang="ts">\nconst x = ref<boolean>(true)\n</script>\n',
    })
    expect(r.code).toBe(0)
  })

  it('rejects a generic call in a plain-JS block', () => {
    // The exact shape of the real incident.
    const r = runChecker({
      'A.vue': "<template><div/></template>\n<script setup>\nconst x = ref(readPanelState<boolean>('k'))\n</script>\n",
    })
    expect(r.code).toBe(1)
    expect(r.stderr).toContain('generic call')
    expect(r.stderr).toContain('A.vue')
  })

  it('rejects type annotations, casts, interfaces and enums in a plain-JS block', () => {
    const cases: Array<[string, string]> = [
      ['annotation', 'const x: string = "a"'],
      ['cast', 'const y = e as Event'],
      ['nonnull', 'const z = a!.b'],
      ['interface', 'interface Foo { a: string }'],
      ['enum', 'enum E { A }'],
      ['type params', 'function f<T>(a) { return a }'],
    ]
    // One fixture tree, one subprocess. Running the checker per case made this
    // test's wall clock the sum of six node startups (each loading TypeScript);
    // under full-suite load that crossed the 5s test timeout and reported a
    // timeout as if the guard were broken. Per-case files also give a stronger
    // assertion than a bare exit code: every construct must be named.
    const files: Record<string, string> = {}
    for (const [label, body] of cases) {
      files[`${label}.vue`] = `<template><div/></template>\n<script setup>\n${body}\n</script>\n`
    }
    const r = runChecker(files)
    expect(r.code, 'TS syntax in a plain-JS block must fail the check').toBe(1)
    for (const [label] of cases) {
      expect(r.stderr, `${label} should be rejected`).toContain(`${label}.vue`)
    }
  })

  it('rejects TS-only constructs the node visitor does not enumerate', () => {
    // These are all invalid JavaScript but none of them is one of the eight
    // node kinds the visitor recognises. Reporting only "the visitor matched a
    // type node" therefore let every one of these through, which is the same
    // class of bug as the original incident (compiles/passes, fails at runtime
    // or in the bundler). The judgement must also accept "acorn says this is
    // not JavaScript".
    const cases: Array<[string, string]> = [
      ['namespace', 'namespace NS { export const a = 1 }'],
      ['type-only-import', "import type { Foo } from './foo'"],
      ['decorator', '@decorator class Foo {}'],
      ['tagged-template-generic', 'const a = tag<boolean>`x`'],
      ['angle-bracket-cast', 'const x = <string>foo'],
    ]
    // Same single-subprocess shape as the previous case (see the note there).
    const files: Record<string, string> = {}
    for (const [label, body] of cases) {
      files[`${label}.vue`] = `<template><div/></template>\n<script setup>\n${body}\n</script>\n`
    }
    const r = runChecker(files)
    expect(r.code).toBe(1)
    for (const [label] of cases) {
      expect(r.stderr, `${label} should be rejected`).toContain(`${label}.vue`)
    }
  })

  it('checks a plain <script> block, not just <script setup>', () => {
    // PopupMenu.vue keeps a module-scope counter in a plain <script> block next
    // to its <script setup>. Module-scope code throws identically when it
    // references a free variable, so scanning only the setup block left that
    // whole block unchecked.
    const r = runChecker({
      'A.vue': [
        '<template><div/></template>',
        '<script>',
        'const bad = nextMenuSeq<number>()',
        '</script>',
        '<script setup>',
        'const ok = 1',
        '</script>',
      ].join('\n'),
    })
    expect(r.code).toBe(1)
    expect(r.stderr).toContain('generic call')
    expect(r.stderr).toContain('A.vue:3')
  })

  it('does not fire on ordinary comparisons or generic-looking text', () => {
    // The false-positive set that a regex-based check got wrong: a comparison
    // chain, a generic inside a string, and the literal text `<script setup>`
    // inside a comment (which fooled the regex version into scanning the
    // template of a lang="ts" file).
    const r = runChecker({
      'A.vue': [
        '<template><div/></template>',
        '<script setup>',
        '// a <script setup> block in prose must not confuse the scanner',
        'const a = 1, b = 2, c = 3',
        'const cmp = a < b > c',
        'const s = "f<T>(x)"',
        '</script>',
      ].join('\n'),
    })
    expect(r.code).toBe(0)
  })

  it('does not fire on a comparison chain whose operands are parenthesised', () => {
    // `a < b > c` and `min < val > (limit)` are the SAME JS parse (two
    // comparisons), but only the second one is ALSO a valid TS generic call —
    // TS reads `val` as a type argument. A check that reports on shape alone
    // flags it, breaking ordinary code. The distinction that works is whether
    // the would-be type argument is a declared binding: `val` is, `string` is
    // not (and a free reference is exactly what throws at runtime).
    const r = runChecker({
      'A.vue': [
        '<template><div/></template>',
        '<script setup>',
        'const val = 3, limit = 9, min = 1',
        'const ok = min < val > (limit)',
        'if (val < limit > (min)) { console.log(ok) }',
        'const arr = []',
        'const n = arr.length < val > (0)',
        '</script>',
      ].join('\n'),
    })
    expect(r.code).toBe(0)
  })

  it('reports the line number in the FILE, not in the extracted block', () => {
    const r = runChecker({
      'A.vue': [
        '<template>',
        '  <div/>',
        '</template>',
        '<script setup>',
        'const ok = 1',
        "const bad = f<boolean>('x')",
        '</script>',
      ].join('\n'),
    })
    expect(r.code).toBe(1)
    // Line 6 of the file (1-based), not line 2 of the script block.
    expect(r.stderr).toMatch(/A\.vue:6\b/)
  })

  it('is clean on the real repository', () => {
    // Regression net for the actual tree: this must stay green so a future
    // occurrence is caught by the same command CI runs.
    //
    // This walks every .vue file in the repo through compiler-sfc, which is
    // ~5s on its own — right on vitest's 5s default. Under the full suite the
    // machine is saturated by the other workers and it crossed the limit
    // (measured: 5.4s isolated, timeout under load), so the budget is stated
    // explicitly rather than inherited.
    const stdout = execFileSync(process.execPath, [SCRIPT, '--list'], {
      cwd: REPO_ROOT,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    expect(stdout).toContain('OK')
  }, 30_000)
})

describe('every plain-JS .vue file really has no lang attribute', () => {
  it('the scanner\'s JS/TS split matches what compiler-sfc reports', () => {
    // Cross-check the assumption the scanner is built on: a block with no `lang`
    // is plain JS, and one with lang="ts" is typed. If a future compiler-sfc
    // version changed the default, the scanner would silently stop covering
    // anything — this pins it.
    const jsBlock = '<template><div/></template>\n<script setup>\nconst a = 1\n</script>'
    const tsBlock = '<template><div/></template>\n<script setup lang="ts">\nconst a = 1\n</script>'
    expect(parseSfc(jsBlock).descriptor.scriptSetup?.lang).toBeUndefined()
    expect(parseSfc(tsBlock).descriptor.scriptSetup?.lang).toBe('ts')
  })
})
