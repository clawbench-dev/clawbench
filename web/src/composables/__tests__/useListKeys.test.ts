import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { useListKeys } from '@/composables/useListKeys'

function setup(isOpen = () => true) {
  const down = vi.fn()
  const up = vi.fn()
  const confirm = vi.fn()
  const nav = { down, up, confirm }
  const C = defineComponent({
    setup() {
      useListKeys({ nav, isOpen })
      return { nav }
    },
    template: '<div><input class="box" /><button class="btn">x</button></div>',
  })
  const wrapper = mount(C)

  function fireFrom(target: HTMLElement | null, key: string) {
    const ev = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
    if (target) Object.defineProperty(ev, 'target', { value: target, configurable: true })
    document.dispatchEvent(ev)
  }

  return { wrapper, down, up, confirm, fireFrom }
}

describe('useListKeys', () => {
  it('ArrowDown/ArrowUp/Enter from any non-editable target drive the nav', () => {
    const { wrapper, down, up, confirm, fireFrom } = setup()
    fireFrom(wrapper.element, 'ArrowDown')
    expect(down).toHaveBeenCalledTimes(1)
    fireFrom(wrapper.element, 'ArrowUp')
    expect(up).toHaveBeenCalledTimes(1)
    fireFrom(wrapper.element, 'Enter')
    expect(confirm).toHaveBeenCalledTimes(1)
  })

  it('ignores arrows while focus is in an input', () => {
    const { wrapper, down, fireFrom } = setup()
    fireFrom(wrapper.find('.box').element, 'ArrowDown')
    expect(down).not.toHaveBeenCalled()
  })

  it('ignores Enter while focus is in an input', () => {
    const { wrapper, confirm, fireFrom } = setup()
    fireFrom(wrapper.find('.box').element, 'Enter')
    expect(confirm).not.toHaveBeenCalled()
  })

  it('ignores Enter while focus is on an interactive element', () => {
    const { wrapper, confirm, fireFrom } = setup()
    fireFrom(wrapper.find('.btn').element, 'Enter')
    expect(confirm).not.toHaveBeenCalled()
  })

  it('does nothing when the list is not open', () => {
    const { wrapper, down, confirm, fireFrom } = setup(() => false)
    fireFrom(wrapper.element, 'ArrowDown')
    expect(down).not.toHaveBeenCalled()
    fireFrom(wrapper.element, 'Enter')
    expect(confirm).not.toHaveBeenCalled()
  })

  it('removes the listener on unmount', () => {
    const { wrapper, down, fireFrom } = setup()
    wrapper.unmount()
    fireFrom(wrapper.element, 'ArrowDown')
    expect(down).not.toHaveBeenCalled()
  })
})
