import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
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

describe('PopupMenu keyboard navigation (PC)', () => {
  const slotWithThreeButtons = `
    <button class="menu-item item-a">Item A</button>
    <button class="menu-item item-b">Item B</button>
    <button class="menu-item item-c">Item C</button>
  `

  async function mountMenu() {
    const wrapper = mount(PopupMenu, {
      props: { show: true },
      slots: { default: slotWithThreeButtons },
      attachTo: document.body,
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    // Wait for rAF + nextTick (menu auto-focus after open)
    await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))
    await flushPromises()
    return wrapper
  }

  it('ArrowDown focuses the first focusable item', async () => {
    const wrapper = await mountMenu()
    await wrapper.find('.popup-menu').trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement?.textContent).toBe('Item A')
  })

  it('ArrowDown twice focuses the second item', async () => {
    const wrapper = await mountMenu()
    const menu = wrapper.find('.popup-menu')
    await menu.trigger('keydown', { key: 'ArrowDown' })
    await menu.trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement?.textContent).toBe('Item B')
  })

  it('ArrowUp from second item goes back to first', async () => {
    const wrapper = await mountMenu()
    const menu = wrapper.find('.popup-menu')
    await menu.trigger('keydown', { key: 'ArrowDown' })
    await menu.trigger('keydown', { key: 'ArrowDown' })
    await menu.trigger('keydown', { key: 'ArrowUp' })
    expect(document.activeElement?.textContent).toBe('Item A')
  })

  it('Enter clicks the currently focused item', async () => {
    const wrapper = await mountMenu()
    const menu = wrapper.find('.popup-menu')
    const onClick = vi.fn()
    const btn = wrapper.find('.item-b').element
    btn.addEventListener('click', onClick)
    await menu.trigger('keydown', { key: 'ArrowDown' })
    await menu.trigger('keydown', { key: 'ArrowDown' })
    await menu.trigger('keydown', { key: 'Enter' })
    expect(onClick).toHaveBeenCalledTimes(1)
    btn.removeEventListener('click', onClick)
  })

  it('Enter with no selection clicks the first item', async () => {
    const wrapper = await mountMenu()
    const menu = wrapper.find('.popup-menu')
    const onClick = vi.fn()
    const btn = wrapper.find('.item-a').element
    btn.addEventListener('click', onClick)
    await menu.trigger('keydown', { key: 'Enter' })
    expect(onClick).toHaveBeenCalledTimes(1)
    btn.removeEventListener('click', onClick)
  })

  it('Escape closes the menu', async () => {
    const wrapper = await mountMenu()
    await wrapper.find('.popup-menu').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('update:show')).toBeTruthy()
    expect(wrapper.emitted('update:show')[0]).toEqual([false])
  })
})
