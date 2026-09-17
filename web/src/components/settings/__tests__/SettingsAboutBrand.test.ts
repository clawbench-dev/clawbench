import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SettingsAboutBrand from '@/components/settings/SettingsAboutBrand.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          aboutBrandSlogan: '从掌心到桌面',
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
    // The slogan also appears on the login page; keeping it in the locale files
    // is what makes it translatable here.
    expect(mountBrand().find('.about-brand__slogan').text()).toBe('从掌心到桌面')
  })
})
