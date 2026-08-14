import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import SessionSidebar from '@/components/session/SessionSidebar.vue'
import { SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_HEIGHT, SIDEBAR_MAX_HEIGHT } from '@/composables/useSessionSidebar'

const wideScreen = vi.hoisted(() => ({ isWideScreen: { value: true } }))
vi.mock('@/composables/useWideScreenLayout', async () => {
  const { ref } = await import('vue')
  wideScreen.isWideScreen = ref(true)
  return {
    useWideScreenLayout: () => ({ isWideScreen: wideScreen.isWideScreen }),
  }
})
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }) }))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))

const loadSessionsMock = vi.hoisted(() => vi.fn())

vi.mock('@/components/session/SessionList.vue', () => ({
  default: {
    name: 'SessionList',
    template: '<div class="session-list-stub" />',
    emits: ['select', 'archive', 'destroy'],
    methods: { loadSessions: loadSessionsMock },
  },
}))
vi.mock('@/components/session/SessionListHeader.vue', () => ({
  default: {
    name: 'SessionListHeader',
    template: '<div class="header-stub"><slot name="actions" /><button class="refresh-stub" @click="$emit(\'refresh\')" /></div>',
    emits: ['refresh'],
  },
}))
vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<div />' },
}))
vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: { name: 'ModalDialog', template: '<div />' },
}))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: vi.fn().mockResolvedValue(true) }),
}))
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({ agents: { value: [] }, loadAgents: vi.fn() }),
}))
vi.mock('@/stores/app', () => ({
  store: { state: { sessionCount: 0, sessionMaxCount: 0 } },
}))

describe('SessionSidebar', () => {
  let wrapper: ReturnType<typeof mount> | undefined

  beforeEach(() => { vi.clearAllMocks() })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined })

  function mountSidebar(props = {}) {
    wrapper = mount(SessionSidebar, {
      props: { width: 280, currentSessionId: 's1', runningSessionIds: new Set(), ...props },
    })
    return wrapper
  }

  it('renders header and list', () => {
    const wrapper = mountSidebar()
    expect(wrapper.find('.session-list-stub').exists()).toBe(true)
    expect(wrapper.find('.header-stub').exists()).toBe(true)
  })

  it('emits close when collapse button clicked', async () => {
    const wrapper = mountSidebar()
    await wrapper.find('.sidebar-close-btn').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('refresh button reloads the session list', async () => {
    const wrapper = mountSidebar()
    loadSessionsMock.mockClear()
    await wrapper.find('.refresh-stub').trigger('click')
    expect(loadSessionsMock).toHaveBeenCalled()
  })

  it('keeps the collapse button on narrow screen (needed to dismiss the bottom bar)', async () => {
    wideScreen.isWideScreen.value = false
    const wrapper = mountSidebar()
    expect(wrapper.find('.sidebar-close-btn').exists()).toBe(true)
    wideScreen.isWideScreen.value = true
  })

  it('applies bottom-docked positioning on narrow screen', () => {
    wideScreen.isWideScreen.value = false
    const wrapper = mountSidebar()
    expect(wrapper.find('.session-sidebar').classes()).toContain('bottom-docked')
    wideScreen.isWideScreen.value = true
  })

  it('does not apply bottom-docked positioning on wide screen', () => {
    wideScreen.isWideScreen.value = true
    const wrapper = mountSidebar()
    expect(wrapper.find('.session-sidebar').classes()).not.toContain('bottom-docked')
  })

  it('forwards archive with sessionId and backend', async () => {
    const wrapper = mountSidebar()
    const list = wrapper.findComponent({ name: 'SessionList' })
    ;(list.vm as any).$emit('archive', 's1', 'cli')
    await list.vm.$nextTick()
    expect(wrapper.emitted('archive')).toBeTruthy()
    expect(wrapper.emitted('archive')![0]).toEqual(['s1', 'cli'])
  })

  it('forwards select with sessionId and backend', async () => {
    const wrapper = mountSidebar()
    const list = wrapper.findComponent({ name: 'SessionList' })
    ;(list.vm as any).$emit('select', 's1', 'cli')
    await list.vm.$nextTick()
    expect(wrapper.emitted('select')).toBeTruthy()
    expect(wrapper.emitted('select')![0]).toEqual(['s1', 'cli'])
  })

  it('emits resize with width clamped to MIN when dragging too far left', () => {
    const wrapper = mountSidebar()
    const root = wrapper.find('.session-sidebar')
    root.element.getBoundingClientRect = () =>
      ({ right: 300, left: 20, width: 280, height: 600, top: 0, bottom: 600, x: 20, y: 0, toJSON: () => ({}) }) as DOMRect
    const div = wrapper.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()
    // right(300) - clientX(100) = 200 → clamped to MIN(220)
    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 500 }))
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 100 }))
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    expect(wrapper.emitted('resize')?.[0]?.[0]).toBe(SIDEBAR_MIN_WIDTH)
  })

  it('emits resize with width clamped to MAX when dragging too far right', () => {
    const wrapper = mountSidebar()
    const root = wrapper.find('.session-sidebar')
    root.element.getBoundingClientRect = () =>
      ({ right: 300, left: 20, width: 280, height: 600, top: 0, bottom: 600, x: 20, y: 0, toJSON: () => ({}) }) as DOMRect
    const div = wrapper.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()
    // right(300) - clientX(-300) = 600 → clamped to MAX(480)
    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 200 }))
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: -300 }))
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    expect(wrapper.emitted('resize')?.[0]?.[0]).toBe(SIDEBAR_MAX_WIDTH)
  })

  it('emits resize with height clamped to MIN when dragging bottom bar up on narrow screen', () => {
    wideScreen.isWideScreen.value = false
    const wrapper = mountSidebar({ height: 320 })
    const root = wrapper.find('.session-sidebar')
    root.element.getBoundingClientRect = () =>
      ({ right: 400, left: 0, width: 400, height: 320, top: 280, bottom: 600, x: 0, y: 280, toJSON: () => ({}) }) as DOMRect
    const div = wrapper.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()
    // bottom(600) - clientY(300) = 300 → within range, no clamp
    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientY: 500 }))
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientY: 300 }))
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    expect(wrapper.emitted('resize')?.[0]?.[0]).toBe(300)
    wideScreen.isWideScreen.value = true
  })

  it('clamps bottom bar height to MIN on narrow screen', () => {
    wideScreen.isWideScreen.value = false
    const wrapper = mountSidebar({ height: 320 })
    const root = wrapper.find('.session-sidebar')
    root.element.getBoundingClientRect = () =>
      ({ right: 400, left: 0, width: 400, height: 320, top: 280, bottom: 600, x: 0, y: 280, toJSON: () => ({}) }) as DOMRect
    const div = wrapper.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()
    // bottom(600) - clientY(1000) = -400 → clamped to MIN
    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientY: 500 }))
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientY: 1000 }))
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    expect(wrapper.emitted('resize')?.[0]?.[0]).toBe(SIDEBAR_MIN_HEIGHT)
    wideScreen.isWideScreen.value = true
  })
})
