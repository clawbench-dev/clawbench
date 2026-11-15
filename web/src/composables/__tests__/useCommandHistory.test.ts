import { describe, expect, it, vi, beforeEach } from 'vitest'

const mockApiGet = vi.fn()
const mockApiPost = vi.fn()
const mockApiDelete = vi.fn()

vi.mock('@/utils/api', () => ({
  apiGet: (...args: any[]) => mockApiGet(...args),
  apiPost: (...args: any[]) => mockApiPost(...args),
  apiDelete: (...args: any[]) => mockApiDelete(...args),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { w: vi.fn(), d: vi.fn(), i: vi.fn(), e: vi.fn() },
}))

import {
  useCommandHistory,
  _resetCommandHistoryForTesting,
  type TerminalCommand,
} from '@/composables/useCommandHistory'

function makeCommand(overrides: Partial<TerminalCommand> = {}): TerminalCommand {
  return {
    id: 1,
    command: 'git status',
    use_count: 1,
    last_used_at: '2026-01-01 00:00:00',
    project_path: '',
    project_only: false,
    ...overrides,
  }
}

beforeEach(() => {
  mockApiGet.mockReset()
  mockApiPost.mockReset()
  mockApiDelete.mockReset()
  _resetCommandHistoryForTesting()
})

describe('useCommandHistory', () => {
  it('loads the list', async () => {
    mockApiGet.mockResolvedValue([makeCommand()])
    const { items, fetchHistory } = useCommandHistory()

    await fetchHistory()

    expect(mockApiGet).toHaveBeenCalledWith('/api/terminal/history?limit=100')
    expect(items.value).toHaveLength(1)
    expect(items.value[0].command).toBe('git status')
  })

  it('passes the search term through', async () => {
    mockApiGet.mockResolvedValue([])
    const { fetchHistory } = useCommandHistory()

    await fetchHistory('git')

    expect(mockApiGet).toHaveBeenCalledWith('/api/terminal/history?q=git&limit=100')
  })

  it('keeps the previous list when the request fails', async () => {
    mockApiGet.mockResolvedValue([makeCommand()])
    const { items, fetchHistory } = useCommandHistory()
    await fetchHistory()

    mockApiGet.mockRejectedValue(new Error('offline'))
    await fetchHistory()

    expect(items.value).toHaveLength(1)
  })

  it('syncs from the shell history files once per TTL', async () => {
    mockApiPost.mockResolvedValue({ added: 3 })
    mockApiGet.mockResolvedValue([])
    const { syncFromShells } = useCommandHistory()

    expect(await syncFromShells()).toBe(3)
    expect(await syncFromShells()).toBe(0)
    expect(mockApiPost).toHaveBeenCalledTimes(1)

    // force bypasses the TTL (the panel's manual "import" action)
    expect(await syncFromShells(true)).toBe(3)
    expect(mockApiPost).toHaveBeenCalledTimes(2)
  })

  it('refresh imports first, then loads the list', async () => {
    mockApiPost.mockResolvedValue({ added: 2 })
    mockApiGet.mockResolvedValue([makeCommand()])
    const { refresh, items } = useCommandHistory()

    await refresh()

    expect(mockApiPost).toHaveBeenCalledTimes(1)
    expect(mockApiGet).toHaveBeenCalledTimes(1)
    expect(items.value).toHaveLength(1)
  })

  it('removes one entry locally after the server confirms', async () => {
    mockApiGet.mockResolvedValue([makeCommand({ id: 1 }), makeCommand({ id: 2, command: 'ls' })])
    const { items, fetchHistory, remove } = useCommandHistory()
    await fetchHistory()

    mockApiDelete.mockResolvedValue({ success: true })
    expect(await remove(1)).toBe(true)

    expect(mockApiDelete).toHaveBeenCalledWith('/api/terminal/history/1')
    expect(items.value).toHaveLength(1)
    expect(items.value[0].id).toBe(2)
  })

  it('keeps the entry when the delete fails', async () => {
    mockApiGet.mockResolvedValue([makeCommand({ id: 1 })])
    const { items, fetchHistory, remove } = useCommandHistory()
    await fetchHistory()

    mockApiDelete.mockRejectedValue(new Error('nope'))
    expect(await remove(1)).toBe(false)
    expect(items.value).toHaveLength(1)
  })

  it('clears everything', async () => {
    mockApiGet.mockResolvedValue([makeCommand()])
    const { items, fetchHistory, clear } = useCommandHistory()
    await fetchHistory()

    mockApiDelete.mockResolvedValue({ success: true })
    expect(await clear()).toBe(true)
    expect(mockApiDelete).toHaveBeenCalledWith('/api/terminal/history')
    expect(items.value).toHaveLength(0)
  })

  it('shares state between callers', async () => {
    mockApiGet.mockResolvedValue([makeCommand()])
    const a = useCommandHistory()
    const b = useCommandHistory()

    await a.fetchHistory()

    expect(b.items.value).toHaveLength(1)
  })
})
