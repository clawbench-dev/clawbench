import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import TransferProgressBar from '../TransferProgressBar.vue'

/**
 * The shared visual for uploads and downloads. Both adapters feed it, so these
 * tests pin the contract they rely on: a label, an optional detail, a value, a
 * bar width and a cancel button.
 */
function mountBar(props: Record<string, unknown> = {}) {
  return mount(TransferProgressBar, {
    props: {
      visible: true,
      label: 'report.pdf',
      value: '25%',
      percent: 25,
      cancelTitle: 'Cancel',
      ...props,
    },
  })
}

describe('TransferProgressBar', () => {
  it('renders nothing when not visible', () => {
    expect(mountBar({ visible: false }).find('.transfer-progress').exists()).toBe(false)
  })

  it('renders label, value and bar width', () => {
    const wrapper = mountBar()
    expect(wrapper.find('.transfer-progress-label').text()).toBe('report.pdf')
    expect(wrapper.find('.transfer-progress-value').text()).toBe('25%')
    expect(wrapper.find('.transfer-progress-fill').attributes('style')).toContain('width: 25%')
  })

  it('omits the detail slot when not provided', () => {
    expect(mountBar().find('.transfer-progress-detail').exists()).toBe(false)
  })

  it('is not width-capped by default (upload bars fill their panel)', () => {
    expect(mountBar().find('.transfer-progress').classes())
      .not.toContain('transfer-progress--centered')
  })

  it('caps and centers the width when centered is set', () => {
    // A download bar spans the whole window, so at desktop widths it needs a
    // cap or it stretches edge to edge.
    const wrapper = mountBar({ centered: true })
    expect(wrapper.find('.transfer-progress').classes()).toContain('transfer-progress--centered')
  })

  it('renders the detail slot when provided', () => {
    const wrapper = mountBar({ detail: '2/4' })
    expect(wrapper.find('.transfer-progress-detail').text()).toBe('2/4')
  })

  it('animates indeterminately with no fixed width', () => {
    const wrapper = mountBar({ percent: 0, indeterminate: true })
    const fill = wrapper.find('.transfer-progress-fill')
    expect(fill.classes()).toContain('transfer-progress-fill--indeterminate')
    expect(fill.attributes('style') ?? '').not.toContain('width')
  })

  it('exposes the value tooltip', () => {
    const wrapper = mountBar({ valueTitle: '512 B / 2.0 KB' })
    expect(wrapper.find('.transfer-progress-value').attributes('title')).toBe('512 B / 2.0 KB')
  })

  it('uses the provided cancel title and emits cancel on click', async () => {
    const wrapper = mountBar({ cancelTitle: '取消' })
    const btn = wrapper.find('.transfer-progress-cancel')
    expect(btn.attributes('title')).toBe('取消')

    await btn.trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })
})
