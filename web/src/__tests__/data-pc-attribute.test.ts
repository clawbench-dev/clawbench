import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'

// Minimal component that mirrors App.vue's data-pc binding logic
const TestPCComponent = defineComponent({
  setup() {
    const isPC = ref(true)
    return { isPC }
  },
  render() {
    return h('div', { class: 'app-container', 'data-pc': this.isPC }, 'App')
  },
})

describe('data-pc attribute', () => {
  const originalUA = navigator.userAgent

  beforeEach(() => {
    vi.stubGlobal('navigator', {
      ...navigator,
      userAgent: originalUA,
      maxTouchPoints: 0,
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sets data-pc="true" when isPC is true', () => {
    const wrapper = mount(TestPCComponent)
    const el = wrapper.find('.app-container')
    expect(el.attributes('data-pc')).toBe('true')
  })

  it('sets data-pc="false" when isPC is false', async () => {
    const wrapper = mount(TestPCComponent)
    wrapper.vm.isPC = false
    await wrapper.vm.$nextTick()
    const el = wrapper.find('.app-container')
    expect(el.attributes('data-pc')).toBe('false')
  })

  it('data-pc attribute is absent when isPC is undefined', async () => {
    const wrapper = mount(TestPCComponent)
    wrapper.vm.isPC = undefined
    await wrapper.vm.$nextTick()
    const el = wrapper.find('.app-container')
    expect(el.attributes('data-pc')).toBeUndefined()
  })
})
