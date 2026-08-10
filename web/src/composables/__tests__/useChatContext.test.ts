import { describe, it, expect, beforeEach } from 'vitest'
import { useChatContext } from '../useChatContext.ts'

describe('useChatContext', () => {
  // Get a fresh reference before each test — module-level state persists
  // between tests, so we need to clearAll() first.
  let ctx: ReturnType<typeof useChatContext>

  beforeEach(() => {
    ctx = useChatContext()
    ctx.clearAll()
  })

  describe('attachedFiles', () => {
    it('addAttachedFile adds a file entry', () => {
      ctx.addAttachedFile('/some/path.txt')
      expect(ctx.attachedFiles.value).toEqual([{ path: '/some/path.txt', isDir: false }])
    })

    it('addAttachedFile adds a file entry with line info', () => {
      ctx.addAttachedFile('/src/foo.ts', false, 10, 20)
      expect(ctx.attachedFiles.value).toEqual([{ path: '/src/foo.ts', isDir: false, startLine: 10, endLine: 20 }])
    })

    it('addAttachedFile upgrades existing entry with line info', () => {
      ctx.addAttachedFile('/src/foo.ts')
      expect(ctx.attachedFiles.value).toEqual([{ path: '/src/foo.ts', isDir: false }])

      ctx.addAttachedFile('/src/foo.ts', false, 10, 20)
      expect(ctx.attachedFiles.value).toEqual([{ path: '/src/foo.ts', isDir: false, startLine: 10, endLine: 20 }])
    })

    it('addAttachedFile does not overwrite existing line info', () => {
      ctx.addAttachedFile('/src/foo.ts', false, 5, 15)
      ctx.addAttachedFile('/src/foo.ts', false, 10, 20)
      expect(ctx.attachedFiles.value).toEqual([{ path: '/src/foo.ts', isDir: false, startLine: 5, endLine: 15 }])
    })

    it('addAttachedFile adds a directory entry', () => {
      ctx.addAttachedFile('/src', true)
      expect(ctx.attachedFiles.value).toEqual([{ path: '/src', isDir: true }])
    })

    it('addAttachedFile does not add duplicates', () => {
      ctx.addAttachedFile('/some/path.txt')
      ctx.addAttachedFile('/some/path.txt')
      expect(ctx.attachedFiles.value).toHaveLength(1)
    })

    it('addAttachedFile ignores empty string', () => {
      ctx.addAttachedFile('')
      expect(ctx.attachedFiles.value).toHaveLength(0)
    })

    it('removeAttachedFile removes by index', () => {
      ctx.addAttachedFile('/a.txt')
      ctx.addAttachedFile('/b.txt')
      ctx.removeAttachedFile(0)
      expect(ctx.attachedFiles.value).toHaveLength(1)
      expect(ctx.attachedFiles.value[0].path).toBe('/b.txt')
    })

    it('hasAttachedFile returns true for existing path', () => {
      ctx.addAttachedFile('/a.txt')
      expect(ctx.hasAttachedFile('/a.txt')).toBe(true)
      expect(ctx.hasAttachedFile('/b.txt')).toBe(false)
    })

    it('hasAttachedFile returns false for empty path', () => {
      expect(ctx.hasAttachedFile('')).toBe(false)
    })

    it('removeAttachedFileByPath removes by path', () => {
      ctx.addAttachedFile('/a.txt')
      ctx.addAttachedFile('/b.txt')
      ctx.removeAttachedFileByPath('/a.txt')
      expect(ctx.attachedFiles.value).toHaveLength(1)
      expect(ctx.attachedFiles.value[0].path).toBe('/b.txt')
    })

    it('removeAttachedFileByPath does nothing for non-existent path', () => {
      ctx.addAttachedFile('/a.txt')
      ctx.removeAttachedFileByPath('/z.txt')
      expect(ctx.attachedFiles.value).toHaveLength(1)
    })

    it('toggleAttachedFile adds when not present', () => {
      ctx.toggleAttachedFile('/a.txt')
      expect(ctx.attachedFiles.value.some(f => f.path === '/a.txt')).toBe(true)
    })

    it('toggleAttachedFile removes when already present', () => {
      ctx.addAttachedFile('/a.txt')
      ctx.toggleAttachedFile('/a.txt')
      expect(ctx.attachedFiles.value.some(f => f.path === '/a.txt')).toBe(false)
    })

    it('toggleAttachedFile does nothing for empty path', () => {
      ctx.toggleAttachedFile('')
      expect(ctx.attachedFiles.value).toHaveLength(0)
    })

    it('toggleAttachedFile preserves isDir when adding', () => {
      ctx.toggleAttachedFile('/src', true)
      expect(ctx.attachedFiles.value).toEqual([{ path: '/src', isDir: true }])
    })
  })

  describe('quoteData', () => {
    it('setQuoteData sets quote data', () => {
      const data = { text: 'hello', filePath: '/foo.ts', language: 'typescript', startLine: 1, endLine: 5 }
      ctx.setQuoteData(data)
      expect(ctx.quoteData.value).toEqual(data)
    })

    it('setQuoteData clears with null', () => {
      const data = { text: 'hello', filePath: '/foo.ts', language: 'typescript', startLine: 1, endLine: 5 }
      ctx.setQuoteData(data)
      ctx.setQuoteData(null)
      expect(ctx.quoteData.value).toBeNull()
    })
  })

  describe('stagedQuotes', () => {
    const first = { text: 'const a = 1', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 1 }

    it('keeps ordered selections with optional notes', () => {
      ctx.addStagedQuote(first, 'first note')
      ctx.addStagedQuote({ ...first, text: 'const b = 2', startLine: 2, endLine: 2 })

      expect(ctx.stagedQuotes.value.map(item => ({ text: item.text, note: item.note }))).toEqual([
        { text: 'const a = 1', note: 'first note' },
        { text: 'const b = 2', note: '' },
      ])
    })

    it('deduplicates an identical selection and updates a non-empty note', () => {
      const original = ctx.addStagedQuote(first, 'old note')
      const duplicate = ctx.addStagedQuote({ ...first }, 'new note')

      expect(ctx.stagedQuotes.value).toHaveLength(1)
      expect(duplicate.id).toBe(original.id)
      expect(ctx.stagedQuotes.value[0].note).toBe('new note')
    })

    it('does not erase an existing note when duplicate note is empty', () => {
      ctx.addStagedQuote(first, 'keep me')
      ctx.addStagedQuote({ ...first }, '   ')
      expect(ctx.stagedQuotes.value[0].note).toBe('keep me')
    })

    it('keeps partially overlapping ranges as separate selections', () => {
      ctx.addStagedQuote({ ...first, startLine: 1, endLine: 5 })
      ctx.addStagedQuote({ ...first, text: 'overlap', startLine: 4, endLine: 8 })
      expect(ctx.stagedQuotes.value).toHaveLength(2)
    })

    it('removes one staged quote by id without affecting the others', () => {
      const firstItem = ctx.addStagedQuote(first)
      const secondItem = ctx.addStagedQuote({ ...first, text: 'second', startLine: 2, endLine: 2 })
      ctx.removeStagedQuote(firstItem.id)

      expect(ctx.stagedQuotes.value.map(item => item.id)).toEqual([secondItem.id])
    })
  })

  describe('clearAll', () => {
    it('clears both attachedFiles and quoteData', () => {
      ctx.addAttachedFile('/a.txt')
      ctx.addAttachedFile('/b.txt')
      ctx.setQuoteData({ text: 'hello', filePath: '/foo.ts', language: 'typescript', startLine: 1, endLine: 5 })

      ctx.clearAll()

      expect(ctx.attachedFiles.value).toHaveLength(0)
      expect(ctx.quoteData.value).toBeNull()
      expect(ctx.stagedQuotes.value).toHaveLength(0)
    })
  })

  describe('singleton behavior', () => {
    it('multiple useChatContext() calls share the same state', () => {
      const ctx2 = useChatContext()
      ctx.addAttachedFile('/shared.txt')
      expect(ctx2.attachedFiles.value.some(f => f.path === '/shared.txt')).toBe(true)
    })
  })
})
