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
  isUrlEntry: (f: any) => f?.kind === 'url' && !!f?.url,
  isSafeExternalUrl: (u: string | undefined) => !!u && /^https?:\/\//.test(u),
}))

vi.mock('@/utils/fileManager.ts', () => ({
  isThumbableExt: (p: string) => /\.(png|jpg|jpeg|gif|webp)$/i.test(p),
}))

vi.mock('@/utils/fileIcon.ts', () => ({
  getFileIcon: () => 'FileText',
  getFileIconColor: () => '#8b8b8b',
  buildPathThumbUrl: (path: string) => `/api/file/thumb?path=${encodeURIComponent(path)}&w=80`,
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

  it('falls back to icon + filename for SVG (image the backend cannot thumbnail)', () => {
    const wrapper = mountList([{ path: 'img/logo.svg' }])
    // Image-but-not-thumbable must never render a blank card
    expect(wrapper.find('.attachment-image-only').exists()).toBe(false)
    expect(wrapper.find('.attachment-thumb-img').exists()).toBe(false)
    expect(wrapper.find('.attachment-filename').exists()).toBe(true)
    expect(wrapper.text()).toContain('logo.svg')
  })

  it('applies upload class for upload paths', () => {
    const wrapper = mountList([{ path: '/upload/file.txt' }])
    expect(wrapper.find('.attachment-upload').exists()).toBe(true)
  })

  it('applies ref class for non-upload paths', () => {
    const wrapper = mountList([{ path: 'src/main.ts' }])
    expect(wrapper.find('.attachment-ref').exists()).toBe(true)
  })

  it('emits file-tag-click on click with the full entry', async () => {
    const wrapper = mountList([{ path: 'src/main.ts' }])
    await wrapper.find('.chat-file-attachment').trigger('click')
    expect(wrapper.emitted('file-tag-click')).toBeTruthy()
    expect(wrapper.emitted('file-tag-click')![0]).toEqual([{ path: 'src/main.ts' }])
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

  // ── URL attachments (forge issue/PR references from "Analyze with AI") ──

  it('renders a URL attachment as a real link with its label', () => {
    // Regression: the reloaded message showed an empty chip with a generic file
    // icon and no href, because this component had no URL branch and the chip
    // fell through to the file-card path (path is a label, not a filesystem
    // path, so the filename resolved to '').
    const wrapper = mountList([
      { path: 'acme/widgets#451', kind: 'url', url: 'https://github.com/acme/widgets/issues/451' },
    ])
    const link = wrapper.find('a.chat-file-attachment')
    expect(link.exists(), 'a URL entry must render as an anchor').toBe(true)
    expect(link.attributes('href')).toBe('https://github.com/acme/widgets/issues/451')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toContain('noopener')
    // The label is shown, not a blank filename.
    expect(wrapper.text()).toContain('acme/widgets#451')
  })

  it('falls back to the address when a URL entry has no label', () => {
    const wrapper = mountList([{ path: '', kind: 'url', url: 'https://example.com/x' }])
    expect(wrapper.text()).toContain('https://example.com/x')
  })

  it('does not emit file-tag-click for a URL entry', async () => {
    // A URL is not a file: clicking must navigate, never open a file preview.
    const wrapper = mountList([{ path: 'a#1', kind: 'url', url: 'https://example.com/a' }])
    await wrapper.find('a.chat-file-attachment').trigger('click')
    expect(wrapper.emitted('file-tag-click')).toBeFalsy()
  })

  it('renders a non-http(s) URL inert rather than as a live link', () => {
    // The href is data restored from the DB, so a javascript: address must not
    // become a clickable link.
    const wrapper = mountList([{ path: 'evil', kind: 'url', url: 'javascript:alert(1)' }])
    const link = wrapper.find('a.chat-file-attachment')
    expect(link.attributes('href')).toBeUndefined()
    expect(link.classes()).toContain('attachment-url-inert')
  })

  it('renders URL and file attachments side by side', () => {
    const wrapper = mountList([
      { path: 'acme/widgets#451', kind: 'url', url: 'https://github.com/acme/widgets/issues/451' },
      { path: 'src/main.ts' },
    ])
    expect(wrapper.findAll('.chat-file-attachment').length).toBe(2)
    expect(wrapper.find('a.chat-file-attachment').exists()).toBe(true)
    expect(wrapper.find('span.chat-file-attachment').exists()).toBe(true)
  })
})
