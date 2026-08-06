import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: { file: { editor: { save: 'Save', saving: 'Saving', cancel: 'Cancel', dirty: 'Unsaved' } } } },
})

vi.mock('@/composables/useMarkdownDiff.ts', () => ({ diffMarkers: ref([]), openDiffDrawer: vi.fn() }))
vi.mock('@/composables/useFileRefresh.ts', () => ({ flashRanges: ref([]), flashType: ref('add') }))
vi.mock('@/stores/app.ts', () => ({ store: { state: { projectRoot: '/p', homeDir: '/home' } } }))
vi.mock('@/composables/useQuoteQuestion.ts', () => ({ useQuoteQuestion: () => ({ showBar: vi.fn() }) }))

import CodeMirrorViewer from '../CodeMirrorViewer.vue'

const sleep = (ms: number) => new Promise(r => setTimeout(r, ms))

function gutterClasses() {
  return [...document.querySelectorAll('.cm-gutter')].map(g => (g.className || ''))
}

describe('CodeMirrorViewer (real CodeMirror)', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  function mountViewer(props = {}) {
    return mount(CodeMirrorViewer, {
      props: { content: 'const a = 1\nconst b = 2\n', language: 'javascript', ...props },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })
  }

  it('defaults to read-only mode (no action bar)', () => {
    const wrapper = mountViewer()
    expect(wrapper.find('.code-editor-actions').exists()).toBe(false)
    expect(wrapper.classes()).toContain('cm-readonly')
  })

  it('shows action bar in editable mode', () => {
    const wrapper = mountViewer({ editable: true })
    expect(wrapper.find('.code-editor-actions').exists()).toBe(true)
    expect(wrapper.classes()).not.toContain('cm-readonly')
  })

  it('emits save with current content on save button click', async () => {
    const wrapper = mountViewer({ editable: true, content: 'const y = 2' })
    await wrapper.find('.editor-btn.primary').trigger('click')
    expect(wrapper.emitted('save')?.[0][0]).toBe('const y = 2')
  })

  it('emits cancel on cancel button click', async () => {
    const wrapper = mountViewer({ editable: true })
    await wrapper.find('.editor-btn:not(.primary)').trigger('click')
    expect(wrapper.emitted('cancel')).toBeTruthy()
  })

  it('does not include basicSetup (no fold gutter)', async () => {
    mountViewer()
    await sleep(50)
    expect(document.querySelector('.cm-foldGutter')).toBeNull()
  })

  it('toggles line numbers via prop', async () => {
    const wrapper = mountViewer({ showLineNumbers: true })
    await sleep(50)
    expect(gutterClasses().some(c => c.includes('cm-lineNumbers'))).toBe(true)
    await wrapper.setProps({ showLineNumbers: false })
    await sleep(80)
    expect(gutterClasses().some(c => c.includes('cm-lineNumbers'))).toBe(false)
  })

  it('toggles word wrap via prop', async () => {
    const wrapper = mountViewer({ wordWrap: false })
    await sleep(50)
    expect(document.querySelector('.cm-lineWrapping')).toBeNull()
    await wrapper.setProps({ wordWrap: true })
    await sleep(80)
    expect(document.querySelector('.cm-lineWrapping')).not.toBeNull()
    await wrapper.setProps({ wordWrap: false })
    await sleep(80)
    expect(document.querySelector('.cm-lineWrapping')).toBeNull()
  })

  it('renders editor content', async () => {
    mountViewer({ content: 'hello world' })
    await sleep(50)
    expect(document.body.textContent).toContain('hello world')
  })

  it('updates the document when content prop changes', async () => {
    const wrapper = mountViewer({ content: 'line one' })
    await sleep(50)
    await wrapper.setProps({ content: 'line two' })
    await sleep(50)
    expect(document.body.textContent).toContain('line two')
  })
})
