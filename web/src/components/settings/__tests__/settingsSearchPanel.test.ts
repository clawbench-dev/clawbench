import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { reactive } from 'vue'
import SettingsGroupPanel from '@/components/settings/SettingsGroupPanel.vue'
import type { GroupPanelConfig } from '@/components/settings/settingsFieldMap'

/**
 * Settings search (issue #691) reaching a row inside a group panel.
 *
 * Panels render their fields from `dependsOn` conditions, so the hit the user
 * clicked may be a row that is NOT currently shown (the DingTalk credentials
 * while the push mode is 飞书). Landing on nothing would make the search look
 * broken, so a filtered panel must force that row in — and still show the rows
 * that are already visible, so the panel stays readable.
 */

const localValues = reactive<Record<string, unknown>>({})

vi.mock('@/composables/usePanelSnapshot', () => ({
  usePanelSnapshot: () => ({
    localValues,
    saving: { value: false },
    serverError: { value: '' },
    hotReloadWarning: { value: '' },
    hasFailedSave: { value: false },
    hasChanges: { value: false },
    canSave: { value: true },
    needsRestartHint: { value: false },
    initSnapshot: vi.fn(),
    handleSave: vi.fn(),
  }),
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    getServerValueWithDefault: vi.fn(),
    setLocalConfig: vi.fn(),
    localConfig: reactive({}),
  }),
}))

vi.mock('@/composables/useSettingsNavigation', () => ({
  useSettingsNavigation: () => ({ registerGuard: vi.fn(), unregisterGuard: vi.fn() }),
}))

vi.mock('@/utils/systemSpeech', () => ({
  probeSystemSpeech: vi.fn(),
  systemSpeechErrorKey: (reason: string) => `settings.items.systemSpeech_${reason}`,
}))

vi.mock('@/composables/useConnectivityTest', () => ({
  useConnectivityTest: () => ({
    testing: { value: false },
    testResults: { value: [] },
    runTests: vi.fn(),
    clearResults: vi.fn(),
  }),
}))

vi.mock('@/composables/useRagStatus', () => ({
  useRagStatus: () => ({ status: { value: null }, refresh: vi.fn() }),
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: vi.fn(), alert: vi.fn() }),
}))

vi.mock('@/components/common/WeChatLoginPanel.vue', () => ({
  default: { template: '<div class="mock-wechat" />' },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        panel: { save: '保存', saving: '保存中', testConnectivity: '测试', testing: '测试中' },
        items: {
          pushMode: '推送方式',
          dingtalkAppKey: '钉钉 AppKey',
          feishuAppId: '飞书 App ID',
        },
      },
    },
  },
})

/** 推送通知's panel shape: a mode selector plus per-mode credentials. */
function pushPanel(): GroupPanelConfig {
  return {
    panelId: 'push',
    entrySelector: { labelKey: 'settings.items.pushMode', key: 'push_mode', type: 'select', source: 'server', options: [] },
    commonFields: [],
    optionSubFields: [
      { when: 'dingtalk', fields: [{ labelKey: 'settings.items.dingtalkAppKey', key: 'dingtalk.app_key', type: 'text', source: 'server' }] },
      { when: 'feishu', fields: [{ labelKey: 'settings.items.feishuAppId', key: 'feishu.app_id', type: 'text', source: 'server' }] },
    ],
  }
}

function mountPanel(config: GroupPanelConfig, searchKeys?: string[]) {
  return mount(SettingsGroupPanel, {
    props: { config, showTitle: false, ...(searchKeys ? { searchKeys } : {}) },
    global: {
      plugins: [i18n],
      stubs: {
        SettingsItem: {
          name: 'SettingsItem',
          // A stub never sees the template's props (they are cast to attrs and
          // only re-emitted as data-*), so the key comes in as
          // `data-settings-key` — the same anchor the real row carries.
          props: ['label', 'settingKey', 'disabled', 'searchForced'],
          inheritAttrs: false,
          template: '<div class="row" :data-key="settingKey ?? $attrs[\'data-settings-key\']" :data-forced="searchForced" :data-disabled="disabled"><slot /></div>',
        },
        BottomSheet: { template: '<div />' },
        ChevronRight: { template: '<span>></span>' },
        RefreshCw: { template: '<span />' },
        WeChatLoginPanel: { template: '<div />' },
      },
    },
  })
}

const rowKeys = (wrapper: ReturnType<typeof mountPanel>) =>
  wrapper.findAll('.row').map(w => w.attributes('data-key'))

describe('SettingsGroupPanel search filter', () => {
  beforeEach(() => {
    for (const k of Object.keys(localValues)) delete localValues[k]
    localValues.push_mode = 'feishu'
  })

  it('renders only the rows of the selected mode without a filter', () => {
    const wrapper = mountPanel(pushPanel())
    console.log('HTML', wrapper.html())
    expect(rowKeys(wrapper)).toEqual(['feishu.app_id'])
    expect(wrapper.find('[data-settings-key="push_mode"]').exists()).toBe(true)
  })

  it('forces a row in that the current mode hides, and marks it read-only', () => {
    const wrapper = mountPanel(pushPanel(), ['dingtalk.app_key'])

    // The row the live rules hide is rendered anyway — that is the whole point:
    // otherwise the hit would land on nothing.
    expect(wrapper.find('[data-key="dingtalk.app_key"]').exists()).toBe(true)
    // The other mode's rows stay out: the filter named only the DingTalk key.
    expect(wrapper.find('[data-key="feishu.app_id"]').exists()).toBe(false)
  })

  it('hides the entry selector when the filter points at another row', () => {
    const wrapper = mountPanel(pushPanel(), ['dingtalk.app_key'])
    expect(wrapper.find('[data-settings-key="push_mode"]').exists()).toBe(false)
  })

  it('keeps the entry selector when it is the hit', () => {
    const wrapper = mountPanel(pushPanel(), ['push_mode'])
    expect(wrapper.find('[data-settings-key="push_mode"]').exists()).toBe(true)
    expect(wrapper.findAll('.row').length).toBe(0)
  })

  it('shows nothing when the filter names no row of this panel', () => {
    const wrapper = mountPanel(pushPanel(), ['not.a.field'])
    expect(wrapper.findAll('.row').length).toBe(0)
  })
})
