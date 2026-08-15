import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'

describe('LoadingIndicator', () => {
  it('renders a spinner with role=status', () => {
    const wrapper = mount(LoadingIndicator)
    const status = wrapper.get('[role="status"]')
    expect(status.find('.li-spinner').exists()).toBe(true)
  })

  it('renders label text when provided', () => {
    const wrapper = mount(LoadingIndicator, { props: { label: '加载中...' } })
    expect(wrapper.find('.li-label').text()).toBe('加载中...')
  })

  it('omits label element when no label provided', () => {
    const wrapper = mount(LoadingIndicator)
    expect(wrapper.find('.li-label').exists()).toBe(false)
  })

  it('defaults to md size and block (non-inline) layout', () => {
    const wrapper = mount(LoadingIndicator)
    expect(wrapper.find('.loading-indicator').classes()).toContain('size-md')
    expect(wrapper.find('.loading-indicator').classes()).not.toContain('inline')
  })

  it('applies size, inline, overlay and center classes', () => {
    const wrapper = mount(LoadingIndicator, {
      props: { size: 'sm', inline: true, overlay: true, center: false },
    })
    const el = wrapper.find('.loading-indicator')
    expect(el.classes()).toContain('size-sm')
    expect(el.classes()).toContain('inline')
    expect(el.classes()).toContain('overlay')
    expect(el.classes()).not.toContain('is-center')
  })

  it('applies fixed full-screen class when fixed prop is set', () => {
    const wrapper = mount(LoadingIndicator, { props: { fixed: true } })
    const el = wrapper.find('.loading-indicator')
    expect(el.classes()).toContain('fixed')
    expect(el.classes()).not.toContain('overlay')
  })

  it('overlay and fixed are mutually applied when both set', () => {
    const wrapper = mount(LoadingIndicator, { props: { overlay: true, fixed: true } })
    const el = wrapper.find('.loading-indicator')
    expect(el.classes()).toContain('overlay')
    expect(el.classes()).toContain('fixed')
  })

  it('renders default slot content', () => {
    const wrapper = mount(LoadingIndicator, {
      slots: { default: '<span class="extra">extra</span>' },
    })
    expect(wrapper.find('.extra').text()).toBe('extra')
  })
})
