import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AttachmentTags from '../AttachmentTags.vue'

// Pass the real module through and override only `useI18n`: the component now
// reaches `useFileUpload` (the shared batch-terminate implementation), whose
// import graph includes `@/i18n` — a mock that exported nothing but `useI18n`
// made that transitive import fail with "No createI18n export".
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

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

  // ── URL attachment chips ──

  it('renders a URL attachment as a link', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: 'acme/widgets#1', kind: 'url', url: 'https://github.com/acme/widgets/issues/1' }],
      },
    })
    const link = wrapper.find('a.attachment-url')
    expect(link.exists()).toBe(true)
    expect(link.attributes('href')).toBe('https://github.com/acme/widgets/issues/1')
    expect(link.attributes('target')).toBe('_blank')
    expect(wrapper.text()).toContain('acme/widgets#1')
  })

  it('renders a non-http(s) URL inert instead of a live link', () => {
    // This renderer draws the same persisted FileEntry shape as the message
    // bubble, so it needs the same guard: history navigation can restore a
    // stored entry straight into the composer's attachments.
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: 'evil', kind: 'url', url: 'javascript:alert(1)' }],
      },
    })
    const link = wrapper.find('a.attachment-url')
    expect(link.attributes('href')).toBeUndefined()
    expect(link.classes()).toContain('attachment-url-inert')
  })

  it('renders the line range suffix on a ranged reference card', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 8 }],
      },
    })
    expect(wrapper.find('.attachment-filename').text()).toContain('guide.md')
    expect(wrapper.find('.attachment-filename .attachment-range').text()).toBe(':5-8')
  })

  it('falls back to icon + filename for SVG (image the backend cannot thumbnail)', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: '.clawbench/uploads/diagram.svg', isDir: false }],
      },
    })
    // Image-but-not-thumbable must never render a blank pill
    expect(wrapper.find('.attachment-image-only').exists()).toBe(false)
    expect(wrapper.find('.attachment-thumb-img').exists()).toBe(false)
    expect(wrapper.find('.attachment-filename').text()).toContain('diagram.svg')
  })

  it('emits the full entry on card click and on remove', async () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 8 }],
      },
    })
    await wrapper.find('.attachment-ref').trigger('click')
    expect(wrapper.emitted('file-click')![0]).toEqual([{ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 8 }])

    await wrapper.find('.attachment-ref .attachment-close-btn').trigger('click')
    expect(wrapper.emitted('remove')![0]).toEqual([{ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 8 }])
  })

  it('uses a composite key so two ranges of one file render as separate cards', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [
          { path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 8 },
          { path: 'docs/guide.md', isDir: false, startLine: 20, endLine: 30 },
        ],
      },
    })
    expect(wrapper.findAll('.attachment-ref')).toHaveLength(2)
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

  // ── Whole-batch terminate ──
  //
  // The drawer's terminate button is unreachable during an upload: picking
  // files closes the drawer, so this strip is the only thing on screen. Its
  // per-card "×" aborts ONE file and the sequential loop continues, so without
  // this control there was no way to stop the batch.
  it('renders no batch-terminate control when nothing is uploading', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: 'src/main.go' }],
        pendingFiles: [{
          path: 'src/done.go', previewUrl: null, isImage: false,
          uploading: false, progress: 100, size: 10,
        }],
      },
    })
    expect(wrapper.find('.chat-attachment-terminate').exists()).toBe(false)
  })

  it('renders the batch-terminate control while a file is uploading', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [],
        pendingFiles: [{
          path: '', previewUrl: null, isImage: false,
          uploading: true, progress: 40, size: 10,
        }],
      },
    })
    const btn = wrapper.find('.chat-attachment-terminate')
    expect(btn.exists()).toBe(true)
    // The i18n mock returns the key, so this pins the reuse of the drawer's
    // label rather than a second wording for the same action.
    expect(btn.attributes('title')).toBe('chat.attach.terminateUpload')
    expect(btn.attributes('aria-label')).toBe('chat.attach.terminateUpload')
  })

  it('cancels the WHOLE batch on click, not just one file', async () => {
    const { useFileUpload } = await import('@/composables/useFileUpload')
    const upload = useFileUpload()
    upload.clearPendingFiles()
    upload.attachedFiles.value.splice(0)

    const wrapper = mount(AttachmentTags, {
      props: {
        files: [],
        pendingFiles: [{
          path: '', previewUrl: null, isImage: false,
          uploading: true, progress: 40, size: 10,
        }],
      },
    })

    await wrapper.find('.chat-attachment-terminate').trigger('click')

    // The batch flag stops the sequential loop in useFileUpload — the per-card
    // "×" (remove-pending) never sets it, which is exactly why one file's "×"
    // lets the rest of the batch continue.
    expect(upload.chatUploadCancelled.value).toBe(true)
    // Every in-flight row is dropped here rather than left at its last
    // percentage (abort() alone never touches pendingFiles).
    expect(upload.pendingFiles.value.filter(f => f.uploading)).toHaveLength(0)
    // Distinct from the per-card "×", which only emits remove-pending.
    expect(wrapper.emitted('remove-pending')).toBeFalsy()
  })

  it('does NOT set the batch flag when only one pending card is removed', async () => {
    // Contrast that pins the difference between the two controls: the "×" is
    // a per-file abort, so the rest of the batch keeps uploading.
    const { useFileUpload } = await import('@/composables/useFileUpload')
    const upload = useFileUpload()
    upload.clearPendingFiles()
    upload.chatUploadCancelled.value = false

    const wrapper = mount(AttachmentTags, {
      props: {
        files: [],
        pendingFiles: [{
          path: '', previewUrl: null, isImage: false,
          uploading: true, progress: 40, size: 10,
        }],
      },
    })

    await wrapper.find('.attachment-pending .attachment-close-btn').trigger('click')

    expect(upload.chatUploadCancelled.value).toBe(false)
  })
})
