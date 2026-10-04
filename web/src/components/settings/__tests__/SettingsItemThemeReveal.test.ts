import { beforeAll, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { OptionPreview } from '@/components/settings/SettingsItem.vue'
import SettingsItem from '@/components/settings/SettingsItem.vue'

// jsdom has no window.matchMedia — resolveThemeId('auto') needs it
beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  })
})

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: { zh: { common: { ok: '确定' }, settings: { items: { resetToDefault: '重置' } } } },
})

vi.mock('lucide-vue-next', () => ({
  Eye: { name: 'Eye', template: '<span />' },
  EyeOff: { name: 'EyeOff', template: '<span />' },
  RefreshCw: { name: 'RefreshCw', template: '<span />' },
  ChevronsUpDown: { name: 'ChevronsUpDown', template: '<span />' },
  Palette: { name: 'Palette', template: '<span />' },
  Sun: { name: 'Sun', template: '<span />' },
  Moon: { name: 'Moon', template: '<span />' },
}))

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    isOpen: { value: false },
    effectiveOpen: { value: false },
    open: vi.fn(),
    close: vi.fn(),
    toggle: vi.fn(),
  }),
}))

// Spy on the reveal helpers so we can assert the wiring without a real browser.
const applyThemeWithReveal = vi.fn((apply: () => void) => { apply(); return null })
const headerThemeOrigin = vi.fn(() => ({ x: 111, y: 22 }))
vi.mock('@/utils/themeReveal', () => ({
  applyThemeWithReveal: (apply: () => void, opts: unknown) => applyThemeWithReveal(apply, opts),
  headerThemeOrigin: () => headerThemeOrigin(),
}))

const globalStubs = {
  BottomSheet: {
    template: '<div class="bs-stub"><slot name="header" /><slot /></div>',
    props: ['open'],
  },
}

const previews: Record<string, OptionPreview> = {
  'github-light': { type: 'color', bg: '#f8f9fa', text: '#212529', accent: '#4a90d9', themeId: 'github-light' },
  'github-dark': { type: 'color', bg: '#161b22', text: '#c9d1d9', accent: '#58a6ff', themeId: 'github-dark' },
}

function mountItem(extra: Record<string, unknown> = {}) {
  return mount(SettingsItem, {
    props: {
      label: '主题',
      type: 'select',
      modelValue: 'github-light',
      options: [
        { label: 'Light', value: 'github-light' },
        { label: 'Dark', value: 'github-dark' },
      ],
      optionPreviews: previews,
      ...extra,
    },
    global: { stubs: globalStubs, plugins: [i18n] },
  })
}

describe('SettingsItem revealOnSelect', () => {
  it('reveals from the header origin and still emits the value', async () => {
    applyThemeWithReveal.mockClear()
    headerThemeOrigin.mockClear()
    const wrapper = mountItem({ revealOnSelect: true })
    const vm = wrapper.vm as any

    vm.$.setupState.selectOption('github-dark')
    await wrapper.vm.$nextTick()
    await Promise.resolve()
    await wrapper.vm.$nextTick()

    expect(headerThemeOrigin).toHaveBeenCalledTimes(1)
    expect(applyThemeWithReveal).toHaveBeenCalledTimes(1)
    expect(applyThemeWithReveal.mock.calls[0][1]).toEqual({ origin: { x: 111, y: 22 } })
    expect(wrapper.emitted('update:modelValue')).toBeTruthy()
    expect(wrapper.emitted('update:modelValue')![0]).toEqual(['github-dark'])
  })

  it('does not reveal when revealOnSelect is off (terminal grid / plain select)', async () => {
    applyThemeWithReveal.mockClear()
    headerThemeOrigin.mockClear()
    const wrapper = mountItem()
    const vm = wrapper.vm as any

    vm.$.setupState.selectOption('github-dark')
    await wrapper.vm.$nextTick()
    await Promise.resolve()

    expect(applyThemeWithReveal).not.toHaveBeenCalled()
    expect(headerThemeOrigin).not.toHaveBeenCalled()
    // The plain path emits synchronously.
    expect(wrapper.emitted('update:modelValue')![0]).toEqual(['github-dark'])
  })
})
