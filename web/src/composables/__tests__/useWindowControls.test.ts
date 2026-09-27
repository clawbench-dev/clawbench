import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, nextTick, type Ref } from 'vue'
import { useWindowControls } from '@/composables/useWindowControls'

type Controls = ReturnType<typeof useWindowControls>

/**
 * Mount a host component and hand back the composable's own return value.
 *
 * Deliberately not `wrapper.vm`: a component's render proxy unwraps refs, so
 * `vm.isMaximized` would be a plain boolean and `vm.isMaximized.value` would be
 * undefined — an assertion that passes for the wrong reason. The composable
 * must also run inside a setup() for its lifecycle hooks to attach, so it
 * cannot be called bare.
 */
function mountControls(): Controls {
  let controls!: Controls
  mount(defineComponent({
    setup() {
      controls = useWindowControls()
      return () => null
    },
  }))
  return controls
}

/** The bridge surface the composable consumes, with every call recorded. */
function stubBridge(overrides: Record<string, unknown> = {}) {
  const native = {
    hasCustomWindowControls: vi.fn(() => true),
    isWindowMaximized: vi.fn(async () => false),
    windowMinimize: vi.fn(),
    windowToggleMaximize: vi.fn(),
    windowClose: vi.fn(),
    ...overrides,
  }
  vi.stubGlobal('ClawBenchNative', native)
  return native
}

function dispatchWindowState(maximized: unknown) {
  window.dispatchEvent(new CustomEvent('clawbench-window-state', { detail: { maximized } }))
}

/** Let the initial `isWindowMaximized()` promise settle. */
async function flushInitialRead() {
  await nextTick()
  await Promise.resolve()
  await Promise.resolve()
}

describe('useWindowControls', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('exposes the controls when the host says the window is frameless', () => {
    stubBridge()
    expect(mountControls().hasCustomControls).toBe(true)
  })

  it('hides the controls on macOS and Android, where the host reports false', () => {
    // macOS keeps its native traffic lights and Android has no window chrome.
    // Showing the cluster anyway would put two sets of controls on a framed
    // window; the gating is the host's answer, not a platform guess here.
    stubBridge({ hasCustomWindowControls: vi.fn(() => false) })
    expect(mountControls().hasCustomControls).toBe(false)
  })

  it('hides the controls when the bridge is absent entirely (plain web)', () => {
    vi.stubGlobal('ClawBenchNative', undefined)
    expect(mountControls().hasCustomControls).toBe(false)
  })

  it('hides the controls when an older host lacks the method', () => {
    // The method is optional in the shared contract; a host that predates it
    // must fall back to the native frame rather than render dead buttons.
    vi.stubGlobal('ClawBenchNative', { isNativeApp: () => true })
    expect(mountControls().hasCustomControls).toBe(false)
  })

  it('forwards the three button actions to the host', () => {
    const native = stubBridge()
    const controls = mountControls()
    controls.minimize()
    controls.toggleMaximize()
    controls.close()
    expect(native.windowMinimize).toHaveBeenCalledTimes(1)
    expect(native.windowToggleMaximize).toHaveBeenCalledTimes(1)
    expect(native.windowClose).toHaveBeenCalledTimes(1)
  })

  it('reads the initial maximize state on mount', async () => {
    // The main process cannot know when this listener attaches, so it does not
    // push during load — the initial value has to be queried, or a window
    // restored maximized shows the wrong glyph until the first change.
    const native = stubBridge({ isWindowMaximized: vi.fn(async () => true) })
    const controls = mountControls()
    await flushInitialRead()
    expect(native.isWindowMaximized).toHaveBeenCalled()
    expect((controls.isMaximized as Ref<boolean>).value).toBe(true)
  })

  it('tracks the pushed maximize state', async () => {
    // The window is the authority: an OS snap or a double-click on the drag
    // region changes the state without going through our IPC toggle.
    stubBridge()
    const controls = mountControls()
    expect(controls.isMaximized.value).toBe(false)

    dispatchWindowState(true)
    await nextTick()
    expect(controls.isMaximized.value).toBe(true)

    dispatchWindowState(false)
    await nextTick()
    expect(controls.isMaximized.value).toBe(false)
  })

  it('does not let a late initial read clobber a pushed state', async () => {
    // A REAL race, not a test artifact: the initial read is a snapshot taken at
    // mount. If the window changes state before that read resolves, applying the
    // stale snapshot last would show the wrong glyph until the next change.
    // Verified by mutating the guard away — without it this resolves false.
    let resolveRead!: (v: boolean) => void
    stubBridge({ isWindowMaximized: vi.fn(() => new Promise<boolean>((r) => { resolveRead = r })) })

    const controls = mountControls()
    dispatchWindowState(true)
    await nextTick()
    expect(controls.isMaximized.value).toBe(true)

    // The stale read finally answers "not maximized"; the push must win.
    resolveRead(false)
    await flushInitialRead()
    expect(controls.isMaximized.value).toBe(true)
  })

  it('ignores a malformed window-state payload', async () => {
    // A detail of the wrong shape must not be coerced into the glyph state.
    stubBridge()
    const controls = mountControls()
    dispatchWindowState('yes')
    dispatchWindowState(undefined)
    await nextTick()
    expect(controls.isMaximized.value).toBe(false)
  })

  it('stops tracking the state after unmount', async () => {
    stubBridge()
    let controls!: Controls
    const wrapper = mount(defineComponent({
      setup() {
        controls = useWindowControls()
        return () => null
      },
    }))
    wrapper.unmount()
    // A listener that outlived the component would still mutate the ref.
    dispatchWindowState(true)
    await nextTick()
    expect(controls.isMaximized.value).toBe(false)
  })

  it('keeps the default glyph when the initial read rejects', async () => {
    // A failing IPC read must not take the header down with it.
    stubBridge({ isWindowMaximized: vi.fn(async () => { throw new Error('ipc gone') }) })
    const controls = mountControls()
    await flushInitialRead()
    expect(controls.isMaximized.value).toBe(false)
  })
})
