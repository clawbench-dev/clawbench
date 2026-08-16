import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AsyncComponentError from '@/components/common/AsyncComponentError.vue'

describe('AsyncComponentError', () => {
  it('renders label as the message', () => {
    const wrapper = mount(AsyncComponentError, { props: { label: '代码编辑器加载失败' } })
    expect(wrapper.text()).toContain('代码编辑器加载失败')
  })

  it('falls back to a default message when no label is provided', () => {
    const wrapper = mount(AsyncComponentError)
    expect(wrapper.text()).toContain('组件加载失败')
  })

  it('emits retry when the button is clicked', async () => {
    const wrapper = mount(AsyncComponentError)
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })
})
