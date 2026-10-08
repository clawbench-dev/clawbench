import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { parseGroupRouting, stripGroupProtocolTags } from '@/utils/groupRouting.ts'

/**
 * Go ↔ TS parity guard for group routing. Both implementations are pinned to
 * `internal/grouprouting/testdata/parity_corpus.json`.
 */
function findCorpus(): string {
  const rel = 'internal/grouprouting/testdata/parity_corpus.json'
  for (const root of [process.cwd(), resolve(process.cwd(), '..')]) {
    const candidate = resolve(root, rel)
    if (existsSync(candidate)) return candidate
  }
  throw new Error(`parity corpus not found relative to ${process.cwd()}`)
}

interface Corpus {
  cases: Array<{
    name: string
    text: string
    want: {
      found: boolean
      end: boolean
      speakers: string[]
      instruction: string
      before: string
      after: string
      bcc: Array<{ targets: string[]; content: string }>
    }
  }>
  stripCases: Array<{
    name: string
    text: string
    want: string
  }>
}

describe('groupRouting parity with internal/grouprouting', () => {
  const corpus: Corpus = JSON.parse(readFileSync(findCorpus(), 'utf-8'))

  it('has cases', () => {
    expect(corpus.cases.length).toBeGreaterThan(0)
  })

  for (const tc of corpus.cases) {
    it(tc.name, () => {
      const r = parseGroupRouting(tc.text)
      expect(r.found).toBe(tc.want.found)
      expect(r.end).toBe(tc.want.end)
      expect(r.speakers).toEqual(tc.want.speakers)
      expect(r.instruction).toBe(tc.want.instruction)
      expect(r.before).toBe(tc.want.before)
      expect(r.after).toBe(tc.want.after ?? '')
      expect(r.bcc).toEqual(tc.want.bcc ?? [])
    })
  }

  // StripProtocolTags is the fail-closed injection / quote / TTS boundary. It is
  // security-critical, so it gets its own corpus set — an earlier divergence on
  // an unclosed private note leaked the note into the quoted text (which is then
  // inlined into EVERY member's prompt).
  it('has stripCases', () => {
    expect(corpus.stripCases.length).toBeGreaterThan(0)
  })

  for (const tc of corpus.stripCases) {
    it(`strip: ${tc.name}`, () => {
      expect(stripGroupProtocolTags(tc.text)).toBe(tc.want)
    })
  }
})
