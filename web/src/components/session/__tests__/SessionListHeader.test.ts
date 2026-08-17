import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SessionListHeader from '@/components/session/SessionListHeader.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }),
}))

function mountHeader(props = {}) {
  return mount(SessionListHeader, {
    props: { sessionCount: 0, sessionMaxCount: 10, ...props },
  })
}

describe('SessionListHeader', () => {
  it('renders counter when maxCount > 0', () => {
    const wrapper = mountHeader({ sessionCount: 5, sessionMaxCount: 10 })
    expect(wrapper.find('.session-counter').exists()).toBe(true)
  })

  it('does not render counter when maxCount is 0', () => {
    const wrapper = mountHeader({ sessionMaxCount: 0 })
    expect(wrapper.find('.session-counter').exists()).toBe(false)
  })

  it('renders default action buttons (search + create)', async () => {
    const wrapper = mountHeader()
    const search = wrapper.find('.header-action-btn[data-action="search"]')
    const create = wrapper.find('.header-action-btn[data-action="create"]')
    expect(search.exists()).toBe(true)
    expect(create.exists()).toBe(true)
  })

  it('emits open-search when search clicked', async () => {
    const wrapper = mountHeader()
    await wrapper.find('.header-action-btn[data-action="search"]').trigger('click')
    expect(wrapper.emitted('open-search')).toBeTruthy()
  })

  it('emits create when create clicked', async () => {
    const wrapper = mountHeader()
    await wrapper.find('.header-action-btn[data-action="create"]').trigger('click')
    expect(wrapper.emitted('create')).toBeTruthy()
  })

  it('renders a leading extra button passed via slot', () => {
    const wrapper = mount(SessionListHeader, {
      props: { sessionCount: 0, sessionMaxCount: 0 },
      slots: { actions: '<button class="pin-stub" />' },
    })
    expect(wrapper.find('.pin-stub').exists()).toBe(true)
  })

  it('when pinned, keeps search/create and also shows a refresh button', () => {
    const wrapper = mountHeader({ pinned: true })
    expect(wrapper.find('.header-action-btn[data-action="search"]').exists()).toBe(true)
    expect(wrapper.find('.header-action-btn[data-action="create"]').exists()).toBe(true)
    expect(wrapper.find('.header-action-btn[data-action="refresh"]').exists()).toBe(true)
  })

  it('emits refresh when the pinned refresh button is clicked', async () => {
    const wrapper = mountHeader({ pinned: true })
    await wrapper.find('.header-action-btn[data-action="refresh"]').trigger('click')
    expect(wrapper.emitted('refresh')).toBeTruthy()
  })
})
