import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import RichTextBlock from '../RichTextBlock.vue'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

function tableBlock(table: string): string {
  return '<div class="table-block-wrapper word-wrap">'
    + '<div class="table-block-header"><span class="table-block-label">Table</span></div>'
    + `<div class="table-wrap">${table}</div></div>`
}

const NUMERIC = '<table><thead><tr><th>region</th><th>q1</th></tr></thead>'
  + '<tbody><tr><td>East</td><td>10</td></tr><tr><td>West</td><td>5</td></tr></tbody></table>'

const TEXT = '<table><thead><tr><th>name</th><th>city</th></tr></thead>'
  + '<tbody><tr><td>ann</td><td>paris</td></tr><tr><td>bob</td><td>berlin</td></tr></tbody></table>'

function mountBlock(html: string, streaming = false) {
  return mount(RichTextBlock, {
    props: { html, streaming },
    global: { plugins: [i18n], stubs: { teleport: true } },
    attachTo: document.body,
  })
}

describe('RichTextBlock', () => {
  it('renders a numeric table through the shared DataTable', () => {
    const wrapper = mountBlock(`<p>before</p>${tableBlock(NUMERIC)}<p>after</p>`)
    expect(wrapper.find('.dt').exists()).toBe(true)
    expect(wrapper.findAll('.dt-grid tbody tr')).toHaveLength(2)
    // 表头栏也在组件里（标题就是简单表/数据表的切换器）
    expect(wrapper.find('.dt-head').exists()).toBe(true)
    expect(wrapper.text()).toContain('before')
    expect(wrapper.text()).toContain('after')
    wrapper.unmount()
  })

  it('renders a text table through the same control, in simple mode', () => {
    const wrapper = mountBlock(tableBlock(TEXT))
    expect(wrapper.find('.dt').exists()).toBe(true)
    expect(wrapper.find('.dt').classes()).not.toContain('is-data')
    expect(wrapper.find('.dt-bar').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not split while streaming', () => {
    const wrapper = mountBlock(tableBlock(NUMERIC), true)
    expect(wrapper.find('.dt').exists()).toBe(false)
    expect(wrapper.find('table').exists()).toBe(true)
    wrapper.unmount()
  })
})
