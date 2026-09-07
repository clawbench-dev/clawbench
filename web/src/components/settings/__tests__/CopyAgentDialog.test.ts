import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import CopyAgentDialog from '@/components/settings/CopyAgentDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          agentCopyTitle: 'Duplicate Agent',
          agentCopyPlaceholder: 'Enter new agent name',
          agentCopyConfirm: 'Duplicate',
          agentCopyEmptyName: 'Name cannot be empty',
          agentCopy: 'Copy',
          agentName: 'Name',
        },
      },
      common: {
        cancel: 'Cancel',
      },
    },
  },
})

// CopyAgentDialog renders inside ModalDialog which Teleports its overlay to
// <body>, so element lookups must go through document.body — the dialog
// content is not part of the component's own wrapper tree.
function $(selector: string): HTMLElement | null {
  return document.body.querySelector(selector)
}

let wrapper: VueWrapper | null = null
let host: HTMLDivElement | null = null

beforeEach(() => {
  vi.useRealTimers()
})

afterEach(() => {
  vi.useRealTimers()
  if (wrapper) {
    wrapper.unmount()
    wrapper = null
  }
  if (host?.parentNode) host.parentNode.removeChild(host)
  host = null
  document.body.querySelectorAll('.modal-overlay').forEach((el) => el.remove())
})

function mountDialog(sourceName = 'Claude', open = true) {
  host = document.createElement('div')
  document.body.appendChild(host)
  wrapper = mount(CopyAgentDialog, {
    props: { sourceName, open },
    attachTo: host,
    global: { plugins: [i18n] },
  })
  return wrapper
}

describe('CopyAgentDialog', () => {
  it('renders with pre-filled name', async () => {
    const w = mountDialog('Claude')!
    await w.vm.$nextTick()
    const input = $('.copy-agent-dialog__input')
    expect(input).toBeTruthy()
    // Verify the internal ref was set when the dialog opened
    const vm = w.vm as any
    expect(vm.$.setupState.newName).toContain('Claude')
  })

  it('emits close when cancel button clicked', async () => {
    const w = mountDialog()!
    const cancelBtn = $('.fbtn')!
    cancelBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(w.emitted('close')).toBeTruthy()
  })

  it('emits confirmed with trimmed name on submit', async () => {
    const w = mountDialog('Test')!
    // Set the reactive ref via setupState and force re-render
    const vm = w.vm as any
    vm.$.setupState.newName = '  My Agent  '
    w.vm.$forceUpdate()
    await w.vm.$nextTick()
    const submitBtn = $('.fbtn-primary')!
    submitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(w.emitted('confirmed')).toBeTruthy()
    expect(w.emitted('confirmed')![0]).toEqual(['My Agent'])
  })

  it('disables submit button when name is empty', async () => {
    mountDialog('')
    await nextTick()
    const submitBtn = $('.fbtn-primary') as HTMLButtonElement
    expect(submitBtn.disabled).toBe(true)
  })

  it('emits close when overlay clicked', async () => {
    vi.useFakeTimers()
    const w = mountDialog()!
    await nextTick()
    const overlay = $('.modal-overlay')!
    overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    // ModalDialog runs a 250ms leave animation before emitting close
    expect(w.emitted('close')).toBeFalsy()
    vi.advanceTimersByTime(250)
    await nextTick()
    expect(w.emitted('close')).toBeTruthy()
  })

  it('renders header and input field', async () => {
    mountDialog()
    await nextTick()
    expect($('.copy-agent-dialog__input')).toBeTruthy()
    expect($('.fbtn')).toBeTruthy()
    expect($('.fbtn-primary')).toBeTruthy()
  })

  it('does not render when open is false', async () => {
    mountDialog('Claude', false)
    await nextTick()
    expect($('.modal-overlay')).toBeFalsy()
  })

  it('pre-fills name when opened after being closed', async () => {
    const w = mountDialog('Claude', false)!
    await nextTick()
    expect($('.modal-overlay')).toBeFalsy()

    await w.setProps({ open: true, sourceName: 'DeepSeek' })
    await nextTick()
    expect($('.modal-overlay')).toBeTruthy()
    const vm = w.vm as any
    expect(vm.$.setupState.newName).toContain('DeepSeek')
  })
})
