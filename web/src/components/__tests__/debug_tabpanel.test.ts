import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, h } from 'vue'
import TabPanel from '@/components/common/TabPanel.vue'

describe('Debug TabPanel', () => {
  it('renders slot content when active - with render function slot', async () => {
    const wrapper = mount(TabPanel, {
      props: { tabId: 'chat', activeTab: 'chat' },
      slots: { 
        default: () => h('div', { class: 'inner' }, 'Hello')
      },
    })
    await nextTick()
    console.log('html:', wrapper.html())
    expect(wrapper.find('.inner').text()).toBe('Hello')
  })

  it('renders slot content when active - with template string', async () => {
    const wrapper = mount(TabPanel, {
      props: { tabId: 'chat', activeTab: 'chat' },
      slots: { 
        default: '<div class="inner">Hello</div>'
      },
    })
    await nextTick()
    console.log('html:', wrapper.html())
    expect(wrapper.find('.inner').text()).toBe('Hello')
  })
})
