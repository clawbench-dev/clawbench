import { describe, expect, it, vi, beforeEach } from 'vitest'
import { nextTick } from 'vue'
import { useTerminalFileDrop } from '@/composables/useTerminalFileDrop'

// ── Mocks ────────────────────────────────────────────────────

const mockApiGet = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => mockApiGet(...args),
}))

const mockHandleFolderDropExpanded = vi.fn()
vi.mock('@/composables/useFileUpload', () => ({
  useFileUpload: () => ({
    handleFolderDropExpanded: mockHandleFolderDropExpanded,
  }),
}))

const mockHasAttachDragData = vi.fn(() => false)
vi.mock('@/utils/attachDrag', () => ({
  hasAttachDragData: (dt: unknown) => mockHasAttachDragData(dt),
  // The composable only imports hasAttachDragData; the rest are stubbed so the
  // module shape matches the real one.
  ATTACH_DRAG_MIME: 'application/x-clawbench-attach',
}))

// ── Helpers ──────────────────────────────────────────────────

/** A drag event carrying OS files, mirroring what the browser delivers. */
function osFileDragEvent() {
  return {
    dataTransfer: { types: ['Files'], files: [] },
    preventDefault: vi.fn(),
  } as unknown as DragEvent
}

/** A drag event that is NOT an OS-file drag (e.g. a text selection drag). */
function nonFileDragEvent() {
  return {
    dataTransfer: { types: ['text/plain'], files: [] },
    preventDefault: vi.fn(),
  } as unknown as DragEvent
}

function setup(overrides: { sessionId?: string; fallbackDir?: string } = {}) {
  return useTerminalFileDrop({
    getSessionId: () => overrides.sessionId ?? 'sess-1',
    getFallbackDir: () => overrides.fallbackDir ?? '/launch/dir',
  })
}

/**
 * Flush the prefetch's `.then/.catch/.finally` chain.
 *
 * The composable keeps a request in flight until the whole chain settles, and
 * a `setTimeout(0)` drains every pending microtask — `nextTick` only drains one.
 */
function flushPrefetch() {
  return new Promise((resolve) => setTimeout(resolve, 0))
}

beforeEach(() => {
  mockApiGet.mockReset()
  mockHandleFolderDropExpanded.mockReset()
  mockHasAttachDragData.mockReset().mockReturnValue(false)
  mockApiGet.mockResolvedValue({ cwd: '/live/dir', hasSession: true, running: true })
})

// ── Tests ────────────────────────────────────────────────────

describe('useTerminalFileDrop — target directory', () => {
  it('uploads to the LIVE cwd fetched from the status endpoint', async () => {
    const drop = setup()
    await drop.onDragEnter(osFileDragEvent())
    // Let the prefetch settle before the drop, as it would in a real drag.
    await vi.waitFor(() => expect(mockApiGet).toHaveBeenCalled())

    drop.onDrop(osFileDragEvent())

    expect(mockHandleFolderDropExpanded).toHaveBeenCalledTimes(1)
    const [, dir] = mockHandleFolderDropExpanded.mock.calls[0]
    expect(dir).toBe('/live/dir')
  })

  it('falls back to the tab launch directory when the status fetch fails', async () => {
    mockApiGet.mockRejectedValue(new Error('offline'))
    const drop = setup({ fallbackDir: '/launch/dir' })
    drop.onDragEnter(osFileDragEvent())
    await flushPrefetch()

    drop.onDrop(osFileDragEvent())

    const [, dir] = mockHandleFolderDropExpanded.mock.calls[0]
    expect(dir).toBe('/launch/dir')
  })

  it('maps an empty fallback to "." so the upload lands in the project root', async () => {
    // A tab created without an explicit cwd carries '' — meaning "project
    // root". Passing '' would make the server use .clawbench/uploads/ instead.
    mockApiGet.mockRejectedValue(new Error('offline'))
    const drop = setup({ fallbackDir: '' })
    drop.onDragEnter(osFileDragEvent())
    await flushPrefetch()

    drop.onDrop(osFileDragEvent())

    const [, dir] = mockHandleFolderDropExpanded.mock.calls[0]
    expect(dir).toBe('.')
  })

  it('falls back when the status response carries no cwd', async () => {
    mockApiGet.mockResolvedValue({ hasSession: false })
    const drop = setup({ fallbackDir: '/launch/dir' })
    drop.onDragEnter(osFileDragEvent())
    await flushPrefetch()

    drop.onDrop(osFileDragEvent())

    const [, dir] = mockHandleFolderDropExpanded.mock.calls[0]
    expect(dir).toBe('/launch/dir')
  })
})

