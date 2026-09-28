import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'

// ────────────────────────────────────────────────────────────
// TaskTab — fallback when the requested task no longer exists.
//
// The scenario: a chat card (or a notification / deep link) points at a task
// id. By the time the user taps it the task has been deleted. The detail page
// is gated on `selectedTaskData`, so without a fallback branch the whole panel
// renders as an empty shell with no way back. These tests pin the fallback,
// and — just as important — pin that it does NOT appear for a task that is
// simply not loaded yet.
// ────────────────────────────────────────────────────────────

// Child pages are stubbed: only the branch selection matters here.
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/components/task/TaskListPage.vue', () => ({
  default: { name: 'TaskListPage', template: '<div class="stub-list" />' },
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
  default: { name: 'TaskCreateHintDialog', props: { open: Boolean }, template: '<div />' },
}))

// Navigation state is module-level in the real composable, so each test needs
// its own handle on it. The mock returns REAL refs (a plain { value } object
// would be read as truthy in the template and mis-render the form branch).
const nav = vi.hoisted(() => ({
  currentView: null as unknown,
  selectedTaskId: null as unknown,
  tasksLoaded: null as unknown,
  execDetailOpen: null as unknown,
  navigateToList: vi.fn(),
}))

vi.mock('@/composables/useTaskTab', async () => {
  const { ref } = await import('vue')
  nav.currentView = ref('list')
  nav.selectedTaskId = ref(null)
  nav.tasksLoaded = ref(false)
  nav.execDetailOpen = ref(false)
  return {
    useTaskTab: () => ({
      currentView: nav.currentView,
      selectedTaskId: nav.selectedTaskId,
      selectedExecData: ref(null),
      execDetailOpen: nav.execDetailOpen,
      formViewOpen: ref(false),
      formMode: ref('create'),
      tasksLoaded: nav.tasksLoaded,
      goBack: vi.fn(),
      navigateToTaskSettings: vi.fn(),
      navigateToList: nav.navigateToList,
      closeExecDetail: vi.fn(),
      openCreateForm: vi.fn(),
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

// The store holds the task list the fallback keys off. It must be `reactive`
// like the real store — a plain object would leave the component's computed
// stale and make the "task deleted mid-session" case silently pass.
const storeMock = vi.hoisted(() => ({ store: null as unknown }))
vi.mock('@/stores/app', async () => {
  const { reactive } = await import('vue')
  const s = reactive({ state: { tasks: [] as Record<string, unknown>[] } })
  storeMock.store = s
  return { store: s }
})

import TaskTab from '../TaskTab.vue'

type StoreShape = { state: { tasks: Record<string, unknown>[] } }

function setTasks(tasks: Record<string, unknown>[]) {
  ;(storeMock.store as StoreShape).state.tasks = tasks
}

function mountTab() {
  return mount(TaskTab, { props: { active: true } })
}

beforeEach(() => {
  setTasks([])
  ;(nav.currentView as { value: string }).value = 'list'
  ;(nav.selectedTaskId as { value: number | null }).value = null
  ;(nav.tasksLoaded as { value: boolean }).value = false
  ;(nav.execDetailOpen as { value: boolean }).value = false
  nav.navigateToList.mockClear()
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('TaskTab — task no longer exists', () => {
  it('shows the fallback when the list is loaded but lacks the requested id', () => {
    // List is authoritative and does not contain task 43.
    ;(nav.currentView as { value: string }).value = 'settings'
    ;(nav.selectedTaskId as { value: number | null }).value = 43
    ;(nav.tasksLoaded as { value: boolean }).value = true
    setTasks([{ id: 1, name: 'Other task' }])

    const wrapper = mountTab()

    expect(wrapper.find('.task-missing').exists()).toBe(true)
    // The real detail page must not be mounted with a null task.
    expect(wrapper.find('.stub-detail').exists()).toBe(false)
    expect(wrapper.find('.stub-list').exists()).toBe(false)
  })

  it('does NOT show the fallback before the list has loaded (no false "deleted")', () => {
    // Same missing id, but the list has never been fetched — a valid task would
    // look identical here, so claiming "deleted" would be a lie.
    ;(nav.currentView as { value: string }).value = 'settings'
    ;(nav.selectedTaskId as { value: number | null }).value = 43
    ;(nav.tasksLoaded as { value: boolean }).value = false
    setTasks([])

    const wrapper = mountTab()

    expect(wrapper.find('.task-missing').exists()).toBe(false)
    expect(wrapper.find('.task-pending').exists()).toBe(true)
  })

  it('renders the detail page when the task IS present', () => {
    ;(nav.currentView as { value: string }).value = 'settings'
    ;(nav.selectedTaskId as { value: number | null }).value = 7
    ;(nav.tasksLoaded as { value: boolean }).value = true
    setTasks([{ id: 7, name: 'Existing task' }])

    const wrapper = mountTab()

    expect(wrapper.find('.stub-detail').exists()).toBe(true)
    expect(wrapper.find('.task-missing').exists()).toBe(false)
  })

  it('returns to the list when the fallback button is clicked', async () => {
    ;(nav.currentView as { value: string }).value = 'settings'
    ;(nav.selectedTaskId as { value: number | null }).value = 43
    ;(nav.tasksLoaded as { value: boolean }).value = true
    setTasks([])

    const wrapper = mountTab()
    await wrapper.find('.task-missing button').trigger('click')

    expect(nav.navigateToList).toHaveBeenCalledTimes(1)
  })

  it('shows the fallback when the viewed task disappears mid-session', async () => {
    // User is on the detail page; the task is then deleted (store refresh drops
    // it). The panel must swap to the fallback rather than go blank.
    ;(nav.currentView as { value: string }).value = 'settings'
    ;(nav.selectedTaskId as { value: number | null }).value = 7
    ;(nav.tasksLoaded as { value: boolean }).value = true
    setTasks([{ id: 7, name: 'Doomed task' }])

    const wrapper = mountTab()
    expect(wrapper.find('.stub-detail').exists()).toBe(true)

    setTasks([])
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.task-missing').exists()).toBe(true)
    expect(wrapper.find('.stub-detail').exists()).toBe(false)
  })

  it('prefers the fallback over the exec detail for a deleted task', () => {
    // A run's output is anchored to a task that no longer exists. The exec
    // detail branch is listed after the fallback on purpose, so stale output
    // must not win: the deleted notice is the truthful thing to show.
    ;(nav.currentView as { value: string }).value = 'settings'
    ;(nav.selectedTaskId as { value: number | null }).value = 43
    ;(nav.tasksLoaded as { value: boolean }).value = true
    ;(nav.execDetailOpen as { value: boolean }).value = true
    setTasks([])

    const wrapper = mountTab()

    expect(wrapper.find('.task-missing').exists()).toBe(true)
    expect(wrapper.find('.stub-exec').exists()).toBe(false)
  })

  it('still shows the exec detail when the task exists', () => {
    // Guards the ordering fix from over-reaching: a live task's run detail is
    // unaffected.
    ;(nav.currentView as { value: string }).value = 'settings'
    ;(nav.selectedTaskId as { value: number | null }).value = 7
    ;(nav.tasksLoaded as { value: boolean }).value = true
    ;(nav.execDetailOpen as { value: boolean }).value = true
    setTasks([{ id: 7, name: 'Live task' }])

    const wrapper = mountTab()

    expect(wrapper.find('.stub-exec').exists()).toBe(true)
    expect(wrapper.find('.task-missing').exists()).toBe(false)
  })
})
