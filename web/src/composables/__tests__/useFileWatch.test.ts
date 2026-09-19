import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, ref, nextTick } from 'vue'

// Mock the store — the composable calls loadFiles / loadGitBranch on dir_change.
const mockLoadFiles = vi.fn()
const mockLoadGitBranch = vi.fn()
// Mutable so tests can exercise the "project root not loaded yet" path.
// Hoisted: vi.mock runs before module-scope declarations.
const mockStoreState = vi.hoisted(() => ({ projectRoot: '/p' }))
vi.mock('@/stores/app.ts', () => ({
  store: {
    loadFiles: (...args: unknown[]) => mockLoadFiles(...args),
    loadGitBranch: () => mockLoadGitBranch(),
    state: mockStoreState,
  },
}))

// Mock file refresh — file_change drives refreshCurrentFile, guarded by
// wasRecentlySaved so our own saves don't cause a refresh flash.
const mockRefreshCurrentFile = vi.fn()
const mockWasRecentlySaved = vi.fn(() => false)
vi.mock('@/composables/useFileRefresh.ts', () => ({
  refreshCurrentFile: (...args: unknown[]) => mockRefreshCurrentFile(...args),
  wasRecentlySaved: (...args: unknown[]) => mockWasRecentlySaved(...args),
}))

// Media watch — the composable reads the on-screen image list and bumps a
// version per changed path. The real module touches the DOM, so it is stubbed.
// vi.mock is hoisted above every other statement (and above the vue import), so
// the factory cannot close over module-scope bindings. It builds the ref itself
// and publishes it on a hoisted container for the tests to drive.
const mediaMocks = vi.hoisted(() => ({
  bumpMediaVersion: vi.fn(() => true),
  ensureMediaObserver: vi.fn(),
  mediaPaths: null as { value: string[] } | null,
}))
const mockBumpMediaVersion = mediaMocks.bumpMediaVersion
const mockEnsureMediaObserver = mediaMocks.ensureMediaObserver
vi.mock('@/composables/useMediaWatch.ts', async () => {
  const { ref } = await import('vue')
  mediaMocks.mediaPaths = ref<string[]>([])
  return {
    bumpMediaVersion: (...args: unknown[]) => mediaMocks.bumpMediaVersion(...args),
    ensureMediaObserver: () => mediaMocks.ensureMediaObserver(),
    mediaPaths: mediaMocks.mediaPaths,
    setMediaProjectRoot: () => {},
  }
})
/** The ref the composable watches, created by the mock factory above. */
function mockMediaPaths(): { value: string[] } {
  return mediaMocks.mediaPaths as { value: string[] }
}

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import { useFileWatch } from '@/composables/useFileWatch'

let mockWsInstances: MockWebSocket[] = []

class MockWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3

  url: string
  readyState: number = MockWebSocket.CONNECTING
  onopen: ((ev: Event) => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onclose: ((ev: CloseEvent) => void) | null = null
  onerror: ((ev: Event) => void) | null = null
  sentMessages: string[] = []

  constructor(url: string) {
    this.url = url
    mockWsInstances.push(this)
  }

  send(data: string) {
    this.sentMessages.push(data)
  }

  close() {
    if (this.readyState === MockWebSocket.CLOSED) return
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.(new CloseEvent('close'))
  }

  /** Server → client message. */
  receive(data: object) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }))
  }

  /** Server → client non-JSON payload (malformed frame). */
  receiveRaw(data: string) {
    this.onmessage?.(new MessageEvent('message', { data }))
  }

  simulateOpen() {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.(new Event('open'))
  }

  simulateError() {
    this.onerror?.(new Event('error'))
  }
}

function latestWs(): MockWebSocket {
  return mockWsInstances[mockWsInstances.length - 1]
}

/** Parse the sent frames, skipping any non-JSON noise. */
function sentFrames(ws: MockWebSocket): Record<string, unknown>[] {
  return ws.sentMessages.map((m) => JSON.parse(m))
}

interface Harness {
  wrapper: ReturnType<typeof mount>
  fileManagerOpen: ReturnType<typeof ref<boolean>>
  currentDir: ReturnType<typeof ref<string>>
  currentFile: ReturnType<typeof ref<{ path: string } | null>>
}

function setupWatch(initial: { open?: boolean; dir?: string; file?: string | null } = {}): Harness {
  const fileManagerOpen = ref(initial.open ?? true)
  const currentDir = ref(initial.dir ?? '')
  const currentFile = ref<{ path: string } | null>(initial.file ? { path: initial.file } : null)

  const TestComponent = defineComponent({
    setup() {
      useFileWatch({ fileManagerOpen, currentDir, currentFile })
      return () => h('div')
    },
  })
  const wrapper = mount(TestComponent)
  return { wrapper, fileManagerOpen, currentDir, currentFile }
}

