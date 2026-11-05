import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import LiveChatPanel from '../LiveChatPanel.vue'

/**
 * The live chat is one row per message: avatar, then name/time, then the
 * bubble. The avatar is what makes a sender recognisable without re-reading
 * the name on every line, and it is the half of the row that the compact
 * redesign added — so the tests pin its presence per message and the mirrored
 * self row, not the pixel styling (jsdom applies no stylesheets).
 */
function mountPanel(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(LiveChatPanel, {
    props: {
      messages: [
        { id: 'm1', from: 'v1', name: '小明', text: '大家听得到吗？', at: 1_700_000_001_000, self: false },
        { id: 'm2', from: 'me', name: '我', text: '听得到。', at: 1_700_000_002_000, self: true },
      ],
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs: {
        SpeakerAvatar: { props: ['name'], template: '<span class="sa" :data-name="name" />' },
      },
    },
  })
}

describe('LiveChatPanel', () => {
  it('gives every message its own avatar, in message order', () => {
    const w = mountPanel()
    const avatars = w.findAll('.sa')
    expect(avatars).toHaveLength(2)
    expect(avatars[0].attributes('data-name')).toBe('小明')
    expect(avatars[1].attributes('data-name')).toBe('我')
  })

  it('puts the name/time and the bubble inside the body beside the avatar', () => {
    const w = mountPanel()
    const body = w.findAll('.chat-row__body')
    expect(body).toHaveLength(2)
    expect(body[0].find('.chat-row__meta').text()).toContain('小明')
    expect(body[0].find('.chat-row__text').text()).toBe('大家听得到吗？')
  })

  it('marks the self row for mirroring', () => {
    const w = mountPanel()
    const rows = w.findAll('.chat-row')
    expect(rows[0].classes()).not.toContain('chat-row--self')
    expect(rows[1].classes()).toContain('chat-row--self')
  })

  it('sends the trimmed draft and clears the field', async () => {
    const w = mountPanel()
    await w.find('[data-role="chat-input"]').setValue('  hello  ')
    await w.find('form').trigger('submit')
    expect(w.emitted('send')?.[0]).toEqual(['hello'])
    expect((w.find('[data-role="chat-input"]').element as HTMLInputElement).value).toBe('')
  })

  it('does not send whitespace', async () => {
    const w = mountPanel()
    await w.find('[data-role="chat-input"]').setValue('   ')
    await w.find('form').trigger('submit')
    expect(w.emitted('send')).toBeUndefined()
  })

  it('disables the composer when the panel is read-only', () => {
    const w = mountPanel({ disabled: true })
    expect(w.find('[data-role="chat-input"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-action="chat-send"]').attributes('disabled')).toBeDefined()
  })

  it('keeps the empty state when there is nothing to show', () => {
    const w = mountPanel({ messages: [] })
    // The shared empty block (MeetingEmpty), not a bare muted line: a lone
    // sentence in the top corner reads as a message that failed to render.
    const empty = w.find('[data-role="chat-empty"]')
    expect(empty.exists()).toBe(true)
    expect(empty.classes()).toContain('meeting-empty')
    expect(w.findAll('.chat-row')).toHaveLength(0)
  })

  it('opens on the newest message instead of the first one ever sent', async () => {
    // The 聊天 tab is re-created when it is switched back to, and the side
    // panel is `v-show`: opening it used to land on the first message of the
    // meeting, with no new line able to bring the reader back down.
    const w = mountPanel()
    let scrollTop = 0
    const list = w.find('.live-chat__list').element
    Object.defineProperty(list, 'scrollHeight', { value: 900, configurable: true })
    Object.defineProperty(list, 'clientHeight', { value: 300, configurable: true })
    Object.defineProperty(list, 'scrollTop', {
      get: () => scrollTop,
      set: (v: number) => {
        scrollTop = v
      },
      configurable: true,
    })

    await w.vm.$nextTick()
    expect(scrollTop).toBe(900)
  })

  it('scrolls to the bottom only when the reader is already there', async () => {
    const w = mountPanel({ messages: [] })
    // Let the mount-time "open on the newest line" pass run against the real
    // (empty) jsdom box: the geometry below describes messages that arrive
    // afterwards.
    await w.vm.$nextTick()
    const list = w.find('.live-chat__list').element
    let scrollTop = 0
    Object.defineProperty(list, 'scrollHeight', { value: 1000, configurable: true })
    Object.defineProperty(list, 'clientHeight', { value: 400, configurable: true })
    Object.defineProperty(list, 'scrollTop', {
      get: () => scrollTop,
      set: (v: number) => {
        scrollTop = v
      },
      configurable: true,
    })

    // Far from the bottom: a new line must not yank the reader down.
    scrollTop = 100
    await w.setProps({ messages: [{ id: 'x', name: '小明', text: 'a', at: 1_700_000_001_000, self: false }] })
    await w.vm.$nextTick()
    expect(scrollTop).toBe(100)

    // At the bottom: follow the conversation.
    scrollTop = 600
    await w.setProps({
      messages: [
        { id: 'x', name: '小明', text: 'a', at: 1_700_000_001_000, self: false },
        { id: 'y', name: '小明', text: 'b', at: 1_700_000_002_000, self: false },
      ],
    })
    await w.vm.$nextTick()
    await w.vm.$nextTick()
    expect(scrollTop).toBe(1000)
  })
})
