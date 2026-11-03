import { describe, it, expect, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import TaskScriptCard from '../TaskScriptCard.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('lucide-vue-next', () => ({
  ChevronDown: { name: 'ChevronDown', props: { size: Number }, template: '<svg data-icon="ChevronDown" />' },
  Terminal: { name: 'Terminal', props: { size: Number }, template: '<svg data-icon="Terminal" />' },
}))

// The editor is real CodeMirror; the card's contract is the collapse/expand
// behaviour and the script value it hands down.
//
// `__esModule` matters: the card loads it with `() => import(...)`, and Vue's
// async component unwraps `.default` only from a Module namespace object
// (which is what a real dynamic import yields). A plain object would be handed
// to the renderer as if it were the component.
vi.mock('@/components/task/TaskScriptEditor.vue', () => ({
  __esModule: true,
  default: {
    name: 'TaskScriptEditor',
    props: ['modelValue', 'language', 'placeholder', 'disabled'],
    template: '<div class="script-editor-stub" :data-script="modelValue" :data-disabled="String(disabled)" />',
  },
}))

function mountCard(task: Record<string, unknown>) {
  return mount(TaskScriptCard, { props: { task } })
}

describe('TaskScriptCard', () => {
  it('starts collapsed', () => {
    // The script is reference material, not the primary content.
    const wrapper = mountCard({ script: 'echo hi' })
    expect(wrapper.find('.card-chevron').classes()).toContain('is-collapsed')
    // v-show keeps it mounted but hidden.
    expect(wrapper.find('.script-body').isVisible()).toBe(false)
  })

  it('expands on title click', async () => {
    const wrapper = mountCard({ script: 'echo hi' })
    await wrapper.find('.card-title.is-collapsible').trigger('click')
    expect(wrapper.find('.script-body').isVisible()).toBe(true)
    expect(wrapper.find('.card-chevron').classes()).not.toContain('is-collapsed')
  })

  it('uses the shared card chrome classes (same as the prompt card)', () => {
    // The gating script card must be visually identical to the prompt card.
    // That only holds if both use the global primitives — a local class name
    // here would render unstyled, which is exactly the bug this guards.
    const wrapper = mountCard({ script: 'echo hi' })
    expect(wrapper.find('.overview-card').exists()).toBe(true)
    expect(wrapper.find('.card-title').exists()).toBe(true)
    expect(wrapper.find('.card-title-text').exists()).toBe(true)
    expect(wrapper.find('.card-toggle-btn').exists()).toBe(true)
  })

  it('passes the task script to a disabled editor', async () => {
    const wrapper = mountCard({ script: 'test -f x && echo changed' })
    // The editor is an async component (its CodeMirror chunk must not be in the
    // first-screen bundle). It renders the shared loading component until the
    // chunk resolves, so wait for the editor rather than for one microtask.
    await vi.waitFor(() => expect(wrapper.find('.script-editor-stub').exists()).toBe(true))
    const editor = wrapper.find('.script-editor-stub')
    expect(editor.attributes('data-script')).toBe('test -f x && echo changed')
    // Read-only: the detail view must not offer to edit the stored script.
    expect(editor.attributes('data-disabled')).toBe('true')
  })

  it('loads the editor chunk through the shared retry/error wrapper', () => {
    // A bare defineAsyncComponent caches the rejected load forever: Vue only
    // resets `pendingRequest` from `onError`, so one failed fetch left this
    // section permanently blank. jsdom cannot reproduce a chunk fetch failure,
    // so the contract is read off the source (same guard as StatsTabHost).
    const src = readFileSync(join(__dirname, '..', 'TaskScriptCard.vue'), 'utf8')
    expect(src).toMatch(/defineAsyncComponent\(\s*buildAsyncComponentOptions\(\{/)
    expect(src).toMatch(/buildAsyncComponentOptions\(\{\s*loader: \(\) => import\('@\/components\/task\/TaskScriptEditor\.vue'\)/)
    // The old shape — a loader handed straight to defineAsyncComponent.
    expect(src).not.toMatch(/defineAsyncComponent\(\s*(async\s*)?\(\)\s*=>/)
  })
})
