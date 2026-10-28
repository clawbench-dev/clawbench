import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'

// ── Mocks ─────────────────────────────────────────────────────────────────
// The picker's effects go through useSettingsConfig (persistence + the side
// effect that flips data-theme). Stub the setters so this file can assert on the
// exact call instead of on localStorage plumbing that useSettingsConfig already
// tests on its own.
const { setLocalConfigFn, localConfigState } = vi.hoisted(() => ({
  setLocalConfigFn: vi.fn(),
  localConfigState: { theme: 'auto' as string | null },
}))
vi.mock('@/composables/useSettingsConfig', () => ({
  localConfig: localConfigState,
  setLocalConfig: setLocalConfigFn,
}))

// The reveal is a View Transition; jsdom has none. Stub it so the wiring
// (which callback, which origin) is observable, and so `applyChange` actually
// runs — otherwise the theme would never be applied in the test.
const { applyThemeWithRevealFn, originFromElementFn, headerThemeOriginFn } = vi.hoisted(() => ({
  applyThemeWithRevealFn: vi.fn((apply: () => void) => { apply(); return null }),
  originFromElementFn: vi.fn(() => ({ x: 700, y: 22 })),
  headerThemeOriginFn: vi.fn(() => ({ x: 999, y: 0 })),
}))
vi.mock('@/utils/themeReveal', () => ({
  applyThemeWithReveal: applyThemeWithRevealFn,
  originFromElement: originFromElementFn,
  headerThemeOrigin: headerThemeOriginFn,
}))

vi.mock('@/components/common/PopupMenu.vue', () => ({
  default: {
    name: 'PopupMenu',
    props: ['show', 'targetElement', 'maxWidth', 'maxHeight', 'menuItemsCount', 'anchor', 'appSurface'],
    emits: ['update:show', 'after-leave'],
    template: '<div class="popup-stub" v-if="show"><slot /></div>',
  },
}))

import ShareThemeButton from '@/share/ShareThemeButton.vue'
import { THEME_IDS, getThemeLabelKey } from '@/utils/themeMeta'

// jsdom has no matchMedia; the 'auto' row resolves the system scheme through it
// (getThemePreviewColor(resolveThemeId('auto'))). Stub a light scheme.
function stubMatchMedia() {
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })))
}
stubMatchMedia()

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      appHeader: { themePicker: 'Switch theme' },
      terminal: { theme: 'Theme' },
      settings: { items: Object.fromEntries([['themeAuto', 'Auto'], ...THEME_IDS.map(id => [getThemeLabelKey(id).split('.').pop()!, id])]) },
    },
  },
})

function mountButton() {
  return mount(ShareThemeButton, { global: { plugins: [i18n] } })
}

beforeEach(() => {
  stubMatchMedia()
  setLocalConfigFn.mockClear()
  applyThemeWithRevealFn.mockClear()
  originFromElementFn.mockClear()
  headerThemeOriginFn.mockClear()
  localConfigState.theme = 'auto'
})

afterEach(() => {
  document.body.querySelectorAll('.popup-stub, .theme-picker').forEach(el => el.remove())
})

