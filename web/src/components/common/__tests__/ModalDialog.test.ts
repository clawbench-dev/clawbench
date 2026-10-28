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
    // Width comes from the two tiers in css/variables.css via `size`; passing
    // the old `maxWidth` prop must be inert rather than silently re-introducing
    // an inline override.
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

  it('defaults to the narrow tier', async () => {
    // An unclassified dialog lands on the readable column, not a 960px void.
    const wrapper = mount(ModalDialog, {
      props: { open: true },
      slots: { default: 'Body' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await flushPromises()
    const dialog = wrapper.find('.modal-dialog')
    expect(dialog.classes()).toContain('modal-dialog--narrow')
    expect(dialog.classes()).not.toContain('modal-dialog--wide')
  })

  it('maps size="wide" onto the wide modifier class', async () => {
    const wrapper = mount(ModalDialog, {
      props: { open: true, size: 'wide' },
      slots: { default: 'Body' },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await flushPromises()
    const dialog = wrapper.find('.modal-dialog')
    expect(dialog.classes()).toContain('modal-dialog--wide')
    expect(dialog.classes()).not.toContain('modal-dialog--narrow')
    expect(dialog.attributes('style')).toBeUndefined()
  })

  it('rejects any third tier', () => {
    // The validator is the fence around "exactly two": `size="compact"` (the
    // retired variant) must not silently render an untiered card.
    const validator = ModalDialog.props!.size.validator as (v: string) => boolean
    expect(validator('narrow')).toBe(true)
    expect(validator('wide')).toBe(true)
    expect(validator('compact')).toBe(false)
    expect(validator('900px')).toBe(false)
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
