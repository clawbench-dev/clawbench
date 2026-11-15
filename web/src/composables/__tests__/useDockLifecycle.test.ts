import { defineComponent, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const startBackground = vi.fn()
const stopBackground = vi.fn()
const onEnter = vi.fn()
const onLeave = vi.fn()
const hidden = ref(new Set<string>())
const featureDisabled = ref(new Set<string>())
const unavailable = ref(new Set<string>())

vi.mock('@/composables/dockPluginRuntime', () => ({
  resolveDockPlugin: (id: string) => ({
    startBackground: id === 'mail' ? startBackground : undefined,
    onEnter: () => onEnter(id),
    onLeave: () => onLeave(id),
  }),
  startDockBackground: () => {
    startBackground()
    return stopBackground
  },
  runDockOnEnter: (id: string) => onEnter(id),
  runDockOnLeave: (id: string) => onLeave(id),
}))

// The lifecycle reads the SAME availability object as the dock itself: a hidden
// entry, a switched-off feature or a missing capability all stop the poller.
vi.mock('@/composables/useDockAvailability', async () => {
  const { computed } = await import('vue')
  return {
    useDockAvailability: () => ({
      availability: computed(() => ({
        hidden: hidden.value,
        featureDisabled: featureDisabled.value,
        unavailable: unavailable.value,
      })),
    }),
  }
})

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
    onEnter.mockClear()
    onLeave.mockClear()
    featureDisabled.value = new Set()
    unavailable.value = new Set()
    hidden.value = new Set()
  })

  it('starts a visible, reachable plugin background and stops it on unmount', async () => {
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

    unavailable.value = new Set(['mail'])
    await nextTick()
    expect(stopBackground).toHaveBeenCalledTimes(1)

    unavailable.value = new Set()
    await nextTick()
    expect(startBackground).toHaveBeenCalledTimes(2)

    wrapper.unmount()
  })

  it('stops when the server feature is switched off', async () => {
    const wrapper = mount(Host)
    await nextTick()
    expect(startBackground).toHaveBeenCalledTimes(1)

    featureDisabled.value = new Set(['mail'])
    await nextTick()
    expect(stopBackground).toHaveBeenCalledTimes(1)

    wrapper.unmount()
  })

  it('enters a tab once, owes it a leave, and does not re-enter the same tab', () => {
    // Mounted so the lifecycle's watcher is scoped: a bare call would leave an
    // orphan watchEffect reacting to the next test's state.
    let lifecycle!: ReturnType<typeof useDockLifecycle>
    const Harness = defineComponent({
      setup() {
        lifecycle = useDockLifecycle()
        return () => null
      },
    })
    const wrapper = mount(Harness)

    lifecycle.enter('mail')
    lifecycle.enter('mail')
    expect(onEnter.mock.calls.map((c) => c[0])).toEqual(['mail'])
    expect(onLeave).not.toHaveBeenCalled()

    lifecycle.enter('browse')
    expect(onEnter.mock.calls.map((c) => c[0])).toEqual(['mail', 'browse'])
    expect(onLeave.mock.calls.map((c) => c[0])).toEqual(['mail'])

    // Leaving clears the debt, so the same tab can be entered again.
    lifecycle.leave()
    expect(onLeave.mock.calls.map((c) => c[0])).toEqual(['mail', 'browse'])
    lifecycle.enter('browse')
    expect(onEnter.mock.calls.map((c) => c[0])).toEqual(['mail', 'browse', 'browse'])
    expect(onLeave.mock.calls.map((c) => c[0])).toEqual(['mail', 'browse'])

    wrapper.unmount()
  })

  it('owes the current tab its onLeave on unmount, exactly once', async () => {
    // Unmounting is leaving: without this the hook only fired from `enter()`,
    // so a plugin could never pause work when its host went away.
    let lifecycle!: ReturnType<typeof useDockLifecycle>
    const Harness = defineComponent({
      setup() {
        lifecycle = useDockLifecycle()
        return () => null
      },
    })
    const harness = mount(Harness)
    await nextTick()

    lifecycle.enter('mail')
    expect(onLeave).not.toHaveBeenCalled()

    harness.unmount()
    // Exactly once: a second call would mean `leave()` ran both here and in
    // `enter`'s bookkeeping.
    expect(onLeave.mock.calls.map((c) => c[0])).toEqual(['mail'])
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
