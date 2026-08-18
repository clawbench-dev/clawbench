import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ModalDialog from '../ModalDialog.vue'

describe('ModalDialog', () => {
  it('renders slot content when open', async () => {
    const wrapper = mount(ModalDialog, {
      props: { open: true, title: 'Test Title' },
      slots: { default: 'Body content' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Body content')
    expect(wrapper.text()).toContain('Test Title')
  })

  it('emits close on overlay click', async () => {
    vi.useFakeTimers()
    const wrapper = mount(ModalDialog, {
      props: { open: true },
      slots: { default: 'Body' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await flushPromises()
    const overlay = wrapper.find('.modal-overlay')
    await overlay.trigger('click')
    vi.advanceTimersByTime(300)
    expect(wrapper.emitted('close')).toBeTruthy()
    vi.useRealTimers()
  })

  it('does not render when not opened', () => {
    const wrapper = mount(ModalDialog, {
      props: { open: false },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.modal-overlay').exists()).toBe(false)
  })

  it('applies maxWidth style', async () => {
    const wrapper = mount(ModalDialog, {
      props: { open: true, maxWidth: 600 },
      slots: { default: 'Body' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await flushPromises()
    const dialog = wrapper.find('.modal-dialog')
    expect(dialog.attributes('style')).toContain('780px')
  })

  it('exposes close method', async () => {
    vi.useFakeTimers()
    const wrapper = mount(ModalDialog, {
      props: { open: true },
      slots: { default: 'Body' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await flushPromises()
    wrapper.vm.close()
    vi.advanceTimersByTime(300)
    expect(wrapper.emitted('close')).toBeTruthy()
    vi.useRealTimers()
  })
})
