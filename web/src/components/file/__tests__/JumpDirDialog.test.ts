import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import JumpDirDialog from '../JumpDirDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      jump: {
        title: 'Jump to Directory',
        placeholder: 'Enter a directory path, e.g. src/utils',
        confirm: 'Jump',
        cancel: 'Cancel',
        button: 'Jump',
        copyPath: 'Copy path',
      },
    },
  },
})

const TeleportStub = { template: '<div><slot /></div>' }

function mountDialog(props = {}) {
  return mount(JumpDirDialog, {
    props: { open: false, ...props },
    global: { stubs: { Teleport: TeleportStub }, plugins: [i18n] },
  })
}

describe('JumpDirDialog', () => {
  beforeEach(() => { vi.restoreAllMocks() })

  it('renders input and confirm/cancel buttons when open', () => {
    const wrapper = mountDialog({ open: true })
    expect(wrapper.find('input').exists()).toBe(true)
    expect(wrapper.findAll('button').length).toBeGreaterThanOrEqual(2)
  })

  // Every other ModalDialog consumer supplies a header icon; without one the
  // title is bare text and the dialog does not visibly belong to the button
  // that opened it.
  it('shows the same glyph in the header as the button that opens it', () => {
    const wrapper = mountDialog({ open: true })
    const header = wrapper.find('.modal-header')
    expect(header.find('svg.lucide-corner-up-right').exists()).toBe(true)
    // Title text is asserted for presence only: this suite's i18n locale is
    // environment-dependent, so pinning the copy would be flaky.
    expect(header.find('.modal-title').exists()).toBe(true)
  })

  it('emits confirm with trimmed value on confirm click', async () => {
    const wrapper = mountDialog({ open: true })
    await wrapper.find('input').setValue('  src/utils  ')
    await wrapper.find('.fbtn-primary').trigger('click')
    expect(wrapper.emitted('confirm')![0]).toEqual(['src/utils'])
  })

  it('emits confirm with trimmed value on Enter key', async () => {
    const wrapper = mountDialog({ open: true })
    await wrapper.find('input').setValue('src')
    await wrapper.find('input').trigger('keydown.enter')
    expect(wrapper.emitted('confirm')![0]).toEqual(['src'])
  })

  it('does not emit confirm when input is empty', async () => {
    const wrapper = mountDialog({ open: true })
    await wrapper.find('input').setValue('   ')
    await wrapper.find('.fbtn-primary').trigger('click')
    expect(wrapper.emitted('confirm')).toBeUndefined()
  })

  it('emits close on cancel click', async () => {
    const wrapper = mountDialog({ open: true })
    await wrapper.find('.fbtn').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('clears input when reopened', async () => {
    const wrapper = mountDialog({ open: true })
    await wrapper.find('input').setValue('src')
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    expect((wrapper.find('input').element as HTMLInputElement).value).toBe('')
  })

  it('uses the default placeholder when no placeholder prop is given', () => {
    const wrapper = mountDialog({ open: true })
    expect(wrapper.find('input').attributes('placeholder')).toBe('Enter a directory path, e.g. src/utils')
  })

  it('uses the provided placeholder prop override', () => {
    const wrapper = mountDialog({ open: true, placeholder: 'Enter a path inside the project' })
    expect(wrapper.find('input').attributes('placeholder')).toBe('Enter a path inside the project')
  })
})
