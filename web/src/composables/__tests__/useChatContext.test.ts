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

    it('addAttachedFile keeps a whole-file and a ranged entry for the same path separate', () => {
      ctx.addAttachedFile('/src/foo.ts')
      ctx.addAttachedFile('/src/foo.ts', false, 10, 20)
      expect(ctx.attachedFiles.value).toEqual([
        { path: '/src/foo.ts', isDir: false },
        { path: '/src/foo.ts', isDir: false, startLine: 10, endLine: 20 },
      ])
    })

    it('addAttachedFile keeps two distinct line ranges of one file separate', () => {
      ctx.addAttachedFile('/src/foo.ts', false, 5, 15)
      ctx.addAttachedFile('/src/foo.ts', false, 10, 20)
      expect(ctx.attachedFiles.value).toEqual([
        { path: '/src/foo.ts', isDir: false, startLine: 5, endLine: 15 },
        { path: '/src/foo.ts', isDir: false, startLine: 10, endLine: 20 },
      ])
    })

    it('addAttachedFile is a no-op when the exact same range is already attached', () => {
      ctx.addAttachedFile('/src/foo.ts', false, 10, 20)
      ctx.addAttachedFile('/src/foo.ts', false, 10, 20)
      expect(ctx.attachedFiles.value).toHaveLength(1)
    })

    it('removeAttachedFileByPath with a range removes only that range', () => {
      ctx.addAttachedFile('/src/foo.ts')
      ctx.addAttachedFile('/src/foo.ts', false, 5, 15)
      ctx.addAttachedFile('/src/foo.ts', false, 10, 20)
      ctx.removeAttachedFileByPath('/src/foo.ts', 10, 20)
      expect(ctx.attachedFiles.value).toEqual([
        { path: '/src/foo.ts', isDir: false },
        { path: '/src/foo.ts', isDir: false, startLine: 5, endLine: 15 },
      ])
    })

    it('hasAttachedFile with a range matches only that exact range', () => {
      ctx.addAttachedFile('/src/foo.ts')
      ctx.addAttachedFile('/src/foo.ts', false, 5, 15)
      expect(ctx.hasAttachedFile('/src/foo.ts')).toBe(true)
      expect(ctx.hasAttachedFile('/src/foo.ts', 5, 15)).toBe(true)
      expect(ctx.hasAttachedFile('/src/foo.ts', 10, 20)).toBe(false)
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

    it('addAttachedFile reports whether the entry was actually added', () => {
      expect(ctx.addAttachedFile('/some/path.txt')).toBe(true)
      // Already attached — the drop handler uses this to say "already there"
      // instead of claiming it added something.
      expect(ctx.addAttachedFile('/some/path.txt')).toBe(false)
      expect(ctx.addAttachedFile('')).toBe(false)
      expect(ctx.attachedFiles.value).toHaveLength(1)
    })

    it('addAttachedFile reports true for a distinct range of an already-attached path', () => {
      expect(ctx.addAttachedFile('/src/foo.ts')).toBe(true)
      expect(ctx.addAttachedFile('/src/foo.ts', false, 10, 20)).toBe(true)
      expect(ctx.addAttachedFile('/src/foo.ts', false, 10, 20)).toBe(false)
      expect(ctx.attachedFiles.value).toHaveLength(2)
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

    it('treats the same text from two different messages as two quotes', () => {
      // Regression guard: messageId is part of the quote's identity. Before it
      // was included, quoting the same sentence out of two chat messages
      // collapsed into one and the first annotation was silently kept.
      const quoted = { ...first, sourceKind: 'message' as const }
      const fromFirst = ctx.addStagedQuote({ ...quoted, messageId: 11 }, 'first')
      const fromSecond = ctx.addStagedQuote({ ...quoted, messageId: 22 }, 'second')

      expect(ctx.stagedQuotes.value).toHaveLength(2)
      expect(fromFirst.id).not.toBe(fromSecond.id)
    })

    it('still deduplicates the same text from the same message', () => {
      const quoted = { ...first, sourceKind: 'message' as const, messageId: 11 }
      const original = ctx.addStagedQuote(quoted, 'old')
      const duplicate = ctx.addStagedQuote({ ...quoted }, 'new')

      expect(ctx.stagedQuotes.value).toHaveLength(1)
      expect(duplicate.id).toBe(original.id)
      expect(ctx.stagedQuotes.value[0].note).toBe('new')
    })

    // The same rule as messageId, for the locators a dragged row carries. A
    // label is NOT unique: the task schema has no unique constraint on `name`,
    // and a workflow name+number can repeat across repositories. Comparing only
    // the label collapsed such quotes into one and silently dropped the other.
    it('treats two tasks that share a name as two quotes', () => {
      const dragged = { ...first, text: '', filePath: 'Nightly build', language: 'task' }
      const a = ctx.addStagedQuote({ ...dragged, taskId: 11 }, 'first')
      const b = ctx.addStagedQuote({ ...dragged, taskId: 22 }, 'second')

      expect(ctx.stagedQuotes.value).toHaveLength(2)
      expect(a.id).not.toBe(b.id)
    })

    it('still deduplicates the same task dragged twice', () => {
      const dragged = { ...first, text: '', filePath: 'Nightly build', language: 'task', taskId: 11 }
      const original = ctx.addStagedQuote(dragged)
      const duplicate = ctx.addStagedQuote({ ...dragged })

      expect(ctx.stagedQuotes.value).toHaveLength(1)
      expect(duplicate.id).toBe(original.id)
    })

    it('treats two commits with the same subject as two quotes', () => {
      const dragged = { ...first, text: '', filePath: 'a1b2c3d fix login', language: 'diff' }
      const a = ctx.addStagedQuote({ ...dragged, commitSha: 'a1b2c3d4' })
      const b = ctx.addStagedQuote({ ...dragged, commitSha: 'e5f6a7b8' })

      expect(ctx.stagedQuotes.value).toHaveLength(2)
      expect(a.id).not.toBe(b.id)
    })

    it('treats the same object quoted from two repositories as two quotes', () => {
      // Two repos can each have a PR #7, so the url must be part of the identity.
      const dragged = { ...first, text: '', filePath: 'o/r#7', language: 'pr' }
      const a = ctx.addStagedQuote({ ...dragged, url: 'https://a/7' })
      const b = ctx.addStagedQuote({ ...dragged, url: 'https://b/7' })

      expect(ctx.stagedQuotes.value).toHaveLength(2)
    })

    it('updateStagedQuoteNote rewrites the addressed quote only', () => {
      const target = ctx.addStagedQuote(first, 'before')
      ctx.addStagedQuote({ ...first, text: 'const b = 2', startLine: 2, endLine: 2 }, 'untouched')

      ctx.updateStagedQuoteNote(target.id, 'after')

      expect(ctx.stagedQuotes.value.map(item => item.note)).toEqual(['after', 'untouched'])
    })

    it('updateStagedQuoteNote trims the note', () => {
      const target = ctx.addStagedQuote(first)
      ctx.updateStagedQuoteNote(target.id, '  padded  ')
      expect(ctx.stagedQuotes.value[0].note).toBe('padded')
    })

    it('updateStagedQuoteNote can clear a note', () => {
      const target = ctx.addStagedQuote(first, 'remove me')
      ctx.updateStagedQuoteNote(target.id, '')
      expect(ctx.stagedQuotes.value[0].note).toBe('')
    })

    it('updateStagedQuoteNote is a no-op for an unknown id', () => {
      ctx.addStagedQuote(first, 'keep')
      ctx.updateStagedQuoteNote('does-not-exist', 'changed')
      expect(ctx.stagedQuotes.value[0].note).toBe('keep')
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

  describe('per-session attachment drafts', () => {
    it('snapshotAttachments stores and restoreAttachments restores files + quotes', () => {
      ctx.addAttachedFile('/a.txt', false, 1, 2)
      ctx.addStagedQuote({ text: 'const x = 1', filePath: '/b.ts', language: 'ts', startLine: 3, endLine: 3 }, 'note')

      ctx.snapshotAttachments('session-1')
      ctx.clearAll()
      expect(ctx.attachedFiles.value).toHaveLength(0)
      expect(ctx.stagedQuotes.value).toHaveLength(0)

      ctx.restoreAttachments('session-1')
      expect(ctx.attachedFiles.value).toEqual([{ path: '/a.txt', isDir: false, startLine: 1, endLine: 2 }])
      expect(ctx.stagedQuotes.value).toHaveLength(1)
      expect(ctx.stagedQuotes.value[0].note).toBe('note')
      expect(ctx.stagedQuotes.value[0].filePath).toBe('/b.ts')
    })

    it('snapshotAttachments stores single quoteData', () => {
      ctx.setQuoteData({ text: 'hello', filePath: '/foo.ts', language: 'typescript', startLine: 1, endLine: 5 })
      ctx.snapshotAttachments('session-q')
      ctx.clearAll()
      ctx.restoreAttachments('session-q')
      expect(ctx.quoteData.value).toEqual({ text: 'hello', filePath: '/foo.ts', language: 'typescript', startLine: 1, endLine: 5 })
    })

    it('keeps snapshots isolated per session', () => {
      ctx.addAttachedFile('/a.txt')
      ctx.snapshotAttachments('session-1')
      ctx.clearAll()
      ctx.addAttachedFile('/b.txt')
      ctx.snapshotAttachments('session-2')
      ctx.clearAll()

      ctx.restoreAttachments('session-1')
      expect(ctx.attachedFiles.value.map(f => f.path)).toEqual(['/a.txt'])
      ctx.restoreAttachments('session-2')
      expect(ctx.attachedFiles.value.map(f => f.path)).toEqual(['/b.txt'])
    })

    it('restoreAttachments does not leak mutation back into the snapshot', () => {
      ctx.addAttachedFile('/a.txt')
      ctx.snapshotAttachments('session-1')
      ctx.restoreAttachments('session-1')
      ctx.removeAttachedFile(0)
      ctx.restoreAttachments('session-1')
      expect(ctx.attachedFiles.value.map(f => f.path)).toEqual(['/a.txt'])
    })

    it('discardAttachmentDraft removes the snapshot for a session', () => {
      ctx.addAttachedFile('/a.txt')
      ctx.snapshotAttachments('session-1')
      ctx.discardAttachmentDraft('session-1')
      ctx.clearAll()
      ctx.restoreAttachments('session-1')
      expect(ctx.attachedFiles.value).toHaveLength(0)
    })

    // ── Cross-session staging (the session picker's "add" path) ──
    // These write into a session's STORED draft rather than the live input,
    // because the user picked a session other than the one on screen.

    it('stageQuoteIntoDraft writes to the target draft and leaves the live input alone', () => {
      ctx.addStagedQuote({ text: 'live', filePath: '/live.ts', language: 'ts', startLine: 1, endLine: 1 })

      const staged = ctx.stageQuoteIntoDraft('session-2', { text: 'x', filePath: '/x.ts', language: 'ts', startLine: 2, endLine: 2 }, 'note')

      expect(staged).not.toBeNull()
      // The live input must be untouched — the card belongs to another session.
      expect(ctx.stagedQuotes.value.map(q => q.filePath)).toEqual(['/live.ts'])

      // Opening that session surfaces the card.
      ctx.clearAll()
      ctx.restoreAttachments('session-2')
      expect(ctx.stagedQuotes.value.map(q => q.filePath)).toEqual(['/x.ts'])
      expect(ctx.stagedQuotes.value[0].note).toBe('note')
    })

    // The reachability bug this guards: restoreAttachments is a no-op when no
    // snapshot exists, so staging into a never-opened session must CREATE one.
    it('stageQuoteIntoDraft creates a draft for a session that has never been opened', () => {
      ctx.stageQuoteIntoDraft('never-opened', { text: 'y', filePath: '/y.ts', language: 'ts', startLine: 1, endLine: 1 })
      ctx.restoreAttachments('never-opened')
      expect(ctx.stagedQuotes.value.map(q => q.filePath)).toEqual(['/y.ts'])
    })

    it('stageQuoteIntoDraft dedupes by locator and updates the note in place', () => {
      const q = { text: 'z', filePath: '/z.ts', language: 'ts', startLine: 1, endLine: 1 }
      const first = ctx.stageQuoteIntoDraft('session-3', q, 'one')
      const second = ctx.stageQuoteIntoDraft('session-3', q, 'two')
      expect(second!.id).toBe(first!.id)
      ctx.restoreAttachments('session-3')
      expect(ctx.stagedQuotes.value).toHaveLength(1)
      expect(ctx.stagedQuotes.value[0].note).toBe('two')
    })

    it('stageQuoteIntoDraft with no session id is a no-op', () => {
      expect(ctx.stageQuoteIntoDraft('', { text: 'a', filePath: '/a.ts', language: '', startLine: 0, endLine: 0 })).toBeNull()
      ctx.restoreAttachments('')
      expect(ctx.stagedQuotes.value).toHaveLength(0)
    })

    it('stageQuoteIntoDraft keeps per-session isolation', () => {
      ctx.stageQuoteIntoDraft('s-a', { text: 'a', filePath: '/a.ts', language: '', startLine: 0, endLine: 0 })
      ctx.stageQuoteIntoDraft('s-b', { text: 'b', filePath: '/b.ts', language: '', startLine: 0, endLine: 0 })
      ctx.restoreAttachments('s-a')
      expect(ctx.stagedQuotes.value.map(q => q.filePath)).toEqual(['/a.ts'])
      ctx.restoreAttachments('s-b')
      expect(ctx.stagedQuotes.value.map(q => q.filePath)).toEqual(['/b.ts'])
    })

    it('stageAttachmentIntoDraft stages a file into the target draft only', () => {
      const added = ctx.stageAttachmentIntoDraft('session-4', { path: '/a.txt', isDir: false })
      expect(added).toBe(true)
      expect(ctx.attachedFiles.value).toHaveLength(0)

      ctx.restoreAttachments('session-4')
      expect(ctx.attachedFiles.value.map(f => f.path)).toEqual(['/a.txt'])
    })

    it('stageAttachmentIntoDraft reports false for a duplicate and for an empty path', () => {
      ctx.stageAttachmentIntoDraft('session-5', { path: '/a.txt', isDir: false })
      expect(ctx.stageAttachmentIntoDraft('session-5', { path: '/a.txt', isDir: false })).toBe(false)
      expect(ctx.stageAttachmentIntoDraft('session-5', { path: '', isDir: false })).toBe(false)
      expect(ctx.stageAttachmentIntoDraft('', { path: '/a.txt', isDir: false })).toBe(false)
    })

    it('stageAttachmentIntoDraft keeps a ranged entry separate from the whole-file entry', () => {
      ctx.stageAttachmentIntoDraft('session-6', { path: '/a.txt', isDir: false })
      ctx.stageAttachmentIntoDraft('session-6', { path: '/a.txt', isDir: false, startLine: 5, endLine: 9 })
      ctx.restoreAttachments('session-6')
      expect(ctx.attachedFiles.value).toHaveLength(2)
    })

    it('staging a quote and a file into the same session keeps both', () => {
      ctx.stageQuoteIntoDraft('session-7', { text: 'q', filePath: '/q.ts', language: 'ts', startLine: 1, endLine: 1 })
      ctx.stageAttachmentIntoDraft('session-7', { path: '/f.txt', isDir: false })
      ctx.restoreAttachments('session-7')
      expect(ctx.stagedQuotes.value.map(q => q.filePath)).toEqual(['/q.ts'])
      expect(ctx.attachedFiles.value.map(f => f.path)).toEqual(['/f.txt'])
    })

    it('restoreAttachments with no snapshot is a no-op', () => {
      ctx.addAttachedFile('/a.txt')
      ctx.restoreAttachments('never-snapshotted')
      expect(ctx.attachedFiles.value.map(f => f.path)).toEqual(['/a.txt'])
    })

    it('snapshot/restore ignore empty session ids', () => {
      ctx.addAttachedFile('/a.txt')
      ctx.snapshotAttachments('')
      ctx.clearAll()
      ctx.restoreAttachments('')
      expect(ctx.attachedFiles.value).toHaveLength(0)
    })
  })
})
