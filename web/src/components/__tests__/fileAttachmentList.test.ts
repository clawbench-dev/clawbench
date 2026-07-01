import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import FileAttachmentList from '@/components/chat/FileAttachmentList.vue'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: { chat: { attach: { openFile: 'Open file' } } } } })

describe('FileAttachmentList', () => {
  function mountComponent(files) {
    return mount(FileAttachmentList, {
      props: { files },
      global: { plugins: [i18n] },
    })
  }

  it('renders nothing when files array is empty', () => {
    const wrapper = mountComponent([])
    expect(wrapper.find('.chat-files').exists()).toBe(false)
  })

  it('renders file cards for each file', () => {
    const wrapper = mountComponent(['src/main.go', 'src/app.ts'])
    const cards = wrapper.findAll('.chat-file-attachment')
    expect(cards).toHaveLength(2)
  })

  it('applies attachment-upload class for upload paths', () => {
    const wrapper = mountComponent(['.clawbench/uploads/image.png'])
    const card = wrapper.find('.chat-file-attachment')
    expect(card.classes()).toContain('attachment-upload')
  })

  it('applies attachment-ref class for project paths', () => {
    const wrapper = mountComponent(['src/main.go'])
    const card = wrapper.find('.chat-file-attachment')
    expect(card.classes()).toContain('attachment-ref')
  })

  it('renders file name in attachment-filename for non-image files', () => {
    const wrapper = mountComponent(['src/main.go'])
    expect(wrapper.find('.attachment-filename').text()).toBe('main.go')
  })

  it('does not render icon or thumb area for non-image files', () => {
    const wrapper = mountComponent(['src/main.go'])
    expect(wrapper.find('.attachment-thumb').exists()).toBe(false)
    expect(wrapper.find('.attachment-thumb-img').exists()).toBe(false)
  })

  it('renders thumbnail img for image files', () => {
    const wrapper = mountComponent(['img/photo.png'])
    expect(wrapper.find('.attachment-thumb-img').exists()).toBe(true)
    expect(wrapper.find('.attachment-filename').exists()).toBe(false)
  })

  it('applies attachment-image-only class for image files', () => {
    const wrapper = mountComponent(['img/photo.png'])
    const card = wrapper.find('.chat-file-attachment')
    expect(card.classes()).toContain('attachment-image-only')
  })

  it('emits file-tag-click on card click', async () => {
    const wrapper = mountComponent(['src/main.go'])
    await wrapper.find('.chat-file-attachment').trigger('click')
    expect(wrapper.emitted('file-tag-click')).toBeTruthy()
    expect(wrapper.emitted('file-tag-click')[0][0]).toBe('src/main.go')
  })

  it('supports {path} object format', () => {
    const wrapper = mountComponent([{ path: 'src/main.go' }])
    expect(wrapper.findAll('.chat-file-attachment')).toHaveLength(1)
    expect(wrapper.find('.attachment-filename').text()).toBe('main.go')
  })

  it('container uses horizontal scroll layout', () => {
    const wrapper = mountComponent(['a.txt', 'b.txt'])
    const container = wrapper.find('.chat-files')
    expect(container.exists()).toBe(true)
    const style = getComputedStyle(container.element)
    expect(style.flexWrap).toBe('nowrap')
  })
})
