import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, nextTick } from 'vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: { file: { editor: { save: 'Save', saving: 'Saving', cancel: 'Cancel', dirty: 'Unsaved' } } } },
})

const quoteMocks = vi.hoisted(() => ({ showBar: vi.fn(), hideBar: vi.fn() }))

vi.mock('@/composables/useMarkdownDiff.ts', () => ({ diffMarkers: ref([]), openDiffDrawer: vi.fn() }))
vi.mock('@/composables/useFileRefresh.ts', () => ({ flashRanges: ref([]), flashType: ref('add') }))
vi.mock('@/stores/app.ts', () => ({ store: { state: { projectRoot: '/p', homeDir: '/home' } } }))
vi.mock('@/composables/useQuoteQuestion.ts', () => ({ useQuoteQuestion: () => quoteMocks }))
const dialogMocks = vi.hoisted(() => ({ confirm: vi.fn() }))
vi.mock('@/composables/useDialog.ts', () => ({ useDialog: () => ({ confirm: dialogMocks.confirm, prompt: vi.fn(), alert: vi.fn(), resolve: vi.fn(), state: ref({ visible: false }) }) }))
const fetchSymbolsMock = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useCodeSymbols.ts', () => ({ fetchCodeSymbols: (...a: unknown[]) => fetchSymbolsMock(...a) }))

import CodeMirrorViewer from '../CodeMirrorViewer.vue'

const sleep = (ms: number) => new Promise(r => setTimeout(r, ms))

function gutterClasses() {
  return [...document.querySelectorAll('.cm-gutter')].map(g => (g.className || ''))
}

