import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import PopupMenu from '../PopupMenu.vue'

// Track computeMenuStyle options so tests can assert scrollable behavior.
// hoisted: vi.mock is hoisted above imports, so the fn must be created here.
const { computeMenuStyle } = vi.hoisted(() => ({
  computeMenuStyle: vi.fn(() => ({ top: '10px', left: '20px' })),
}))
vi.mock('@/utils/popupMenuPosition', () => ({
  computeMenuStyle,
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
})
