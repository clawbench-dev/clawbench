import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
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

function mountDialog(sourceName = 'Claude') {
  return mount(CopyAgentDialog, {
    props: { sourceName },
    global: { plugins: [i18n] },
  })
}

describe('CopyAgentDialog', () => {
  it('renders with pre-filled name', async () => {
    const wrapper = mountDialog('Claude')
    await wrapper.vm.$nextTick()
    const input = wrapper.find('.copy-agent-dialog__input')
    expect(input.exists()).toBe(true)
    // Verify the internal ref was set correctly on mount
    const vm = wrapper.vm as any
    expect(vm.$.setupState.newName).toContain('Claude')
  })

  it('emits close when cancel button clicked', async () => {
    const wrapper = mountDialog()
    const cancelBtn = wrapper.find('.copy-agent-dialog__btn--cancel')
    await cancelBtn.trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('emits confirmed with trimmed name on submit', async () => {
    const wrapper = mountDialog('Test')
    // Set the reactive ref via setupState and force re-render
    const vm = wrapper.vm as any
    vm.$.setupState.newName = '  My Agent  '
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()
    const submitBtn = wrapper.find('.copy-agent-dialog__btn--submit')
    await submitBtn.trigger('click')
    expect(wrapper.emitted('confirmed')).toBeTruthy()
    expect(wrapper.emitted('confirmed')![0]).toEqual(['My Agent'])
  })

  it('disables submit button when name is empty', async () => {
    const wrapper = mountDialog('')
    await wrapper.vm.$nextTick()
    const submitBtn = wrapper.find('.copy-agent-dialog__btn--submit')
    expect((submitBtn.element as HTMLButtonElement).disabled).toBe(true)
  })

  it('emits close when overlay clicked', async () => {
    const wrapper = mountDialog()
    const overlay = wrapper.find('.copy-agent-dialog-overlay')
    await overlay.trigger('click.self')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('renders header and input field', async () => {
    const wrapper = mountDialog()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.copy-agent-dialog__header').exists()).toBe(true)
    expect(wrapper.find('.copy-agent-dialog__input').exists()).toBe(true)
    expect(wrapper.find('.copy-agent-dialog__btn--cancel').exists()).toBe(true)
    expect(wrapper.find('.copy-agent-dialog__btn--submit').exists()).toBe(true)
  })
})
