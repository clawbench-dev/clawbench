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
        copyServerAddress: '复制服务器侧地址',
        directionForwardHint: '在本机访问服务器上的服务（ssh -L）',
        directionReverseHint: '把本机服务暴露到服务器（ssh -R）',
        reverseInactiveHint: '等待客户端建立反向隧道',
        portItem: { active: '活跃', connecting: '连接中', tunnelDown: '隧道断开', inactive: '离线', disabled: '已禁用' },
      },
    },
  },
})

vi.mock('lucide-vue-next', () => ({
  Box: { name: 'Box', template: '<span class="icon-box" />' },
  Copy: { name: 'Copy', template: '<span class="icon-copy" />' },
  ExternalLink: { name: 'ExternalLink', template: '<span class="icon-open" />' },
  RefreshCw: { name: 'RefreshCw', template: '<span class="icon-refresh" />' },
  RotateCw: { name: 'RotateCw', template: '<span class="icon-rotate" />' },
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

  it('does not dim the whole card when disabled', () => {
    const wrapper = mountItem({ enabled: false })
    const cardStyle = getComputedStyle(wrapper.find('.proxy-port-item').element).opacity
    expect(cardStyle).toBe('1')
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

  describe('tunnelReady three-state status dot', () => {
    it('shows tunnel-down when the local listener is not reachable', () => {
      // The server may still report active=true (its probe targets the port on
      // the SERVER); the local probe is what matters for the dot.
      const wrapper = mountItem({ active: true, tunnelReady: false })
      expect(wrapper.find('.port-status').classes()).toContain('tunnel-down')
      expect(wrapper.find('.port-status').attributes('title')).toBe('隧道断开')
    })

    it('shows active when the local listener is up and the target is active', () => {
      const wrapper = mountItem({ active: true, tunnelReady: true })
      expect(wrapper.find('.port-status').classes()).toContain('active')
    })

    it('shows inactive when the tunnel is up but the target service is down', () => {
      const wrapper = mountItem({ active: false, tunnelReady: true })
      expect(wrapper.find('.port-status').classes()).toContain('inactive')
      expect(wrapper.find('.port-status').attributes('title')).toBe('离线')
    })

    it('falls back to the legacy logic when tunnelReady is not probed (null)', () => {
      // Guards the Vue Boolean-prop coercion trap: if tunnelReady were declared
      // as type Boolean, an absent prop would become `false` and every unprobed
      // port would be painted red.
      const activeWrapper = mountItem({ active: true })
      expect(activeWrapper.find('.port-status').classes()).toContain('active')

      const tunnelDownWrapper = mountItem({ active: false, tunnelDisconnected: true })
      expect(tunnelDownWrapper.find('.port-status').classes()).toContain('tunnel-down')

      const offlineWrapper = mountItem({ active: false })
      expect(offlineWrapper.find('.port-status').classes()).toContain('inactive')
    })

    it('lets disabled outrank a failed local probe', () => {
      const wrapper = mountItem({ enabled: false, tunnelReady: false })
      expect(wrapper.find('.port-status').classes()).toContain('disabled')
    })
  })

  describe('direction', () => {
    it('defaults to the forward direction with an up arrow', () => {
      const wrapper = mountItem()
      const badge = wrapper.find('.port-direction')
      expect(badge.classes()).toContain('forward')
      expect(badge.text()).toBe('↑')
    })

    it('marks a reverse mapping with a down arrow', () => {
      const wrapper = mountItem({ direction: 'reverse' })
      const badge = wrapper.find('.port-direction')
      expect(badge.classes()).toContain('reverse')
      expect(badge.text()).toBe('↓')
      expect(badge.attributes('title')).toBe('把本机服务暴露到服务器（ssh -R）')
    })

    it('replaces the browser actions with copy-address for reverse mappings', () => {
      const wrapper = mountItem({ direction: 'reverse' })
      // There is no local listener to open, so the browser buttons must be gone.
      expect(wrapper.find('.port-action-btn.sandbox').exists()).toBe(false)
      expect(wrapper.find('.port-action-btn.open').exists()).toBe(false)
      expect(wrapper.find('.port-action-btn.copy-address').exists()).toBe(true)
    })

    it('keeps the browser actions for forward mappings', () => {
      const wrapper = mountItem({ direction: 'forward' })
      expect(wrapper.find('.port-action-btn.sandbox').exists()).toBe(true)
      expect(wrapper.find('.port-action-btn.open').exists()).toBe(true)
      expect(wrapper.find('.port-action-btn.copy-address').exists()).toBe(false)
    })

    it('emits copyAddress with the server-side port and protocol', async () => {
      const wrapper = mountItem({ direction: 'reverse', localPort: 9000, protocol: 'https' })
      await wrapper.find('.port-action-btn.copy-address').trigger('click')
      expect(wrapper.emitted('copyAddress')).toBeTruthy()
      expect(wrapper.emitted('copyAddress')![0]).toEqual([9000, 'https'])
    })

    it('points the target chip leftward at the local service', () => {
      const wrapper = mountItem({ direction: 'reverse', port: 3000, localPort: 9000, host: '10.0.0.7' })
      const chip = wrapper.find('.port-target')
      expect(chip.text()).toContain('10.0.0.7:3000')
      expect(chip.text()).toContain('←')
      expect(chip.classes()).toContain('reverse')
    })

    it('names the waiting reason for an inactive reverse mapping', () => {
      const wrapper = mountItem({ direction: 'reverse', active: false })
      expect(wrapper.find('.port-status').classes()).toContain('inactive')
      expect(wrapper.find('.port-status').attributes('title')).toBe('等待客户端建立反向隧道')
    })

    it('shows active for a bound reverse mapping even though tunnelReady is null', () => {
      // Reverse mappings have no local listener, so the probe is never populated.
      const wrapper = mountItem({ direction: 'reverse', active: true, tunnelReady: null })
      expect(wrapper.find('.port-status').classes()).toContain('active')
    })
  })
})
