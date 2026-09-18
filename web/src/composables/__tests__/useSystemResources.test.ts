import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick, defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

// ── Controllable useGlobalEvents fake ──
// The composable wires a module-level `watch(connected, ...)` and registers an
// onEvent handler, so the fake must expose a real ref plus a handler registry
// and a record of everything the composable sends.
// A mutable holder rather than module constants: the composable registers a
// module-scope `watch(connected, ...)`, so a ref reused across tests would be
// observed by every previously-loaded module instance too. Replacing the ref in
// beforeEach orphans those stale watchers (they hold the old ref).
const holder = {
  connected: ref(false),
  handlers: [] as Array<(event: string, data: unknown) => void>,
  sent: [] as any[],
}

vi.mock('@/composables/useGlobalEvents', () => ({
  useGlobalEvents: () => ({
    connected: holder.connected,
    onEvent: (h: (event: string, data: unknown) => void) => {
      holder.handlers.push(h)
      return () => {
        const i = holder.handlers.indexOf(h)
        if (i >= 0) holder.handlers.splice(i, 1)
      }
    },
    sendWsMessage: (m: any) => holder.sent.push(m),
  }),
}))

// The composable must not poll anymore — this asserts that directly.
const mockFetch = vi.fn()
globalThis.fetch = mockFetch as any

const listeners: Record<string, EventListener> = {}
const origAdd = document.addEventListener
const origRemove = document.removeEventListener

/** Emit a server event to every registered handler. */
function emitEvent(event: string, data: unknown) {
  for (const h of [...holder.handlers]) h(event, data)
}

/** Connect the fake socket and let the module-level watcher run. */
async function connectSocket() {
  holder.connected.value = true
  await nextTick()
}

/** The last metrics_preference the composable sent, or undefined. */
function lastDeclaration() {
  return [...holder.sent].reverse().find((m) => m?.type === 'metrics_preference')
}

/** All metrics_preference messages sent so far. */
function declarations() {
  return holder.sent.filter((m) => m?.type === 'metrics_preference')
}

const SAMPLE_RESOURCES = {
  cpu: { percent: 25.5, core_count: 4 },
  memory: { used: 4000000000, total: 8000000000, percent: 50 },
  disk: { used: 50000000000, total: 200000000000, percent: 25 },
  disk_io: { read_rate: 1, write_rate: 2 },
  network: { upload_rate: 1024, download_rate: 51200 },
  load: { load1: 1.0, load5: 0.8, load15: 0.6 },
}

/** Reset the module-level refcounts between tests by re-importing fresh. */
async function freshComposable() {
  vi.resetModules()
  const mod = await import('../useSystemResources')
  return mod
}

beforeEach(() => {
  // A brand-new ref orphans watchers registered by earlier module instances.
  holder.connected = ref(false)
  holder.handlers = []
  holder.sent = []
  mockFetch.mockReset()
  document.addEventListener = vi.fn((event: string, handler: EventListener) => {
    listeners[event] = handler
  }) as any
  document.removeEventListener = vi.fn((event: string) => {
    delete listeners[event]
  }) as any
})

afterEach(() => {
  document.addEventListener = origAdd
  document.removeEventListener = origRemove
})

