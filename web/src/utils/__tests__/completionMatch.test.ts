import { describe, expect, it } from 'vitest'
import {
  fuzzyMatch,
  buildFileCandidates,
  parseAtQuery,
  parseSlashQuery,
  middleEllipsis,
  type FileCandidateSources,
} from '@/utils/completionMatch.ts'

describe('fuzzyMatch', () => {
  it('returns null when the query characters cannot be found in order', () => {
    expect(fuzzyMatch('xyz', 'main.ts')).toBeNull()
    // right characters, wrong order
    expect(fuzzyMatch('sm', 'main.ts')).toBeNull()
  })

  it('matches an empty query with a zero score', () => {
    expect(fuzzyMatch('', 'main.ts')).toEqual({ score: 0, positions: [] })
  })

  it('returns matched positions in order', () => {
    const m = fuzzyMatch('mts', 'main.ts')
    expect(m).not.toBeNull()
    expect(m!.positions).toEqual([0, 5, 6])
    expect(m!.score).toBeGreaterThan(0)
  })

  it('is case insensitive', () => {
    expect(fuzzyMatch('MAIN', 'main.ts')).not.toBeNull()
    expect(fuzzyMatch('main', 'MAIN.TS')).not.toBeNull()
  })

  it('scores consecutive matches higher than scattered ones', () => {
    const consecutive = fuzzyMatch('xy', 'xyzabc')!
    const scattered = fuzzyMatch('xz', 'xyzabc')!
    expect(consecutive.score).toBeGreaterThan(scattered.score)
  })

  it('scores a word-boundary match higher than a mid-word one', () => {
    const boundary = fuzzyMatch('a', 'q-abc')!
    const midWord = fuzzyMatch('a', 'qabc')!
    expect(boundary.score).toBeGreaterThan(midWord.score)
  })

  it('treats a camelCase transition as a word boundary', () => {
    const camel = fuzzyMatch('b', 'aBc')!
    const flat = fuzzyMatch('b', 'abc')!
    expect(camel.score).toBeGreaterThan(flat.score)
  })

  it('scores a match at the start higher than one deep inside', () => {
    const atStart = fuzzyMatch('m', 'main')!
    const inside = fuzzyMatch('m', 'xmain')!
    expect(atStart.score).toBeGreaterThan(inside.score)
  })
})

describe('buildFileCandidates', () => {
  const base: FileCandidateSources = {}

  it('returns an empty list for empty sources', () => {
    expect(buildFileCandidates(base, '')).toEqual([])
  })

  it('dedupes by path, keeping the highest-priority source', () => {
    const items = buildFileCandidates({
      recentOpen: [{ path: 'src/a.ts' }],
      currentDir: [{ path: 'src/a.ts' }],
      recentRef: [{ path: 'src/a.ts' }],
    }, '')
    expect(items).toHaveLength(1)
    expect(items[0].source).toBe('recent-open')
  })

  it('excludes already-attached paths', () => {
    const items = buildFileCandidates({
      currentDir: [{ path: 'src/a.ts' }, { path: 'src/b.ts' }],
    }, '', ['src/a.ts'])
    expect(items.map(i => i.key)).toEqual(['src/b.ts'])
  })

  it('derives label from basename and description from dir', () => {
    const items = buildFileCandidates({ currentDir: [{ path: 'src/chat/Input.vue' }] }, '')
    expect(items[0].label).toBe('Input.vue')
    expect(items[0].description).toBe('src/chat')
  })

  it('with an empty query sorts by source priority then path alphabetically', () => {
    const items = buildFileCandidates({
      recentOpen: [{ path: 'z/z.ts' }, { path: 'a/a.ts' }],
      currentDir: [{ path: 'm.ts' }],
    }, '')
    expect(items.map(i => i.key)).toEqual(['a/a.ts', 'z/z.ts', 'm.ts'])
  })

  it('with a query fuzzy-matches the basename and drops non-matches', () => {
    const items = buildFileCandidates({
      currentDir: [{ path: 'src/main.ts' }, { path: 'src/other.ts' }],
    }, 'main')
    expect(items.map(i => i.key)).toEqual(['src/main.ts'])
    expect(items[0].positions).toEqual([0, 1, 2, 3])
  })

  it('breaks score ties by source priority', () => {
    const items = buildFileCandidates({
      recentOpen: [{ path: 'lib/main.go' }],
      currentDir: [{ path: 'src/main.ts' }],
    }, 'main')
    // identical fuzzy scores on the basenames -> recent-open wins the tie
    expect(items[0].source).toBe('recent-open')
  })

  it('truncates to 50 candidates', () => {
    const currentDir = Array.from({ length: 60 }, (_, i) => ({ path: `f${String(i).padStart(2, '0')}.ts` }))
    const items = buildFileCandidates({ currentDir }, '')
    expect(items).toHaveLength(50)
  })

  it('normalizes backslashes before dedupe', () => {
    const items = buildFileCandidates({
      recentOpen: [{ path: 'src\\a.ts' }],
      currentDir: [{ path: 'src/a.ts' }],
    }, '')
    expect(items).toHaveLength(1)
    expect(items[0].source).toBe('recent-open')
  })

  it('treats an absolute attached path as the same file when a project root is known', () => {
    const items = buildFileCandidates({
      currentDir: [{ path: 'src/a.ts' }, { path: 'src/b.ts' }],
    }, '', ['/home/u/proj/src/a.ts'], '/home/u/proj')
    expect(items.map(i => i.key)).toEqual(['src/b.ts'])
  })

  it('relativizes absolute candidates so they dedupe against relative ones', () => {
    const items = buildFileCandidates({
      recentOpen: [{ path: '/home/u/proj/src/a.ts' }],
      currentDir: [{ path: 'src/a.ts' }],
    }, '', [], '/home/u/proj')
    expect(items).toHaveLength(1)
    expect(items[0].key).toBe('src/a.ts')
    expect(items[0].source).toBe('recent-open')
  })

  it('without a project root leaves absolute paths untouched', () => {
    const items = buildFileCandidates({
      currentDir: [{ path: '/home/u/proj/src/a.ts' }],
    }, '')
    expect(items[0].key).toBe('/home/u/proj/src/a.ts')
  })

  it('carries isDir through to the candidate', () => {
    const items = buildFileCandidates({
      currentDir: [
        { path: 'src', isDir: true },
        { path: 'src/a.ts' },
        { path: 'assets/logo.png', isDir: false },
      ],
    }, '')
    const byKey = new Map(items.map(i => [i.key, i.isDir]))
    expect(byKey.get('src')).toBe(true)
    expect(byKey.get('src/a.ts')).toBe(false)
    expect(byKey.get('assets/logo.png')).toBe(false)
  })

  it('keeps isDir when the winning source is the one carrying it', () => {
    // recent-open wins the dedupe; its isDir flag must survive.
    const items = buildFileCandidates({
      recentOpen: [{ path: 'docs', isDir: true }],
      currentDir: [{ path: 'docs', isDir: true }],
    }, '')
    expect(items).toHaveLength(1)
    expect(items[0].source).toBe('recent-open')
    expect(items[0].isDir).toBe(true)
  })
})

