import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import TabPanel from '@/components/common/TabPanel.vue'

describe('Slot test with SFC', () => {
  it('renders SFC with named slot', async () => {
    const wrapper = mount(TabPanel, {
      props: { tabId: 'chat', activeTab: 'chat' },
    })
    console.log('html:', wrapper.html())
    expect(wrapper.find('.tab-panel').exists()).toBe(true)
  })
})
