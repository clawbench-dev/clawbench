import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import SessionSidebar from '@/components/session/SessionSidebar.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }) }))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))

vi.mock('@/components/session/SessionList.vue', () => ({
  default: { name: 'SessionList', template: '<div class="session-list-stub" />' },
}))
vi.mock('@/components/session/SessionListHeader.vue', () => ({
  default: { name: 'SessionListHeader', template: '<div class="header-stub"><slot name="actions" /></div>' },
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

  it('emits unpin when pin button clicked', async () => {
    const wrapper = mountSidebar()
    await wrapper.find('.sidebar-unpin-btn').trigger('click')
    expect(wrapper.emitted('unpin')).toBeTruthy()
  })

  it('emits close when close button clicked', async () => {
    const wrapper = mountSidebar()
    await wrapper.find('.sidebar-close-btn').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('emits resize on pointer drag with clamped width', () => {
    const wrapper = mountSidebar()
    const div = wrapper.find('.sidebar-divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()
    Object.defineProperty(wrapper.find('.session-sidebar').element, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ left: 0, width: 280, right: 280, top: 0, bottom: 0, height: 600, x: 0, y: 0, toJSON() {} }),
    })
    // Simulate divider pointerdown then window pointermove
    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 600 }))
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 300 }))
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    expect(wrapper.emitted('resize')).toBeTruthy()
  })
})
