import { describe, it, expect, vi } from 'vitest'
import { ref } from 'vue'
import { mount } from '@vue/test-utils'
import ToastNotification from '../ToastNotification.vue'
import { readWebFile } from '@/testUtils/readWebFile'

function makeToast(overrides = {}) {
  return {
    visible: ref(true),
    type: ref('info'),
    message: ref('Test message'),
    icon: ref(''),
    onClick: ref(null),
    dismiss: vi.fn(),
    ...overrides,
  }
}

describe('ToastNotification', () => {
  it('renders message when visible', () => {
    const toast = makeToast()
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.text()).toContain('Test message')
  })

  it('applies type class', () => {
    const toast = makeToast({ type: ref('error') })
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.toast').classes()).toContain('toast-error')
  })

  it('renders icon when present', () => {
    const toast = makeToast({ icon: ref('✓') })
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.toast-icon').text()).toBe('✓')
  })

  it('calls dismiss on click', async () => {
    const toast = makeToast()
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await wrapper.find('.toast').trigger('click')
    expect(toast.dismiss).toHaveBeenCalled()
  })

  it('calls onClick and dismiss when onClick is set', async () => {
    const onClick = vi.fn()
    const toast = makeToast({ onClick: ref(onClick) })
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    await wrapper.find('.toast').trigger('click')
    expect(onClick).toHaveBeenCalled()
    expect(toast.dismiss).toHaveBeenCalled()
  })

  it('hides when not visible', () => {
    const toast = makeToast({ visible: ref(false) })
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.toast').exists()).toBe(false)
  })

  // ── loading type ──
  it('renders a spinner instead of the icon for the loading type', () => {
    const toast = makeToast({ type: ref('loading'), icon: ref('🔄'), message: ref('Forking session… 2s') })
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.toast').classes()).toContain('toast-loading')
    expect(wrapper.find('.toast-spinner').exists()).toBe(true)
    // The icon slot must not also render — a spinner AND an emoji reads as two
    // conflicting signals.
    expect(wrapper.find('.toast-icon').exists()).toBe(false)
    expect(wrapper.text()).toContain('Forking session… 2s')
  })

  it('still renders the icon for non-loading types', () => {
    const toast = makeToast({ type: ref('info'), icon: ref('🔄') })
    const wrapper = mount(ToastNotification, {
      props: { toast },
      global: { stubs: { Teleport: { template: '<div><slot/></div>' } } },
    })
    expect(wrapper.find('.toast-spinner').exists()).toBe(false)
    expect(wrapper.find('.toast-icon').exists()).toBe(true)
  })

  // The toast is a floating pill: jsdom resolves neither var() nor layout, so
  // the shape is pinned at the source level (same pattern as
  // chatActionBarShape.test.ts). `--radius-lg` (14px) left the short toasts
  // looking like rounded rectangles; a capsule needs the pill token.
  it('shapes the toast as a capsule via the pill radius token', () => {
    const source = readWebFile('src/components/common/ToastNotification.vue')
    const m = source.match(/\n\s*\.toast\s*\{([\s\S]*?)\n\s*\}/)
    expect(m, 'the .toast base rule should exist').not.toBeNull()
    const base = m![1]
    expect(base).toMatch(/border-radius:\s*var\(--radius-full\)/)
    expect(base, '--radius-lg is the rounded-rect that was replaced').not.toMatch(
      /border-radius:\s*var\(--radius-lg\)/,
    )
  })
})
