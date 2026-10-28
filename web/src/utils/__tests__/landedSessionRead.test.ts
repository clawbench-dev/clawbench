import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

/**
 * markLandedSessionRead closes the gap the "load history ≠ mark read" split
 * (360241f44) opened for LANDING paths: a project switch resolves identity in
 * initSessionFromAPI and the remounted panel's first loadHistory fetches the
 * conversation directly, so no switchSession runs and the unread badge of the
 * session now on screen would stay lit until the user switched away and back.
 *
 * These pin the two guards and the ordering, which is where the defect lives:
 * the naive version marks read for a cross-project jump too (403, because it
 * has no project_path) and refreshes the list before the read settles (the
 * badge reappears from a stale list response).
 */

const readMock = vi.fn()
vi.mock('@/utils/sessionRead', () => ({
  markSessionRead: (...args: unknown[]) => readMock(...args),
}))

import { markLandedSessionRead } from '@/utils/landedSessionRead'

const base = {
  sessionId: 'sess-1',
  isWideScreen: false,
  chatCollapsed: false,
  activeTab: 'chat',
}

describe('markLandedSessionRead', () => {
  beforeEach(() => {
    readMock.mockReset().mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('marks the landed-on session read when the chat panel is on screen', async () => {
    const onMarked = vi.fn()
    await markLandedSessionRead({ ...base, onMarked })

    expect(readMock).toHaveBeenCalledTimes(1)
    // No project_path: the landing happens AFTER setProject() flipped the
    // cookie, so the backend's cookie fallback already proves ownership.
    expect(readMock.mock.calls[0]).toEqual(['sess-1'])
    expect(onMarked).toHaveBeenCalledTimes(1)
  })

  it('does nothing without a session id', async () => {
    const onMarked = vi.fn()
    await markLandedSessionRead({ ...base, sessionId: '', onMarked })

    expect(readMock).not.toHaveBeenCalled()
    expect(onMarked).not.toHaveBeenCalled()
  })

  it('skips a cross-project jump — Phase 7 marks that read with project_path', async () => {
    // Marking here as well would issue the identical POST without project_path
    // while the session belongs to the project just left, i.e. a guaranteed
    // 403 plus a warning log for a read that already happened correctly.
    const onMarked = vi.fn()
    await markLandedSessionRead({ ...base, pendingSessionId: 'sess-1', onMarked })

    expect(readMock).not.toHaveBeenCalled()
    expect(onMarked).not.toHaveBeenCalled()
  })

  it('does not clear the badge when the landing panel is not the chat', async () => {
    // Narrow layout with the file manager restored: the user is not looking at
    // the conversation, so its badge is still telling the truth.
    const onMarked = vi.fn()
    await markLandedSessionRead({ ...base, activeTab: 'browse', onMarked })

    expect(readMock).not.toHaveBeenCalled()
    expect(onMarked).not.toHaveBeenCalled()
  })

  it('does not clear the badge when the wide-screen chat column is collapsed', async () => {
    const onMarked = vi.fn()
    await markLandedSessionRead({
      ...base,
      isWideScreen: true,
      chatCollapsed: true,
      activeTab: 'browse',
      onMarked,
    })

    expect(readMock).not.toHaveBeenCalled()
    expect(onMarked).not.toHaveBeenCalled()
  })

  it('clears the badge on a wide screen with the chat column open, whatever the left tab', async () => {
    const onMarked = vi.fn()
    await markLandedSessionRead({
      ...base,
      isWideScreen: true,
      chatCollapsed: false,
      activeTab: 'terminal',
      onMarked,
    })

    expect(readMock).toHaveBeenCalledTimes(1)
    expect(onMarked).toHaveBeenCalledTimes(1)
  })

  it('refreshes the list only after the read settles', async () => {
    // The list response carries the per-session unread count, so refreshing
    // first reads the pre-read state and re-lights the badge this call exists
    // to clear — the same ordering requirement switchSession documents.
    let releaseRead: (() => void) | null = null
    readMock.mockImplementation(() => new Promise<void>((r) => { releaseRead = r }))
    const onMarked = vi.fn()

    const done = markLandedSessionRead({ ...base, onMarked })

    await Promise.resolve()
    expect(onMarked, 'list refreshed while the read was still in flight').not.toHaveBeenCalled()

    releaseRead!()
    await done
    expect(onMarked).toHaveBeenCalledTimes(1)
  })
})
