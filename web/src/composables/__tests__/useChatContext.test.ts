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

  describe('clearAll', () => {
    it('clears both attachedFiles and quoteData', () => {
      ctx.addAttachedFile('/a.txt')
      ctx.addAttachedFile('/b.txt')
      ctx.setQuoteData({ text: 'hello', filePath: '/foo.ts', language: 'typescript', startLine: 1, endLine: 5 })

      ctx.clearAll()

      expect(ctx.attachedFiles.value).toHaveLength(0)
      expect(ctx.quoteData.value).toBeNull()
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
