import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import RealAgentNameTag from '../RealAgentNameTag.vue'

function makeI18n() {
  return createI18n({
    legacy: false,
    locale: 'en',
    messages: { en: { group: { realAgent: 'Agent' } } },
  })
}

function mountTag(props: Record<string, unknown>) {
  return mount(RealAgentNameTag, { props, global: { plugins: [makeI18n()] } })
}

describe('RealAgentNameTag', () => {
  it('renders the real agent name when it differs from the display name', () => {
    const w = mountTag({ agentName: 'Claude', displayName: 'Alice' })
    expect(w.find('.real-agent-name-tag').exists()).toBe(true)
    expect(w.text()).toBe('Claude')
    expect(w.find('.real-agent-name-tag').attributes('title')).toBe('Agent')
  })

  it('renders nothing when the nickname IS the real agent name', () => {
    const w = mountTag({ agentName: 'Claude', displayName: 'Claude' })
    expect(w.find('.real-agent-name-tag').exists()).toBe(false)
    expect(w.text()).toBe('')
  })

  it('renders nothing when there is no agent name', () => {
    expect(mountTag({ displayName: 'Alice' }).find('.real-agent-name-tag').exists()).toBe(false)
    expect(mountTag({ agentName: '', displayName: 'Alice' }).find('.real-agent-name-tag').exists()).toBe(false)
  })

  it('merges a caller-supplied class onto the root (consumer hooks)', () => {
    const w = mountTag({ agentName: 'Claude', displayName: 'Alice', class: 'msg-speaker-real' })
    const tag = w.find('.real-agent-name-tag')
    expect(tag.classes()).toContain('real-agent-name-tag')
    expect(tag.classes()).toContain('msg-speaker-real')
  })
})
