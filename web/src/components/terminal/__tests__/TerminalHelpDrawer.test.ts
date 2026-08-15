import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import TerminalHelpDrawer from '@/components/terminal/TerminalHelpDrawer.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      terminal: {
        helpTitle: '终端操作帮助',
        helpSectionGestures: '手势操作',
        helpSectionShortcuts: '快捷键',
        helpSectionCommon: '常用操作',
        helpGestureSwipe: '滑动',
        helpGestureSwipeDesc: '单指上/下/左/右滑',
        helpGesturePinch: '双指捏合',
        helpGesturePinchDesc: '缩放字号',
        helpShortcutCtrlC: 'Ctrl+C',
        helpShortcutCtrlCDesc: '中断',
        helpCommonCopy: '复制文本',
        helpCommonCopyDesc: '选区复制',
        helpVolumeKeys: '音量键',
        helpVolumeKeysDesc: 'Android：音量上/下键',
      },
    },
  },
})

let wrapper: VueWrapper<any> | null = null
let container: HTMLDivElement

beforeEach(() => {
  container = document.createElement('div')
  document.body.appendChild(container)
})

afterEach(() => {
  if (wrapper) {
    wrapper.unmount()
    wrapper = null
  }
  if (container.parentNode) {
    document.body.removeChild(container)
  }
})

function $titles() {
  return Array.from(document.querySelectorAll('.th-section-title')).map((e) => e.textContent)
}
function $items() {
  return Array.from(document.querySelectorAll('.th-item'))
}

describe('TerminalHelpDrawer', () => {
  it('shows gestures only on touch platforms, shortcuts only on desktop', async () => {
    wrapper = mount(TerminalHelpDrawer, {
      props: { open: true, gestures: true, appMode: false },
      global: { plugins: [i18n] },
    })
    await new Promise((r) => setTimeout(r, 50))

    // Touch: gestures yes, desktop keyboard shortcuts no
    expect($titles()).toEqual(['手势操作', '常用操作'])
  })

  it('hides the gestures section and shows shortcuts on desktop (non-touch)', async () => {
    wrapper = mount(TerminalHelpDrawer, {
      props: { open: true, gestures: false, appMode: false },
      global: { plugins: [i18n] },
    })
    await new Promise((r) => setTimeout(r, 50))

    expect($titles()).toEqual(['快捷键', '常用操作'])
    expect($titles()).not.toContain('手势操作')
  })

  it('adds the volume-key item only in Android app mode', async () => {
    wrapper = mount(TerminalHelpDrawer, {
      props: { open: true, gestures: true, appMode: true },
      global: { plugins: [i18n] },
    })
    await new Promise((r) => setTimeout(r, 50))

    const names = $items().map((el) => el.querySelector('.th-name')?.textContent)
    expect(names).toContain('音量键')
  })

  it('omits the volume-key item outside Android app mode', async () => {
    wrapper = mount(TerminalHelpDrawer, {
      props: { open: true, gestures: true, appMode: false },
      global: { plugins: [i18n] },
    })
    await new Promise((r) => setTimeout(r, 50))

    const names = $items().map((el) => el.querySelector('.th-name')?.textContent)
    expect(names).not.toContain('音量键')
  })

  it('shows no content when closed', async () => {
    wrapper = mount(TerminalHelpDrawer, {
      props: { open: false, gestures: true, appMode: false },
      global: { plugins: [i18n] },
    })
    await new Promise((r) => setTimeout(r, 50))

    expect(document.body.querySelector('.th-section')).toBeNull()
  })
})
