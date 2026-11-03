import { describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { flushPromises, mount } from '@vue/test-utils'
import { toRaw } from 'vue'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import MemberScreenCard from '../MemberScreenCard.vue'

/**
 * The panel's handle on a member's picture: whose it is, the preview, and the
 * three things a host can do about it.
 */

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

function mountCard(props: Record<string, unknown>) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MemberScreenCard, {
    props: props as never,
    global: { plugins: [i18n] },
  })
}

describe('MemberScreenCard', () => {
  it('names the member whose picture is on air', () => {
    const wrapper = mountCard({ name: 'Alex', stream: null })
    // No caption line under the card — the pill carries the visible name — so
    // the sentence survives as the group's accessible name.
    expect(wrapper.attributes('aria-label')).toBe("Alex's shared screen")
    expect(wrapper.find('.capture-source__name').exists()).toBe(false)
  })

  it('tags the picture with the sharer, avatar first', () => {
    const wrapper = mountCard({ name: 'Alex', stream: null })
    const who = wrapper.find('[data-role="member-screen-who"]')
    expect(who.exists()).toBe(true)
    expect(who.find('.speaker-avatar').exists()).toBe(true)
    expect(who.find('.member-screen-card__who-name').text()).toBe('Alex')
    // Truncation must not cost the full name: it stays in the tooltip.
    expect(who.attributes('title')).toBe('Alex')
  })

  it('truncates a long nickname instead of widening the tag', () => {
    // The pill is on the picture: growing with the name would cover the frame.
    const src = readFileSync(join(__dirname, '..', 'MemberScreenCard.vue'), 'utf8')
    const pill = src.slice(src.indexOf('.member-screen-card__who {'))
    expect(pill).toMatch(/max-width:\s*calc\(100% - 8 \* var\(--dp\)\)/)
    const name = src.slice(src.indexOf('.member-screen-card__who-name {'))
    expect(name).toMatch(/text-overflow:\s*ellipsis/)
    expect(name).toMatch(/min-width:\s*0/)
  })

  it('plays the stream it is given, and the next one', async () => {
    const first = { id: 'stream-1' } as unknown as MediaStream
    const second = { id: 'stream-2' } as unknown as MediaStream
    const wrapper = mountCard({ name: 'Alex', stream: first })
    await flushPromises()

    const video = wrapper.find('video').element as HTMLVideoElement
    // test-utils hands props through a reactive object, so compare the
    // underlying stream rather than the proxy it is read back as.
    expect(toRaw(video.srcObject as object)).toBe(first)

    await wrapper.setProps({ stream: second })
    await flushPromises()
    expect(toRaw(video.srcObject as object)).toBe(second)
  })

  it('emits the three actions from the card itself', async () => {
    const wrapper = mountCard({ name: 'Alex', stream: null, windowAvailable: true })

    await wrapper.find('[data-action="show-screen-fullscreen"]').trigger('click')
    await wrapper.find('[data-action="open-screen-window"]').trigger('click')
    await wrapper.find('[data-action="stop-screen-share"]').trigger('click')

    expect(wrapper.emitted('fullscreen')).toHaveLength(1)
    expect(wrapper.emitted('window')).toHaveLength(1)
    expect(wrapper.emitted('stop')).toHaveLength(1)
  })

  it('offers no window where none can be opened', () => {
    const wrapper = mountCard({ name: 'Alex', stream: null, windowAvailable: false })
    expect(wrapper.find('[data-action="open-screen-window"]').exists()).toBe(false)
    // Stopping and going back to the full screen are always possible.
    expect(wrapper.find('[data-action="show-screen-fullscreen"]').exists()).toBe(true)
    expect(wrapper.find('[data-action="stop-screen-share"]').exists()).toBe(true)
  })

  it('keeps only stop while the picture is already in its own window', () => {
    const wrapper = mountCard({ name: 'Alex', stream: null, windowAvailable: true, windowOpen: true })
    // Both view actions would be moves the host cannot make from here: the
    // picture is already detached, and the hint under the card says so.
    expect(wrapper.find('[data-action="show-screen-fullscreen"]').exists()).toBe(false)
    expect(wrapper.find('[data-action="open-screen-window"]').exists()).toBe(false)
    expect(wrapper.find('[data-action="stop-screen-share"]').exists()).toBe(true)
  })
})
