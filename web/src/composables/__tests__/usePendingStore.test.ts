import { describe, it, expect } from 'vitest'
import { usePendingStore, createPendingMessage } from '@/composables/usePendingStore'

describe('usePendingStore', () => {
  // ── getPending ──────────────────────────────────────────────
  describe('getPending', () => {
    it('returns empty array for unknown session', () => {
      const store = usePendingStore()
      expect(store.getPending('unknown')).toEqual([])
    })

    it('returns added messages for a known session', () => {
      const store = usePendingStore()
      const msg = createPendingMessage('hello')
      store.addPending('s1', msg)
      expect(store.getPending('s1')).toHaveLength(1)
      expect(store.getPending('s1')[0].content).toBe('hello')
    })
  })

  // ── addPending ──────────────────────────────────────────────
  describe('addPending', () => {
    it('adds message to correct session', () => {
      const store = usePendingStore()
      const msg = createPendingMessage('test msg')
      store.addPending('s1', msg)
      expect(store.getPending('s1')).toHaveLength(1)
      expect(store.getPending('s1')[0].content).toBe('test msg')
    })

    it('multiple sessions are independent', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('msg for s1'))
      store.addPending('s2', createPendingMessage('msg for s2'))
      expect(store.getPending('s1')).toHaveLength(1)
      expect(store.getPending('s2')).toHaveLength(1)
      expect(store.getPending('s1')[0].content).toBe('msg for s1')
      expect(store.getPending('s2')[0].content).toBe('msg for s2')
    })

    it('appends multiple messages to same session', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('first'))
      store.addPending('s1', createPendingMessage('second'))
      expect(store.getPending('s1')).toHaveLength(2)
      expect(store.getPending('s1')[0].content).toBe('first')
      expect(store.getPending('s1')[1].content).toBe('second')
    })
  })

  // ── removePending ───────────────────────────────────────────
  describe('removePending', () => {
    it('removes message by text content', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('remove me'))
      store.addPending('s1', createPendingMessage('keep me'))
      store.removePending('s1', 'remove me')
      expect(store.getPending('s1')).toHaveLength(1)
      expect(store.getPending('s1')[0].content).toBe('keep me')
    })

    it('is no-op for unknown session', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('msg'))
      store.removePending('unknown', 'msg')
      expect(store.getPending('s1')).toHaveLength(1)
    })

    it('is no-op for unknown text', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('msg'))
      store.removePending('s1', 'nonexistent')
      expect(store.getPending('s1')).toHaveLength(1)
    })

    it('removes only the first match when duplicates exist', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('dup'))
      store.addPending('s1', createPendingMessage('dup'))
      store.removePending('s1', 'dup')
      expect(store.getPending('s1')).toHaveLength(1)
    })
  })

  // ── removePendingAt ─────────────────────────────────────────
  describe('removePendingAt', () => {
    it('removes message by valid index', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('first'))
      store.addPending('s1', createPendingMessage('second'))
      store.removePendingAt('s1', 0)
      expect(store.getPending('s1')).toHaveLength(1)
      expect(store.getPending('s1')[0].content).toBe('second')
    })

    it('is no-op for negative index', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('msg'))
      store.removePendingAt('s1', -1)
      expect(store.getPending('s1')).toHaveLength(1)
    })

    it('is no-op for index >= length', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('msg'))
      store.removePendingAt('s1', 1)
      store.removePendingAt('s1', 100)
      expect(store.getPending('s1')).toHaveLength(1)
    })

    it('is no-op for unknown session', () => {
      const store = usePendingStore()
      store.removePendingAt('unknown', 0)
      expect(store.getPending('unknown')).toEqual([])
    })
  })

  // ── syncFromBackendQueue ────────────────────────────────────
  describe('syncFromBackendQueue', () => {
    it('adds new messages from backend queue', () => {
      const store = usePendingStore()
      store.syncFromBackendQueue('s1', [{ text: 'hello' }])
      const pending = store.getPending('s1')
      expect(pending).toHaveLength(1)
      expect(pending[0].content).toBe('hello')
      expect(pending[0].pending).toBe(true)
    })

    it('removes stale messages not in backend queue', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('stale'))
      store.syncFromBackendQueue('s1', [{ text: 'fresh' }])
      const pending = store.getPending('s1')
      expect(pending).toHaveLength(1)
      expect(pending[0].content).toBe('fresh')
    })

    it('does not add duplicates for existing messages', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('existing'))
      store.syncFromBackendQueue('s1', [{ text: 'existing' }])
      expect(store.getPending('s1')).toHaveLength(1)
    })

    it('handles empty text by using empty string', () => {
      const store = usePendingStore()
      store.syncFromBackendQueue('s1', [{ text: '' }])
      const pending = store.getPending('s1')
      expect(pending).toHaveLength(1)
      expect(pending[0].content).toBe('')
    })

    it('handles missing text field by defaulting to empty string', () => {
      const store = usePendingStore()
      store.syncFromBackendQueue('s1', [{}])
      const pending = store.getPending('s1')
      expect(pending).toHaveLength(1)
      expect(pending[0].content).toBe('')
    })

    it('merges files and filePaths from backend item', () => {
      const store = usePendingStore()
      store.syncFromBackendQueue('s1', [{
        text: 'with files',
        files: ['a.txt'],
        filePaths: ['b.txt'],
      }])
      const pending = store.getPending('s1')
      expect(pending[0].files).toHaveLength(2)
      expect(pending[0].files[0].path).toBe('a.txt')
      expect(pending[0].files[1].path).toBe('b.txt')
    })

    it('handles missing files and filePaths gracefully', () => {
      const store = usePendingStore()
      store.syncFromBackendQueue('s1', [{ text: 'no files' }])
      const pending = store.getPending('s1')
      expect(pending[0].files).toHaveLength(0)
    })

    it('clears all pending when backend queue is empty', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('old'))
      store.syncFromBackendQueue('s1', [])
      expect(store.getPending('s1')).toHaveLength(0)
    })

    it('preserves messages that exist in both local and backend', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('keep'))
      store.addPending('s1', createPendingMessage('remove'))
      store.syncFromBackendQueue('s1', [{ text: 'keep' }])
      const pending = store.getPending('s1')
      expect(pending).toHaveLength(1)
      expect(pending[0].content).toBe('keep')
    })

    it('never touches non-pending messages in other sessions', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('s1 msg'))
      store.addPending('s2', createPendingMessage('s2 msg'))
      store.syncFromBackendQueue('s1', [])
      expect(store.getPending('s1')).toHaveLength(0)
      expect(store.getPending('s2')).toHaveLength(1)
    })
  })

  // ── clearPending ────────────────────────────────────────────
  describe('clearPending', () => {
    it('clears specific session only', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('msg1'))
      store.addPending('s2', createPendingMessage('msg2'))
      store.clearPending('s1')
      expect(store.getPending('s1')).toHaveLength(0)
      expect(store.getPending('s2')).toHaveLength(1)
    })

    it('is no-op for unknown session', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('msg'))
      store.clearPending('unknown')
      expect(store.getPending('s1')).toHaveLength(1)
    })
  })

  // ── clearAllPending ─────────────────────────────────────────
  describe('clearAllPending', () => {
    it('clears all sessions', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('msg1'))
      store.addPending('s2', createPendingMessage('msg2'))
      store.clearAllPending()
      expect(store.getPending('s1')).toHaveLength(0)
      expect(store.getPending('s2')).toHaveLength(0)
    })
  })

  // ── hasPending ──────────────────────────────────────────────
  describe('hasPending', () => {
    it('returns false for unknown session', () => {
      const store = usePendingStore()
      expect(store.hasPending('unknown')).toBe(false)
    })

    it('returns true when session has pending messages', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('msg'))
      expect(store.hasPending('s1')).toBe(true)
    })

    it('returns false after all messages are removed', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('msg'))
      store.removePending('s1', 'msg')
      expect(store.hasPending('s1')).toBe(false)
    })

    it('returns false after clearPending', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('msg'))
      store.clearPending('s1')
      expect(store.hasPending('s1')).toBe(false)
    })
  })

  // ── createPendingMessage ────────────────────────────────────
  describe('createPendingMessage', () => {
    it('creates message with correct structure', () => {
      const msg = createPendingMessage('hello')
      expect(msg.role).toBe('user')
      expect(msg.content).toBe('hello')
      expect(msg.blocks).toEqual([{ type: 'text', text: 'hello' }])
      expect(msg.files).toEqual([])
      expect(msg.pending).toBe(true)
      expect(msg.createdAt).toBeTruthy()
    })

    it('maps file paths to { path } objects', () => {
      const msg = createPendingMessage('with files', ['a.ts', 'b.ts'])
      expect(msg.files).toEqual([{ path: 'a.ts' }, { path: 'b.ts' }])
    })

    it('defaults to empty files array', () => {
      const msg = createPendingMessage('no files')
      expect(msg.files).toEqual([])
    })

    it('handles empty text', () => {
      const msg = createPendingMessage('')
      expect(msg.content).toBe('')
      expect(msg.blocks).toEqual([])
    })

    it('produces a valid ISO date string', () => {
      const msg = createPendingMessage('test')
      const date = new Date(msg.createdAt)
      expect(date.toISOString()).toBe(msg.createdAt)
    })
  })

  // ── Immutable updates (new array references) ──────────────
  describe('immutable updates', () => {
    it('addPending creates a new array reference', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('first'))
      const ref1 = store.getPending('s1')
      store.addPending('s1', createPendingMessage('second'))
      const ref2 = store.getPending('s1')
      expect(ref1).not.toBe(ref2)
    })

    it('removePending creates a new array reference', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('a'))
      store.addPending('s1', createPendingMessage('b'))
      const ref1 = store.getPending('s1')
      store.removePending('s1', 'a')
      const ref2 = store.getPending('s1')
      expect(ref1).not.toBe(ref2)
    })

    it('removePendingAt creates a new array reference', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('a'))
      store.addPending('s1', createPendingMessage('b'))
      const ref1 = store.getPending('s1')
      store.removePendingAt('s1', 0)
      const ref2 = store.getPending('s1')
      expect(ref1).not.toBe(ref2)
    })

    it('syncFromBackendQueue creates a new array reference', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('a'))
      const ref1 = store.getPending('s1')
      store.syncFromBackendQueue('s1', [{ text: 'a' }])
      const ref2 = store.getPending('s1')
      expect(ref1).not.toBe(ref2)
    })

    it('syncFromBackendQueue creates new array even when content unchanged', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('a'))
      const ref1 = store.getPending('s1')
      // Same content, but still creates new array for Vue reactivity guarantee
      store.syncFromBackendQueue('s1', [{ text: 'a' }])
      const ref2 = store.getPending('s1')
      expect(ref1).not.toBe(ref2)
      expect(ref2).toHaveLength(1)
      expect(ref2[0].content).toBe('a')
    })

    it('syncFromBackendQueue drain scenario: removing pending creates new array', () => {
      // Simulates the queue_drain flow: pending 'hello' is drained (removed from backend queue)
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('hello'))
      store.addPending('s1', createPendingMessage('world'))
      const ref1 = store.getPending('s1')

      // Backend drains 'hello' — only 'world' remains in queue
      store.syncFromBackendQueue('s1', [{ text: 'world' }])
      const ref2 = store.getPending('s1')

      expect(ref1).not.toBe(ref2)
      expect(ref2).toHaveLength(1)
      expect(ref2[0].content).toBe('world')
    })

    it('addPending then removePending produces yet another new array', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('x'))
      const ref1 = store.getPending('s1')
      store.removePending('s1', 'x')
      const ref2 = store.getPending('s1')
      expect(ref1).not.toBe(ref2)
      expect(ref2).toHaveLength(0)
    })
  })

  // ── drain transition sequence ──────────────────────────────
  describe('drain transition sequence', () => {
    it('addPending → syncFromBackendQueue(drain) removes drained item', () => {
      // Simulates: user sends msg while AI running (addPending),
      // then queue_drain arrives (syncFromBackendQueue with drained item removed)
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('question A'))
      store.addPending('s1', createPendingMessage('question B'))
      expect(store.getPending('s1')).toHaveLength(2)

      // 'question A' is drained — backend queue only has 'question B'
      store.syncFromBackendQueue('s1', [{ text: 'question B' }])
      const pending = store.getPending('s1')
      expect(pending).toHaveLength(1)
      expect(pending[0].content).toBe('question B')
    })

    it('addPending → addPending → syncFromBackendQueue drains first, keeps second', () => {
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('first'))
      store.addPending('s1', createPendingMessage('second'))

      // Drain 'first' — only 'second' remains
      store.syncFromBackendQueue('s1', [{ text: 'second' }])
      const pending = store.getPending('s1')
      expect(pending).toHaveLength(1)
      expect(pending[0].content).toBe('second')
    })

    it('addPending → syncFromBackendQueue(empty) clears all pending', () => {
      // All drained or cancelled
      const store = usePendingStore()
      store.addPending('s1', createPendingMessage('orphan'))
      store.syncFromBackendQueue('s1', [])
      expect(store.getPending('s1')).toHaveLength(0)
    })
  })

  // ── Cross-session isolation ─────────────────────────────────
  describe('cross-session isolation', () => {
    it('adding to session A never affects session B', () => {
      const store = usePendingStore()
      store.addPending('A', createPendingMessage('A msg'))
      expect(store.getPending('B')).toEqual([])
    })

    it('removing from session A never affects session B', () => {
      const store = usePendingStore()
      store.addPending('A', createPendingMessage('shared text'))
      store.addPending('B', createPendingMessage('shared text'))
      store.removePending('A', 'shared text')
      expect(store.getPending('A')).toHaveLength(0)
      expect(store.getPending('B')).toHaveLength(1)
    })

    it('clearing session A never affects session B', () => {
      const store = usePendingStore()
      store.addPending('A', createPendingMessage('A msg'))
      store.addPending('B', createPendingMessage('B msg'))
      store.clearPending('A')
      expect(store.getPending('A')).toHaveLength(0)
      expect(store.getPending('B')).toHaveLength(1)
    })

    it('syncFromBackendQueue on session A never affects session B', () => {
      const store = usePendingStore()
      store.addPending('A', createPendingMessage('A msg'))
      store.addPending('B', createPendingMessage('B msg'))
      store.syncFromBackendQueue('A', [])
      expect(store.getPending('A')).toHaveLength(0)
      expect(store.getPending('B')).toHaveLength(1)
    })
  })
})
