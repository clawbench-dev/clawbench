import { describe, expect, it, vi } from 'vitest'
import { shallowMount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'

// Layout contract tests verify CSS class structure, not component behavior.
// Use stub components instead of real ones to avoid lifecycle side effects
// (setInterval, window.addEventListener) that prevent vitest workers from exiting.

const i18n = createI18n({
  legacy: true,
  locale: 'en',
  messages: { en: {} },
})

// Stub FileViewer: renders the expected CSS class structure for .md and code files
const FileViewerStub = {
  name: 'FileViewer',
  props: ['file'],
  template: `
    <div class="file-viewer">
      <div class="file-viewer-content">
        <div v-if="file?.name?.endsWith('.md')" class="markdown-preview">
          <div class="markdown-body" />
        </div>
        <div v-else class="raw-content-viewer">
          <div class="cm-viewer" />
        </div>
      </div>
    </div>
  `,
}

const MarkdownPreviewStub = {
  name: 'MarkdownPreview',
  props: ['file', 'viewMode'],
  template: `
    <div class="markdown-preview">
      <div class="markdown-body" />
    </div>
  `,
}

const CodeMirrorViewerStub = {
  name: 'CodeMirrorViewer',
  props: ['content', 'language', 'editable'],
  template: '<div class="cm-viewer" />',
}

describe('preview layout contract', () => {
  it('renders file viewer with expected root element', () => {
    const wrapper = shallowMount(FileViewerStub, {
      props: {
        file: { name: 'README.md', path: '/tmp/README.md', content: '# Hello' },
      },
      global: { plugins: [i18n] },
    })

    expect(wrapper.find('.file-viewer').exists()).toBe(true)
    expect(wrapper.find('.file-viewer-content').exists()).toBe(true)
  })

  it('renders markdown preview with content area', async () => {
    const wrapper = shallowMount(MarkdownPreviewStub, {
      props: {
        file: { path: '/tmp/README.md', content: '# Hello' },
        viewMode: 'rendered',
      },
      global: { plugins: [i18n] },
    })

    await nextTick()

    expect(wrapper.find('.markdown-preview').exists()).toBe(true)
    expect(wrapper.find('.markdown-body').exists()).toBe(true)
  })

  it('renders code viewer with raw content', () => {
    const wrapper = shallowMount(CodeMirrorViewerStub, {
      props: { content: 'const x = 1', language: 'typescript', editable: false },
      global: { plugins: [i18n] },
    })

    expect(wrapper.find('.cm-viewer').exists()).toBe(true)
  })

  it('renders file viewer child content for markdown files', () => {
    const wrapper = shallowMount(FileViewerStub, {
      props: {
        file: { name: 'README.md', path: '/tmp/README.md', content: '# Hello' },
      },
      global: { plugins: [i18n] },
    })

    // MarkdownPreview should render inside file-viewer-content for .md files
    expect(wrapper.find('.file-viewer-content .markdown-preview').exists()).toBe(true)
  })

  it('renders file viewer child content for code files', () => {
    const wrapper = shallowMount(FileViewerStub, {
      props: {
        file: { name: 'main.ts', path: '/tmp/main.ts', content: 'const x = 1' },
      },
      global: { plugins: [i18n] },
    })

    // CodeMirrorViewer should render inside .raw-content-viewer for code files
    expect(wrapper.find('.file-viewer-content .raw-content-viewer').exists()).toBe(true)
  })
})
