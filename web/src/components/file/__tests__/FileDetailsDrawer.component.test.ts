import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }),
  createI18n: (opts: any) => ({
    global: { t: (k: string) => k, locale: { value: opts?.locale ?? 'en' } },
    install() {},
  }),
}))

const { mockStore } = vi.hoisted(() => ({
  mockStore: {
    state: {
      projectRoot: '/proj',
      currentDir: '/proj/src',
      dirEntries: [
        { name: 'foo.ts', modified: '2025-01-01T00:00:00Z' },
      ],
    },
  },
}))

vi.mock('@/stores/app.ts', () => ({ store: mockStore }))

vi.mock('@/utils/fileType.ts', () => ({
  getFileType: (name: string) => ({
    label: 'TypeScript',
    isMarkdown: false,
    color: '#000',
  }),
  formatFileSize: (size: number) => `${size} B`,
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    name: 'BottomSheet',
    props: ['open', 'title'],
    emits: ['close'],
    template: '<div class="bs-stub" :data-open="String(open)"><slot name="header" /><slot /></div>',
  }),
}))

vi.mock('@/components/common/FileIcon.vue', () => ({
  default: defineComponent({
    name: 'FileIcon',
    props: ['path', 'isDir', 'size'],
    template: '<span class="file-icon-stub" />',
  }),
}))

import FileDetailsDrawer from '@/components/file/FileDetailsDrawer.vue'

const i18n = (require('vue-i18n') as any).createI18n({ legacy: false, locale: 'en' })

function mountDrawer(props: Record<string, unknown> = {}, provideData: Record<string, any> = {}) {
  return mount(FileDetailsDrawer, {
    props,
    global: {
      stubs: { Teleport: true },
      plugins: [i18n],
      provide: { toast: provideData.toast ?? null },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('FileDetailsDrawer — mount', () => {
  it('mounts with null file', () => {
    const wrapper = mountDrawer({ file: null, open: true })
    expect(wrapper.exists()).toBe(true)
  })

  it('mounts with valid file', () => {
    const wrapper = mountDrawer({
      file: { name: 'foo.ts', path: 'src/foo.ts' },
      open: true,
    })
    expect(wrapper.exists()).toBe(true)
    expect(wrapper.find('.details-body').exists()).toBe(true)
  })

  it('emits close when bottom sheet closes', async () => {
    const wrapper = mountDrawer({ file: { name: 'foo.ts', path: 'src/foo.ts' }, open: true })
    const bs = wrapper.findComponent({ name: 'BottomSheet' })
    await bs.vm.$emit('close')
    expect(wrapper.emitted('close')).toBeTruthy()
  })
})

describe('FileDetailsDrawer — detailItems', () => {
  it('includes fileName, path, type rows', () => {
    const wrapper = mountDrawer({
      file: { name: 'foo.ts', path: 'src/foo.ts' },
      open: true,
    })
    expect(wrapper.text()).toContain('foo.ts')
    expect(wrapper.text()).toContain('/proj/src/foo.ts')
    expect(wrapper.text()).toContain('TypeScript')
  })

  it('includes link target row for symlinks', () => {
    const wrapper = mountDrawer({
      file: { name: 'link.ts', path: 'src/link.ts', isSymlink: true, linkTarget: '/target/path' },
      open: true,
    })
    expect(wrapper.text()).toContain('/target/path')
  })

  it('shows broken link label when symlink target missing', () => {
    const wrapper = mountDrawer({
      file: { name: 'link.ts', path: 'src/link.ts', isSymlink: true },
      open: true,
    })
    expect(wrapper.text()).toContain('file.details.brokenLink')
  })

  it('includes size row when size is provided', () => {
    const wrapper = mountDrawer({
      file: { name: 'foo.ts', path: 'src/foo.ts', size: 1024 },
      open: true,
    })
    expect(wrapper.text()).toContain('1024 B')
  })

  it('includes modified time when entry has modified date', () => {
    const wrapper = mountDrawer({
      file: { name: 'foo.ts', path: '/proj/src/foo.ts' },
      open: true,
    })
    expect(wrapper.text()).toContain('file.details.modifiedTime')
  })

  it('includes line count and char count when content is provided', () => {
    const wrapper = mountDrawer({
      file: { name: 'foo.ts', path: 'src/foo.ts', content: 'line1\nline2\nline3' },
      open: true,
    })
    expect(wrapper.text()).toContain('3')
    expect(wrapper.text()).toContain('17')
  })

  it('always includes encoding row', () => {
    const wrapper = mountDrawer({
      file: { name: 'foo.ts', path: 'src/foo.ts' },
      open: true,
    })
    expect(wrapper.text()).toContain('UTF-8')
  })
})

describe('FileDetailsDrawer — copyValue', () => {
  it('uses navigator.clipboard when available', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    ;(navigator as any).clipboard = { writeText }
    const toastShow = vi.fn()
    const wrapper = mountDrawer(
      { file: { name: 'foo.ts', path: 'src/foo.ts' }, open: true },
      { toast: { show: toastShow } }
    )
    const vm = wrapper.vm as any
    const wrap = document.createElement('div')
    wrap.innerHTML = '<span class="details-value">value</span><button class="details-copy-btn"></button>'
    Object.defineProperty(wrap, 'closest', { value: () => wrap })
    const btn = wrap.querySelector('.details-copy-btn')!
    const txt = wrap.querySelector('.details-value')!
    const ev = { currentTarget: wrap } as any
    vm.copyValue('value', ev)
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('value')
    expect(btn.classList.contains('copied')).toBe(true)
    expect(txt.classList.contains('copied')).toBe(true)
    expect(toastShow).toHaveBeenCalled()
  })

  it('falls back to execCommand when clipboard API unavailable', async () => {
    ;(navigator as any).clipboard = undefined
    ;(document as any).execCommand = vi.fn(() => true)
    const wrapper = mountDrawer(
      { file: { name: 'foo.ts', path: 'src/foo.ts' }, open: true }
    )
    const vm = wrapper.vm as any
    const wrap = document.createElement('div')
    wrap.innerHTML = '<span class="details-value">value</span><button class="details-copy-btn"></button>'
    Object.defineProperty(wrap, 'closest', { value: () => wrap })
    const ev = { currentTarget: wrap } as any
    vm.copyValue('value', ev)
    expect((document as any).execCommand).toHaveBeenCalledWith('copy')
  })

  it('catches clipboard rejection and uses fallback', async () => {
    ;(navigator as any).clipboard = {
      writeText: () => Promise.reject(new Error('denied')),
    }
    ;(document as any).execCommand = vi.fn(() => true)
    const wrapper = mountDrawer(
      { file: { name: 'foo.ts', path: 'src/foo.ts' }, open: true }
    )
    const vm = wrapper.vm as any
    const wrap = document.createElement('div')
    wrap.innerHTML = '<span class="details-value">value</span><button class="details-copy-btn"></button>'
    Object.defineProperty(wrap, 'closest', { value: () => wrap })
    const ev = { currentTarget: wrap } as any
    vm.copyValue('value', ev)
    await flushPromises()
    expect((document as any).execCommand).toHaveBeenCalledWith('copy')
  })
})