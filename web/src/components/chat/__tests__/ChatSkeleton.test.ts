import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ChatSkeleton from '@/components/chat/ChatSkeleton.vue'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The session-switch placeholder. It replaces the lone spinner that used to sit
 * in the empty message area, so these assertions pin the properties that make it
 * read as "a conversation is loading" rather than a generic grey box:
 * alternating assistant/user rows, an avatar disc on assistant rows only, and
 * multi-line bubbles.
 */
describe('ChatSkeleton', () => {
  it('renders alternating assistant and user rows', () => {
    const wrapper = mount(ChatSkeleton)
    const rows = wrapper.findAll('.chat-skeleton-row')

    expect(rows.length).toBeGreaterThan(1)
    const roles = rows.map((r) => (r.classes().includes('user') ? 'user' : 'assistant'))
    expect(roles).toContain('assistant')
    expect(roles).toContain('user')
  })

  it('puts an avatar disc on assistant rows only', () => {
    const wrapper = mount(ChatSkeleton)
    const rows = wrapper.findAll('.chat-skeleton-row')

    for (const row of rows) {
      const isUser = row.classes().includes('user')
      expect(row.find('.chat-skeleton-avatar').exists()).toBe(!isUser)
    }
    // The disc must actually be circular — a square avatar would read as a bug.
    expect(wrapper.find('.chat-skeleton-avatar').classes()).toContain('skeleton-circle')
  })

  it('renders multiple text lines per assistant bubble', () => {
    const wrapper = mount(ChatSkeleton)
    const firstAssistant = wrapper.find('.chat-skeleton-row.assistant')
    expect(firstAssistant.findAll('.chat-skeleton-line').length).toBeGreaterThan(1)
  })

  it('is hidden from assistive tech (the panel announces the load)', () => {
    const wrapper = mount(ChatSkeleton)
    expect(wrapper.find('.chat-skeleton').attributes('aria-hidden')).toBe('true')
  })

  it('uses the shared global skeleton primitive, not a local fill', () => {
    // The fill + shimmer live in web/css/components.css so the header skeleton
    // (App.vue) and this component share one implementation. A local background
    // here would silently diverge from the header.
    const src = readWebFile('src/components/chat/ChatSkeleton.vue')
    expect(src).toContain('skeleton-block')
    expect(src, 'the fill must come from the global primitive').not.toMatch(
      /\.chat-skeleton-line\s*\{[^}]*background/,
    )
  })
})
