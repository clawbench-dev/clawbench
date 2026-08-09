import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ProxyPortItem from '@/components/proxy/ProxyPortItem.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { edit: '编辑', delete: '删除' },
      proxy: {
        openInSandbox: '沙箱打开',
        openInBrowser: '浏览器打开',
        reconnectPort: '重连',
        enable: '启用',
        disable: '禁用',
        portItem: { active: '活跃', connecting: '连接中', tunnelDown: '隧道断开', inactive: '离线', disabled: '已禁用' },
      },
    },
  },
})

vi.mock('lucide-vue-next', () => ({
  Box: { name: 'Box', template: '<span class="icon-box" />' },
  ExternalLink: { name: 'ExternalLink', template: '<span class="icon-open" />' },
  RotateCcw: { name: 'RotateCcw', template: '<span class="icon-reconnect" />' },
  Pencil: { name: 'Pencil', template: '<span class="icon-edit" />' },
  Trash2: { name: 'Trash2', template: '<span class="icon-delete" />' },
}))

function mountItem(props: Record<string, any> = {}) {
  return mount(ProxyPortItem, {
    props: { port: 8080, localPort: 8080, host: '', name: '', protocol: 'http', active: true, enabled: true, ...props },
    global: { plugins: [i18n] },
  })
}

describe('ProxyPortItem', () => {
  it('renders port number and protocol badge', () => {
    const wrapper = mountItem()
    expect(wrapper.find('.port-number').text()).toBe('8080')
    expect(wrapper.find('.port-protocol').text()).toBe('http')
  })

  it('shows target when target port differs from local port', () => {
    const wrapper = mountItem({ port: 8080, localPort: 8081, host: '192.168.1.1' })
    expect(wrapper.find('.port-target').text()).toContain('192.168.1.1:8080')
  })

  it('shows name when provided', () => {
    const wrapper = mountItem({ name: 'Vite Dev' })
    expect(wrapper.find('.port-name').text()).toBe('Vite Dev')
  })

  it('applies disabled class and disabled status when enabled=false', () => {
    const wrapper = mountItem({ enabled: false, active: true })
    expect(wrapper.find('.proxy-port-item').classes()).toContain('disabled')
    expect(wrapper.find('.port-status').classes()).toContain('disabled')
  })

  it('renders toggle in on state when enabled', () => {
    const wrapper = mountItem({ enabled: true })
    expect(wrapper.find('.toggle-switch').classes()).toContain('on')
  })

  it('renders toggle in off state when disabled', () => {
    const wrapper = mountItem({ enabled: false })
    expect(wrapper.find('.toggle-switch').classes()).not.toContain('on')
  })

  it('disables action buttons when port is disabled', () => {
    const wrapper = mountItem({ enabled: false })
    const openBtn = wrapper.find('.port-action-btn.open')
    expect(openBtn.attributes('disabled')).toBeDefined()
  })

  it('enables action buttons when port is enabled', () => {
    const wrapper = mountItem({ enabled: true })
    const openBtn = wrapper.find('.port-action-btn.open')
    expect(openBtn.attributes('disabled')).toBeUndefined()
  })

  it('emits toggleEnabled with toggled value when switch clicked', async () => {
    const wrapper = mountItem({ enabled: true, localPort: 8080 })
    await wrapper.find('.toggle-switch').trigger('click')
    expect(wrapper.emitted('toggleEnabled')).toBeTruthy()
    expect(wrapper.emitted('toggleEnabled')![0]).toEqual([8080, false])
  })

  it('emits toggleEnabled to re-enable when switch clicked while disabled', async () => {
    const wrapper = mountItem({ enabled: false, localPort: 8080 })
    await wrapper.find('.toggle-switch').trigger('click')
    expect(wrapper.emitted('toggleEnabled')![0]).toEqual([8080, true])
  })

  it('emits openExternal with localPort, protocol, host when browser button clicked', async () => {
    const wrapper = mountItem({ localPort: 8080, protocol: 'https', host: 'api.example.com' })
    await wrapper.find('.port-action-btn.open').trigger('click')
    expect(wrapper.emitted('openExternal')).toBeTruthy()
    expect(wrapper.emitted('openExternal')![0]).toEqual([8080, 'https', 'api.example.com'])
  })

  it('emits open with localPort, protocol, host when sandbox button clicked', async () => {
    const wrapper = mountItem({ localPort: 8080, protocol: 'https', host: 'api.example.com' })
    await wrapper.find('.port-action-btn.sandbox').trigger('click')
    expect(wrapper.emitted('open')).toBeTruthy()
    expect(wrapper.emitted('open')![0]).toEqual([8080, 'https', 'api.example.com'])
  })

  it('emits remove on delete click', async () => {
    const wrapper = mountItem({ localPort: 8080 })
    await wrapper.find('.port-action-btn.delete').trigger('click')
    expect(wrapper.emitted('remove')).toBeTruthy()
    expect(wrapper.emitted('remove')![0]).toEqual([8080])
  })

  it('emits edit on edit click', async () => {
    const wrapper = mountItem({ localPort: 8080 })
    await wrapper.find('.port-action-btn.edit').trigger('click')
    expect(wrapper.emitted('edit')).toBeTruthy()
    expect(wrapper.emitted('edit')![0]).toEqual([8080])
  })
})
