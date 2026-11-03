import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import UploadProgressBar from '@/components/common/UploadProgressBar.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: { zh: { common: { cancel: '取消' }, file: { uploading: '上传中...' } } },
})

function mountBar(props: { visible: boolean; progress: number; done: number; total: number; speed?: number }) {
  return mount(UploadProgressBar, {
    props: { speed: 0, ...props },
    global: { plugins: [i18n] },
  })
}

describe('UploadProgressBar', () => {
  it('renders the byte-based bar width and the item count when visible', () => {
    const wrapper = mountBar({ visible: true, progress: 50, done: 2, total: 4 })

    expect(wrapper.find('.transfer-progress').exists()).toBe(true)
    expect(wrapper.find('.transfer-progress-detail').text()).toContain('2/4')
    // The bar width is driven by `progress` (bytes), not the item count — a
    // single large file can hold the percentage still while items advance.
    expect(wrapper.find('.transfer-progress-fill').attributes('style')).toContain('width: 50%')
  })

  it('labels the bar with the upload action, not a file name', () => {
    // Uploads aggregate many files, so there is no single name to show.
    const wrapper = mountBar({ visible: true, progress: 10, done: 1, total: 3 })
    expect(wrapper.find('.transfer-progress-label').text()).toBe('上传中...')
  })

  it('renders nothing when not visible', () => {
    const wrapper = mountBar({ visible: false, progress: 0, done: 0, total: 0 })

    expect(wrapper.find('.transfer-progress').exists()).toBe(false)
  })

  it('emits cancel when the cancel button is clicked', async () => {
    const wrapper = mountBar({ visible: true, progress: 10, done: 1, total: 3 })

    const cancelBtn = wrapper.find('.transfer-progress-cancel')
    expect(cancelBtn.exists()).toBe(true)
    await cancelBtn.trigger('click')

    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })

  it('appends the transfer rate to the right-hand value', () => {
    const wrapper = mountBar({ visible: true, progress: 45, done: 2, total: 4, speed: 1.5 * 1024 * 1024 })

    expect(wrapper.find('.transfer-progress-value').text()).toBe('45% · 1.5 MB/s')
    // The item count keeps its own slot — the rate must not displace it.
    expect(wrapper.find('.transfer-progress-detail').text()).toBe('2/4')
  })

  it('leaves the value as a plain percentage while the rate is unknown', () => {
    const wrapper = mountBar({ visible: true, progress: 45, done: 2, total: 4, speed: 0 })

    expect(wrapper.find('.transfer-progress-value').text()).toBe('45%')
  })

  it('localizes the cancel button tooltip instead of hard-coding Chinese', () => {
    const wrapper = mountBar({ visible: true, progress: 0, done: 0, total: 1 })

    expect(wrapper.find('.transfer-progress-cancel').attributes('title')).toBe('取消')
  })
})
