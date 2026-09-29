import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'

/**
 * The task panel header's root crumb carries the section's dock glyph (Clock).
 *
 * Why this test exists: the four task pages (list / detail / form / exec detail)
 * all render their header through this one component, so the glyph lands on all
 * four at once. Without a test, a later "tidy up the imports" pass could drop
 * the icon and every page would silently lose it — a missing import renders an
 * empty <svg>-less span, which no text assertion would notice.
 */

// vi.mock factories are hoisted above the imports, so anything they close over
// must be created with vi.hoisted. `ref` is not available at that point (the vue
// import has not run), so these are plain `{ value }` holders — every value is
// set before mount, so no reactivity is needed.
const { state, nav } = vi.hoisted(() => ({
  state: { tasks: [] as Array<{ id: number; name: string }> },
  nav: {
    currentView: { value: 'list' as 'list' | 'settings' },
    selectedTaskId: { value: null as number | null },
    execDetailOpen: { value: false },
    formViewOpen: { value: false },
    formMode: { value: 'create' as 'create' | 'edit' },
    navigateToList: vi.fn(),
    navigateToTaskSettings: vi.fn(),
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

vi.mock('@/composables/useTaskTab', () => ({
  useTaskTab: () => nav,
}))

vi.mock('@/stores/app', () => ({
  store: { state },
}))

import TaskBreadcrumb from '@/components/task/TaskBreadcrumb.vue'

function mountCrumb() {
  return mount(TaskBreadcrumb)
}

describe('TaskBreadcrumb root crumb glyph', () => {
  beforeEach(() => {
    state.tasks = []
    nav.currentView.value = 'list'
    nav.selectedTaskId.value = null
    nav.execDetailOpen.value = false
    nav.formViewOpen.value = false
    nav.formMode.value = 'create'
  })

  it('renders the section glyph inside the root crumb', () => {
    const wrapper = mountCrumb()
    const root = wrapper.find('.crumb')
    expect(root.exists()).toBe(true)
    // The icon must actually be an svg — asserting the class alone would pass
    // even if the icon component were never imported (empty element).
    expect(root.find('svg.crumb-icon').exists(), 'root crumb must render a real icon svg').toBe(true)
  })

  it('still renders the root label text next to the glyph', () => {
    const wrapper = mountCrumb()
    expect(wrapper.find('.crumb').text()).toContain('task.title')
  })

  it('puts the glyph only on the root crumb, not on the drill-down crumbs', () => {
    state.tasks = [{ id: 7, name: 'Nightly build' }]
    nav.selectedTaskId.value = 7
    nav.currentView.value = 'settings'
    const wrapper = mountCrumb()

    const crumbs = wrapper.findAll('.crumb')
    expect(crumbs.length).toBeGreaterThan(1)
    // Root keeps its glyph; the task-name crumb stays text-only so the row does
    // not turn into a row of repeated icons.
    expect(crumbs[0].find('.crumb-icon').exists()).toBe(true)
    expect(crumbs[1].find('.crumb-icon').exists()).toBe(false)
  })

  it('keeps the glyph class the flex-shrink rule targets', () => {
    // The crumbs share one horizontally-scrolling flex row, so a long task name
    // must not shrink the icon to zero. The `flex-shrink: 0` declaration itself
    // is pinned by the source-sniffing guard in taskAndStatsGlyphs.css.test.ts
    // (jsdom has no CSS engine); here we pin that the element carries the class.
    const wrapper = mountCrumb()
    expect(wrapper.find('.crumb .crumb-icon').exists()).toBe(true)
  })
})
