#!/usr/bin/env node
/**
 * Guard: a `.vue` file whose `<script>` / `<script setup>` block is plain JS must
 * not contain TypeScript-only syntax.
 *
 * Why this cannot be caught by the existing tooling:
 *
 *   - `vue-tsc` does not type-check blocks WITHOUT `lang="ts"`. Verified
 *     directly: a deliberately broken block (`x < boolean > ('a')`) produces no
 *     error at all, because the block is never fed to the type checker.
 *     `npm run typecheck` passing therefore says nothing about the ~76 plain-JS
 *     blocks here.
 *   - Most of it is not a SYNTAX error either, which is what makes it so quiet.
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
 * The judgement is split in two, because the two halves need different tests:
 *
 *   1. The block is NOT valid JavaScript. Nothing here can ship, whatever the
 *      construct is — so this is reported even when the visitor below does not
 *      recognise the node (`namespace`, `import type`, decorators, ...). The
 *      acorn error supplies the position.
 *   2. The block IS valid JavaScript but means something else. This is the
 *      dangerous half and the reason this guard exists: `f<T>(x)` parses as
 *      `(f < T) > x`, so it compiles cleanly and throws only at runtime when `T`
 *      turns out to be an undefined variable.
 *
 *      Telling a real generic call apart from an ordinary comparison chain
 *      cannot be done from the shape alone — `ref<string>('x')` and
 *      `min < val > (limit)` are the SAME two-comparison AST in JS. The
 *      difference is whether the would-be type argument is a declared binding:
 *      `val` is, `string`/`T` are not. That is also exactly the failure mode
 *      (a free variable), so the check is "would this reference throw?", not
 *      "does this look like a type".
 *
 * Regexes were tried first and rejected — they produced false positives on the
 * literal text `<script setup>` inside comments, and would equally trip on
 * generics inside strings or comments.
 *
 * Usage:
 *   node scripts/check-js-script-blocks.mjs          # scan web/src, exit 1 on hit
 *   node scripts/check-js-script-blocks.mjs --list   # print a summary when clean
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
 * Parse `code` as JavaScript. Returns the acorn error when it is not valid.
 *
 * This is load-bearing: TypeScript's own parser is SILENT in JS mode. It
 * accepts `const x: string = 1`, `e as Event`, `a!.b`, `interface Foo {}` and
 * `enum E {}` without a single diagnostic (verified), so "the TS parser found a
 * type node" alone would flag nothing reliably. A real JS parser is the only
 * thing that can say "this text is not JavaScript".
 */
function parseAsJs(code) {
  try {
    acorn.parse(code, { ecmaVersion: 'latest', sourceType: 'module' })
    return { ok: true }
  } catch (err) {
    return { ok: false, err }
  }
}

/**
 * Every name declared in the block: variables, parameters, function/class/enum
 * declarations, imports, and catch bindings. Used to tell a generic call's type
 * argument (a free variable → throws) from an ordinary comparison operand (a
 * declared binding → harmless).
 */
function collectDeclaredNames(sf) {
  const names = new Set()
  const addBinding = (name) => {
    if (!name) return
    if (ts.isIdentifier(name)) {
      names.add(name.text)
    } else if (ts.isObjectBindingPattern(name) || ts.isArrayBindingPattern(name)) {
      for (const el of name.elements) {
        if (ts.isBindingElement(el)) addBinding(el.name)
      }
    }
  }
  function visit(node) {
    if (ts.isVariableDeclaration(node)) addBinding(node.name)
    else if (ts.isParameter(node)) addBinding(node.name)
    else if (
      ts.isFunctionDeclaration(node) ||
      ts.isClassDeclaration(node) ||
      ts.isEnumDeclaration(node)
    ) {
      addBinding(node.name)
    } else if (ts.isImportClause(node)) {
      addBinding(node.name) // default import
    } else if (ts.isImportSpecifier(node) || ts.isNamespaceImport(node)) {
      addBinding(node.name)
    } else if (ts.isCatchClause(node) && node.variableDeclaration) {
      addBinding(node.variableDeclaration.name)
    }
    ts.forEachChild(node, visit)
  }
  visit(sf)
  return names
}

