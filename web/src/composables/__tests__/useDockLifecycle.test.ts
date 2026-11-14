import { defineComponent, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const startBackground = vi.fn()
const stopBackground = vi.fn()
const available = ref(true)
const hidden = ref(new Set<string>())

vi.mock('@/composables/dockPluginRuntime', () => ({
  getDockPluginRuntime: (id: string) => (id === 'mail' ? { startBackground } : undefined),
  isDockPluginAvailable: () => available.value,
  startDockBackground: () => {
    startBackground()
    return stopBackground
  },
  runDockOnEnter: vi.fn(),
  runDockOnLeave: vi.fn(),
}))

vi.mock('@/composables/useDockPlugins', () => ({
  useDockPlugins: () => ({ hidden }),
}))

import { useDockLifecycle } from '@/composables/useDockLifecycle'

const Host = defineComponent({
  setup() {
    useDockLifecycle()
    return () => null
  },
})

describe('useDockLifecycle', () => {
  beforeEach(() => {
    startBackground.mockClear()
    stopBackground.mockClear()
    available.value = true
    hidden.value = new Set()
  })

  it('starts a visible, available plugin background and stops it on unmount', async () => {
    const wrapper = mount(Host)
    await nextTick()
    expect(startBackground).toHaveBeenCalledTimes(1)

    wrapper.unmount()
    expect(stopBackground).toHaveBeenCalledTimes(1)
  })

  it('stops when the plugin becomes unavailable and restarts when it returns', async () => {
    const wrapper = mount(Host)
    await nextTick()
    expect(startBackground).toHaveBeenCalledTimes(1)

    available.value = false
    await nextTick()
    expect(stopBackground).toHaveBeenCalledTimes(1)

    available.value = true
    await nextTick()
    expect(startBackground).toHaveBeenCalledTimes(2)

    wrapper.unmount()
  })

  it('stops when the plugin is hidden by the user', async () => {
    const wrapper = mount(Host)
    await nextTick()
    expect(startBackground).toHaveBeenCalledTimes(1)

    hidden.value = new Set(['mail'])
    await nextTick()
    expect(stopBackground).toHaveBeenCalledTimes(1)

    wrapper.unmount()
  })
})
