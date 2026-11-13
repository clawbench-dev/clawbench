import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import PanelWindowControls from '@/components/common/PanelWindowControls.vue'
import {
  _resetForTest,
  setChatCollapsed,
  useWideScreenLayout,
} from '@/composables/useWideScreenLayout'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

let wrapper: VueWrapper | null = null

function mountControls(props: { closable?: boolean } = {}) {
  wrapper = mount(PanelWindowControls, { props, attachTo: document.body })
  return wrapper
}

function control(action: 'min' | 'max' | 'close') {
  const index = action === 'min' ? 0 : action === 'max' ? 1 : 2
  return wrapper!.findAll('.panel-control')[index]
}

beforeEach(() => {
  _resetForTest()
  localStorage.clear()
  // initWideScreen() reads window.innerWidth once; jsdom is 1024, which is a
  // wide screen. Tests that need the narrow case flip the ref afterwards.
  useWideScreenLayout()
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.getElementById('contentArea')?.remove()
  _resetForTest()
})

describe('PanelWindowControls', () => {
  it('renders nothing on a narrow screen (no second pane to manage)', () => {
    useWideScreenLayout().isWideScreen.value = false
    mountControls()
    expect(wrapper!.find('.panel-window-controls').exists()).toBe(false)
  })

  it('renders minimize and maximize on a wide screen', () => {
    mountControls()
    expect(wrapper!.findAll('.panel-control')).toHaveLength(2)
    expect(control('min').attributes('title')).toBe('nav.minimizePanel')
    expect(control('max').attributes('title')).toBe('nav.maximizePanel')
    // No close cell by default: closing the panel is what minimize does.
    expect(wrapper!.findAll('.panel-control--close')).toHaveLength(0)
  })

  it('renders a close cell only when the host asks for one', () => {
    // The file viewer is the one panel where close means something different
    // (close the open file), so it opts in.
    mountControls({ closable: true })
    expect(wrapper!.findAll('.panel-control')).toHaveLength(3)
    expect(control('close').attributes('title')).toBe('nav.closePanel')
  })

  it('close emits and does not touch the layout', async () => {
    const { leftCollapsed, chatCollapsed } = useWideScreenLayout()
    // The panel starts collapsed by default (VS Code style), so the assertion
    // is "unchanged", not "visible".
    const before = { left: leftCollapsed.value, chat: chatCollapsed.value }
    mountControls({ closable: true })

    await control('close').trigger('click')

    expect(wrapper!.emitted('close')).toHaveLength(1)
    expect(leftCollapsed.value).toBe(before.left)
    expect(chatCollapsed.value).toBe(before.chat)
  })

  it('maximize toggles the chat pane and the glyph/title follow it', async () => {
    const { chatCollapsed } = useWideScreenLayout()
    mountControls()
    expect(control('max').attributes('title')).toBe('nav.maximizePanel')

    await control('max').trigger('click')
    expect(chatCollapsed.value).toBe(true)
    expect(control('max').attributes('title')).toBe('nav.restorePanel')

    await control('max').trigger('click')
    expect(chatCollapsed.value).toBe(false)
    expect(control('max').attributes('title')).toBe('nav.maximizePanel')
  })

  it('minimize hides the panel, like a window minimizing to the dock', async () => {
    const { leftCollapsed, chatCollapsed } = useWideScreenLayout()
    setChatCollapsed(true) // minimizing from the maximized state must still work

    mountControls()
    await control('min').trigger('click')

    expect(leftCollapsed.value).toBe(true)
    expect(chatCollapsed.value).toBe(false)
  })
})