/**
 * Every name a type argument list mentions, plus a flag for TS's primitive
 * keywords (`string`, `number`, `boolean`, ...).
 *
 * The keyword flag matters: `ref<string>('x')` is the exact incident shape, and
 * `string` is a `StringKeyword` node, NOT an Identifier — so a name-only walk
 * returns an empty list and the caller would conclude "nothing undeclared here,
 * harmless". A primitive keyword can never be a variable, so it always counts
 * as undeclared.
 */
function typeArgumentNames(typeArgs) {
  const out = []
  let sawPrimitiveKeyword = false
  const PRIMITIVE_KEYWORDS = new Set([
    ts.SyntaxKind.StringKeyword,
    ts.SyntaxKind.NumberKeyword,
    ts.SyntaxKind.BooleanKeyword,
    ts.SyntaxKind.AnyKeyword,
    ts.SyntaxKind.UnknownKeyword,
    ts.SyntaxKind.NeverKeyword,
    ts.SyntaxKind.VoidKeyword,
    ts.SyntaxKind.ObjectKeyword,
    ts.SyntaxKind.SymbolKeyword,
    ts.SyntaxKind.BigIntKeyword,
    ts.SyntaxKind.UndefinedKeyword,
    ts.SyntaxKind.NullKeyword,
  ])
  const visitType = (t) => {
    if (!t) return
    if (PRIMITIVE_KEYWORDS.has(t.kind)) {
      sawPrimitiveKeyword = true
      return
    }
    if (ts.isIdentifier(t)) {
      out.push(t.text)
      return
    }
    if (ts.isTypeReferenceNode(t)) {
      visitType(t.typeName)
      if (t.typeArguments) t.typeArguments.forEach(visitType)
      return
    }
    if (ts.isQualifiedName(t)) {
      visitType(t.left)
      return
    }
    if (ts.isUnionTypeNode(t) || ts.isIntersectionTypeNode(t)) {
      t.types.forEach(visitType)
      return
    }
    if (ts.isParenthesizedTypeNode(t) || ts.isArrayTypeNode(t)) {
      visitType(t.type ?? t.elementType)
      return
    }
    if (ts.isLiteralTypeNode(t)) return
    // Anything else (object/function/tuple types): fall back to every
    // identifier inside, so an unknown shape is still checked rather than
    // silently skipped.
    const walk = (n) => {
      if (ts.isIdentifier(n)) {
        out.push(n.text)
        return
      }
      ts.forEachChild(n, walk)
    }
    ts.forEachChild(t, walk)
  }
  typeArgs.forEach(visitType)
  return { names: out, sawPrimitiveKeyword }
}

/**
 * Every construct that only exists when the source is read as TypeScript.
 * Returns descriptions with block-relative line numbers.
 */
