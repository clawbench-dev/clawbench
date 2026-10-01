import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import FileChangeNav from '../FileChangeNav.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string, p?: any) => (p ? `${k}:${JSON.stringify(p)}` : k) }),
}))

describe('FileChangeNav', () => {
  it('renders nothing when there are no changes', () => {
    const w = mount(FileChangeNav, { props: { count: 0, index: 0 } })
    expect(w.find('.file-change-nav').exists()).toBe(false)
  })

  it('shows the change count and position', () => {
    const w = mount(FileChangeNav, { props: { count: 3, index: 1 } })
    expect(w.find('.file-change-nav').exists()).toBe(true)
    expect(w.text()).toContain('file.changeNav.count')
    expect(w.text()).toContain('2/3')
  })

  it('emits prev / next / clear', async () => {
    const w = mount(FileChangeNav, { props: { count: 3, index: 1 } })
    await w.find('.fcn-btn-prev').trigger('click')
    await w.find('.fcn-btn-next').trigger('click')
    await w.find('.fcn-btn-clear').trigger('click')
    expect(w.emitted('prev')).toHaveLength(1)
    expect(w.emitted('next')).toHaveLength(1)
    expect(w.emitted('clear')).toHaveLength(1)
  })

  it('disables prev at the first change and next at the last', () => {
    const first = mount(FileChangeNav, { props: { count: 3, index: 0 } })
    expect((first.find('.fcn-btn-prev').element as HTMLButtonElement).disabled).toBe(true)
    const last = mount(FileChangeNav, { props: { count: 3, index: 2 } })
    expect((last.find('.fcn-btn-next').element as HTMLButtonElement).disabled).toBe(true)
  })
})
