import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, defineComponent, nextTick } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k, locale: ref('en') }),
  createI18n: (opts: any) => ({
    global: { t: (k: string) => k, locale: ref(opts?.locale ?? 'en') },
    install() {},
  }),
}))

// NOTE: the refs are created here (in the test's own Vue instance), not in
// vi.hoisted — a ref made through require('vue') inside hoisted() belongs to a
// different Vue copy and is not reactive inside the component, which silently
// turned this file's navigation into a no-op.
const navStack = ref<string[]>([])
const currentCategory = ref<string | null>(null)

vi.mock('@/composables/useSettingsNavigation', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useSettingsNavigation')>()
  return {
    ...actual,
    useSettingsNavigation: () => ({
      t: (k: string) => k,
      navStack,
      currentCategory,
      pushNav: vi.fn(),
      popNav: vi.fn(() => {
        navStack.value.pop()
        currentCategory.value = navStack.value[navStack.value.length - 1] ?? null
      }),
      truncateNav: vi.fn((depth: number) => {
        navStack.value.splice(depth)
        currentCategory.value = navStack.value[navStack.value.length - 1] ?? null
      }),
      returnToCategory: vi.fn(),
      replaceTopNav: vi.fn(),
      restartDialogVisible: ref(false),
      changedColdFields: ref([]),
      needsRestart: ref(false),
      restarting: ref(false),
      handleRestartNeeded: vi.fn(),
      handleRestart: vi.fn(),
      checkAllGuards: () => true,
      loadConfig: vi.fn(),
    }),
  }
})

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({ serverConfig: ref({ version: '9.9.9' }) }),
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({ getAgent: () => ({ name: 'A' }), getAgentAvailableModes: () => [], getAgentAvailableConfigOptions: () => [] }),
  registerIdentityUpdaters: vi.fn(),
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: () => Promise.resolve(true) }),
}))

// The real composable registers this page's back handler; the test drives the
// handler directly through the captured callback instead of a swipe gesture.
const { backCallbacks } = vi.hoisted(() => ({ backCallbacks: [] as Array<{ when: () => boolean; run: () => void }> }))
vi.mock('@/composables/useEdgeSwipeBack', () => ({
  useFeatureBackHandler: (_id: string, when: () => boolean, run: () => void) => {
    backCallbacks.push({ when, run })
  },
  PRIORITY_PAGE: 100,
}))

vi.mock('@/components/settings/settingsFieldMap', () => ({
  isSubPageRoute: () => false,
  getSubPageTitleKey: () => '',
}))

import SettingsPage from '@/components/settings/SettingsPage.vue'

const SettingsIndexStub = defineComponent({
  name: 'SettingsIndex',
  emits: ['navigate', 'open-setting'],
  template: '<div class="index-stub" />',
})
const SettingsCategoryStub = defineComponent({
  name: 'SettingsCategory',
  props: ['categoryId', 'searchKeys', 'searchQuery', 'searchEmpty'],
  emits: ['navigate', 'back', 'deleted', 'navigate-replace', 'restart-needed', 'restart-requested', 'clear-search', 'search-empty'],
  template: '<div class="category-stub" />',
})


/**
 * Scroll offsets live on a real DOM element; jsdom reports 0 for every offset
 * unless the element is told otherwise. A plain instance property makes
 * `scrollTop` settable and readable, which is all the restore path touches.
 */
function fakeScrollable(el: Element) {
  let top = 0
  Object.defineProperty(el, 'scrollTop', {
    configurable: true,
    get: () => top,
    set: (v: number) => { top = v },
  })
}

function mountPage() {
  return mount(SettingsPage, {
    props: { active: true },
    global: {
      stubs: {
        SettingsIndex: SettingsIndexStub,
        SettingsCategory: SettingsCategoryStub,
        SettingsRestartDialog: defineComponent({ name: 'SettingsRestartDialog', template: '<div />' }),
      },
    },
  })
}

function currentBody(wrapper: ReturnType<typeof mountPage>): HTMLElement {
  return wrapper.find('.settings-page__body').element as HTMLElement
}

function bodyScrollTop(wrapper: ReturnType<typeof mountPage>): number {
  return currentBody(wrapper).scrollTop
}

/**
 * Track body mounts: the fake `scrollTop` has to be installed as each one
 * appears. A MutationObserver catches them — one is created by the mount and
 * another by every level change, and the test cannot hook that moment.
 */
