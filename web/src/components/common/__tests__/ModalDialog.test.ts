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

  it('does not accept an arbitrary per-call-site width', async () => {
    // Width lives in modal-card.css (`.modal-dialog { --modal-max-width: 900px }`)
    // so it can never drift per dialog. Passing the old `maxWidth` prop must be
    // inert rather than silently re-introducing an inline override.
    const wrapper = mount(ModalDialog, {
      props: { open: true, maxWidth: 600 } as never,
      slots: { default: 'Body' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await flushPromises()
    const dialog = wrapper.find('.modal-dialog')
    expect(dialog.attributes('style')).toBeUndefined()
    expect(dialog.attributes('style') ?? '').not.toContain('max-width')
  })

  it('applies the named compact width variant without inline styles', async () => {
    const wrapper = mount(ModalDialog, {
      props: { open: true, size: 'compact' },
      slots: { default: 'Body' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await flushPromises()
    const dialog = wrapper.find('.modal-dialog')
    expect(dialog.classes()).toContain('modal-dialog--compact')
    expect(dialog.attributes('style')).toBeUndefined()
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
