#!/usr/bin/env node
/**
 * Guard: a `.vue` file whose `<script setup>` block is plain JS must not contain
 * TypeScript-only syntax.
 *
 * Why this cannot be caught by the existing tooling:
 *
 *   - `vue-tsc` does not type-check `<script setup>` blocks WITHOUT
 *     `lang="ts"`. Verified directly: a deliberately broken block
 *     (`x < boolean > ('a')`) produces no error at all, because the block is
 *     never fed to the type checker. `npm run typecheck` passing therefore says
 *     nothing about the ~79 plain-JS component files here.
 *   - It is not a SYNTAX error either, which is what makes it so quiet.
 *     `readPanelState<boolean>('x')` is a legal JS expression: `a < b > c` is
 *     two comparisons. The bundler compiles it happily and the failure only
 *     appears at RUNTIME, as `ReferenceError: boolean is not defined` from the
 *     component's setup(), which unmounts that whole subtree.
 *
 *   Real incident (2026-10-23): FileManagerContent.vue (plain JS) received
 *   `readPanelState<boolean>('fmPreviewOpen')`. `boolean` became a free
 *   variable, setup() threw, and the file manager stopped rendering entirely.
 *   The build was green, typecheck was green, and the unit tests were green —
 *   every gate missed it because none of them parses that block as TypeScript.
 *
 * The judgement used here is exact rather than heuristic: each block is parsed
 * twice, once as TS and once as JS, and any construct that carries
 * `typeArguments`/type annotations under the TS parse but not under the JS parse
 * is TS-only syntax that cannot survive in a JS block. Regexes were tried first
 * and rejected — they produced false positives on the literal text
 * `<script setup>` inside comments, and would equally trip on generics inside
 * strings or comments.
 *
 * Usage:
 *   node scripts/check-js-script-blocks.mjs          # scan web/src, exit 1 on hit
 *   node scripts/check-js-script-blocks.mjs --list   # print every hit
 */
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'
import * as acorn from 'acorn'
import { parse as parseSfc } from '@vue/compiler-sfc'

const ROOT = fileURLToPath(new URL('..', import.meta.url))
const SRC = join(ROOT, 'web/src')

/** Every `.vue` file under `dir`, recursively. */
function listVueFiles(dir) {
  const out = []
  for (const name of readdirSync(dir)) {
    const full = join(dir, name)
    if (statSync(full).isDirectory()) out.push(...listVueFiles(full))
    else if (name.endsWith('.vue')) out.push(full)
  }
  return out
}

/**
 * Parse `code` and report every construct that only exists when the source is
 * read as TypeScript. Returns a list of human-readable descriptions with line
 * numbers, relative to `code`.
 */
