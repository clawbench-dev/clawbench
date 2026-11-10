import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import SessionListTabs from '@/components/session/SessionListTabs.vue'
import { localConfig } from '@/composables/useSettingsConfig'

// Whitelist mock would make `createI18n` undefined — `useSettingsConfig` pulls
// it in through `@/i18n`, and the whole suite then fails at import. Keep the
// real exports and override only the composable.
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (k: string) => k, locale: { value: 'zh' } }) }
})

// The bar and the pane must agree on when they exist: both read the same
// singleton groups + the same local setting. Mocked here so the test drives
// them directly instead of through the real fetch pipeline.
const crossState = vi.hoisted(() => ({
  groups: null as any,
  total: null as any,
}))
vi.mock('@/composables/useCrossProjectSessions', async () => {
  const { ref } = await import('vue')
  crossState.groups = ref<any[]>([])
  crossState.total = ref(0)
  return {
    useCrossProjectSessions: () => ({ groups: crossState.groups, total: crossState.total }),
  }
})

function mountTabs(props: Record<string, unknown> = {}) {
  return mount(SessionListTabs, { props: { activeTab: 'project', ...props } })
}

describe('SessionListTabs', () => {
  beforeEach(() => {
    crossState.groups.value = []
    crossState.total.value = 0
    localConfig.sessionOtherProjectMode = 'tabs'
  })
  afterEach(() => {
    localConfig.sessionOtherProjectMode = 'strip'
  })

  it('renders nothing in strip mode', () => {
    localConfig.sessionOtherProjectMode = 'strip'
    crossState.groups.value = [{ name: '/p/a', sessions: [] }]
    const wrapper = mountTabs()
    expect(wrapper.find('.session-tabs').exists()).toBe(false)
  })

  it('renders nothing when there are no other projects', () => {
    // A single-option tab bar would just eat height.
    const wrapper = mountTabs()
    expect(wrapper.find('.session-tabs').exists()).toBe(false)
  })

  it('renders two tabs and badges the active-session count', () => {
    crossState.groups.value = [{ name: '/p/a', sessions: [] }]
    crossState.total.value = 3
    const wrapper = mountTabs()

    const tabs = wrapper.findAll('.session-tab')
    expect(tabs.length).toBe(2)
    expect(tabs[0].text()).toBe('session.tabProject')
    expect(tabs[1].text()).toContain('session.tabCross')
    expect(wrapper.find('.session-tab-badge').text()).toBe('3')
  })

  it('omits the badge when only idle projects exist', () => {
    // Cards for merely-recently-used projects carry no active sessions; a "0"
    // would read as an error rather than a count.
    crossState.groups.value = [{ name: '/p/a', sessions: [] }]
    crossState.total.value = 0
    const wrapper = mountTabs()
    expect(wrapper.find('.session-tab-badge').exists()).toBe(false)
  })

  it('marks the active tab and emits the switch', async () => {
    crossState.groups.value = [{ name: '/p/a', sessions: [] }]
    const wrapper = mountTabs({ activeTab: 'cross' })

    const tabs = wrapper.findAll('.session-tab')
    expect(tabs[1].classes()).toContain('active')
    expect(tabs[1].attributes('aria-selected')).toBe('true')

    await tabs[0].trigger('click')
    expect(wrapper.emitted('update:activeTab')).toEqual([['project']])
  })

  it('appears as soon as the setting flips, without a remount', async () => {
    crossState.groups.value = [{ name: '/p/a', sessions: [] }]
    localConfig.sessionOtherProjectMode = 'strip'
    const wrapper = mountTabs()
    expect(wrapper.find('.session-tabs').exists()).toBe(false)

    localConfig.sessionOtherProjectMode = 'tabs'
    await flushPromises()
    expect(wrapper.find('.session-tabs').exists()).toBe(true)
  })
})
