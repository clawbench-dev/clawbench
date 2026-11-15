import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'
import FeatureStatusRow from '@/components/settings/FeatureStatusRow.vue'

const mockServerConfig = ref<Record<string, unknown>>({})

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    serverConfig: mockServerConfig,
    // Dot-path resolver against the reactive fixture, mirroring the real one.
    getServerValueWithDefault: (dotPath: string) => {
      const parts = dotPath.split('.')
      let current: unknown = mockServerConfig.value
      for (const part of parts) {
        if (current == null || typeof current !== 'object') return undefined
        current = (current as Record<string, unknown>)[part]
      }
      return current
    },
  }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          featureStatusEnabled: '已启用',
          featureStatusDisabled: '已停用',
          featureStatusGoManage: '去功能编排',
        },
      },
    },
  },
})

function mountRow(props: Record<string, unknown> = {}) {
  return mount(FeatureStatusRow, {
    props: { enableKey: 'forge.enabled', label: 'GitHub/GitLab 集成', ...props },
    global: {
      plugins: [i18n],
      stubs: {
        'lucide-check': true,
        'lucide-power-off': true,
        'lucide-chevron-right': true,
      },
    },
  })
}

describe('FeatureStatusRow', () => {
  beforeEach(() => {
    mockServerConfig.value = {}
  })

  it('reports an enabled feature', () => {
    mockServerConfig.value = { forge: { enabled: true } }
    const wrapper = mountRow()

    expect(wrapper.text()).toContain('已启用')
    expect(wrapper.text()).not.toContain('已停用')
    expect(wrapper.find('.feature-status__pill--on').exists()).toBe(true)
  })

  it('reports a disabled feature', () => {
    mockServerConfig.value = { forge: { enabled: false } }
    const wrapper = mountRow()

    expect(wrapper.text()).toContain('已停用')
    expect(wrapper.find('.feature-status__pill--off').exists()).toBe(true)
  })

  it('treats a missing key as disabled rather than crashing', () => {
    const wrapper = mountRow({ enableKey: 'share.enabled' })
    expect(wrapper.text()).toContain('已停用')
  })

  it('carries no control — the switch lives in the feature manager', () => {
    mockServerConfig.value = { forge: { enabled: true } }
    const wrapper = mountRow()

    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.find('.settings-item__switch').exists()).toBe(false)
  })

  it('emits navigate when 去功能编排 is clicked', async () => {
    mockServerConfig.value = { forge: { enabled: true } }
    const wrapper = mountRow()

    await wrapper.get('[data-action="feature-status-manage"]').trigger('click')
    expect(wrapper.emitted('navigate')).toBeTruthy()
    expect(wrapper.emitted('navigate')!.length).toBe(1)
  })

  it('renders the description when given', () => {
    mockServerConfig.value = { forge: { enabled: true } }
    const wrapper = mountRow({ description: '关闭后会解除所有仓库关联' })

    expect(wrapper.find('.feature-status__desc').text()).toContain('关闭后会解除所有仓库关联')
  })

  it('omits the description block when there is none', () => {
    mockServerConfig.value = { forge: { enabled: true } }
    const wrapper = mountRow()
    expect(wrapper.find('.feature-status__desc').exists()).toBe(false)
  })

  // The pill must follow the config, not a snapshot: the manager saves the flag
  // elsewhere, and this row has to flip without remounting.
  it('reacts to the config changing underneath it', async () => {
    mockServerConfig.value = { forge: { enabled: true } }
    const wrapper = mountRow()
    expect(wrapper.text()).toContain('已启用')

    mockServerConfig.value = { forge: { enabled: false } }
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('已停用')
  })
})
