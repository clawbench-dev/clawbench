import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { SOURCE_KIND_PRIORITY } from '@/composables/useSkillsState'

/**
 * Go ↔ TS parity guard for skill-source priority.
 *
 * The `#` menu folds same-named skills by source priority so the row it shows is
 * the one the backend will resolve. That ordering lives in two places:
 *   - Go: `internal/skill/types.go` — the numeric SourceKind values ARE the
 *     priority, and `SourceKind.String()` maps them to the API's
 *     `source_kind` strings.
 *   - TS: `SOURCE_KIND_PRIORITY` in useSkillsState.ts.
 *
 * A comment saying "must match" cannot catch a reorder, so this test reads the
 * Go source and derives the expected order from the real constants. It is
 * deliberately source-parsing (not an API call): the two files are in the same
 * repo, and the Go values are compile-time constants with no runtime endpoint
 * that exposes their ordering.
 */
function findGoTypes(): string {
  const rel = 'internal/skill/types.go'
  for (const root of [process.cwd(), resolve(process.cwd(), '..')]) {
    const candidate = resolve(root, rel)
    if (existsSync(candidate)) return candidate
  }
  throw new Error(`Go source not found relative to ${process.cwd()}`)
}

/**
 * Parse the `SourceKind` constants and the String() mapping out of types.go,
 * returning the API strings ordered by their numeric value (ascending = highest
 * priority first).
 */
function goSourcePriority(): string[] {
  const src = readFileSync(findGoTypes(), 'utf-8')

  // 1. Numeric values: `SourceOwnNative SourceKind = 0`, …
  const values = new Map<string, number>()
  const constRe = /^\s*(Source\w+)\s+SourceKind\s*=\s*(\d+)/gm
  for (const m of src.matchAll(constRe)) {
    values.set(m[1], Number(m[2]))
  }
  expect(values.size, 'no SourceKind constants found — did types.go change shape?').toBeGreaterThan(0)

  // 2. The String() switch maps each constant to the API string.
  //    `case SourceOwnNative: return "own"` — tolerate the return on the next line.
  const strings = new Map<string, string>()
  const caseRe = /case\s+(Source\w+):\s*return\s+"([^"]+)"/g
  for (const m of src.matchAll(caseRe)) {
    strings.set(m[1], m[2])
  }

  return [...values.entries()]
    .sort((a, b) => a[1] - b[1])
    .map(([name, value]) => {
      const s = strings.get(name)
      expect(s, `${name} (value ${value}) has no String() case in types.go`).toBeTruthy()
      return s as string
    })
}

describe('skill source priority parity with internal/skill/types.go', () => {
  it('matches the Go SourceKind numeric ordering', () => {
    expect(SOURCE_KIND_PRIORITY).toEqual(goSourcePriority())
  })

  it('covers exactly the five source kinds, with no duplicates', () => {
    expect(new Set(SOURCE_KIND_PRIORITY).size).toBe(SOURCE_KIND_PRIORITY.length)
    expect([...SOURCE_KIND_PRIORITY].sort()).toEqual(['git', 'other', 'own', 'project', 'user'])
  })
})