function findTsOnlySyntax(code) {
  // Always parse as TS: JS mode would reject (or silently swallow) the very
  // syntax we are looking for, so there would be nothing to report.
  const sf = ts.createSourceFile('block.ts', code, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const found = []

  const lineOf = (node) => {
    const pos = sf.getLineAndCharacterOfPosition(node.getStart(sf))
    return pos.line + 1
  }

  function visit(node) {
    // A generic call/instantiation: `f<T>(…)` / `new C<T>()`.
    if ((ts.isCallExpression(node) || ts.isNewExpression(node)) && node.typeArguments?.length) {
      found.push({ line: lineOf(node), what: 'generic call (type arguments)' })
    }
    // An explicit type argument list on any other expression.
    if (ts.isExpressionWithTypeArguments(node) && node.typeArguments?.length) {
      found.push({ line: lineOf(node), what: 'type arguments' })
    }
    // A type annotation on a declaration or parameter: `const x: T`, `(a: T)`.
    if (node.type && (ts.isVariableDeclaration(node) || ts.isParameter(node) || ts.isPropertyDeclaration(node))) {
      found.push({ line: lineOf(node), what: 'type annotation' })
    }
    // An `as` / `satisfies` cast.
    if (ts.isAsExpression(node) || ts.isSatisfiesExpression(node)) {
      found.push({ line: lineOf(node), what: `'as' cast` })
    }
    // A non-null assertion: `foo!.bar`.
    if (ts.isNonNullExpression(node)) {
      found.push({ line: lineOf(node), what: 'non-null assertion' })
    }
    // A type alias / interface declaration — a statement, not an expression.
    if (ts.isInterfaceDeclaration(node) || ts.isTypeAliasDeclaration(node)) {
      found.push({ line: lineOf(node), what: 'type declaration' })
    }
    // An enum — runtime TS, but still invalid in a JS block.
    if (ts.isEnumDeclaration(node)) {
      found.push({ line: lineOf(node), what: 'enum declaration' })
    }
    // `declare` / `abstract` modifiers.
    if (ts.isClassDeclaration(node) && node.modifiers?.some(
      (m) => m.kind === ts.SyntaxKind.DeclareKeyword || m.kind === ts.SyntaxKind.AbstractKeyword,
    )) {
      found.push({ line: lineOf(node), what: 'declare/abstract modifier' })
    }
    // Type-parameter lists on function/arrow/class declarations: `function f<T>()`.
    if (node.typeParameters?.length && (
      ts.isFunctionDeclaration(node) || ts.isFunctionExpression(node) ||
      ts.isArrowFunction(node) || ts.isClassDeclaration(node) || ts.isMethodDeclaration(node)
    )) {
      found.push({ line: lineOf(node), what: 'type parameter list' })
    }
    ts.forEachChild(node, visit)
  }

  visit(sf)
  return found
}

/**
 * Whether `code` is valid plain JavaScript.
 *
 * This is the second half of the judgement and it is load-bearing: TypeScript's
 * own parser is SILENT in JS mode. It accepts `const x: string = 1`,
 * `e as Event`, `a!.b`, `interface Foo {}` and `enum E {}` without a single
 * diagnostic (verified), so "the TS parser found a type node" alone would flag
 * nothing reliably — and filtering by "TS found it, JS mode didn't" is
 * impossible for the same reason. A real JS parser is the only thing that can
 * say "this text is not JavaScript".
 */
function isPlainJs(code) {
  try {
    acorn.parse(code, { ecmaVersion: 'latest', sourceType: 'module' })
    return true
  } catch {
    return false
  }
}

/** Scan one `.vue` file. Returns `[]` when it is fine. */
function checkFile(file) {
  const source = readFileSync(file, 'utf8')
  const { descriptor, errors } = parseSfc(source, { filename: file })
  if (errors.length > 0) {
    // A file that does not parse is a different problem; report it rather than
    // silently passing (a broken SFC would otherwise look "clean" here).
    return [{ line: 0, what: `SFC parse error: ${errors[0].message}` }]
  }
  const setup = descriptor.scriptSetup
  if (!setup) return []
  // `lang` is 'ts'/'tsx' for typed blocks; absent (or 'js') means plain JS.
  if (setup.lang === 'ts' || setup.lang === 'tsx') return []

  const hits = findTsOnlySyntax(setup.content)
  if (hits.length === 0) return []

  // Two independent reasons a TS-looking construct is a real defect here:
  //
  //   1. The text is not valid JavaScript at all (annotations, `as`, `!`,
  //      interface/enum/type-parameter declarations). The bundler would fail.
  //   2. The text IS valid JavaScript but with a DIFFERENT meaning. This is the
  //      dangerous half and the reason this guard exists: `f<T>(x)` parses as
  //      `(f < T) > x`, so it compiles cleanly and throws only at runtime when
  //      `T` turns out to be an undefined variable. A generic call is exactly
  //      this case, so it is reported even though acorn accepts it.
  //
  // A block that is valid JS AND contains no generic call is left alone — that
  // is ordinary code which merely has a `<` and `>` in it.
  const parsedAsJs = isPlainJs(setup.content)
  const genericCalls = hits.filter((h) => h.what === 'generic call (type arguments)')
  const relevant = parsedAsJs
    ? genericCalls
    : hits.filter((h) => h.what !== 'generic call (type arguments)' || genericCalls.includes(h))

  // Translate block-relative lines back to file lines so the report points at
  // the real source line, not an offset into the extracted block.
  const offset = setup.loc.start.line - 1
  return relevant.map((h) => ({ line: h.line + offset, what: h.what }))
}

function main() {
  const listOnly = process.argv.includes('--list')
  const files = listVueFiles(SRC)
  const failures = []

  for (const file of files) {
    for (const hit of checkFile(file)) {
      failures.push({ file: relative(ROOT, file), ...hit })
    }
  }

  if (failures.length === 0) {
    if (listOnly) console.log(`OK: ${files.length} .vue files, no TS syntax in plain-JS script blocks.`)
    return 0
  }

  console.error('TypeScript syntax found in a <script setup> block without lang="ts":')
  console.error('')
  for (const f of failures) {
    console.error(`  ${f.file}:${f.line}  ${f.what}`)
  }
  console.error('')
  console.error('These blocks are parsed as plain JavaScript, so this syntax does not')
  console.error('become a type annotation — it becomes a runtime expression that throws')
  console.error('(e.g. `f<T>(x)` is two comparisons and `T` is an undefined variable),')
  console.error('taking the whole component down at setup(). Fix by either removing the')
  console.error('TS syntax or adding lang="ts" to the block.')
  return 1
}

process.exit(main())
