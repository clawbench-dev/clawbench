import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SkillsSetting from '@/components/settings/SkillsSetting.vue'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const setServerValue = vi.fn(async () => ({ needsRestart: false, changedColdFields: [], warnings: [] }))
let serverValues: Record<string, unknown> = {}

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    getServerValueWithDefault: (key: string) => serverValues[key],
    setServerValue: (...a: unknown[]) => setServerValue(...(a as [])),
  }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      settings: {
        items: {
          skillsEnabled: 'Enable skill injection',
          skillsEnabledDesc: 'desc',
        },
      },
    },
  },
})

function skillsResponse(enabled = true) {
  return {
    ok: true,
    json: async () => ({ enabled, dirs: [], refresh_hours: 6, last_sync_at: 0, repos: [], skills: [] }),
  } as unknown as Response
}

// The card renders a SettingsItem switch; stub it down to a real checkbox so the
// test drives the same event the component listens for.
function mountCard() {
  return mount(SkillsSetting, {
    props: { description: 'desc' },
    global: {
      plugins: [i18n],
      stubs: {
        SettingsItem: {
          props: ['modelValue', 'label'],
          emits: ['update:modelValue'],
          // No TS casts here: this is a runtime-compiled template string, so a
          // type assertion is a syntax error (the real component is SFC-compiled).
          template: `<label class="settings-item">{{ label }}
            <input class="sw" type="checkbox" :checked="modelValue"
                   @change="$emit('update:modelValue', $event.target.checked)" />
          </label>`,
        },
      },
    },
  })
}

describe('SkillsSetting (master switch)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    serverValues = { 'skills.enabled': true }
    vi.stubGlobal('fetch', vi.fn(async () => skillsResponse()))
  })

  it('reflects the configured enabled state', async () => {
    serverValues = { 'skills.enabled': false }
    const wrapper = mountCard()
    await flushPromises()

    expect((wrapper.find('.sw').element as HTMLInputElement).checked).toBe(false)
  })

  it('persists a toggle to skills.enabled', async () => {
    const wrapper = mountCard()
    await flushPromises()

    await wrapper.find('.sw').setValue(false)
    await flushPromises()

    expect(setServerValue).toHaveBeenCalledWith('skills.enabled', false)
  })

  it('toggles back on', async () => {
    serverValues = { 'skills.enabled': false }
    const wrapper = mountCard()
    await flushPromises()

    await wrapper.find('.sw').setValue(true)
    await flushPromises()

    expect(setServerValue).toHaveBeenCalledWith('skills.enabled', true)
  })

  // The card is only the master switch — the directory and repo lists moved to
  // their own cards, so this one must not render their inputs.
  it('renders no directory or repo controls', async () => {
    const wrapper = mountCard()
    await flushPromises()

    expect(wrapper.find('.skills-dir').exists()).toBe(false)
    expect(wrapper.find('.skills-repo').exists()).toBe(false)
    expect(wrapper.find('.skills-sync').exists()).toBe(false)
  })
})