function findTsOnlySyntax(sf) {
  const found = []
  const lineOf = (node) => sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1

  function visit(node) {
    // A generic call/instantiation: `f<T>(…)` / `new C<T>()` / `tag<T>\`…\``.
    if (
      (ts.isCallExpression(node) || ts.isNewExpression(node) || ts.isTaggedTemplateExpression(node)) &&
      node.typeArguments?.length
    ) {
      found.push({ line: lineOf(node), what: 'generic call (type arguments)', node })
    }
    // An explicit type argument list on any other expression.
    if (ts.isExpressionWithTypeArguments(node) && node.typeArguments?.length) {
      found.push({ line: lineOf(node), what: 'type arguments', node })
    }
    // A type annotation on a declaration or parameter: `const x: T`, `(a: T)`.
    if (node.type && (ts.isVariableDeclaration(node) || ts.isParameter(node) || ts.isPropertyDeclaration(node))) {
      found.push({ line: lineOf(node), what: 'type annotation', node })
    }
    // An `as` / `satisfies` cast.
    if (ts.isAsExpression(node) || ts.isSatisfiesExpression(node)) {
      found.push({ line: lineOf(node), what: `'as' cast`, node })
    }
    // A non-null assertion: `foo!.bar`.
    if (ts.isNonNullExpression(node)) {
      found.push({ line: lineOf(node), what: 'non-null assertion', node })
    }
    // A type alias / interface declaration — a statement, not an expression.
    if (ts.isInterfaceDeclaration(node) || ts.isTypeAliasDeclaration(node)) {
      found.push({ line: lineOf(node), what: 'type declaration', node })
    }
    // An enum — runtime TS, but still invalid in a JS block.
    if (ts.isEnumDeclaration(node)) {
      found.push({ line: lineOf(node), what: 'enum declaration', node })
    }
    // `<string>foo` — the angle-bracket cast, which only exists in .ts.
    if (ts.isTypeAssertionExpression(node)) {
      found.push({ line: lineOf(node), what: 'angle-bracket cast', node })
    }
    // `namespace` blocks and `import type` are TS-only statements.
    if (ts.isModuleDeclaration(node)) {
      found.push({ line: lineOf(node), what: 'namespace declaration', node })
    }
    if (ts.isImportDeclaration(node) && node.importClause?.isTypeOnly) {
      found.push({ line: lineOf(node), what: 'type-only import', node })
    }
    // `declare` / `abstract` modifiers.
    if (ts.isClassDeclaration(node) && node.modifiers?.some(
      (m) => m.kind === ts.SyntaxKind.DeclareKeyword || m.kind === ts.SyntaxKind.AbstractKeyword,
    )) {
      found.push({ line: lineOf(node), what: 'declare/abstract modifier', node })
    }
    // Type-parameter lists on function/arrow/class declarations: `function f<T>()`.
    if (node.typeParameters?.length && (
      ts.isFunctionDeclaration(node) || ts.isFunctionExpression(node) ||
      ts.isArrowFunction(node) || ts.isClassDeclaration(node) || ts.isMethodDeclaration(node)
    )) {
      found.push({ line: lineOf(node), what: 'type parameter list', node })
    }
    ts.forEachChild(node, visit)
  }

  visit(sf)
  return found
}

/**
 * Check one plain-JS block. Returns `[]` when it is fine.
 *
 * `lineOffset` is added to the block-relative lines so the report points at the
 * real source line rather than an offset into the extracted block.
 */
function checkBlock(content, lineOffset) {
  const parsedAsJs = parseAsJs(content)
  const sf = ts.createSourceFile('block.ts', content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const hits = findTsOnlySyntax(sf)

  if (!parsedAsJs.ok) {
    // Not JavaScript at all. Report what we recognised, or — when the visitor
    // does not know the construct — the parser's own complaint. Either way this
    // block cannot ship, and skipping it because no node matched would let
    // exactly the class of bug this guard exists for through.
    const relevant = hits.map((h) => ({ line: h.line + lineOffset, what: h.what }))
    if (relevant.length > 0) return relevant
    const errLine = parsedAsJs.err?.loc?.line ?? 1
    const reason = parsedAsJs.err?.message ?? 'syntax error'
    return [{ line: errLine + lineOffset, what: `not valid JavaScript (${reason})` }]
  }

  // Valid JavaScript: only a generic call is dangerous, and only when its
  // "type argument" is not a declared binding — that is the reference that
  // throws at runtime. `min < val > (limit)` with `val` in scope is ordinary
  // arithmetic and must not be reported.
  const declared = collectDeclaredNames(sf)
  return hits
    .filter((h) => h.what === 'generic call (type arguments)')
    .filter((h) => {
      const { names, sawPrimitiveKeyword } = typeArgumentNames(h.node.typeArguments)
      if (sawPrimitiveKeyword) return true
      return names.some((name) => !declared.has(name))
    })
    .map((h) => ({ line: h.line + lineOffset, what: h.what }))
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

  const blocks = [descriptor.script, descriptor.scriptSetup].filter(Boolean)
  const out = []
  for (const block of blocks) {
    // `lang` is 'ts'/'tsx' for typed blocks; absent (or 'js') means plain JS.
    if (block.lang === 'ts' || block.lang === 'tsx') continue
    // A plain `<script>` block is parsed as JS just like `<script setup>` —
    // module-scope code throws in exactly the same way (`PopupMenu.vue` keeps
    // its shared counter there), so checking only the setup block left a hole.
    out.push(...checkBlock(block.content, block.loc.start.line - 1))
  }
  return out
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

  console.error('TypeScript syntax found in a <script> block without lang="ts":')
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
