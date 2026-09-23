import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'

// ── Mocks ──

// Child pages are irrelevant to the "+" gating logic under test; stub them so
// the assertions can look at a single, stable marker element.
vi.mock('@/components/task/TaskListPage.vue', () => ({
  default: {
    name: 'TaskListPage',
    template: '<button class="stub-create" @click="$emit(\'create\')" />',
  },
}))
vi.mock('@/components/task/TaskDetailPage.vue', () => ({
  default: { name: 'TaskDetailPage', template: '<div class="stub-detail" />' },
}))
vi.mock('@/components/task/TaskExecDetail.vue', () => ({
  default: { name: 'TaskExecDetail', template: '<div class="stub-exec" />' },
}))
vi.mock('@/components/task/TaskFormPage.vue', () => ({
  default: { name: 'TaskFormPage', template: '<div class="stub-form" />' },
}))
vi.mock('@/components/task/TaskCreateHintDialog.vue', () => ({
  default: {
    name: 'TaskCreateHintDialog',
    props: { open: Boolean },
    template: '<div v-if="open" class="stub-hint" />',
  },
}))

const { mockOpenCreateForm } = vi.hoisted(() => ({ mockOpenCreateForm: vi.fn() }))

// The composable must hand back REAL refs: Vue only auto-unwraps values that
// carry `__v_isRef`, so a plain `{ value: … }` object would be read as truthy
// in the template and TaskTab would render the form branch immediately.
vi.mock('@/composables/useTaskTab', async () => {
  const { ref } = await import('vue')
  return {
    useTaskTab: () => ({
      currentView: ref('list'),
      selectedTaskId: ref(null),
      selectedExecData: ref(null),
      execDetailOpen: ref(false),
      formViewOpen: ref(false),
      formMode: ref('create'),
      goBack: vi.fn(),
      navigateToTaskSettings: vi.fn(),
      navigateToList: vi.fn(),
      closeExecDetail: vi.fn(),
      openCreateForm: mockOpenCreateForm,
      openEditForm: vi.fn(),
      closeForm: vi.fn(),
      loadTasks: vi.fn(),
    }),
  }
})

vi.mock('@/composables/useEdgeSwipeBack', () => ({
  useFeatureBackHandler: vi.fn(),
  PRIORITY_PAGE: 100,
}))

vi.mock('@/stores/app', () => ({ store: { state: { tasks: [] } } }))

import TaskTab from '../TaskTab.vue'
import { TASK_CREATE_HINT_KEY } from '@/composables/useTaskCreateHint'

function mountTab() {
  return mount(TaskTab, { props: { active: true } })
}

function hint(wrapper: ReturnType<typeof mountTab>) {
  return wrapper.findComponent({ name: 'TaskCreateHintDialog' })
}

beforeEach(() => {
  localStorage.clear()
  mockOpenCreateForm.mockClear()
})

afterEach(() => {
  localStorage.clear()
  document.body.innerHTML = ''
})

describe('TaskTab — "+" create entry point', () => {
  it('shows the AI hint instead of the form on the first click', async () => {
    const wrapper = mountTab()

    await wrapper.find('.stub-create').trigger('click')

    expect(wrapper.find('.stub-hint').exists()).toBe(true)
    // The form must not open behind the hint.
    expect(mockOpenCreateForm).not.toHaveBeenCalled()
    expect(wrapper.find('.stub-form').exists()).toBe(false)
  })

  it('opens the form when the user picks "create manually"', async () => {
    const wrapper = mountTab()
    await wrapper.find('.stub-create').trigger('click')

    hint(wrapper).vm.$emit('manual')
    await wrapper.vm.$nextTick()

    expect(mockOpenCreateForm).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.stub-hint').exists()).toBe(false)
  })

  it('opens the form directly once the hint has been dismissed', async () => {
    localStorage.setItem(TASK_CREATE_HINT_KEY, 'true')
    const wrapper = mountTab()

    await wrapper.find('.stub-create').trigger('click')

    expect(mockOpenCreateForm).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.stub-hint').exists()).toBe(false)
  })

  it('closes the hint without opening the form when it is dismissed in-place', async () => {
    const wrapper = mountTab()
    await wrapper.find('.stub-create').trigger('click')
    expect(wrapper.find('.stub-hint').exists()).toBe(true)

    hint(wrapper).vm.$emit('close')
    await wrapper.vm.$nextTick()

    // Closing the hint (overlay click / Escape / back) returns to the list —
    // it must not be treated as consent to open the manual form.
    expect(wrapper.find('.stub-hint').exists()).toBe(false)
    expect(mockOpenCreateForm).not.toHaveBeenCalled()
  })

  it('shows the hint again after a plain close, but not after a dismissal', async () => {
    const wrapper = mountTab()

    await wrapper.find('.stub-create').trigger('click')
    hint(wrapper).vm.$emit('close')
    await wrapper.vm.$nextTick()

    // Not a dismissal → the next "+" still shows the hint.
    await wrapper.find('.stub-create').trigger('click')
    expect(wrapper.find('.stub-hint').exists()).toBe(true)

    // The dialog itself persists the flag; emulate that write.
    localStorage.setItem(TASK_CREATE_HINT_KEY, 'true')
    hint(wrapper).vm.$emit('close')
    await wrapper.vm.$nextTick()

    await wrapper.find('.stub-create').trigger('click')
    expect(wrapper.find('.stub-hint').exists()).toBe(false)
    expect(mockOpenCreateForm).toHaveBeenCalledTimes(1)
  })
})
