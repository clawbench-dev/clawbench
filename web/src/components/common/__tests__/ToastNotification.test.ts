import { describe, it, expect, vi } from 'vitest'
import { ref } from 'vue'
import { mount } from '@vue/test-utils'
import ToastNotification from '../ToastNotification.vue'

function makeToast(overrides = {}) {
  return {
    visible: ref(true),
    type: ref('info'),
    message: ref('Test message'),
    icon: ref(''),
    onClick: ref(null),
    dismiss: vi.fn(),
    ...overrides,
  }
}

describe('ToastNotification', () => {
  it('renders message when visible', () => {
    const toast = makeToast()
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.text()).toContain('Test message')
  })

  it('applies type class', () => {
    const toast = makeToast({ type: ref('error') })
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.toast').classes()).toContain('toast-error')
  })

  it('renders icon when present', () => {
    const toast = makeToast({ icon: ref('✓') })
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.toast-icon').text()).toBe('✓')
  })

  it('calls dismiss on click', async () => {
    const toast = makeToast()
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await wrapper.find('.toast').trigger('click')
    expect(toast.dismiss).toHaveBeenCalled()
  })

  it('calls onClick and dismiss when onClick is set', async () => {
    const onClick = vi.fn()
    const toast = makeToast({ onClick: ref(onClick) })
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await wrapper.find('.toast').trigger('click')
    expect(onClick).toHaveBeenCalled()
    expect(toast.dismiss).toHaveBeenCalled()
  })

  it('hides when not visible', () => {
    const toast = makeToast({ visible: ref(false) })
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.toast').exists()).toBe(false)
  })
})
