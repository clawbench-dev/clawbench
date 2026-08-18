import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import FileIcon from '../FileIcon.vue'

vi.mock('@/utils/materialIcons', () => ({
  getFileIconUrl: (path: string) => Promise.resolve(`/icons/${path}.svg`),
  getFolderIconUrl: (path: string, open: boolean) => Promise.resolve(`/icons/folder-${open ? 'open' : 'closed'}-${path}.svg`),
}))

describe('FileIcon', () => {
  it('renders file icon', async () => {
    const wrapper = mount(FileIcon, { props: { path: 'test.ts' } })
    await flushPromises()
    const img = wrapper.find('img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toContain('test.ts')
  })

  it('renders folder icon', async () => {
    const wrapper = mount(FileIcon, { props: { path: 'src', isDir: true } })
    await flushPromises()
    const img = wrapper.find('img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toContain('folder-closed')
  })

  it('renders open folder icon', async () => {
    const wrapper = mount(FileIcon, { props: { path: 'src', isDir: true, isDirOpen: true } })
    await flushPromises()
    const img = wrapper.find('img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toContain('folder-open')
  })

  it('applies custom size', async () => {
    const wrapper = mount(FileIcon, { props: { path: 'test.ts', size: 24 } })
    await flushPromises()
    const img = wrapper.find('img')
    expect(img.attributes('style')).toContain('24px')
  })

  it('sets correct alt text for file', async () => {
    const wrapper = mount(FileIcon, { props: { path: 'test.ts' } })
    await flushPromises()
    expect(wrapper.find('img').attributes('alt')).toBe('file: test.ts')
  })

  it('sets correct alt text for folder', async () => {
    const wrapper = mount(FileIcon, { props: { path: 'src', isDir: true } })
    await flushPromises()
    expect(wrapper.find('img').attributes('alt')).toBe('folder: src')
  })
})
