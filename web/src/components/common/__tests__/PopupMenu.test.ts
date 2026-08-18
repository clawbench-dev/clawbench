import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PopupMenu from '../PopupMenu.vue'

vi.mock('@/utils/popupMenuPosition', () => ({
  computeMenuStyle: () => ({ top: '10px', left: '20px' }),
}))

describe('PopupMenu', () => {
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
})
