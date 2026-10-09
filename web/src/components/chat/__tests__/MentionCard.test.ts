import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import MentionCard from '../MentionCard.vue'
import type { StagedMention } from '@/composables/useChatContext.ts'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

function mention(over: Partial<StagedMention> = {}): StagedMention {
  return { id: 'mt-1', memberId: 'm-a', name: 'Alice', agentId: 'a-1', backend: 'claude', note: '', ...over }
}

function mountCard(props: Record<string, unknown> = {}) {
  return mount(MentionCard, { props: { mention: mention(), ...props } })
}

describe('MentionCard', () => {
  // The card must read as a PERSON, not as a payload: a round avatar, a
  // proportional-font name, and a neutral surface — three orthogonal signals
  // that survive every theme (colour alone is the weakest signal). The classes
  // below are what carry them, so they are asserted by name.
  it('carries the member-card classes (not the attachment-card ones)', () => {
    const wrapper = mountCard()
    expect(wrapper.classes()).toContain('mention-card')
    // Deliberately NOT a .chat-file-attachment: that family is the file/quote
    // card shape (square icon, mono label, accent tint).
    expect(wrapper.classes()).not.toContain('chat-file-attachment')
  })

  it('shows the member name in a proportional (non-mono) label', () => {
    const wrapper = mountCard()
    const label = wrapper.find('.mention-card-name')
    expect(label.text()).toBe('Alice')
    expect(label.classes()).not.toContain('attachment-filename')
  })

  it('renders the agent avatar', () => {
    const wrapper = mountCard()
    expect(wrapper.find('.mention-card-avatar').exists()).toBe(true)
  })

  describe('private-note state', () => {
    it('marks the card as private when a note exists', () => {
      expect(mountCard({ mention: mention({ note: '机密' }) }).classes()).toContain('is-private')
    })

    it('does not mark it private without a note', () => {
      expect(mountCard({ mention: mention({ note: '' }) }).classes()).not.toContain('is-private')
    })

    it('shows the lock ONLY when a note is set (presence means "there is a note")', () => {
      // The lock is the signal that a private note exists. An empty card shows
      // no lock at all — its presence is the affordance, so it must be absent
      // without a note.
      expect(mountCard({ mention: mention({ note: '' }) }).find('.mention-card-lock').exists()).toBe(false)
      expect(mountCard({ mention: mention({ note: 'x' }) }).find('.mention-card-lock').exists()).toBe(true)
    })
  })

  it('does not print the private note on the card', () => {
    // Same rule as the quote card: the annotation is not inlined into the chip
    // (the row is a single nowrap line). It is reachable via the tooltip.
    const wrapper = mountCard({ mention: mention({ note: '只有你能看到的秘密' }) })
    expect(wrapper.text()).not.toContain('只有你能看到的秘密')
  })

  it('uses the note as the tooltip when present', () => {
    expect(mountCard({ mention: mention({ note: '机密' }) }).attributes('title')).toBe('机密')
  })

  it('falls back to the name for the tooltip', () => {
    expect(mountCard({ mention: mention({ note: '' }) }).attributes('title')).toBe('Alice')
  })

  it('emits click with the mention when the card body is clicked', async () => {
    const m = mention()
    const wrapper = mountCard({ mention: m })
    await wrapper.trigger('click')
    expect(wrapper.emitted('click')![0]).toEqual([m])
  })

  it('renders no remove button unless removable', () => {
    expect(mountCard().find('.mention-card-close').exists()).toBe(false)
    expect(mountCard({ removable: true }).find('.mention-card-close').exists()).toBe(true)
  })

  it('emits remove without also emitting click', async () => {
    const m = mention()
    const wrapper = mountCard({ mention: m, removable: true })
    await wrapper.find('.mention-card-close').trigger('click')
    expect(wrapper.emitted('remove')![0]).toEqual([m])
    expect(wrapper.emitted('click')).toBeFalsy()
  })
})
