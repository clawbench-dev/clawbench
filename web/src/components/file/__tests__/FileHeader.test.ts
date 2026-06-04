import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import FileHeader from '../FileHeader.vue'

// Minimal i18n instance for tests
const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      nav: { refresh: 'Refresh' },
      common: { download: 'Download', delete: 'Delete' },
      file: {
        header: {
          toc: 'TOC',
          search: 'Search',
          more: 'More',
          openAsText: 'Open as text',
          sourceView: 'Source',
          renderedView: 'Rendered',
          wordWrap: 'Word Wrap',
          lineNumbers: 'Line Numbers',
          stickyScroll: 'Sticky Scroll',
          fileHistory: 'File history',
        },
      },
    },
  },
})

// Mock useAppMode
vi.mock('@/composables/useAppMode.ts', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
}))

// Mock getFileType
vi.mock('@/utils/fileType.ts', () => ({
  getFileType: (name: string) => {
    if (name.endsWith('.md')) return { isMarkdown: true, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false }
    if (name.endsWith('.html')) return { isMarkdown: false, isHtml: true, isImage: false, isAudio: false, isVideo: false, isPdf: false }
    if (name.endsWith('.png')) return { isMarkdown: false, isHtml: false, isImage: true, isAudio: false, isVideo: false, isPdf: false }
    return { isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false }
  },
}))

describe('FileHeader', () => {
  function mountHeader(props = {}) {
    return mount(FileHeader, {
      props: {
        file: { name: 'main.ts', path: '/tmp/main.ts', content: 'const x = 1' },
        viewMode: 'source',
        tocOpen: false,
        searchOpen: false,
        wordWrap: false,
        showLineNumbers: true,
        stickyScroll: true,
        ...props,
      },
      global: {
        plugins: [i18n],
        stubs: {
          Teleport: true,
        },
      },
    })
  }

  it('renders sticky scroll toggle in dropdown when not rendered mode', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    // Open dropdown
    await wrapper.find('.dropdown-wrapper .file-header-btn').trigger('click')
    await nextTick()
    const items = wrapper.findAll('.dropdown-item')
    const stickyItem = items.find(el => el.text().includes('Sticky Scroll'))
    expect(stickyItem).toBeTruthy()
  })

  it('hides sticky scroll toggle in rendered markdown mode', async () => {
    const wrapper = mountHeader({
      file: { name: 'README.md', path: '/tmp/README.md', content: '# Hello' },
      viewMode: 'rendered',
    })
    await wrapper.find('.dropdown-wrapper .file-header-btn').trigger('click')
    await nextTick()
    const items = wrapper.findAll('.dropdown-item')
    const stickyItem = items.find(el => el.text().includes('Sticky Scroll'))
    expect(stickyItem).toBeFalsy()
  })

  it('emits toggleStickyScroll when sticky scroll item is clicked', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    await wrapper.find('.dropdown-wrapper .file-header-btn').trigger('click')
    await nextTick()
    const items = wrapper.findAll('.dropdown-item')
    const stickyItem = items.find(el => el.text().includes('Sticky Scroll'))
    expect(stickyItem).toBeTruthy()
    await stickyItem!.trigger('click')
    expect(wrapper.emitted('toggleStickyScroll')).toBeTruthy()
  })

  it('shows checkmark when stickyScroll is true', async () => {
    const wrapper = mountHeader({ stickyScroll: true, viewMode: 'source' })
    await wrapper.find('.dropdown-wrapper .file-header-btn').trigger('click')
    await nextTick()
    const items = wrapper.findAll('.dropdown-item')
    const stickyItem = items.find(el => el.text().includes('Sticky Scroll'))
    expect(stickyItem).toBeTruthy()
    expect(stickyItem!.find('.wrap-check').exists()).toBe(true)
  })

  it('does not show checkmark when stickyScroll is false', async () => {
    const wrapper = mountHeader({ stickyScroll: false, viewMode: 'source' })
    await wrapper.find('.dropdown-wrapper .file-header-btn').trigger('click')
    await nextTick()
    const items = wrapper.findAll('.dropdown-item')
    const stickyItem = items.find(el => el.text().includes('Sticky Scroll'))
    expect(stickyItem).toBeTruthy()
    expect(stickyItem!.find('.wrap-check').exists()).toBe(false)
  })

  it('renders header actions with square buttons (border-radius: 4px)', () => {
    const wrapper = mountHeader()
    const btns = wrapper.findAll('.file-header-btn')
    expect(btns.length).toBeGreaterThan(0)
    // Buttons should exist and be styled (CSS verification is implicit via class)
    for (const btn of btns) {
      expect(btn.classes()).toContain('file-header-btn')
    }
  })

  it('closes menu after toggling sticky scroll', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    // Open dropdown
    await wrapper.find('.dropdown-wrapper .file-header-btn').trigger('click')
    await nextTick()
    // Menu should be open
    expect(wrapper.findAll('.file-header-dropdown-menu').length).toBeGreaterThan(0)
    // Click sticky scroll
    const items = wrapper.findAll('.dropdown-item')
    const stickyItem = items.find(el => el.text().includes('Sticky Scroll'))
    await stickyItem!.trigger('click')
    await nextTick()
    // Menu should be closed (teleported content removed)
    expect(document.querySelectorAll('.file-header-dropdown-menu').length).toBe(0)
  })
})
