import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import PopupMenu from '../PopupMenu.vue'
import {
  _resetHandlers,
  canNavigateBack,
  handleBackNavigation,
  registerBackHandler,
  PRIORITY_OVERLAY,
} from '@/composables/useBackHandler'

// Track computeMenuStyle options so tests can assert scrollable behavior.
// hoisted: vi.mock is hoisted above imports, so the fn must be created here.
const { computeMenuStyle } = vi.hoisted(() => ({
  computeMenuStyle: vi.fn(() => ({ top: '10px', left: '20px' })),
}))
vi.mock('@/utils/popupMenuPosition', () => ({
  computeMenuStyle,
}))

// The containing-block probe is exercised by its own unit tests; here it only
// needs to be controllable so the fallback path can be driven.
const { measureFixedContainingBlock } = vi.hoisted(() => ({
  measureFixedContainingBlock: vi.fn(() => null),
}))
vi.mock('@/utils/fixedPosition', () => ({
  measureFixedContainingBlock,
}))

describe('PopupMenu', () => {
  beforeEach(() => {
    computeMenuStyle.mockClear()
    computeMenuStyle.mockReturnValue({ top: '10px', left: '20px' })
  })
  it('renders when show is true', () => {
    const wrapper = mount(PopupMenu, {
      props: { show: true },
      slots: { default: '<div class="item">Menu Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.popup-menu').exists()).toBe(true)
    expect(wrapper.text()).toContain('Menu Item')
  })

  it('hides when show is false', () => {
    const wrapper = mount(PopupMenu, {
      props: { show: false },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.popup-menu').exists()).toBe(false)
  })

  it('emits update:show false on click', async () => {
    const wrapper = mount(PopupMenu, {
      props: { show: true },
      slots: { default: '<div>Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await wrapper.find('.popup-menu').trigger('click')
    expect(wrapper.emitted('update:show')).toBeTruthy()
    expect(wrapper.emitted('update:show')[0]).toEqual([false])
  })

  it('emits update:show false on escape', async () => {
    const wrapper = mount(PopupMenu, {
      props: { show: true },
      slots: { default: '<div>Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await wrapper.find('.popup-menu').trigger('keydown.escape')
    expect(wrapper.emitted('update:show')).toBeTruthy()
  })

  it('adds popup-menu--app class when appSurface is set', () => {
    const wrapper = mount(PopupMenu, {
      props: { show: true, appSurface: true },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.popup-menu').classes()).toContain('popup-menu--app')
  })

  it('does not add popup-menu--app class by default', () => {
    const wrapper = mount(PopupMenu, {
      props: { show: true },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.popup-menu').classes()).not.toContain('popup-menu--app')
  })

  it('keeps the root scrollable by default (scrollable:true → overflowY auto)', async () => {
    vi.useFakeTimers()
    computeMenuStyle.mockReturnValue({ top: '10px', left: '20px', overflowY: 'auto' })
    const wrapper = mount(PopupMenu, {
      props: { show: false, targetElement: { getBoundingClientRect: () => ({}) } },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    // Position is (re)computed in requestAnimationFrame after show flips true.
    await wrapper.setProps({ show: true })
    await vi.advanceTimersByTimeAsync(16)
    const style = computeMenuStyle.mock.calls[0]?.[1]
    expect(style?.scrollable).toBe(true)
    vi.useRealTimers()
    wrapper.unmount()
  })

  it('appSurface menus delegate scrolling to the slot content (scrollable:false)', async () => {
    vi.useFakeTimers()
    const wrapper = mount(PopupMenu, {
      props: { show: false, appSurface: true, targetElement: { getBoundingClientRect: () => ({}) } },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await wrapper.setProps({ show: true })
    await vi.advanceTimersByTimeAsync(16)
    const style = computeMenuStyle.mock.calls[0]?.[1]
    expect(style?.scrollable).toBe(false)
    vi.useRealTimers()
    wrapper.unmount()
  })

  it('positions on mount when it mounts already open', async () => {
    // A parent may gate the component with the same condition that opens it
    // (e.g. `v-if="items.length > 0"` alongside `v-model:show`), so it mounts
    // with show=true and the show *watcher* never fires. Without mount-time
    // positioning the menu renders as an unpositioned block in <body> — the
    // menu appears missing and the page grows/jitters.
    vi.useFakeTimers()
    mount(PopupMenu, {
      props: { show: true, targetElement: { getBoundingClientRect: () => ({ top: 100, bottom: 120, left: 10, right: 200 }) } },
      slots: { default: '<div>Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await vi.advanceTimersByTimeAsync(16)

    expect(computeMenuStyle).toHaveBeenCalled()
    vi.useRealTimers()
  })

  it('does not position on mount when it mounts closed', async () => {
    vi.useFakeTimers()
    mount(PopupMenu, {
      props: { show: false, targetElement: { getBoundingClientRect: () => ({}) } },
      slots: { default: '<div>Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await vi.advanceTimersByTimeAsync(16)

    expect(computeMenuStyle).not.toHaveBeenCalled()
    vi.useRealTimers()
  })

  it('repositions when the anchor element is swapped while open', async () => {
    // The Android input recovery rebuilds the chat textarea (:key), so the ref
    // handed to targetElement is rebound to a fresh node. Position is otherwise
    // only recomputed on `show`, scroll and resize, which would leave the menu
    // anchored to the removed element's rect.
    vi.useFakeTimers()
    const first = { getBoundingClientRect: () => ({ top: 10, bottom: 30, left: 0, right: 100 }) }
    const second = { getBoundingClientRect: () => ({ top: 500, bottom: 520, left: 0, right: 100 }) }
    const wrapper = mount(PopupMenu, {
      props: { show: true, targetElement: first },
      slots: { default: '<div>Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await vi.advanceTimersByTimeAsync(16)
    const callsAfterMount = computeMenuStyle.mock.calls.length

    await wrapper.setProps({ targetElement: second })
    await vi.advanceTimersByTimeAsync(16)

    expect(computeMenuStyle.mock.calls.length).toBeGreaterThan(callsAfterMount)
    // And it must read the NEW element's geometry, not the stale one.
    expect(computeMenuStyle).toHaveBeenLastCalledWith(
      second.getBoundingClientRect(),
      expect.anything(),
    )
    vi.useRealTimers()
    wrapper.unmount()
  })

  it('does not reposition on an anchor swap while closed', async () => {
    vi.useFakeTimers()
    const wrapper = mount(PopupMenu, {
      props: { show: false, targetElement: { getBoundingClientRect: () => ({}) } },
      slots: { default: '<div>Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await wrapper.setProps({ targetElement: { getBoundingClientRect: () => ({ top: 1 }) } })
    await vi.advanceTimersByTimeAsync(16)

    expect(computeMenuStyle).not.toHaveBeenCalled()
    vi.useRealTimers()
    wrapper.unmount()
  })

  it('discards a measured containing block that would put the menu off screen', async () => {
    // The probe can return a bogus block (a transformed ancestor mid-Transition,
    // a box measured before layout). Trusting it placed the `#` completion menu
    // 600px BELOW the viewport, where no row could be clicked. The gate must
    // recompute with the zoom-only path instead.
    //
    // jsdom reports no client rects, so the measurement branch is only reached
    // when the element claims to be laid out.
    vi.spyOn(Element.prototype, 'getClientRects').mockReturnValue({ length: 1, item: () => null })
    vi.useFakeTimers()
    measureFixedContainingBlock.mockReturnValue({ left: 0, top: 0, right: 380, bottom: 59, scaleX: 1, scaleY: 1 })
    // First call: the off-screen result. Second call (fallback): a sane one.
    computeMenuStyle
      .mockReturnValueOnce({ position: 'fixed', bottom: '-596.6px', left: '90px' })
      .mockReturnValueOnce({ position: 'fixed', bottom: '62.4px', left: '90px' })

    mount(PopupMenu, {
      props: { show: true, targetElement: { getBoundingClientRect: () => ({ top: 600, bottom: 660, left: 10, right: 200 }) } },
      slots: { default: '<div>Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await vi.advanceTimersByTimeAsync(16)

    expect(computeMenuStyle).toHaveBeenCalledTimes(2)
    // The fallback call omits `containingBlock` entirely.
    expect(computeMenuStyle.mock.calls[1][1].containingBlock).toBeUndefined()
    vi.useRealTimers()
  })

  it('also rejects a measurement that would put the menu ABOVE the viewport', async () => {
    // The mirror of the case above: a containing block measured TOO LARGE gives
    // a huge positive `bottom` (measured 962.4px on a 720px viewport), which
    // parks the `@` member menu above the top edge. A one-sign check misses it.
    vi.spyOn(Element.prototype, 'getClientRects').mockReturnValue({ length: 1, item: () => null })
    vi.useFakeTimers()
    measureFixedContainingBlock.mockReturnValue({ left: 0, top: 0, right: 1280, bottom: 1700, scaleX: 1, scaleY: 1 })
    computeMenuStyle
      .mockReturnValueOnce({ position: 'fixed', bottom: '962.391px', left: '90px' })
      .mockReturnValueOnce({ position: 'fixed', bottom: '62.4px', left: '90px' })

    mount(PopupMenu, {
      props: { show: true, targetElement: { getBoundingClientRect: () => ({ top: 600, bottom: 660, left: 10, right: 200 }) } },
      slots: { default: '<div>Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await vi.advanceTimersByTimeAsync(16)

    expect(computeMenuStyle).toHaveBeenCalledTimes(2)
    expect(computeMenuStyle.mock.calls[1][1].containingBlock).toBeUndefined()
    vi.useRealTimers()
  })

  it('keeps the measured containing block when it lands on screen', async () => {
    vi.spyOn(Element.prototype, 'getClientRects').mockReturnValue({ length: 1, item: () => null })
    vi.useFakeTimers()
    measureFixedContainingBlock.mockReturnValue({ left: 0, top: 0, right: 1280, bottom: 720, scaleX: 1, scaleY: 1 })
    computeMenuStyle.mockReturnValue({ position: 'fixed', bottom: '62.4px', left: '90px' })

    mount(PopupMenu, {
      props: { show: true, targetElement: { getBoundingClientRect: () => ({ top: 600, bottom: 660, left: 10, right: 200 }) } },
      slots: { default: '<div>Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await vi.advanceTimersByTimeAsync(16)

    expect(computeMenuStyle).toHaveBeenCalledTimes(1)
    expect(computeMenuStyle.mock.calls[0][1].containingBlock).toBeTruthy()
    vi.useRealTimers()
})

describe('PopupMenu — back press', () => {
  // The menu is teleported and paints above the modal band, so it is the
  // topmost surface whenever open. Without a registration the press fell
  // through to whatever was behind it: the usage-meter popover stayed on screen
  // and the press navigated the page underneath.
  it('closes on a back press instead of letting it through', async () => {
    _resetHandlers()
    const wrapper = mount(PopupMenu, {
      props: { show: true },
      slots: { default: '<div>Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await nextTick()

    expect(canNavigateBack()).toBe(true)
    expect(handleBackNavigation()).toBe(true)
    expect(wrapper.emitted('update:show')?.at(-1)).toEqual([false])

    wrapper.unmount()
    _resetHandlers()
  })

  it('stops consuming back once closed', async () => {
    _resetHandlers()
    const wrapper = mount(PopupMenu, {
      props: { show: true },
      slots: { default: '<div>Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await nextTick()
    await wrapper.setProps({ show: false })
    await nextTick()

    // A closed menu must not swallow the press — otherwise Back is dead on the
    // page behind it.
    expect(canNavigateBack()).toBe(false)
    expect(handleBackNavigation()).toBe(false)

    wrapper.unmount()
    _resetHandlers()
  })

  it('wins over a drawer and a dialog opened beneath it', async () => {
    _resetHandlers()
    // A dialog registers at PRIORITY_OVERLAY + 1; the menu paints above it.
    registerBackHandler({ id: 'underlying-dialog', canGoBack: () => true, goBack: () => {}, priority: PRIORITY_OVERLAY + 1 })
    const wrapper = mount(PopupMenu, {
      props: { show: true },
      slots: { default: '<div>Item</div>' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await nextTick()

    expect(handleBackNavigation()).toBe(true)
    expect(wrapper.emitted('update:show')?.at(-1)).toEqual([false])

    wrapper.unmount()
    _resetHandlers()
  })
  })

})