describe('ShareThemeButton', () => {
  it('renders a topbar-style icon button with an accessible label', () => {
    const wrapper = mountButton()
    const btn = wrapper.find('button.share-theme-btn')
    expect(btn.exists()).toBe(true)
    // .share-btn is the shared topbar button geometry — a bare <button> would
    // render with the UA outset border next to the TOC/download chips.
    expect(btn.classes()).toContain('share-btn')
    expect(btn.attributes('aria-label')).toBe('Switch theme')
    expect(btn.attributes('aria-expanded')).toBe('false')
    wrapper.unmount()
  })

  it('offers the follow-system option plus every registered theme', async () => {
    const wrapper = mountButton()
    await wrapper.find('button.share-theme-btn').trigger('click')
    await nextTick()

    const rows = wrapper.findAll('.theme-item')
    expect(rows.length).toBe(THEME_IDS.length + 1)
    // The first row is the 'auto' one, carrying the split light/dark swatch.
    expect(rows[0].find('.theme-swatch--auto').exists()).toBe(true)
    // Exactly one row is marked active, and it is the stored value.
    const active = wrapper.findAll('.theme-item.active')
    expect(active.length).toBe(1)
    expect(active[0].text()).toContain('Auto')
    wrapper.unmount()
  })

  it('marks the row matching the stored theme, not the first one', async () => {
    localConfigState.theme = 'dracula'
    const wrapper = mountButton()
    await wrapper.find('button.share-theme-btn').trigger('click')
    await nextTick()

    const active = wrapper.findAll('.theme-item.active')
    expect(active.length).toBe(1)
    expect(active[0].text()).toContain('dracula')
    wrapper.unmount()
  })

  it('gives every row a preview swatch and an accent-tinted indicator', async () => {
    const wrapper = mountButton()
    await wrapper.find('button.share-theme-btn').trigger('click')
    await nextTick()

    for (const row of wrapper.findAll('.theme-item')) {
      expect(row.find('.theme-swatch').exists()).toBe(true)
      expect(row.find('.theme-item-base-icon').exists()).toBe(true)
      // The row surface stays neutral; the preview travels in custom properties.
      expect(row.attributes('style')).toContain('--tterm-preview-bg')
    }
    wrapper.unmount()
  })

  it('does NOT apply the theme on click — only closes the menu', async () => {
    const wrapper = mountButton()
    await wrapper.find('button.share-theme-btn').trigger('click')
    await nextTick()

    await wrapper.findAll('.theme-item')[1].trigger('click')
    await nextTick()

    // Applying here would start the wipe while the menu is still fading out.
    expect(setLocalConfigFn).not.toHaveBeenCalled()
    expect(applyThemeWithRevealFn).not.toHaveBeenCalled()
    expect(wrapper.find('.popup-stub').exists()).toBe(false)
    wrapper.unmount()
  })

  it('applies the chosen value from after-leave, through setLocalConfig', async () => {
    const wrapper = mountButton()
    await wrapper.find('button.share-theme-btn').trigger('click')
    await nextTick()
    await wrapper.findAll('.theme-item')[1].trigger('click')
    await nextTick()

    // The real PopupMenu emits this once its leave transition ends.
    wrapper.findComponent({ name: 'PopupMenu' }).vm.$emit('after-leave')
    await nextTick()

    expect(applyThemeWithRevealFn).toHaveBeenCalledTimes(1)
    expect(setLocalConfigFn).toHaveBeenCalledWith('theme', THEME_IDS[0])
    wrapper.unmount()
  })

  it('wipes from the button itself, falling back to the header origin', async () => {
    const wrapper = mountButton()
    await wrapper.find('button.share-theme-btn').trigger('click')
    await nextTick()
    await wrapper.findAll('.theme-item')[1].trigger('click')
    await nextTick()
    wrapper.findComponent({ name: 'PopupMenu' }).vm.$emit('after-leave')
    await nextTick()

    expect(originFromElementFn).toHaveBeenCalledTimes(1)
    const opts = applyThemeWithRevealFn.mock.calls[0][1] as { origin: unknown }
    expect(opts.origin).toEqual({ x: 700, y: 22 })
    // The header fallback is only consulted when the button has no box.
    expect(headerThemeOriginFn).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('ignores a stray after-leave with nothing pending', async () => {
    const wrapper = mountButton()
    await wrapper.find('button.share-theme-btn').trigger('click')
    await nextTick()

    // Opening then closing without choosing must not re-apply the old theme.
    wrapper.findComponent({ name: 'PopupMenu' }).vm.$emit('after-leave')
    await nextTick()
    expect(applyThemeWithRevealFn).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