/** Bring a socket to the "server sent connected" state. */
function connectAndHandshake(ws: MockWebSocket) {
  ws.simulateOpen()
  ws.receive({ type: 'connected', clientId: 'c-1' })
}

describe('useFileWatch', () => {
  let originalWebSocket: typeof WebSocket

  beforeEach(() => {
    mockWsInstances = []
    mockLoadFiles.mockReset()
    mockLoadGitBranch.mockReset()
    mockRefreshCurrentFile.mockReset()
    mockWasRecentlySaved.mockReset()
    mockWasRecentlySaved.mockReturnValue(false)
    mockBumpMediaVersion.mockReset()
    mockBumpMediaVersion.mockReturnValue(true)
    mockEnsureMediaObserver.mockReset()
    mockMediaPaths().value = []
    originalWebSocket = globalThis.WebSocket
    globalThis.WebSocket = MockWebSocket as unknown as typeof WebSocket
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    globalThis.WebSocket = originalWebSocket
  })

  it('connects to the WebSocket endpoint with dir and file params', () => {
    const { wrapper } = setupWatch({ dir: 'src', file: 'src/a.ts' })
    expect(mockWsInstances).toHaveLength(1)
    const url = latestWs().url
    expect(url).toContain('/api/file/watch/ws')
    expect(url).toContain('dir=src')
    expect(url).toContain('file=src%2Fa.ts')
    wrapper.unmount()
  })

  it('uses wss:// when the page is served over https', () => {
    const originalProtocol = window.location.protocol
    Object.defineProperty(window, 'location', {
      value: { ...window.location, protocol: 'https:' },
      writable: true,
      configurable: true,
    })
    const { wrapper } = setupWatch()
    expect(latestWs().url.startsWith('wss://')).toBe(true)
    wrapper.unmount()
    Object.defineProperty(window, 'location', {
      value: { ...window.location, protocol: originalProtocol },
      writable: true,
      configurable: true,
    })
  })

  it('does not connect while inactive, and connects when the file manager opens', async () => {
    const { wrapper, fileManagerOpen } = setupWatch({ open: false })
    expect(mockWsInstances).toHaveLength(0)

    fileManagerOpen.value = true
    await nextTick()
    expect(mockWsInstances).toHaveLength(1)

    fileManagerOpen.value = false
    await nextTick()
    expect(latestWs().readyState).toBe(MockWebSocket.CLOSED)
    wrapper.unmount()
  })

  it('sends a watch frame after the connected handshake', () => {
    const { wrapper } = setupWatch({ dir: 'lib', file: 'lib/b.ts' })
    const ws = latestWs()
    connectAndHandshake(ws)

    const frames = sentFrames(ws)
    expect(frames).toContainEqual({ type: 'watch', dir: 'lib', file: 'lib/b.ts', files: [] })
    wrapper.unmount()
  })

  it('refreshes the directory listing and git branch on dir_change', () => {
    const { wrapper } = setupWatch({ dir: 'src' })
    const ws = latestWs()
    connectAndHandshake(ws)

    ws.receive({ type: 'dir_change', path: '/p/src/new.txt' })

    expect(mockLoadFiles).toHaveBeenCalledWith('src', false, 0, true)
    expect(mockLoadGitBranch).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('refreshes the open file on file_change', () => {
    const { wrapper } = setupWatch({ dir: 'src', file: 'src/a.ts' })
    const ws = latestWs()
    connectAndHandshake(ws)

    ws.receive({ type: 'file_change', path: '/p/src/a.ts' })

    expect(mockRefreshCurrentFile).toHaveBeenCalledWith({ clearOnError: true, loadDir: true })
    wrapper.unmount()
  })

  it('skips the refresh when the change came from our own save', () => {
    mockWasRecentlySaved.mockReturnValue(true)
    const { wrapper } = setupWatch({ dir: 'src', file: 'src/a.ts' })
    const ws = latestWs()
    connectAndHandshake(ws)

    ws.receive({ type: 'file_change', path: '/p/src/a.ts' })

    expect(mockRefreshCurrentFile).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('ignores file_change when no file is open', () => {
    const { wrapper } = setupWatch({ dir: 'src', file: null })
    const ws = latestWs()
    connectAndHandshake(ws)

    ws.receive({ type: 'file_change', path: '/p/src/a.ts' })

    expect(mockRefreshCurrentFile).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('answers a server ping with a pong', () => {
    const { wrapper } = setupWatch()
    const ws = latestWs()
    connectAndHandshake(ws)

    ws.receive({ type: 'ping' })

    expect(sentFrames(ws)).toContainEqual({ type: 'pong' })
    wrapper.unmount()
  })

  it('keeps the channel open on a server error frame', () => {
    const { wrapper } = setupWatch()
    const ws = latestWs()
    connectAndHandshake(ws)

    ws.receive({ type: 'error', code: 'AccessDenied', message: 'nope' })

    expect(ws.readyState).toBe(MockWebSocket.OPEN)
    // Still usable afterwards.
    ws.receive({ type: 'dir_change', path: '/p/x' })
    expect(mockLoadFiles).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('ignores a malformed frame without closing the connection', () => {
    const { wrapper } = setupWatch()
    const ws = latestWs()
    connectAndHandshake(ws)

    ws.receiveRaw('not json')

    expect(ws.readyState).toBe(MockWebSocket.OPEN)
    wrapper.unmount()
  })

  it('ignores an unknown message type', () => {
    const { wrapper } = setupWatch()
    const ws = latestWs()
    connectAndHandshake(ws)

    ws.receive({ type: 'something_new', path: '/p/x' })

    expect(ws.readyState).toBe(MockWebSocket.OPEN)
    expect(mockLoadFiles).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('re-targets the watch when the directory changes', async () => {
    const { wrapper, currentDir } = setupWatch({ dir: 'src' })
    const ws = latestWs()
    connectAndHandshake(ws)
    ws.sentMessages = []

    currentDir.value = 'lib'
    await nextTick()

    expect(sentFrames(ws)).toContainEqual({ type: 'watch', dir: 'lib', file: '', files: [] })
    wrapper.unmount()
  })

  // ── Media watching ──────────────────────────────────────────────────────

  it('registers on-screen media paths in the watch frame', () => {
    const { wrapper } = setupWatch({ dir: 'src' })
    const ws = latestWs()
    connectAndHandshake(ws)
    ws.sentMessages = []

    mockMediaPaths().value = ['assets/a.png', 'assets/b.png']
    return nextTick().then(() => {
      expect(sentFrames(ws)).toContainEqual({
        type: 'watch',
        dir: 'src',
        file: '',
        files: ['assets/a.png', 'assets/b.png'],
      })
      wrapper.unmount()
    })
  })

  it('connects when media is on screen even with no file manager open', async () => {
    // The chat column can render local images with the file panel closed.
    mockMediaPaths().value = ['assets/a.png']
    const { wrapper } = setupWatch({ open: false })
    await nextTick()

    expect(mockWsInstances).toHaveLength(1)
    wrapper.unmount()
  })

  it('bumps the media version on file_change for a non-open file', () => {
    const { wrapper } = setupWatch({ dir: 'src', file: 'src/a.ts' })
    const ws = latestWs()
    connectAndHandshake(ws)

    ws.receive({ type: 'file_change', path: '/p/assets/diagram.png' })

    expect(mockBumpMediaVersion).toHaveBeenCalledWith('/p/assets/diagram.png')
    // A sibling image changing must NOT refresh the open file's content.
    expect(mockRefreshCurrentFile).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('refreshes the open file when the change names it', () => {
    const { wrapper } = setupWatch({ dir: 'src', file: 'src/a.ts' })
    const ws = latestWs()
    connectAndHandshake(ws)

    ws.receive({ type: 'file_change', path: '/p/src/a.ts' })

    expect(mockBumpMediaVersion).toHaveBeenCalledWith('/p/src/a.ts')
    expect(mockRefreshCurrentFile).toHaveBeenCalledWith({ clearOnError: true, loadDir: true })
    wrapper.unmount()
  })

  it('refreshes when the project root is unknown (cannot rule out a match)', () => {
    // With no root, an absolute event path cannot be related to a relative
    // currentFile.path. Refreshing is the safe choice: a needless refresh only
    // flashes, while skipping one reintroduces the missed-update bug.
    const savedRoot = mockStoreState.projectRoot
    mockStoreState.projectRoot = ''
    const { wrapper } = setupWatch({ dir: 'src', file: 'a.ts' })
    const ws = latestWs()
    connectAndHandshake(ws)

    ws.receive({ type: 'file_change', path: '/p/src/a.ts' })

    expect(mockRefreshCurrentFile).toHaveBeenCalledWith({ clearOnError: true, loadDir: true })
    mockStoreState.projectRoot = savedRoot
    wrapper.unmount()
  })

  it('does not bump media when no path is present in the frame', () => {
    const { wrapper } = setupWatch({ dir: 'src', file: 'src/a.ts' })
    const ws = latestWs()
    connectAndHandshake(ws)

    ws.receive({ type: 'file_change' })

    expect(mockBumpMediaVersion).not.toHaveBeenCalled()
    // No path means we cannot tell it apart from our own file — refresh it.
    expect(mockRefreshCurrentFile).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('starts the media observer on mount', () => {
    const { wrapper } = setupWatch()
    expect(mockEnsureMediaObserver).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('applies exponential backoff across consecutive failed connects', () => {
    const { wrapper } = setupWatch()

    // Deliberately do NOT complete the handshake: a successful `connected`
    // resets the backoff (correctly), so escalation is only observable across
    // consecutive failures.
    expect(mockWsInstances).toHaveLength(1)

    // First failure → base delay.
    latestWs().close()
    vi.advanceTimersByTime(1999)
    expect(mockWsInstances).toHaveLength(1)
    vi.advanceTimersByTime(1)
    expect(mockWsInstances).toHaveLength(2)

    // Second failure → 2x.
    latestWs().close()
    vi.advanceTimersByTime(3999)
    expect(mockWsInstances).toHaveLength(2)
    vi.advanceTimersByTime(1)
    expect(mockWsInstances).toHaveLength(3)

    // Third failure → 4x.
    latestWs().close()
    vi.advanceTimersByTime(7999)
    expect(mockWsInstances).toHaveLength(3)
    vi.advanceTimersByTime(1)
    expect(mockWsInstances).toHaveLength(4)

    // Fourth failure → capped at maxDelay (15000), not 16000.
    latestWs().close()
    vi.advanceTimersByTime(14999)
    expect(mockWsInstances).toHaveLength(4)
    vi.advanceTimersByTime(1)
    expect(mockWsInstances).toHaveLength(5)

    wrapper.unmount()
  })

  it('resets the backoff after a successful handshake', () => {
    const { wrapper } = setupWatch()

    // Two failures push the delay up to 4000.
    latestWs().close()
    vi.advanceTimersByTime(2000)
    latestWs().close()
    vi.advanceTimersByTime(4000)
    expect(mockWsInstances).toHaveLength(3)

    // A successful connect must reset it back to the base delay.
    connectAndHandshake(latestWs())
    latestWs().close()
    vi.advanceTimersByTime(1999)
    expect(mockWsInstances).toHaveLength(3)
    vi.advanceTimersByTime(1)
    expect(mockWsInstances).toHaveLength(4)

    wrapper.unmount()
  })

  it('does not reconnect after an intentional disconnect', async () => {
    const { wrapper, fileManagerOpen } = setupWatch()
    connectAndHandshake(latestWs())

    fileManagerOpen.value = false // triggers disconnect()
    await nextTick()
    vi.advanceTimersByTime(60000)

    expect(mockWsInstances).toHaveLength(1)
    wrapper.unmount()
  })

  it('closes the socket on unmount without scheduling a reconnect', () => {
    const { wrapper } = setupWatch()
    const ws = latestWs()
    connectAndHandshake(ws)

    wrapper.unmount()

    expect(ws.readyState).toBe(MockWebSocket.CLOSED)
    vi.advanceTimersByTime(60000)
    expect(mockWsInstances).toHaveLength(1)
  })

  it('schedules exactly one reconnect when error is followed by close', () => {
    const { wrapper } = setupWatch()
    const ws = latestWs()
    connectAndHandshake(ws)

    // A failed handshake fires onerror then onclose; only the close handler
    // schedules, so we must not end up with two sockets.
    ws.simulateError()
    ws.close()
    vi.advanceTimersByTime(2000)

    expect(mockWsInstances).toHaveLength(2)
    vi.advanceTimersByTime(60000)
    expect(mockWsInstances).toHaveLength(2)
    wrapper.unmount()
  })

  it('forces a reconnect when the socket goes silent past the stale window', () => {
    const { wrapper } = setupWatch()
    connectAndHandshake(latestWs())

    // No messages at all. The stale check runs every 15s and trips once the
    // last message is older than 60s, so the first firing that can trip is at
    // t=75s (at t=60s the age is exactly 60s, not yet over the window).
    vi.advanceTimersByTime(74000)
    expect(latestWs().readyState).toBe(MockWebSocket.OPEN)

    vi.advanceTimersByTime(1000)
    expect(latestWs().readyState).toBe(MockWebSocket.CLOSED)

    // The stale check closed the socket, which scheduled a reconnect.
    vi.advanceTimersByTime(2000)
    expect(mockWsInstances).toHaveLength(2)
    wrapper.unmount()
  })

  it('keeps the socket alive while pings keep arriving', () => {
    const { wrapper } = setupWatch()
    const ws = latestWs()
    connectAndHandshake(ws)

    // Pings every 30s refresh lastMessageAt, so the stale check never trips.
    for (let i = 0; i < 5; i++) {
      vi.advanceTimersByTime(30000)
      ws.receive({ type: 'ping' })
    }

    expect(ws.readyState).toBe(MockWebSocket.OPEN)
    expect(mockWsInstances).toHaveLength(1)
    wrapper.unmount()
  })
})