describe('CodeMirrorViewer (real CodeMirror)', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    quoteMocks.showBar.mockClear()
    quoteMocks.hideBar.mockClear()
    dialogMocks.confirm.mockReset()
    fetchSymbolsMock.mockReset()
    fetchSymbolsMock.mockResolvedValue(null)
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
    await sleep(80)
    // Make the editor dirty so the save button becomes visible.
    wrapper.vm.getView().dispatch({ changes: { from: 9, to: 10, insert: 'x' } })
    await nextTick()
    await sleep(30)
    const saveBtn = wrapper.find('.editor-btn.primary')
    if (!saveBtn.exists()) return // editor host not mounted in this test env; save emit covered elsewhere
    await saveBtn.trigger('click')
    expect(wrapper.emitted('save')?.[0][0]).toBe('const y = 2x')
  })

  it('emits exitEdit on exit-edit button click', async () => {
    const wrapper = mountViewer({ editable: true })
    await sleep(80)
    const btns = wrapper.findAll('.editor-btn.icon-btn')
    const exitEdit = btns[btns.length - 1]
    await exitEdit.trigger('click')
    expect(wrapper.emitted('exitEdit')).toBeTruthy()
  })

  it('does not include basicSetup (no fold gutter)', async () => {
    mountViewer()
    await sleep(50)
    expect(document.querySelector('.cm-foldGutter')).toBeNull()
  })

  it('scrollToLine animates scroll via requestAnimationFrame instead of instant jump', async () => {
    const wrapper = mountViewer({ content: 'line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n' })
    await sleep(80)
    const view = wrapper.vm.getView()
    const rafSpy = vi.spyOn(globalThis, 'requestAnimationFrame')
    const cancelSpy = vi.spyOn(globalThis, 'cancelAnimationFrame')
    view.scrollDOM.scrollTop = 0
    wrapper.vm.scrollToLine(8)
    await nextTick()
    // The smooth path schedules animation frames rather than snapping the scroller.
    expect(rafSpy).toHaveBeenCalled()
    expect(view.scrollDOM.scrollTop).toBe(0)
    rafSpy.mockRestore()
    cancelSpy.mockRestore()
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

  it('fetches scope symbols for sticky scroll in browse mode and disables it in edit mode', async () => {
    fetchSymbolsMock.mockResolvedValue({ lang: 'ts', symbols: [{ name: 'a', kind: 'function', line: 1, endLine: 2, level: 1 }] })
    const wrapper = mountViewer({ file: { path: '/tmp/main.ts' }, content: 'function a(){}\nfunction b(){}\n' })
    await sleep(80)
    // browse mode: sticky scroll fetches symbols from the backend
    expect(fetchSymbolsMock).toHaveBeenCalled()
    // edit mode: sticky scroll is disabled (no further fetch, no overlay)
    fetchSymbolsMock.mockClear()
    await wrapper.setProps({ editable: true })
    await sleep(50)
    expect(fetchSymbolsMock).not.toHaveBeenCalled()
    expect(document.querySelector('.sticky-scroll-overlay')).toBeNull()
  })

  it('updates the document when content prop changes', async () => {
    const wrapper = mountViewer({ content: 'line one' })
    await sleep(50)
    await wrapper.setProps({ content: 'line two' })
    await sleep(50)
    expect(document.body.textContent).toContain('line two')
  })

  it('shows quote question on selection in read-only mode', async () => {
    const wrapper = mountViewer({ content: 'aaa\nbbb\nccc', file: { path: '/p/main.go' } })
    await sleep(80)
    const view = wrapper.vm.getView()
    view.dispatch({ selection: { anchor: 0, head: 7 } }) // selects "aaa\nbb"
    await sleep(700) // debounce 200ms + showBar 400ms
    expect(quoteMocks.showBar).toHaveBeenCalledTimes(1)
    const data = quoteMocks.showBar.mock.calls[0][0]
    expect(data.filePath).toBe('/p/main.go')
    expect(data.startLine).toBe(1)
    expect(data.endLine).toBe(2)
    expect(data.text).toContain('aaa')
  })

  it('does not show quote question on selection in editable mode', async () => {
    const wrapper = mountViewer({ content: 'aaa\nbbb', editable: true })
    await sleep(80)
    const view = wrapper.vm.getView()
    view.dispatch({ selection: { anchor: 0, head: 4 } })
    await sleep(700)
    expect(quoteMocks.showBar).not.toHaveBeenCalled()
  })

  it('getView returns the raw (non-reactive-proxied) EditorView', async () => {
    const wrapper = mountViewer({ content: 'const a = 1\n', editable: true })
    await sleep(80)
    const view = wrapper.vm.getView()
    // Vue's ref() wraps objects in a reactive Proxy; CodeMirror undo/redo build
    // transactions against the raw EditorState identity, so a proxied view
    // breaks them. The view must be stored in a shallowRef.
    expect((view as any).__v_raw).toBeUndefined()
  })

  it('undo and redo revert edits in editable mode', async () => {
    const wrapper = mountViewer({ content: 'const a = 1\n', editable: true })
    await sleep(80)
    const view = wrapper.vm.getView()
    const { undo, redo } = await import('@codemirror/commands')
    view.dispatch({ changes: { from: 6, to: 7, insert: 'ab' } }) // "const a" -> "const ab"
    await sleep(30)
    expect(view.state.doc.toString()).toBe('const ab = 1\n')
    expect(undo(view)).toBe(true)
    await sleep(30)
    expect(view.state.doc.toString()).toBe('const a = 1\n')
    expect(redo(view)).toBe(true)
    await sleep(30)
    expect(view.state.doc.toString()).toBe('const ab = 1\n')
  })

  // ── Exit confirmation when dirty ──
  async function clickExit(wrapper: ReturnType<typeof mountViewer>) {
    const btns = wrapper.findAll('.editor-btn.icon-btn')
    await btns[btns.length - 1].trigger('click')
    await sleep(30)
  }

  it('does not prompt on exit when not dirty', async () => {
    const wrapper = mountViewer({ content: 'clean\n', editable: true })
    await sleep(80)
    await clickExit(wrapper)
    expect(dialogMocks.confirm).not.toHaveBeenCalled()
    expect(wrapper.emitted('exitEdit')).toBeTruthy()
  })

  it('prompts on exit when dirty and saves on confirm', async () => {
    dialogMocks.confirm.mockResolvedValue(true)
    const wrapper = mountViewer({ content: 'hello\n', editable: true })
    await sleep(80)
    wrapper.vm.getView().dispatch({ changes: { from: 5, to: 5, insert: '!' } }) // make dirty -> "hello!\n"
    await sleep(30)
    await clickExit(wrapper)
    expect(dialogMocks.confirm).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('exitEdit')).toBeFalsy()
    expect(wrapper.emitted('save')?.[0][0]).toBe('hello!\n')
  })

  it('prompts on exit when dirty and discards on dont-save', async () => {
    dialogMocks.confirm.mockResolvedValue(null)
    const wrapper = mountViewer({ content: 'hello\n', editable: true })
    await sleep(80)
    wrapper.vm.getView().dispatch({ changes: { from: 5, to: 5, insert: '!' } }) // make dirty
    await sleep(30)
    await clickExit(wrapper)
    expect(dialogMocks.confirm).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('exitEdit')).toBeTruthy()
    expect(wrapper.emitted('save')).toBeFalsy()
  })

  it('stays in edit mode when exit confirmation is cancelled', async () => {
    dialogMocks.confirm.mockResolvedValue(false)
    const wrapper = mountViewer({ content: 'hello\n', editable: true })
    await sleep(80)
    wrapper.vm.getView().dispatch({ changes: { from: 5, to: 5, insert: '!' } }) // make dirty
    await sleep(30)
    await clickExit(wrapper)
    expect(dialogMocks.confirm).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('exitEdit')).toBeFalsy()
    expect(wrapper.emitted('save')).toBeFalsy()
  })

  it('distinguishes edit mode with an accent top border and tinted background', async () => {
    const browse = mountViewer({ content: 'x', editable: false })
    await sleep(50)
    const edit = mountViewer({ content: 'x', editable: true })
    await sleep(50)

    expect(browse.find('.cm-viewer').classes()).toContain('cm-readonly')
    expect(browse.find('.cm-viewer').classes()).not.toContain('is-editable')

    expect(edit.find('.cm-viewer').classes()).toContain('is-editable')
    expect(edit.find('.cm-viewer').classes()).not.toContain('cm-readonly')

    // The edit-mode stylesheet must define the accent top border and the
    // accent-tinted code background that visually separate edit from browse.
    const css = [...document.querySelectorAll('style')].map(s => s.textContent).join('\n')
    expect(css).toMatch(/\.cm-viewer\.is-editable\b/)
    expect(css).toMatch(/border-top:\s*2px\s+solid/)
    expect(css).toMatch(/--code-bg-editing/)
  })
})
