import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h, defineComponent } from 'vue'

// Minimal component with named slot
const TestComp = defineComponent({
  template: '<div><slot name="header"><span>default</span></slot><slot /></div>',
})

describe('Slot test', () => {
  it('renders named slot with default content', () => {
    const wrapper = mount(TestComp)
    console.log('html:', wrapper.html())
    expect(wrapper.find('span').text()).toBe('default')
  })

  it('renders named slot with provided content', () => {
    const wrapper = mount(TestComp, {
      slots: { header: '<p class="custom">Custom</p>' },
    })
    console.log('html:', wrapper.html())
    expect(wrapper.find('.custom').text()).toBe('Custom')
  })
})
