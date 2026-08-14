import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AsyncComponentLoader from '@/components/common/AsyncComponentLoader.vue'

describe('AsyncComponentLoader', () => {
  it('renders a spinner with role=status', () => {
    const wrapper = mount(AsyncComponentLoader)
    const status = wrapper.get('[role="status"]')
    expect(status.find('.li-spinner').exists()).toBe(true)
  })

  it('renders label text when provided', () => {
    const wrapper = mount(AsyncComponentLoader, {
      props: { label: '加载终端中...' },
    })
    expect(wrapper.find('.li-label').text()).toBe('加载终端中...')
  })

  it('omits label element when no label provided', () => {
    const wrapper = mount(AsyncComponentLoader)
    expect(wrapper.find('.li-label').exists()).toBe(false)
  })

  it('applies minimal class when minimal prop is set', () => {
    const wrapper = mount(AsyncComponentLoader, { props: { minimal: true } })
    expect(wrapper.find('.async-component-loader').classes()).toContain('minimal')
  })

  it('renders default slot content', () => {
    const wrapper = mount(AsyncComponentLoader, {
      slots: { default: '<span class="extra">extra</span>' },
    })
    expect(wrapper.find('.extra').text()).toBe('extra')
  })
})
