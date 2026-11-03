import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const generate = vi.hoisted(() => vi.fn(async (seed: string, style: string) => `<svg data-seed="${seed}" data-style="${style}"/>`))
vi.mock('@/utils/lazyAvatar', () => ({
  generateAvatarSvgWithStyle: generate,
}))

import SpeakerAvatar from '../SpeakerAvatar.vue'
import { _resetSpeakerAvatars } from '@/utils/speakerAvatar'

beforeEach(() => {
  generate.mockClear()
  _resetSpeakerAvatars()
})

describe('SpeakerAvatar', () => {
  it('holds the box with the initial until the icon arrives, then swaps it in', async () => {
    const w = mount(SpeakerAvatar, { props: { name: '小明' } })
    // The first frame must already be the final size, or every transcript row
    // shifts when 40 avatars land a moment later.
    expect(w.find('.speaker-avatar').exists()).toBe(true)
    expect(w.find('.speaker-avatar__initial').text()).toBe('小')
    expect(w.find('img').exists()).toBe(false)

    await flushPromises()
    expect(w.find('img').attributes('src')).toMatch(/^data:image\/svg\+xml/)
    expect(w.find('.speaker-avatar__initial').exists()).toBe(false)
  })

  it('is decorative: the name is printed next to it, so it is not announced twice', async () => {
    const w = mount(SpeakerAvatar, { props: { name: '小明' } })
    await flushPromises()
    const root = w.find('.speaker-avatar')
    expect(root.attributes('aria-hidden')).toBe('true')
    expect(root.attributes('role')).toBeUndefined()
    expect(w.find('img').attributes('alt')).toBe('')
  })

  it('generates one icon per nickname even when the same person renders twice', async () => {
    mount(SpeakerAvatar, { props: { name: '小明' } })
    mount(SpeakerAvatar, { props: { name: '小明' } })
    await flushPromises()
    expect(generate).toHaveBeenCalledTimes(1)
  })

  it('follows a name change', async () => {
    const w = mount(SpeakerAvatar, { props: { name: '小明' } })
    await flushPromises()
    const first = w.find('img').attributes('src')

    await w.setProps({ name: '小李' })
    await flushPromises()
    expect(w.find('img').attributes('src')).not.toBe(first)
    expect(generate).toHaveBeenCalledWith('小李', expect.any(String))
  })

  it('falls back to "?" for an empty name without calling the generator', async () => {
    const w = mount(SpeakerAvatar, { props: { name: '   ' } })
    await flushPromises()
    expect(w.find('.speaker-avatar__initial').text()).toBe('?')
    expect(generate).not.toHaveBeenCalled()
  })

  it('keeps the initial when the generator fails', async () => {
    generate.mockRejectedValueOnce(new Error('chunk 404'))
    const w = mount(SpeakerAvatar, { props: { name: '小明' } })
    await flushPromises()
    expect(w.find('img').exists()).toBe(false)
    expect(w.find('.speaker-avatar__initial').text()).toBe('小')
  })
})