describe('parseAtQuery', () => {
  it('parses a query in the middle of a sentence', () => {
    expect(parseAtQuery('hello @ma', 9)).toEqual({ start: 6, end: 9, query: 'ma' })
  })

  it('parses a query at the start of the input', () => {
    expect(parseAtQuery('@ma', 3)).toEqual({ start: 0, end: 3, query: 'ma' })
  })

  it('returns an empty query right after typing @', () => {
    expect(parseAtQuery('@', 1)).toEqual({ start: 0, end: 1, query: '' })
  })

  it('ignores an @ that is not preceded by whitespace (email-like)', () => {
    expect(parseAtQuery('a@b', 3)).toBeNull()
  })

  it('rejects a query containing whitespace', () => {
    expect(parseAtQuery('hello @ma wo', 12)).toBeNull()
  })

  it('returns null when the caret is before any @', () => {
    expect(parseAtQuery('hello @ma', 3)).toBeNull()
  })

  it('returns null when there is no @ at all', () => {
    expect(parseAtQuery('hello', 5)).toBeNull()
  })

  it('accepts a newline as the boundary before @', () => {
    expect(parseAtQuery('a\n@b', 4)).toEqual({ start: 2, end: 4, query: 'b' })
  })

  it('uses only the text left of the caret', () => {
    // trailing text after the caret must not join the query
    expect(parseAtQuery('@ma rest', 3)).toEqual({ start: 0, end: 3, query: 'ma' })
  })
})

describe('parseSlashQuery', () => {
  it('parses a bare slash command', () => {
    expect(parseSlashQuery('/cb')).toEqual({ start: 0, end: 3, query: 'cb' })
  })

  it('parses an empty query right after the slash', () => {
    expect(parseSlashQuery('/')).toEqual({ start: 0, end: 1, query: '' })
  })

  it('returns null once a space appears', () => {
    expect(parseSlashQuery('/cb ')).toBeNull()
  })

  it('returns null when the input does not start with a slash', () => {
    expect(parseSlashQuery('hi /cb')).toBeNull()
  })
})

describe('middleEllipsis', () => {
  it('leaves a short string untouched', () => {
    expect(middleEllipsis('src/chat', 20)).toBe('src/chat')
  })

  it('keeps the leading and trailing segments when truncating', () => {
    const out = middleEllipsis('src/components/chat/Input.vue', 20)
    expect(out.length).toBeLessThanOrEqual(20)
    expect(out).toContain('…')
    expect(out.startsWith('src/')).toBe(true)
    expect(out.endsWith('Input.vue')).toBe(true)
  })

  it('handles an empty string', () => {
    expect(middleEllipsis('', 10)).toBe('')
  })
})
