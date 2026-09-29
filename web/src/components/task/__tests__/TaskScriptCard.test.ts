import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
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
vi.mock('@/components/task/TaskScriptEditor.vue', () => ({
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

  it('passes the task script to a disabled editor', () => {
    const wrapper = mountCard({ script: 'test -f x && echo changed' })
    const editor = wrapper.find('.script-editor-stub')
    expect(editor.attributes('data-script')).toBe('test -f x && echo changed')
    // Read-only: the detail view must not offer to edit the stored script.
    expect(editor.attributes('data-disabled')).toBe('true')
  })
})
