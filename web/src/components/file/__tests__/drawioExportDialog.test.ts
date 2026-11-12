import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import DrawioExportDialog from '@/components/file/DrawioExportDialog.vue'

// Only the hint needs real text: it is the one string with an interpolated
// value, and the assertion below checks the directory actually reaches it.
const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: { file: { drawioExport: { projectHint: 'Saved to {dir}' } } } },
})

function mountDialog() {
  return mount(DrawioExportDialog, {
    props: { open: true, dirName: '/proj/docs' },
    global: {
      plugins: [i18n],
      // ModalDialog teleports to body; render inline so the chips are reachable.
      stubs: { Teleport: { template: '<div><slot /></div>' } },
    },
  })
}

describe('DrawioExportDialog', () => {
  it('defaults to PNG into the project folder', async () => {
    const wrapper = mountDialog()
    // Saving next to the diagram is the common case; downloading is the detour.
    expect(wrapper.find('[data-format="png"]').classes()).toContain('active')
    expect(wrapper.find('[data-dest="project"]').classes()).toContain('active')
    expect(wrapper.text()).toContain('/proj/docs')
    wrapper.unmount()
  })

  it('emits the chosen format and destination', async () => {
    const wrapper = mountDialog()
    await wrapper.find('[data-format="svg"]').trigger('click')
    await wrapper.find('[data-dest="download"]').trigger('click')
    await wrapper.findAll('button').at(-1)!.trigger('click')

    expect(wrapper.emitted('confirm')?.[0]).toEqual([{ format: 'svg', destination: 'download' }])
    wrapper.unmount()
  })

  it('resets the choice when reopened', async () => {
    const wrapper = mountDialog()
    await wrapper.find('[data-format="xml"]').trigger('click')
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-format="png"]').classes()).toContain('active')
    wrapper.unmount()
  })
})
