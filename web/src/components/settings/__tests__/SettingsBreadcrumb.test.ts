import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SettingsBreadcrumb from '@/components/settings/SettingsBreadcrumb.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: { zh: { nav: { settings: '设置' } } },
})

function mountBreadcrumb(crumbs: Array<{ depth: number; label: string }>) {
  return mount(SettingsBreadcrumb, {
    props: { crumbs },
    global: { plugins: [i18n] },
  })
}

describe('SettingsBreadcrumb', () => {
  it('renders one crumb per entry with separators between them', () => {
    const wrapper = mountBreadcrumb([
      { depth: 0, label: '设置' },
      { depth: 1, label: '语音朗读' },
      { depth: 2, label: '语音引擎' },
    ])

    expect(wrapper.findAll('.crumb').map(c => c.text())).toEqual(['设置', '语音朗读', '语音引擎'])
    // n crumbs → n-1 separators
    expect(wrapper.findAll('.crumb-sep')).toHaveLength(2)
  })

  it('marks only the last crumb as current', () => {
    const wrapper = mountBreadcrumb([
      { depth: 0, label: '设置' },
      { depth: 1, label: '语音朗读' },
    ])

    const crumbs = wrapper.findAll('.crumb')
    expect(crumbs[0].classes()).not.toContain('current')
    expect(crumbs[1].classes()).toContain('current')
  })

  it('emits the crumb depth when an ancestor is clicked', async () => {
    const wrapper = mountBreadcrumb([
      { depth: 0, label: '设置' },
      { depth: 1, label: '语音朗读' },
      { depth: 2, label: '语音引擎' },
    ])

    await wrapper.findAll('.crumb')[0].trigger('click')
    expect(wrapper.emitted('navigate')?.[0]).toEqual([0])

    await wrapper.findAll('.crumb')[1].trigger('click')
    expect(wrapper.emitted('navigate')?.[1]).toEqual([1])
  })

  it('does not emit when the current (last) crumb is clicked', async () => {
    const wrapper = mountBreadcrumb([
      { depth: 0, label: '设置' },
      { depth: 1, label: '语音朗读' },
    ])

    await wrapper.findAll('.crumb')[1].trigger('click')
    expect(wrapper.emitted('navigate')).toBeUndefined()
  })

  it('marks itself as a horizontal scroll region (edge-swipe-back exemption)', () => {
    const wrapper = mountBreadcrumb([{ depth: 0, label: '设置' }])
    expect(wrapper.find('.settings-breadcrumb').attributes('data-horizontal-scroll')).toBe('true')
  })
})
