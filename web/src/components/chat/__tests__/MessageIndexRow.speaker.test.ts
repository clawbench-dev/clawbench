import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import MessageIndexRow from '../MessageIndexRow.vue'
import { makeSpeakerResolver } from '@/utils/speakerIdentity'

// AgentIcon is heavy (SVG processing); stub it but expose the props under test.
vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: {
    name: 'AgentIcon',
    props: ['backend', 'name', 'avatar', 'size'],
    template: '<span class="agent-icon-stub" :data-backend="backend" :data-name="name" :data-avatar="avatar" :data-size="size" />',
  },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        btw: { anchorTitle: 'side question' },
        messageList: {
          conversationIndexRoleAssistant: 'assistant',
          conversationIndexRoleUser: 'user',
          conversationIndexRoleSystem: 'system',
          conversationIndexNoText: '(no text)',
          userMsgIndexAttachment: 'attachment',
        },
      },
    },
  },
})

function mountRow(msg: Record<string, unknown>, props: Record<string, unknown> = {}) {
  return mount(MessageIndexRow, {
    props: { msg, index: 1, ...props },
    global: { plugins: [i18n] },
  })
}

describe('MessageIndexRow — speaker icon resolution', () => {
  it('renders the generic Bot when no resolver is supplied', () => {
    const w = mountRow({ id: 1, role: 'assistant', content: 'hi' })
    expect(w.find('.agent-icon-stub').exists()).toBe(false)
    // The lucide Bot renders as an <svg>; the chip is present either way.
    expect(w.find('.msg-role-tag').exists()).toBe(true)
  })

  it('renders the resolved agent icon for a single-agent message (empty agentId)', () => {
    const resolve = makeSpeakerResolver({ name: 'Solo', backend: 'claude' }, null)
    const w = mountRow({ id: 1, role: 'assistant', content: 'hi', agentId: '' }, { resolveSpeaker: resolve })
    const icon = w.find('.agent-icon-stub')
    expect(icon.exists()).toBe(true)
    expect(icon.attributes('data-backend')).toBe('claude')
    expect(icon.attributes('data-name')).toBe('Solo')
  })

  it('sizes the agent icon to fill the chip (no gap around the glyph)', () => {
    // The chip is 20px with a 1px border → an 18px content box. `sm` (14px) left
    // a 2px gap per side that made the icon read smaller than its chip; `md`
    // (18px) fills it exactly.
    const resolve = makeSpeakerResolver({ name: 'Solo', backend: 'claude' }, null)
    const w = mountRow({ id: 1, role: 'assistant', content: 'hi', agentId: '' }, { resolveSpeaker: resolve })
    expect(w.find('.agent-icon-stub').attributes('data-size')).toBe('md')
  })

  it('resolves a GROUP member id to that member, not the session agent', () => {
    const resolve = makeSpeakerResolver(
      { name: 'Host', backend: 'codebuddy' },
      { 'member-2': { name: 'Alice', backend: 'claude', avatar: '<svg/>' } },
    )
    const w = mountRow(
      { id: 2, role: 'assistant', content: 'hi', agentId: 'member-2' },
      { resolveSpeaker: resolve },
    )
    const icon = w.find('.agent-icon-stub')
    expect(icon.attributes('data-backend')).toBe('claude')
    expect(icon.attributes('data-name')).toBe('Alice')
    expect(icon.attributes('data-avatar')).toBe('<svg/>')
  })

  it('falls back to Bot for an unknown member id (never misattributes to the host)', () => {
    const resolve = makeSpeakerResolver({ name: 'Host', backend: 'codebuddy' }, {})
    const w = mountRow(
      { id: 3, role: 'assistant', content: 'hi', agentId: 'ghost-member' },
      { resolveSpeaker: resolve },
    )
    expect(w.find('.agent-icon-stub').exists()).toBe(false)
  })

  it('never resolves a speaker for a USER row', () => {
    const resolve = makeSpeakerResolver({ name: 'Solo', backend: 'claude' }, null)
    const w = mountRow({ id: 4, role: 'user', content: 'me' }, { resolveSpeaker: resolve })
    expect(w.find('.agent-icon-stub').exists()).toBe(false)
  })

  it('marks a SYSTEM row with its own role chip, not a user chip', () => {
    // A system row is a group membership announcement; it must never borrow the
    // User chip (which is what the old `role === 'assistant' ? ... : 'role-user'`
    // fallback did).
    const w = mountRow({ id: 5, role: 'system', content: 'Alice joined' })
    const tag = w.find('.msg-role-tag')
    expect(tag.classes()).toContain('role-system')
    expect(tag.classes()).not.toContain('role-user')
    expect(tag.attributes('aria-label')).toBe('system')
    // The unified Megaphone glyph, not the User glyph. Real lucide renders
    // `class="lucide lucide-megaphone"` (this test does not mock the library).
    expect(tag.find('.lucide-megaphone').exists()).toBe(true)
    expect(tag.find('.lucide-user').exists()).toBe(false)
    // The row itself carries a system modifier for the muted styling.
    expect(w.find('.msg-item').classes()).toContain('msg-item--system')
  })

  it('never resolves a speaker for a SYSTEM row', () => {
    const resolve = makeSpeakerResolver({ name: 'Solo', backend: 'claude' }, null)
    const w = mountRow({ id: 6, role: 'system', content: 'joined' }, { resolveSpeaker: resolve })
    expect(w.find('.agent-icon-stub').exists()).toBe(false)
  })
})
