import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import RollingText from '@/components/common/RollingText.vue'

afterEach(() => {
  vi.useRealTimers()
})

function lineText(wrapper: ReturnType<typeof mount>): string {
  const lines = wrapper.findAll('.rolling-text-line')
  return lines.length ? lines[lines.length - 1].text() : ''
}

describe('RollingText', () => {
  it('renders the initial text without waiting', () => {
    vi.useFakeTimers()
    const wrapper = mount(RollingText, { props: { text: 'Bash · npm test' } })
    expect(lineText(wrapper)).toBe('Bash · npm test')
  })

  it('holds the old line until the new one has been stable, then swaps', async () => {
    vi.useFakeTimers()
    const wrapper = mount(RollingText, { props: { text: 'Read' } })
    await wrapper.setProps({ text: 'Bash' })

    // Still throttled — swapping on every frame of a stream would be a blur.
    expect(lineText(wrapper)).toBe('Read')

    vi.advanceTimersByTime(180)
    await nextTick()
    expect(lineText(wrapper)).toBe('Bash')
  })

  it('collapses a burst of changes into the last one', async () => {
    vi.useFakeTimers()
    const wrapper = mount(RollingText, { props: { text: 'a' } })
    await wrapper.setProps({ text: 'b' })
    vi.advanceTimersByTime(100)
    await wrapper.setProps({ text: 'c' })
    vi.advanceTimersByTime(100)
    await wrapper.setProps({ text: 'd' })

    // None of the intermediate values were ever shown.
    expect(lineText(wrapper)).toBe('a')

    vi.advanceTimersByTime(180)
    await nextTick()
    expect(lineText(wrapper)).toBe('d')
  })

  it('does not schedule anything when the text is unchanged', async () => {
    vi.useFakeTimers()
    const wrapper = mount(RollingText, { props: { text: 'same' } })
    const before = vi.getTimerCount()
    await wrapper.setProps({ text: 'same' })
    expect(vi.getTimerCount()).toBe(before)
  })

  it('keeps the full text in the box for the sizer (no duplicate in textContent)', () => {
    const wrapper = mount(RollingText, { props: { text: 'Grep · toolCall' } })
    // The sizer is a ::before, so textContent carries the line exactly once —
    // copy and tests see one copy.
    expect(wrapper.text()).toBe('Grep · toolCall')
    expect(wrapper.attributes('data-text')).toBe('Grep · toolCall')
  })
})
