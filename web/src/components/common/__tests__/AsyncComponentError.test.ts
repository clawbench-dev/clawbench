import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AsyncComponentError from '@/components/common/AsyncComponentError.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

function mountWith(locale: 'en' | 'zh', props = {}) {
  const i18n = createI18n({ legacy: false, locale, messages: { en, zh } })
  return mount(AsyncComponentError, { props, global: { plugins: [i18n] } })
}

describe('AsyncComponentError', () => {
  it('renders label as the message', () => {
    const wrapper = mountWith('zh', { label: '代码编辑器加载失败' })
    expect(wrapper.text()).toContain('代码编辑器加载失败')
  })

  it('falls back to a localized default message when no label is provided', () => {
    // The fallback used to be a hardcoded Chinese literal, so an English user
    // saw Chinese. Both locales must resolve a real sentence, not the raw key.
    const enWrapper = mountWith('en')
    expect(enWrapper.text()).toContain(en.common.componentLoadFailed)
    expect(enWrapper.text()).not.toContain('common.componentLoadFailed')

    const zhWrapper = mountWith('zh')
    expect(zhWrapper.text()).toContain(zh.common.componentLoadFailed)
  })

  it('localizes the retry button label', () => {
    expect(mountWith('en').get('button').text()).toBe(en.common.retry)
    expect(mountWith('zh').get('button').text()).toBe(zh.common.retry)
  })

  it('emits retry when the button is clicked', async () => {
    const wrapper = mountWith('zh')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })
})