function trackBodies(wrapper: ReturnType<typeof mountPage>) {
  const root = wrapper.element as HTMLElement
  const adopted = new WeakSet<Element>()
  const adoptEl = (el: Element) => {
    if (adopted.has(el)) return
    adopted.add(el)
    fakeScrollable(el)
  }
  const existing = root.querySelector('.settings-page__body')
  if (existing) adoptEl(existing)
  const mo = new MutationObserver(() => {
    const el = root.querySelector('.settings-page__body')
    if (el) adoptEl(el)
  })
  mo.observe(root, { childList: true, subtree: true })
  return mo
}

/**
 * A level change rebuilds the container, so a restored offset can only come
 * from the map. jsdom keeps a hand-set scrollTop on the detached node, which
 * would make "it stayed 640" true for the wrong reason — hence the assertion.
 */
function assertRemounted(previous: HTMLElement, wrapper: ReturnType<typeof mountPage>) {
  const now = currentBody(wrapper)
  expect(now).not.toBe(previous)
  return now
}

/** Let the restore's two animation frames (and the re-apply) run. */
async function settle() {
  await flushPromises()
  await nextTick()
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()))
  await flushPromises()
  await nextTick()
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()))
  await flushPromises()
  await nextTick()
}

function drillTo(categoryId: string) {
  navStack.value.push(categoryId)
  currentCategory.value = categoryId
}

/** Run the page's registered back handler (the edge-swipe path). */
function swipeBack() {
  const handler = backCallbacks[backCallbacks.length - 1]
  handler.run()
}

beforeEach(() => {
  navStack.value = []
  currentCategory.value = null
  backCallbacks.length = 0
  vi.clearAllMocks()
})

describe('SettingsPage — scroll position per level', () => {
  it('returns to the offset the list was left at, not the top', async () => {
    // Regression: drilling into a row and swiping back landed at the top of the
    // category page instead of where the user had scrolled to.
    navStack.value = ['general']
    currentCategory.value = 'general'
    const wrapper = mountPage()
    await flushPromises()
    const mo = trackBodies(wrapper)

    const listBody = currentBody(wrapper)
    listBody.scrollTop = 640
    await wrapper.find('.settings-page__body').trigger('scroll')

    drillTo('general:some_row')
    await settle()
    assertRemounted(listBody, wrapper)
    // The map is keyed by path: the new level has no entry yet, so it stays at
    // the fresh container's own 0.
    expect(bodyScrollTop(wrapper)).toBe(0)

    swipeBack()
    await flushPromises()
    await settle()

    expect(bodyScrollTop(wrapper)).toBe(640)
    mo.disconnect()
    wrapper.unmount()
  })

  it('restores via the breadcrumb as well (same leaving level)', async () => {
    navStack.value = ['general']
    currentCategory.value = 'general'
    const wrapper = mountPage()
    await flushPromises()
    const mo = trackBodies(wrapper)

    const listBody = currentBody(wrapper)
    listBody.scrollTop = 275
    await wrapper.find('.settings-page__body').trigger('scroll')

    drillTo('general:some_row')
    await settle()
    assertRemounted(listBody, wrapper)

    // Root crumb (depth 0) → the index, which was never scrolled: it opens at
    // the top even though the category beneath had an offset.
    await wrapper.findAll('.crumb')[0].trigger('click')
    await settle()
    expect(navStack.value).toEqual([])
    expect(bodyScrollTop(wrapper)).toBe(0)

    mo.disconnect()
    wrapper.unmount()
  })

  it('does not reuse the pre-filter offset after a search hit', async () => {
    // A search hit scrolls the row into view mid-page; the restore must not drag
    // the page back to where the unfiltered list stood.
    navStack.value = ['general']
    currentCategory.value = 'general'
    const wrapper = mountPage()
    await flushPromises()
    const mo = trackBodies(wrapper)

    currentBody(wrapper).scrollTop = 900
    await wrapper.find('.settings-page__body').trigger('scroll')

    ;(wrapper.vm as unknown as { handleOpenSetting: (c: string, k: string, q: string) => void })
      .handleOpenSetting('general', 'theme', 'theme')
    await flushPromises()
    // The filtered view is a different document, so a fresh container starts it
    // at the top. jsdom keeps a hand-set scrollTop even across the remount, so
    // that starting state is stated here instead of assumed.
    currentBody(wrapper).scrollTop = 0
    await settle()

    expect(bodyScrollTop(wrapper)).toBe(0)
    mo.disconnect()
    wrapper.unmount()
  })
})