describe('useSystemResources (WS push)', () => {
  it('exposes the composable, the shared resources ref and the four methods', async () => {
    const { useSystemResources } = await freshComposable()
    const api = useSystemResources()
    expect(typeof useSystemResources).toBe('function')
    expect(api.resources.value).toBeDefined()
    for (const m of ['startPolling', 'stopPolling', 'startBackgroundPolling', 'stopBackgroundPolling']) {
      expect(typeof (api as any)[m]).toBe('function')
    }
  })

  it('declares the foreground rate when the panel opens', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()
    const { startPolling } = useSystemResources()

    startPolling()

    expect(lastDeclaration()).toEqual({
      type: 'metrics_preference',
      metrics_enabled: true,
      metrics_interval_ms: 1000,
    })
  })

  it('declares the background rate for background consumers', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()
    const { startBackgroundPolling } = useSystemResources()

    startBackgroundPolling()

    expect(lastDeclaration()).toEqual({
      type: 'metrics_preference',
      metrics_enabled: true,
      metrics_interval_ms: 5000,
    })
  })

  it('prefers the foreground rate when both are active', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()
    const { startPolling, startBackgroundPolling } = useSystemResources()

    startBackgroundPolling()
    startPolling()

    expect(lastDeclaration().metrics_interval_ms).toBe(1000)
  })

  it('downgrades to the background rate when the foreground consumer stops', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()
    const { startPolling, stopPolling, startBackgroundPolling } = useSystemResources()

    startBackgroundPolling()
    startPolling()
    stopPolling()

    expect(lastDeclaration()).toEqual({
      type: 'metrics_preference',
      metrics_enabled: true,
      metrics_interval_ms: 5000,
    })
  })

  it('disables the push once the last consumer stops', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()
    const { startPolling, stopPolling } = useSystemResources()

    startPolling()
    stopPolling()

    expect(lastDeclaration()).toEqual({ type: 'metrics_preference', metrics_enabled: false })
  })

  it('does not declare while disconnected, then declares on connect', async () => {
    const { useSystemResources } = await freshComposable()
    const { startPolling } = useSystemResources()

    // Called before the socket is up — the declaration must not be sent.
    startPolling()
    expect(declarations()).toHaveLength(0)

    await connectSocket()
    expect(lastDeclaration()).toEqual({
      type: 'metrics_preference',
      metrics_enabled: true,
      metrics_interval_ms: 1000,
    })
  })

  it('writes every metric field from a system_resources event', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()
    const { resources } = useSystemResources()

    emitEvent('system_resources', SAMPLE_RESOURCES)

    expect(resources.value.cpu.percent).toBe(25.5)
    expect(resources.value.cpu.core_count).toBe(4)
    expect(resources.value.memory.percent).toBe(50)
    expect(resources.value.disk.percent).toBe(25)
    expect(resources.value.disk_io.read_rate).toBe(1)
    expect(resources.value.network.download_rate).toBe(51200)
    expect(resources.value.load.load1).toBe(1.0)
  })

  it('ignores unrelated events', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()
    const { resources } = useSystemResources()
    const before = JSON.stringify(resources.value)

    emitEvent('task_update', { task_id: '1' })

    expect(JSON.stringify(resources.value)).toBe(before)
  })

  it('ignores a system_resources event with no payload', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()
    const { resources } = useSystemResources()
    const before = JSON.stringify(resources.value)

    emitEvent('system_resources', undefined)

    expect(JSON.stringify(resources.value)).toBe(before)
  })

  it('stops the push while the tab is hidden and resumes when visible', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()
    const { startPolling } = useSystemResources()
    startPolling()

    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    listeners['visibilitychange']?.(new Event('visibilitychange'))
    expect(lastDeclaration()).toEqual({ type: 'metrics_preference', metrics_enabled: false })

    hidden.mockReturnValue(false)
    listeners['visibilitychange']?.(new Event('visibilitychange'))
    expect(lastDeclaration()).toEqual({
      type: 'metrics_preference',
      metrics_enabled: true,
      metrics_interval_ms: 1000,
    })
    hidden.mockRestore()
  })

  it('attaches the visibility listener on first use and removes it after the last stop', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()
    const { startPolling, stopPolling } = useSystemResources()

    startPolling()
    expect(listeners['visibilitychange']).toBeDefined()

    stopPolling()
    expect(listeners['visibilitychange']).toBeUndefined()
  })

  it('shares one resources ref across instances', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()
    const a = useSystemResources()
    const b = useSystemResources()

    emitEvent('system_resources', SAMPLE_RESOURCES)

    expect(a.resources.value).toBe(b.resources.value)
    expect(b.resources.value.cpu.percent).toBe(25.5)
  })

  it('does not re-send an unchanged declaration on repeated calls', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()
    const { startPolling } = useSystemResources()

    startPolling()
    const afterFirst = declarations().length
    startPolling()
    startPolling()

    expect(declarations()).toHaveLength(afterFirst)
  })

  it('never polls over HTTP, even over time', async () => {
    // Fake timers MUST be installed before startPolling(): a timer created
    // under real timers is not captured retroactively, so advancing fake time
    // would never fire a reintroduced poll and the test would pass vacuously.
    vi.useFakeTimers()
    try {
      const { useSystemResources } = await freshComposable()
      await connectSocket()
      const { startPolling, stopPolling } = useSystemResources()

      startPolling()
      emitEvent('system_resources', SAMPLE_RESOURCES)

      // Advance well past the old 1s/5s poll intervals.
      vi.advanceTimersByTime(30000)
      await nextTick()

      stopPolling()
      expect(mockFetch).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('useSystemResources mount lifecycle', () => {
  it('cleans up the declaration when the owning component unmounts', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()

    const TestComponent = defineComponent({
      setup() {
        const { startPolling, startBackgroundPolling } = useSystemResources()
        startPolling()
        startBackgroundPolling()
        return () => h('div')
      },
    })
    const wrapper = mount(TestComponent)
    await nextTick()

    const beforeUnmount = declarations().length
    wrapper.unmount()

    // onUnmounted must release BOTH refcounts, so the last declaration is a
    // disable (otherwise the server would keep sampling for a dead component).
    expect(declarations().length).toBeGreaterThan(beforeUnmount)
    expect(lastDeclaration()).toEqual({ type: 'metrics_preference', metrics_enabled: false })
  })

  it('does not re-declare on unmount when nothing was started', async () => {
    const { useSystemResources } = await freshComposable()
    await connectSocket()

    const TestComponent = defineComponent({
      setup() {
        useSystemResources() // instantiated but never started
        return () => h('div')
      },
    })
    const wrapper = mount(TestComponent)
    await nextTick()
    const before = declarations().length

    wrapper.unmount()

    expect(declarations()).toHaveLength(before)
  })
})
