import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { readFileSync } from 'fs'
import { resolve } from 'path'
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

  it('stays in flow by default (upload bars push their panel content down)', () => {
    expect(mountBar().find('.transfer-progress').classes())
      .not.toContain('transfer-progress--floating')
  })

  it('floats when the floating prop is set', () => {
    // A download bar must not disturb the layout it appears over.
    const wrapper = mountBar({ floating: true })
    expect(wrapper.find('.transfer-progress').classes()).toContain('transfer-progress--floating')
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

/**
 * Source-contract checks for the floating placement. jsdom does not compute
 * `position: fixed`, so these assert the CSS itself rather than a rendered
 * geometry that would silently pass either way.
 */
describe('TransferProgressBar floating placement (source contract)', () => {
  const source = readFileSync(
    resolve(__dirname, '../TransferProgressBar.vue'),
    'utf8',
  )
  const rule = source.match(/\.transfer-progress--floating\s*\{[\s\S]*?\}/)

  it('defines the floating rule', () => {
    expect(rule).toBeTruthy()
  })

  it('takes the bar out of flow so it cannot disturb the layout', () => {
    expect(rule![0]).toContain('position: fixed')
  })

  it('centers it horizontally without needing the container width', () => {
    expect(rule![0]).toContain('left: 50%')
    expect(rule![0]).toContain('transform: translateX(-50%)')
  })

  it('caps the width so it does not stretch edge to edge on desktop', () => {
    expect(rule![0]).toContain('max-width: 520px')
  })

  it('keeps a gutter on narrow viewports where the cap does not apply', () => {
    expect(rule![0]).toContain('width: calc(100% - 2 * var(--space-4, 12px))')
  })

  it('clears the app header and allows a host to override the offset', () => {
    // The share SPA's topbar is 44px, not --header-height, so it overrides.
    expect(rule![0]).toContain('--transfer-progress-top')
    expect(rule![0]).toContain('var(--header-height')
    expect(rule![0]).toContain('var(--header-safe-area-top')
  })

  it('stacks above the header rather than behind it', () => {
    expect(rule![0]).toContain('z-index: var(--z-header')
  })
})
