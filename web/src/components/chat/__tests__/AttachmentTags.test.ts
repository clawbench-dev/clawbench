import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AttachmentTags from '../AttachmentTags.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/utils/fileIcon', () => ({
  buildPathThumbUrl: (path: string) => `http://localhost/thumb/${path}`,
  getFileIconUrl: vi.fn().mockResolvedValue('http://localhost/icon.svg'),
  getFolderIconUrl: vi.fn().mockResolvedValue('http://localhost/folder.svg'),
}))

describe('AttachmentTags', () => {
  it('renders attached files correctly', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: 'src/main.go', isDir: false }],
      },
    })
    expect(wrapper.find('.attachment-filename').text()).toBe('main.go')
  })

  it('renders pending files with local Blob image preview and progress overlay', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [],
        pendingFiles: [
          {
            path: '',
            previewUrl: 'blob:http://localhost/test-blob-123',
            isImage: true,
            uploading: true,
            progress: 45,
            size: 1024,
          },
        ],
      },
    })

    const pendingItem = wrapper.find('.attachment-pending')
    expect(pendingItem.exists()).toBe(true)
    const img = wrapper.find('.attachment-uploading-img')
    expect(img.attributes('src')).toBe('blob:http://localhost/test-blob-123')
    expect(wrapper.find('.attachment-progress-text').text()).toBe('45%')
  })

  it('does not render completed pending files alongside attached files', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: '.clawbench/uploads/screenshot.png', isDir: false }],
        pendingFiles: [{
          path: '.clawbench/uploads/screenshot.png', previewUrl: 'blob:test', isImage: true,
          uploading: false, progress: 100, size: 1024,
        }],
      },
    })

    expect(wrapper.find('.attachment-pending').exists()).toBe(false)
    expect(wrapper.findAll('.attachment-ref')).toHaveLength(1)
  })

  it('emits remove-pending when clicking remove on a pending file', async () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [],
        pendingFiles: [
          {
            path: '',
            previewUrl: 'blob:http://localhost/test-blob-123',
            isImage: true,
            uploading: true,
            progress: 50,
            size: 2048,
          },
        ],
      },
    })

    const removeBtn = wrapper.find('.attachment-pending .attachment-close-btn')
    await removeBtn.trigger('click')

    expect(wrapper.emitted('remove-pending')).toBeTruthy()
    expect(wrapper.emitted('remove-pending')![0]).toEqual([0])
  })
})
