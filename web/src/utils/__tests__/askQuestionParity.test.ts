import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  extractAskMatches,
  normalizeAskInput,
  stripAskMatches,
  type AskItem,
} from '@/utils/askQuestion.ts'

/**
 * Go ↔ TS parity guard.
 *
 * The TypeScript module is a deliberate mirror of `internal/askquestion` (Go),
 * the same way `web/src/utils/version.ts` mirrors `internal/version/compare.go`.
 * Both are pinned to one fixture — `internal/askquestion/testdata/parity_corpus.json`
 * — so a change to either implementation that the other does not share fails
 * here (or in `internal/askquestion/parity_test.go`).
 *
 * The fixture is located relative to the repo root, probing candidate roots so
 * the test passes whether vitest runs from the repo root or from web/.
 * `import.meta.url` is not usable here: the jsdom environment rewrites it to an
 * http URL.
 */
function findCorpus(): string {
  const rel = 'internal/askquestion/testdata/parity_corpus.json'
  for (const root of [process.cwd(), resolve(process.cwd(), '..')]) {
    const candidate = resolve(root, rel)
    if (existsSync(candidate)) return candidate
  }
  throw new Error(`parity corpus not found relative to ${process.cwd()}`)
}

interface Corpus {
  input: Array<{ name: string; raw: Record<string, unknown>; want: AskItem[] }>
  extract: Array<{
    name: string
    text: string
    wantParsed: boolean[]
    wantItems: AskItem[][]
    wantStripped: string
  }>
}

const corpus: Corpus = JSON.parse(readFileSync(findCorpus(), 'utf8'))

describe('ask-question parity corpus (shared with internal/askquestion)', () => {
  it('loads the shared fixture', () => {
    expect(corpus.input.length).toBeGreaterThan(0)
    expect(corpus.extract.length).toBeGreaterThan(0)
  })

  describe('normalizeAskInput matches Go NormalizeInput', () => {
    for (const tc of corpus.input) {
      it(tc.name, () => {
        expect(normalizeAskInput(tc.raw)).toEqual(tc.want)
      })
    }
  })

  describe('extractAskMatches + stripAskMatches match Go Extract/Strip', () => {
    for (const tc of corpus.extract) {
      it(tc.name, () => {
        const matches = extractAskMatches(tc.text)
        expect(matches).toHaveLength(tc.wantParsed.length)
        matches.forEach((m, i) => {
          expect(m.parsed !== null, `match ${i} parsed flag`).toBe(tc.wantParsed[i])
          if (m.parsed) {
            expect(m.parsed, `match ${i} items`).toEqual(tc.wantItems[i])
          }
        })
        expect(stripAskMatches(tc.text, matches)).toBe(tc.wantStripped)
      })
    }
  })
})
