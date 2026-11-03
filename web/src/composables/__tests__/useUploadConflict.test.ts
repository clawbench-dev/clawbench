import { describe, it, expect, beforeEach } from 'vitest'
import {
  askUploadConflict,
  resolveUploadConflict,
  useUploadConflict,
  _resetUploadConflict,
} from '@/composables/useUploadConflict'

const conflict = (name: string) => ({ name, relpath: '', path: name })

describe('useUploadConflict', () => {
  beforeEach(() => {
    _resetUploadConflict()
  })

  it('resolves immediately with rename when there is nothing to ask', async () => {
    const { state } = useUploadConflict()
    await expect(askUploadConflict([])).resolves.toBe('rename')
    expect(state.value.visible).toBe(false)
  })

  it('exposes the conflicting files and settles with the chosen action', async () => {
    const { state } = useUploadConflict()
    const answer = askUploadConflict([conflict('a.txt'), conflict('b.txt')])

    expect(state.value.visible).toBe(true)
    expect(state.value.conflicts.map(c => c.path)).toEqual(['a.txt', 'b.txt'])

    resolveUploadConflict('overwrite')
    await expect(answer).resolves.toBe('overwrite')
    expect(state.value.visible).toBe(false)
    expect(state.value.conflicts).toEqual([])
  })

  it('cancels the previous question when a second batch asks', async () => {
    // Without this the first upload loop would await a promise nobody can ever
    // settle, leaving its batch silently hung for the rest of the session.
    const { state } = useUploadConflict()
    const first = askUploadConflict([conflict('a.txt')])
    const second = askUploadConflict([conflict('b.txt')])

    await expect(first).resolves.toBe('cancel')
    expect(state.value.visible).toBe(true)
    expect(state.value.conflicts.map(c => c.path)).toEqual(['b.txt'])

    resolveUploadConflict('rename')
    await expect(second).resolves.toBe('rename')
  })
})
