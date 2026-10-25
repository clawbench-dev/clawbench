import { describe, expect, it, afterEach, vi } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { useUiScale } from '@/composables/useUiScale'
import { applyUIScale, UI_SCALE_EVENT } from '@/utils/uiScale'

/**
 * Mounts a component that calls `useUiScale()` during setup and exposes the ref,
 * so the lifecycle hooks (onMounted / onBeforeUnmount) actually run. Calling the
 * composable bare would warn about no active instance and never subscribe.
 */
function mountConsumer() {
  let scale: ReturnType<typeof useUiScale> | null = null
  const wrapper = mount(defineComponent({
    setup() {
      scale = useUiScale()
      return () => h('div', String(scale!.value))
    },
  }))
  return { wrapper, scale: scale! }
}

describe('useUiScale', () => {
  afterEach(() => {
    applyUIScale(1)
    vi.restoreAllMocks()
  })

  it('reads the applied scale synchronously at setup', () => {
    // Must not be 1 until mount: a `computed` created right after the call
    // (dock footprint, split clamp) would otherwise be built against the wrong
    // factor and only correct itself a tick later.
    applyUIScale(1.5)
    const { wrapper, scale } = mountConsumer()
    expect(scale.value).toBe(1.5)
    wrapper.unmount()
  })

  it('updates on UI_SCALE_EVENT', async () => {
    const { wrapper, scale } = mountConsumer()
    expect(scale.value).toBe(1)

    applyUIScale(2)
    await nextTick()
    expect(scale.value).toBe(2)
    wrapper.unmount()
  })

  it('stops listening once unmounted', async () => {
    // The listener this replaces was module-level and never removed, which is
    // how one test's scale leaked into the next.
    const { wrapper, scale } = mountConsumer()
    wrapper.unmount()

    applyUIScale(1.5)
    await nextTick()
    expect(scale.value).toBe(1)
  })

  it('removes exactly one listener per instance', () => {
    const remove = vi.spyOn(window, 'removeEventListener')
    const { wrapper } = mountConsumer()
    remove.mockClear()
    wrapper.unmount()
    expect(remove.mock.calls.filter(([type]) => type === UI_SCALE_EVENT)).toHaveLength(1)
  })

  it('keeps two instances independent', async () => {
    const a = mountConsumer()
    const b = mountConsumer()
    applyUIScale(1.5)
    await nextTick()
    expect(a.scale.value).toBe(1.5)
    expect(b.scale.value).toBe(1.5)

    a.wrapper.unmount()
    applyUIScale(2)
    await nextTick()
    // `a` is frozen at what it last saw; `b` follows.
    expect(a.scale.value).toBe(1.5)
    expect(b.scale.value).toBe(2)
    b.wrapper.unmount()
  })
})
