import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => {
        const map: Record<string, string> = {
          'settings.items.agentCreateAcpTitle': 'Add ACP Agent',
          'settings.items.agentName': 'Name',
          'settings.items.agentCreateAcpNamePlaceholder': 'Enter an agent name',
          'settings.items.agentCreateAcpCommand': 'ACP Command',
          'settings.items.agentCreateAcpCommandPlaceholder': 'e.g. trae acp',
          'settings.items.agentCreateAcpHint': 'The command must start an ACP server',
          'settings.items.agentCreateAcpConfirm': 'Add',
          'settings.items.agentCreateAcpCommandRequired': 'The ACP command cannot be empty',
          'settings.items.agentCopyEmptyName': 'The name cannot be empty',
          'common.cancel': 'Cancel',
        }
        return map[key] ?? key
      },
    }),
  }
})

// ModalDialog Teleports to <body>; stub it to a plain wrapper so the form can be
// asserted in place (and so the real component's back-handler registration,
// which needs a live overlay stack, stays out of this test).
vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: {
    name: 'ModalDialog',
    props: ['open', 'title', 'zIndex', 'maxWidth'],
    template: '<div v-if="open" class="modal-stub"><slot /><slot name="footer" /></div>',
  },
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import CreateAcpAgentDialog from '@/components/settings/CreateAcpAgentDialog.vue'

function mountDialog(props: Record<string, unknown> = {}) {
  return mount(CreateAcpAgentDialog, {
    props: { open: true, ...props },
  })
}

function inputs(wrapper: ReturnType<typeof mountDialog>) {
  return wrapper.findAll('input')
}

describe('CreateAcpAgentDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders nothing while closed', () => {
    const wrapper = mountDialog({ open: false })
    expect(wrapper.find('.modal-stub').exists()).toBe(false)
  })

  it('renders a name field and an ACP command field when open', () => {
    const wrapper = mountDialog()
    const fields = inputs(wrapper)
    expect(fields).toHaveLength(2)
    expect(fields[0].attributes('placeholder')).toBe('Enter an agent name')
    expect(fields[1].attributes('placeholder')).toBe('e.g. trae acp')
  })

  it('disables submit until both fields are filled', async () => {
    const wrapper = mountDialog()
    const submit = wrapper.findAll('button').at(-1)!

    expect(submit.attributes('disabled')).toBeDefined()

    const [name, command] = inputs(wrapper)
    await name.setValue('Trae')
    expect(submit.attributes('disabled')).toBeDefined()

    await command.setValue('trae acp')
    expect(submit.attributes('disabled')).toBeUndefined()
  })

  it('treats whitespace-only input as empty', async () => {
    const wrapper = mountDialog()
    const [name, command] = inputs(wrapper)
    await name.setValue('Trae')
    await command.setValue('   ')
    expect(wrapper.findAll('button').at(-1)!.attributes('disabled')).toBeDefined()
  })

  it('emits trimmed name and command on submit', async () => {
    const wrapper = mountDialog()
    const [name, command] = inputs(wrapper)
    await name.setValue('  Trae  ')
    await command.setValue('  trae acp  ')
    await wrapper.findAll('button').at(-1)!.trigger('click')

    expect(wrapper.emitted('confirmed')).toEqual([
      [{ name: 'Trae', command: 'trae acp' }],
    ])
  })

  it('submits on Enter from either field', async () => {
    const wrapper = mountDialog()
    const [name, command] = inputs(wrapper)
    await name.setValue('Trae')
    await command.setValue('trae acp')
    await command.trigger('keydown', { key: 'Enter' })

    expect(wrapper.emitted('confirmed')).toHaveLength(1)
  })

  it('reports the missing command instead of emitting an empty one', async () => {
    const wrapper = mountDialog()
    const [name, command] = inputs(wrapper)
    await name.setValue('Trae')
    // The submit button is disabled in this state, so Enter is the only way to
    // reach submit() — it must still refuse rather than emit an empty command.
    await command.trigger('keydown', { key: 'Enter' })

    expect(wrapper.emitted('confirmed')).toBeUndefined()
    expect(wrapper.find('.create-acp-agent__error').text()).toBe('The ACP command cannot be empty')
  })

  it('refuses to submit a disabled click with no command', async () => {
    const wrapper = mountDialog()
    await inputs(wrapper)[0].setValue('Trae')
    await wrapper.findAll('button').at(-1)!.trigger('click')

    expect(wrapper.emitted('confirmed')).toBeUndefined()
  })

  it('shows a server-supplied error inside the dialog', async () => {
    const wrapper = mountDialog({ errorMessage: 'An agent with this name already exists' })
    expect(wrapper.find('.create-acp-agent__error').text()).toBe('An agent with this name already exists')
  })

  it('clears both fields each time it reopens', async () => {
    const wrapper = mountDialog()
    const [name, command] = inputs(wrapper)
    await name.setValue('Stale')
    await command.setValue('stale acp')

    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })

    const [freshName, freshCommand] = inputs(wrapper)
    expect((freshName.element as HTMLInputElement).value).toBe('')
    expect((freshCommand.element as HTMLInputElement).value).toBe('')
  })

  it('emits close when the cancel button is pressed', async () => {
    const wrapper = mountDialog()
    const cancel = wrapper.findAll('button')[0]
    await cancel.trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
