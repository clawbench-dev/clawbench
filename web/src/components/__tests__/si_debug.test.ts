import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import SearchInput from '@/components/common/SearchInput.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: { search: { defaultPlaceholder: 'Search...', clear: 'Clear' } } },
})

describe('debug', () => {
  it('check focused ref', async () => {
    const wrapper = mount(SearchInput, {
      props: { modelValue: '' },
      global: { plugins: [i18n] },
    })
    const vm = wrapper.vm as any
    console.log('focused before:', vm.focused)
    console.log('typeof focused:', typeof vm.focused)
    
    // Try setting
    vm.focused = true
    await nextTick()
    console.log('focused after set true:', vm.focused)
    console.log('classes:', wrapper.find('.search-pill').classes())
    
    // Check if focused is a Ref
    const focusedVal = vm.focused
    console.log('focused value:', focusedVal, typeof focusedVal)
  })
})
