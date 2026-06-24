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
