import { beforeEach, describe, expect, it, vi } from 'vitest'
;(globalThis as { __VUE_I18N_FULL_INSTALL__?: boolean }).__VUE_I18N_FULL_INSTALL__ = false
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

const h = vi.hoisted(() => ({
  apiGet: vi.fn(),
  create: vi.fn(),
  projectRoot: '/p',
}))

vi.mock('@/utils/api', () => ({ apiGet: h.apiGet }))
vi.mock('@/composables/useKnowledgeBase', () => ({
  kbCreateCollection: h.create,
}))
vi.mock('@/stores/app', () => ({
  // A getter, not a copied value: the test changes h.projectRoot per case.
  store: { state: { get projectRoot() { return h.projectRoot } } },
}))

import KBCollectionPickerDialog from '@/components/kb/KBCollectionPickerDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  missingWarn: false,
  fallbackWarn: false,
  messages: {
    en: {
      common: { loading: 'Loading', cancel: 'Cancel' },
      kb: {
        pickTitle: 'Add to knowledge base',
        pickCollection: 'Choose a knowledge base',
        pickNew: 'New knowledge base…',
        pickConfirm: 'Add',
        pickEmpty: 'No other libraries',
        nameLabel: 'Name',
        namePlaceholder: 'e.g. Policies',
      },
    },
  },
})

const collections = [
  { id: 1, name: 'Global', description: '', scope: 'global', auto_inject: true, doc_count: 0, failed_count: 0, pending_count: 0, created_at: '', updated_at: '' },
  { id: 2, name: 'Project', description: '', scope: 'project', auto_inject: true, doc_count: 0, failed_count: 0, pending_count: 0, created_at: '', updated_at: '' },
]

const ModalDialogStub = {
  props: ['open'],
  template: '<div v-if="open"><slot name="header" /><slot /><slot name="footer" /></div>',
}

function mountDialog(props: Record<string, unknown> = {}) {
  return mount(KBCollectionPickerDialog, {
    props: { open: true, ...props },
    global: {
      plugins: [i18n],
      stubs: { ModalDialog: ModalDialogStub },
    },
  })
}

describe('KBCollectionPickerDialog', () => {
  beforeEach(() => {
    h.apiGet.mockReset()
    h.create.mockReset()
    h.projectRoot = '/p'
    h.apiGet.mockResolvedValue({ collections })
  })

  it('prefers the project collection and emits it on confirm', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    // The select defaults to the project's library, not the global one.
    expect((wrapper.find('[data-role="kb-pick-collection"]').element as HTMLSelectElement).value).toBe('2')
    await wrapper.find('.fbtn-primary').trigger('click')
    expect(wrapper.emitted('confirm')?.[0]).toEqual([{ id: 2, name: 'Project', created: false }])
    wrapper.unmount()
  })

  it('creates a new collection from the prefilled name', async () => {
    h.create.mockResolvedValue({ id: 9, name: 'notes', scope: 'project' })
    const wrapper = mountDialog({ initialName: 'notes.txt' })
    await flushPromises()

    await wrapper.find('[data-role="kb-pick-collection"]').setValue('__new__')
    // The caller's prefill is shown as-is; the caller is responsible for
    // trimming a file extension when it wants to.
    const name = wrapper.find('[data-role="kb-pick-name"]')
    expect((name.element as HTMLInputElement).value).toBe('notes.txt')
    await name.setValue('笔记')
    await wrapper.find('.fbtn-primary').trigger('click')

    expect(h.create).toHaveBeenCalledWith('笔记', '', 'project')
    expect(wrapper.emitted('confirm')?.[0]).toEqual([{ id: 9, name: 'notes', created: true }])
    wrapper.unmount()
  })

  it('move mode hides creation and the excluded library', async () => {
    const wrapper = mountDialog({ allowCreate: false, excludeId: 1 })
    await flushPromises()

    const options = wrapper.findAll('[data-role="kb-pick-collection"] option')
    expect(options.map((o) => o.text())).toEqual(['Project'])
    expect((wrapper.find('[data-role="kb-pick-collection"]').element as HTMLSelectElement).value).toBe('2')

    await wrapper.find('.fbtn-primary').trigger('click')
    expect(wrapper.emitted('confirm')?.[0]).toEqual([{ id: 2, name: 'Project', created: false }])
    wrapper.unmount()
  })

  it('disables confirmation when move mode has no other library', async () => {
    h.apiGet.mockResolvedValue({ collections: [collections[1]] })
    const wrapper = mountDialog({ allowCreate: false, excludeId: 2 })
    await flushPromises()

    expect(wrapper.text()).toContain('No other libraries')
    expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('uses global scope when no project is open', async () => {
    h.projectRoot = ''
    h.create.mockResolvedValue({ id: 9, name: 'notes', scope: 'global' })
    const wrapper = mountDialog({ initialName: 'x' })
    await flushPromises()

    await wrapper.find('[data-role="kb-pick-collection"]').setValue('__new__')
    await wrapper.find('[data-role="kb-pick-name"]').setValue('笔记')
    await wrapper.find('.fbtn-primary').trigger('click')

    expect(h.create).toHaveBeenCalledWith('笔记', '', 'global')
    wrapper.unmount()
  })
})
