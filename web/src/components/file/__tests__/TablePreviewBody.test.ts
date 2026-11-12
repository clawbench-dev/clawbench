import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import en from '@/i18n/locales/en'
import TablePreviewBody from '../TablePreviewBody.vue'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

function mountTable(content: string, extra: Record<string, unknown> = {}) {
  return mount(TablePreviewBody, {
    props: { file: { name: 'data.csv', content, ...extra } },
    global: { plugins: [i18n], stubs: { teleport: true } },
    attachTo: document.body,
  })
}

describe('TablePreviewBody', () => {
  it('parses the file and renders the shared DataTable', () => {
    const wrapper = mountTable('name,qty\napple,3\nbanana,5')
    expect(wrapper.findAll('.dt-grid thead th')).toHaveLength(2)
    expect(wrapper.findAll('.dt-grid tbody tr')).toHaveLength(2)
    expect(wrapper.find('.dt').classes()).toContain('is-data')
    wrapper.unmount()
  })

  it('warns when the file content was truncated', () => {
    const wrapper = mountTable('name,qty\napple,3', { truncated: true })
    expect(wrapper.text()).toContain('truncated')
    wrapper.unmount()
  })

  it('renders a header-only file as an empty table', () => {
    const wrapper = mountTable('name,qty')
    expect(wrapper.find('.dt-empty').text()).toBe('No data')
    wrapper.unmount()
  })

  it('exposes focusSearch / closeSearch for the file viewer header', async () => {
    const wrapper = mountTable('name,city\nann,paris\nbob,berlin')
    expect(wrapper.find('.dt').classes()).not.toContain('is-data')

    ;(wrapper.vm as unknown as { focusSearch: () => void }).focusSearch()
    await nextTick()
    expect(wrapper.find('.dt').classes()).toContain('is-data')
    expect(document.activeElement).toBe(wrapper.find('.dt-search input').element)

    ;(wrapper.vm as unknown as { closeSearch: () => void }).closeSearch()
    await nextTick()
    expect(wrapper.find('.dt-search input').element).not.toBe(document.activeElement)
    wrapper.unmount()
  })
})
