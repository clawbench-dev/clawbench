import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { handleBackNavigation, canNavigateBack, _resetHandlers, PRIORITY_OVERLAY } from '@/composables/useBackHandler'

// BottomSheet imports useBackHandler directly — no mock needed for the core logic.
// We mock appLog to avoid Android bridge calls in test.
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Import BottomSheet after mocks are set up
import BottomSheet from '@/components/common/BottomSheet.vue'

describe('BottomSheet back gesture', () => {
  beforeEach(() => {
    _resetHandlers()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('registers a back handler when open=true', () => {
    const wrapper = mount(BottomSheet, {
      props: { open: true, title: 'Test' },
    })

    expect(canNavigateBack()).toBe(true)

    wrapper.unmount()
    _resetHandlers()
  })

  it('does not register a back handler when open=false', () => {
    const wrapper = mount(BottomSheet, {
      props: { open: false, title: 'Test' },
    })

    expect(canNavigateBack()).toBe(false)

    wrapper.unmount()
  })

  it('back gesture closes the drawer (emits close after animation)', () => {
    const wrapper = mount(BottomSheet, {
      props: { open: true, title: 'Test' },
    })

    // Trigger back navigation
    handleBackNavigation()

    // BottomSheet starts leave animation — close is emitted after 250ms timeout
    vi.advanceTimersByTime(300)

    expect(wrapper.emitted('close')).toBeTruthy()

    wrapper.unmount()
    _resetHandlers()
  })

  it('back gesture instantly closes drawer in instant mode', () => {
    const wrapper = mount(BottomSheet, {
      props: { open: true, title: 'Test', instant: true },
    })

    handleBackNavigation()

    // instant mode — no animation delay, close emitted synchronously
    expect(wrapper.emitted('close')).toBeTruthy()

    wrapper.unmount()
    _resetHandlers()
  })

  it('unregisters back handler when open changes to false', async () => {
    const wrapper = mount(BottomSheet, {
      props: { open: true, title: 'Test' },
    })

    expect(canNavigateBack()).toBe(true)

    await wrapper.setProps({ open: false })
    expect(canNavigateBack()).toBe(false)

    wrapper.unmount()
    _resetHandlers()
  })

  it('closeGuard prop disables back handler registration', () => {
    const wrapper = mount(BottomSheet, {
      props: { open: true, title: 'Test', closeGuard: true },
    })

    expect(canNavigateBack()).toBe(false)

    wrapper.unmount()
    _resetHandlers()
  })

  it('closeGuard prop blocks handleClose (no close emitted)', () => {
    const wrapper = mount(BottomSheet, {
      props: { open: true, title: 'Test', closeGuard: true },
    })

    // Directly call the exposed close method (same path as overlay click / header click)
    wrapper.vm.close()
    vi.advanceTimersByTime(300)
    expect(wrapper.emitted('close')).toBeFalsy()

    // Back gesture also blocked (no back handler registered, so nothing happens)
    handleBackNavigation()
    vi.advanceTimersByTime(300)
    expect(wrapper.emitted('close')).toBeFalsy()

    wrapper.unmount()
    _resetHandlers()
  })

  it('closeGuard lifting re-enables close', async () => {
    const wrapper = mount(BottomSheet, {
      props: { open: true, title: 'Test', closeGuard: true },
    })

    // Close is blocked
    wrapper.vm.close()
    vi.advanceTimersByTime(300)
    expect(wrapper.emitted('close')).toBeFalsy()

    // Lift guard
    await wrapper.setProps({ closeGuard: false })

    // Now close works via exposed method
    wrapper.vm.close()
    vi.advanceTimersByTime(300)
    expect(wrapper.emitted('close')).toBeTruthy()

    wrapper.unmount()
    _resetHandlers()
  })

  it('newer drawer (higher seq) takes priority over older drawer', () => {
    const wrapper1 = mount(BottomSheet, {
      props: { open: true, title: 'Drawer 1' },
    })
    const wrapper2 = mount(BottomSheet, {
      props: { open: true, title: 'Drawer 2' },
    })

    // Both are open — back should close the newer (topmost) drawer
    handleBackNavigation()
    vi.advanceTimersByTime(300)

    // The second drawer (mounted later, higher seq) should close
    expect(wrapper2.emitted('close')).toBeTruthy()
    // The first drawer should NOT be closed
    expect(wrapper1.emitted('close')).toBeFalsy()

    wrapper1.unmount()
    wrapper2.unmount()
    _resetHandlers()
  })
})
