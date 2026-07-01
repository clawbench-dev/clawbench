import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import FileAttachmentList from '../FileAttachmentList.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        attach: { openFile: 'Open file' },
      },
    },
  },
})

vi.mock('@/utils/path.ts', () => ({
  baseName: (p: string) => p.split('/').pop() || '',
}))

vi.mock('@/utils/fileAttachmentUtils.ts', () => ({
  normalizeFileEntry: (f: any) => typeof f === 'string' ? { path: f } : f,
  isUploadPath: (p: string) => p.startsWith('/upload/'),
  isImageFile: (p: string) => /\.(png|jpg|jpeg|gif|webp|svg)$/i.test(p),
}))

vi.mock('@/utils/fileManager.ts', () => ({
  isThumbableExt: (p: string) => /\.(png|jpg|jpeg|gif|webp)$/i.test(p),
}))

describe('FileAttachmentList', () => {
  function mountList(files: any[] = []) {
    return mount(FileAttachmentList, {
      props: { files },
      global: { plugins: [i18n] },
    })
  }

  it('renders nothing when no files', () => {
    const wrapper = mountList([])
    expect(wrapper.find('.chat-files').exists()).toBe(false)
  })

  it('renders file attachments', () => {
    const wrapper = mountList([{ path: 'src/main.ts' }])
    expect(wrapper.find('.chat-files').exists()).toBe(true)
    expect(wrapper.findAll('.chat-file-attachment').length).toBe(1)
  })

  it('shows filename for non-image files', () => {
    const wrapper = mountList([{ path: 'src/main.ts' }])
    expect(wrapper.find('.attachment-filename').exists()).toBe(true)
    expect(wrapper.text()).toContain('main.ts')
  })

  it('shows thumbnail for image files', () => {
    const wrapper = mountList([{ path: 'img/photo.png' }])
    expect(wrapper.find('.attachment-image-only').exists()).toBe(true)
  })

  it('applies upload class for upload paths', () => {
    const wrapper = mountList([{ path: '/upload/file.txt' }])
    expect(wrapper.find('.attachment-upload').exists()).toBe(true)
  })

  it('applies ref class for non-upload paths', () => {
    const wrapper = mountList([{ path: 'src/main.ts' }])
    expect(wrapper.find('.attachment-ref').exists()).toBe(true)
  })

  it('emits file-tag-click on click', async () => {
    const wrapper = mountList([{ path: 'src/main.ts' }])
    await wrapper.find('.chat-file-attachment').trigger('click')
    expect(wrapper.emitted('file-tag-click')).toBeTruthy()
    expect(wrapper.emitted('file-tag-click')![0]).toEqual(['src/main.ts'])
  })

  it('renders multiple files', () => {
    const wrapper = mountList([{ path: 'a.ts' }, { path: 'b.ts' }, { path: 'c.ts' }])
    expect(wrapper.findAll('.chat-file-attachment').length).toBe(3)
  })

  it('handles string paths directly', () => {
    const wrapper = mountList(['src/main.ts' as any])
    expect(wrapper.findAll('.chat-file-attachment').length).toBe(1)
    expect(wrapper.text()).toContain('main.ts')
  })
})
