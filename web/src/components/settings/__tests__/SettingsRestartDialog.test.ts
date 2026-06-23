import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SettingsRestartDialog from '../SettingsRestartDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        restartConfirmTitle: '重启确认',
        restartConfirmMessage: '以下配置变更需要重启服务器才能生效：',
        restartNow: '立即重启',
        restartLater: '稍后',
        items: {
          portForwardEnabled: '启用端口转发',
          terminalEnabled: '启用终端',
        },
      },
    },
  },
})

function mountDialog(changedFields: string[] = []) {
  return mount(SettingsRestartDialog, {
    props: { changedFields },
    global: { plugins: [i18n] },
  })
}

describe('SettingsRestartDialog', () => {
  it('renders translated title and message', () => {
    const wrapper = mountDialog()
    expect(wrapper.text()).toContain('重启确认')
    expect(wrapper.text()).toContain('以下配置变更需要重启服务器才能生效：')
  })

  it('displays translated field labels instead of raw keys', () => {
    const wrapper = mountDialog(['port_forward.enabled', 'terminal.enabled'])
    const listItems = wrapper.findAll('li')
    expect(listItems).toHaveLength(2)
    // Should show translated labels, not raw dot-paths
    expect(listItems[0].text()).toBe('启用端口转发')
    expect(listItems[1].text()).toBe('启用终端')
  })

  it('falls back to raw key for unknown fields', () => {
    const wrapper = mountDialog(['some.unknown.field'])
    const listItems = wrapper.findAll('li')
    expect(listItems[0].text()).toBe('some.unknown.field')
  })

  it('emits restart when restart button is clicked', async () => {
    const wrapper = mountDialog()
    await wrapper.findAll('button')[1].trigger('click') // restart button
    expect(wrapper.emitted('restart')).toBeTruthy()
  })

  it('emits later when later button is clicked', async () => {
    const wrapper = mountDialog()
    await wrapper.findAll('button')[0].trigger('click') // later button
    expect(wrapper.emitted('later')).toBeTruthy()
  })

  it('emits later when overlay background is clicked', async () => {
    const wrapper = mountDialog()
    await wrapper.find('.settings-restart-overlay').trigger('click')
    expect(wrapper.emitted('later')).toBeTruthy()
  })

  it('does not show list when changedFields is empty', () => {
    const wrapper = mountDialog([])
    expect(wrapper.find('ul').exists()).toBe(false)
  })
})
