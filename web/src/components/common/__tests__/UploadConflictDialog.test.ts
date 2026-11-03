import { describe, it, expect, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import UploadConflictDialog from '@/components/common/UploadConflictDialog.vue'
import { askUploadConflict, resolveUploadConflict, useUploadConflict, _resetUploadConflict } from '@/composables/useUploadConflict'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

function mountDialog(locale: 'en' | 'zh' = 'zh') {
  const i18n = createI18n({ legacy: false, locale, messages: { en, zh } })
  return mount(UploadConflictDialog, {
    global: {
      plugins: [i18n],
      stubs: { Teleport: { template: '<div><slot /></div>' } },
    },
  })
}

describe('UploadConflictDialog', () => {
  beforeEach(() => {
    _resetUploadConflict()
  })

  it('stays closed until a batch asks', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    expect(wrapper.find('.modal-overlay').exists()).toBe(false)
  })

  it('lists the conflicting destinations and settles the batch on overwrite', async () => {
    const wrapper = mountDialog()
    const answer = askUploadConflict([
      { name: 'a.txt', relpath: '', path: 'a.txt' },
      { name: 'b.txt', relpath: 'src', path: 'src/b.txt' },
    ])
    await flushPromises()

    expect(wrapper.text()).toContain(zh.upload.conflict.title)
    const rows = wrapper.findAll('.ucd-item').map(r => r.text())
    expect(rows).toEqual(['a.txt', 'src/b.txt'])

    // The direct overwrite button is the point of this dialog.
    const buttons = wrapper.findAll('.modal-footer button')
    const overwrite = buttons.find(b => b.text() === zh.upload.conflict.overwrite)
    expect(overwrite).toBeTruthy()
    await overwrite!.trigger('click')

    await expect(answer).resolves.toBe('overwrite')
    await flushPromises()
    // ModalDialog keeps the node mounted (`v-show` + a leave transition), so
    // the observable truth is the prompt state, not the DOM's existence.
    expect(useUploadConflict().state.value.visible).toBe(false)
  })

  it('settles with rename when "keep both" is chosen', async () => {
    const wrapper = mountDialog()
    const answer = askUploadConflict([{ name: 'a.txt', relpath: '', path: 'a.txt' }])
    await flushPromises()

    const keepBoth = wrapper.findAll('.modal-footer button')
      .find(b => b.text() === zh.upload.conflict.keepBoth)
    await keepBoth!.trigger('click')

    await expect(answer).resolves.toBe('rename')
  })

  it('summarizes the tail instead of listing hundreds of rows', async () => {
    const wrapper = mountDialog()
    const many = Array.from({ length: 20 }, (_, i) => ({
      name: `f${i}.txt`,
      relpath: '',
      path: `f${i}.txt`,
    }))
    askUploadConflict(many)
    await flushPromises()

    expect(wrapper.findAll('.ucd-item')).toHaveLength(8)
    expect(wrapper.text()).toContain('12')
    resolveUploadConflict('cancel')
  })

  it('renders the English copy for an English locale', async () => {
    const wrapper = mountDialog('en')
    askUploadConflict([{ name: 'a.txt', relpath: '', path: 'a.txt' }])
    await flushPromises()

    expect(wrapper.text()).toContain(en.upload.conflict.title)
    expect(wrapper.text()).toContain(en.upload.conflict.overwrite)
    resolveUploadConflict('cancel')
  })
})
