import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import SessionListTabs from '@/components/session/SessionListTabs.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (k: string) => k }) }))

// A hoisted container the mock factory closes over; the actual ref is assigned
// in the module body below (vi.hoisted runs before the vue import initializes,
// so it cannot create a ref itself).
const { holder } = vi.hoisted(() => ({ holder: { total: null as any } }))
vi.mock('@/composables/useCrossProjectSessions', () => ({
  useCrossProjectSessions: () => ({ total: holder.total }),
}))

const totalRef = ref(0)
holder.total = totalRef

describe('SessionListTabs', () => {
  beforeEach(() => { totalRef.value = 0 })

  it('hides the whole bar when there are no other-project active sessions', () => {
    const wrapper = mount(SessionListTabs, { props: { activeTab: 'project' } })
    expect(wrapper.find('.session-tabs').exists()).toBe(false)
  })

  it('renders both tabs with the badge when other projects have active sessions', () => {
    totalRef.value = 3
    const wrapper = mount(SessionListTabs, { props: { activeTab: 'project' } })
    expect(wrapper.find('.session-tabs').exists()).toBe(true)
    expect(wrapper.findAll('.session-tab')).toHaveLength(2)
    expect(wrapper.find('.session-tab-badge').text()).toBe('3')
  })

  it('reactively hides the bar when the last active session disappears', async () => {
    totalRef.value = 2
    const wrapper = mount(SessionListTabs, { props: { activeTab: 'project' } })
    expect(wrapper.find('.session-tabs').exists()).toBe(true)
    totalRef.value = 0
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.session-tabs').exists()).toBe(false)
  })

  it('switches to the project tab when the last active session disappears', async () => {
    totalRef.value = 1
    const wrapper = mount(SessionListTabs, { props: { activeTab: 'cross' } })
    totalRef.value = 0
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('update:activeTab')?.[0]).toEqual(['project'])
  })

  it('stays put when the list empties while already on the project tab', async () => {
    totalRef.value = 1
    const wrapper = mount(SessionListTabs, { props: { activeTab: 'project' } })
    totalRef.value = 0
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('update:activeTab')).toBeFalsy()
  })

  it('falls back on mount when already on the cross tab with no active sessions', () => {
    totalRef.value = 0
    const wrapper = mount(SessionListTabs, { props: { activeTab: 'cross' } })
    expect(wrapper.emitted('update:activeTab')?.[0]).toEqual(['project'])
  })

  it('emits the clicked tab', async () => {
    totalRef.value = 2
    const wrapper = mount(SessionListTabs, { props: { activeTab: 'project' } })
    await wrapper.findAll('.session-tab')[1].trigger('click')
    expect(wrapper.emitted('update:activeTab')?.[0]).toEqual(['cross'])
  })
})
