import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SettingsAboutBrand from '@/components/settings/SettingsAboutBrand.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          aboutBrandSlogan: '多端一体的 AI 工作台',
        },
      },
    },
  },
})

function mountBrand() {
  return mount(SettingsAboutBrand, { global: { plugins: [i18n] } })
}

describe('SettingsAboutBrand', () => {
  it('shows the project logo', () => {
    const wrapper = mountBrand()
    const img = wrapper.find('img.about-brand__logo')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toBe('/logo-128.png')
    expect(img.attributes('alt')).toBe('ClawBench')
  })

  it('shows the product name', () => {
    expect(mountBrand().find('.about-brand__name').text()).toBe('ClawBench')
  })

  it('renders the slogan from i18n rather than a hardcoded string', () => {
    // The About page slogan is its own key (settings.items.aboutBrandSlogan),
    // separate from the login page's login.slogan — the two pages deliberately
    // carry different copy. Keeping it in the locale files is what makes it
    // translatable here.
    expect(mountBrand().find('.about-brand__slogan').text()).toBe('多端一体的 AI 工作台')
  })

  // The render test above mounts with its OWN i18n fixture, so it passes no
  // matter what the real locale files say — reverting the copy in
  // locales/{en,zh}.ts would leave it green. These pin the actual shipped
  // values, the same way forgeKeys.test.ts guards the forge namespace.
  it('ships the About slogan in both real locales', () => {
    expect(zh.settings.items.aboutBrandSlogan).toBe('多端一体的 AI 工作台')
    expect(en.settings.items.aboutBrandSlogan).toBe('An AI Workbench, United Across Devices')
  })

  it('keeps the About slogan distinct from the login slogan', () => {
    // The login page keeps "从掌心到桌面" / "From Palm to Desktop" while the
    // About page carries the workbench line. Collapsing them back to one key
    // (or one value) is exactly the regression this guards.
    for (const loc of [zh, en]) {
      expect(loc.settings.items.aboutBrandSlogan).not.toBe(loc.login.slogan)
    }
  })
})
