import { describe, expect, it, vi, afterEach } from 'vitest'
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { useTextSelectionActive } from '@/composables/useTextSelection'

function Host() {
  return defineComponent({
    setup() {
      const { active } = useTextSelectionActive()
      return { active }
    },
    template: '<div>{{ active }}</div>',
  })
}

function setSelection(nonEmpty: boolean) {
  const sel = window.getSelection()
  const s = sel?.toString() ?? ''
  // Override toString so we don't need a real DOM selection.
  Object.defineProperty(sel!, 'toString', {
    value: () => (nonEmpty ? 'selected text' : ''),
    configurable: true,
  })
}

describe('useTextSelectionActive', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('is inactive when there is no selection', async () => {
    setSelection(false)
    const wrapper = mount(Host())
    await nextTick()
    expect(wrapper.text()).toBe('false')
  })

  it('becomes active on selectionchange with a non-empty selection', async () => {
    const wrapper = mount(Host())
    await nextTick()

    setSelection(true)
    document.dispatchEvent(new Event('selectionchange'))
    await nextTick()
    expect(wrapper.text()).toBe('true')

    setSelection(false)
    document.dispatchEvent(new Event('selectionchange'))
    await nextTick()
    expect(wrapper.text()).toBe('false')
  })

  it('removes its listeners on unmount', () => {
    const addSpy = vi.spyOn(document, 'addEventListener')
    const removeSpy = vi.spyOn(document, 'removeEventListener')
    const wrapper = mount(Host())
    wrapper.unmount()
    expect(removeSpy).toHaveBeenCalledWith('selectionchange', expect.any(Function))
    expect(removeSpy).toHaveBeenCalledWith('mouseup', expect.any(Function))
    addSpy.mockRestore()
    removeSpy.mockRestore()
  })
})
