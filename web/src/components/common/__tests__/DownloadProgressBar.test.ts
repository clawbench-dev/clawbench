import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import DownloadProgressBar from '../DownloadProgressBar.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: { common: { cancel: 'Cancel' } } },
})

function mountBar(props: Record<string, unknown> = {}) {
  return mount(DownloadProgressBar, {
    props: {
      visible: true,
      fileName: 'report.pdf',
      received: 0,
      total: 0,
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

describe('DownloadProgressBar', () => {
  it('renders nothing when not visible', () => {
    const wrapper = mountBar({ visible: false })
    expect(wrapper.find('.transfer-progress').exists()).toBe(false)
  })

  it('shows the file name and a percentage when the size is known', () => {
    const wrapper = mountBar({ received: 512, total: 2048 })
    expect(wrapper.find('.transfer-progress-label').text()).toBe('report.pdf')
    expect(wrapper.find('.transfer-progress-value').text()).toBe('25%')
    expect(wrapper.find('.transfer-progress-fill').attributes('style')).toContain('width: 25%')
  })

  it('shows the byte count as the value tooltip', () => {
    const wrapper = mountBar({ received: 512, total: 2048 })
    expect(wrapper.find('.transfer-progress-value').attributes('title')).toBe('512 B / 2.0 KB')
  })

  it('runs indeterminately when the server sent no Content-Length', () => {
    const wrapper = mountBar({ received: 512, total: 0 })
    const bar = wrapper.find('.transfer-progress-fill')
    expect(bar.classes()).toContain('transfer-progress-fill--indeterminate')
    // No fixed width in indeterminate mode; the animation drives it instead.
    expect(bar.attributes('style') ?? '').not.toContain('width')
    // The value slot then shows received bytes rather than a bogus 0%.
    expect(wrapper.find('.transfer-progress-value').text()).toBe('512 B')
  })

  it('clamps the percentage at 100 when more bytes than declared arrive', () => {
    const wrapper = mountBar({ received: 4096, total: 2048 })
    expect(wrapper.find('.transfer-progress-value').text()).toBe('100%')
  })

  it('floats under the header so it never disturbs the layout', () => {
    const wrapper = mountBar({ received: 1, total: 2 })
    expect(wrapper.find('.transfer-progress').classes()).toContain('transfer-progress--floating')
  })

  it('emits cancel when the button is clicked', async () => {
    const wrapper = mountBar({ received: 1, total: 2 })
    await wrapper.find('.transfer-progress-cancel').trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })
})