describe('useTerminalFileDrop — drag gating', () => {
  it('ignores internal attach drags so file→chat drags are not uploaded', async () => {
    mockHasAttachDragData.mockReturnValue(true)
    const drop = setup()

    drop.onDragEnter(osFileDragEvent())
    drop.onDrop(osFileDragEvent())

    expect(drop.dropActive.value).toBe(false)
    expect(mockHandleFolderDropExpanded).not.toHaveBeenCalled()
    expect(mockApiGet).not.toHaveBeenCalled()
  })

  it('ignores drags that carry no OS files', async () => {
    const drop = setup()

    drop.onDragEnter(nonFileDragEvent())
    drop.onDrop(nonFileDragEvent())

    expect(drop.dropActive.value).toBe(false)
    expect(mockHandleFolderDropExpanded).not.toHaveBeenCalled()
  })

  it('does not fetch the cwd when the session has not connected yet', async () => {
    // Without a session id the endpoint takes the "all sessions" branch and
    // returns no cwd, so the round-trip is pointless.
    const drop = setup({ sessionId: '' })

    drop.onDragEnter(osFileDragEvent())
    await nextTick()

    expect(mockApiGet).not.toHaveBeenCalled()
  })
})

describe('useTerminalFileDrop — cwd prefetch deduplication', () => {
  it('does not issue a request per dragover event', async () => {
    // dragover fires every ~50-350ms; an unguarded fetch would flood the endpoint.
    let resolveFetch: (v: unknown) => void = () => {}
    mockApiGet.mockReturnValue(new Promise((res) => { resolveFetch = res }))

    const drop = setup()
    drop.onDragEnter(osFileDragEvent())
    drop.onDragOver(osFileDragEvent())
    drop.onDragOver(osFileDragEvent())
    drop.onDragOver(osFileDragEvent())

    expect(mockApiGet).toHaveBeenCalledTimes(1)

    resolveFetch({ cwd: '/live/dir' })
    await vi.waitFor(() => expect(drop.dropActive.value).toBe(true))
  })

  it('re-fetches on a new drag so a cd between drops is picked up', async () => {
    const drop = setup()

    drop.onDragEnter(osFileDragEvent())
    await flushPrefetch()
    expect(mockApiGet).toHaveBeenCalledTimes(1)

    // A new drag resets the cached cwd — otherwise a `cd` performed between two
    // drops would upload the second batch into the first directory.
    mockApiGet.mockResolvedValue({ cwd: '/after/cd' })
    drop.onDragLeave()
    drop.onDragEnter(osFileDragEvent())
    await flushPrefetch()
    expect(mockApiGet).toHaveBeenCalledTimes(2)

    drop.onDrop(osFileDragEvent())
    const [, dir] = mockHandleFolderDropExpanded.mock.calls[0]
    expect(dir).toBe('/after/cd')
  })
})

describe('useTerminalFileDrop — overlay state', () => {
  it('pairs dragenter/dragleave via a counter', () => {
    const drop = setup()

    drop.onDragEnter(osFileDragEvent())
    drop.onDragEnter(osFileDragEvent())
    expect(drop.dropActive.value).toBe(true)

    // One leave must not clear it while another enter is outstanding — nested
    // elements each fire their own dragenter/dragleave.
    drop.onDragLeave()
    expect(drop.dropActive.value).toBe(true)

    drop.onDragLeave()
    expect(drop.dropActive.value).toBe(false)
  })

  it('clears the overlay on drop', () => {
    const drop = setup()

    drop.onDragEnter(osFileDragEvent())
    expect(drop.dropActive.value).toBe(true)

    drop.onDrop(osFileDragEvent())
    expect(drop.dropActive.value).toBe(false)
  })

  it('does not let dragleave drive the counter negative', () => {
    const drop = setup()

    drop.onDragLeave()
    drop.onDragLeave()
    expect(drop.dropActive.value).toBe(false)

    // A subsequent enter must still show the overlay.
    drop.onDragEnter(osFileDragEvent())
    expect(drop.dropActive.value).toBe(true)
  })
})
