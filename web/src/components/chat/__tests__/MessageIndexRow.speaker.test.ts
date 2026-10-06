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
    template: '<span class="agent-icon-stub" :data-backend="backend" :data-name="name" :data-avatar="avatar" />',
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
})
