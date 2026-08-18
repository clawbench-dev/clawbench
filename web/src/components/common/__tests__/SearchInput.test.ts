import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SearchInput from '../SearchInput.vue'

const stubs = { Search: true, X: true }

describe('SearchInput', () => {
  it('renders input with placeholder', () => {
    const wrapper = mount(SearchInput, {
      props: { modelValue: '', placeholder: 'Search...' },
      global: { stubs },
    })
    expect(wrapper.find('input').exists()).toBe(true)
    expect(wrapper.find('input').attributes('placeholder')).toBe('Search...')
  })

  it('emits update:modelValue on input', async () => {
    const wrapper = mount(SearchInput, {
      props: { modelValue: '' },
      global: { stubs },
    })
    const input = wrapper.find('input')
    await input.setValue('hello')
    expect(wrapper.emitted('update:modelValue')).toBeTruthy()
    expect(wrapper.emitted('update:modelValue')![0]).toEqual(['hello'])
  })

  it('shows clear button when modelValue is not empty', () => {
    const wrapper = mount(SearchInput, {
      props: { modelValue: 'test' },
      global: { stubs },
    })
    expect(wrapper.find('.search-pill-clear').exists()).toBe(true)
  })

  it('hides clear button when modelValue is empty', () => {
    const wrapper = mount(SearchInput, {
      props: { modelValue: '' },
      global: { stubs },
    })
    expect(wrapper.find('.search-pill-clear').exists()).toBe(false)
  })

  it('emits enter on Enter key', async () => {
    const wrapper = mount(SearchInput, {
      props: { modelValue: 'test' },
      global: { stubs },
    })
    await wrapper.find('input').trigger('keydown.enter')
    expect(wrapper.emitted('enter')).toBeTruthy()
  })

  it('emits down on ArrowDown', async () => {
    const wrapper = mount(SearchInput, {
      props: { modelValue: '' },
      global: { stubs },
    })
    await wrapper.find('input').trigger('keydown', { key: 'ArrowDown' })
    expect(wrapper.emitted('down')).toBeTruthy()
  })

  it('emits up on ArrowUp', async () => {
    const wrapper = mount(SearchInput, {
      props: { modelValue: '' },
      global: { stubs },
    })
    await wrapper.find('input').trigger('keydown', { key: 'ArrowUp' })
    expect(wrapper.emitted('up')).toBeTruthy()
  })

  it('exposes focus method', () => {
    const wrapper = mount(SearchInput, {
      props: { modelValue: '' },
      global: { stubs },
    })
    expect(typeof wrapper.vm.focus).toBe('function')
  })

  it('adds focused class on focus', async () => {
    const wrapper = mount(SearchInput, {
      props: { modelValue: '' },
      global: { stubs },
    })
    await wrapper.find('input').trigger('focus')
    expect(wrapper.find('.search-pill').classes()).toContain('focused')
  })

  it('removes focused class on blur', async () => {
    const wrapper = mount(SearchInput, {
      props: { modelValue: '' },
      global: { stubs },
    })
    await wrapper.find('input').trigger('focus')
    await wrapper.find('input').trigger('blur')
    expect(wrapper.find('.search-pill').classes()).not.toContain('focused')
  })
})
