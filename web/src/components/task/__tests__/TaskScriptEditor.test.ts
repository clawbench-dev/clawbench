import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import TaskScriptEditor from '../TaskScriptEditor.vue'

// Real CodeMirror, like CodeMirrorViewer.test.ts: the point is the v-model
// contract, which only exists once a real EditorView is mounted.
const sleep = (ms: number) => new Promise(r => setTimeout(r, ms))

function mountEditor(props: Record<string, unknown> = {}) {
  return mount(TaskScriptEditor, {
    props: { modelValue: '', language: 'shell', ...props },
    attachTo: document.body,
  })
}

describe('TaskScriptEditor', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  it('mounts a real CodeMirror editor with the initial value', async () => {
    const wrapper = mountEditor({ modelValue: 'echo hi' })
    await sleep(80)
    const view = wrapper.find('.cm-content')
    expect(view.exists()).toBe(true)
    expect(view.text()).toContain('echo hi')
  })

  it('emits update:modelValue when the buffer is edited', async () => {
    const wrapper = mountEditor({ modelValue: 'echo' })
    await sleep(80)
    const view = (wrapper.vm as unknown as { getView: () => { dispatch: (spec: unknown) => void } }).getView()
    view.dispatch({ changes: { from: 4, to: 4, insert: ' hi' } })
    await nextTick()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['echo hi'])
  })

  it('reflects an external modelValue change into the editor', async () => {
    const wrapper = mountEditor({ modelValue: 'first' })
    await sleep(80)
    await wrapper.setProps({ modelValue: 'second' })
    await nextTick()
    await sleep(40)
    expect(wrapper.find('.cm-content').text()).toContain('second')
    expect(wrapper.find('.cm-content').text()).not.toContain('first')
  })

  it('is read-only when disabled', async () => {
    const wrapper = mountEditor({ modelValue: 'echo hi', disabled: true })
    await sleep(80)
    const content = wrapper.find('.cm-content').element as HTMLElement
    expect(content.getAttribute('contenteditable')).toBe('false')
  })

  it('shows the localized placeholder on an empty buffer', async () => {
    // The placeholder is the feature's only in-context documentation of the
    // template variables, so it must actually reach the DOM.
    const wrapper = mountEditor({ modelValue: '', placeholder: 'Exits 0 to run' })
    await sleep(80)
    expect(wrapper.find('.cm-placeholder').exists()).toBe(true)
    expect(wrapper.find('.cm-placeholder').text()).toContain('Exits 0 to run')
  })
})
