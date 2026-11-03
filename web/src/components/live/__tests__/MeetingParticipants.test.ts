import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import MeetingParticipants from '../MeetingParticipants.vue'
import type { LiveViewerInfo } from '@/composables/useLiveShare'

/**
 * The participant list is the host's answer to "who is in this meeting, and
 * what can I do about them". These tests pin the three things the old bare
 * viewer list got wrong: there was no host row, no avatar, and the only action
 * was a mute button that existed solely while somebody happened to be talking.
 */

function viewer(over: Partial<LiveViewerInfo> = {}): LiveViewerInfo {
  return { id: 'v1', name: '小明', talk: 'muted', muted: false, ...over }
}

function mountList(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingParticipants, {
    props: {
      hostName: 'Alex',
      viewers: [],
      presence: {},
      talkEnabled: true,
      sharingId: '',
      controlRequests: new Set<string>(),
      controlledBy: '',
      controlSupported: true,
      ...props,
    },
    global: {
      plugins: [i18n],
      // The real avatar pulls a DiceBear chunk; the icon is decoration here and
      // its own generator has its own tests.
      stubs: { SpeakerAvatar: { props: ['name'], template: '<span class="sa" :data-name="name" />' } },
    },
  })
}

describe('MeetingParticipants', () => {
  it('pins the host first, with their own avatar and role', () => {
    const w = mountList({ viewers: [viewer()] })
    const rows = w.findAll('.mp-row')
    expect(rows).toHaveLength(2)

    const host = rows[0]
    expect(host.attributes('data-role')).toBe('participant-host')
    expect(host.find('.sa').attributes('data-name')).toBe('Alex')
    expect(host.text()).toContain('Alex')
    expect(host.text()).toContain('Host')
  })

  it('gives every viewer an avatar seeded by their nickname', () => {
    const w = mountList({
      viewers: [viewer({ id: 'v1', name: '小明' }), viewer({ id: 'v2', name: '小红' })],
    })
    const names = w.findAll('.mp-row:not(.mp-row--host) .sa').map((n) => n.attributes('data-name'))
    expect(names).toEqual(['小明', '小红'])
  })

  it('suffixes duplicate nicknames instead of showing the same row twice', () => {
    const w = mountList({
      viewers: [viewer({ id: 'v1', name: 'Alex' }), viewer({ id: 'v2', name: 'Alex' })],
    })
    const names = w.findAll('.mp-name').map((n) => n.text())
    expect(names).toEqual(['Alex', 'Alex', 'Alex #2'])
  })

  it('mutes a viewer who is allowed to speak, and offers to lift it once muted', async () => {
    const w = mountList({ viewers: [viewer({ id: 'v1' })] })

    await w.find('[data-action="participant-mute"]').trigger('click')
    expect(w.emitted('mute')).toEqual([['v1']])

    await w.setProps({ viewers: [viewer({ id: 'v1', muted: true })] })
    expect(w.find('[data-role="chip-muted"]').exists()).toBe(true)
    await w.find('[data-action="participant-unmute"]').trigger('click')
    expect(w.emitted('unmute')).toEqual([['v1']])
  })

  it('does not offer a mute button while the room has speech switched off', () => {
    // With the room switch off the viewer cannot speak anyway, so a mute
    // button would be a control that changes nothing.
    const w = mountList({ talkEnabled: false, viewers: [viewer()] })
    expect(w.find('[data-action="participant-mute"]').exists()).toBe(false)
    expect(w.find('[data-action="participant-unmute"]').exists()).toBe(false)
  })

  it('removes a viewer', async () => {
    const w = mountList({ viewers: [viewer({ id: 'v7' })] })
    await w.find('[data-action="participant-kick"]').trigger('click')
    expect(w.emitted('kick')).toEqual([['v7']])
  })

  it('shows who is sharing a screen and lets the host stop it', async () => {
    const w = mountList({ viewers: [viewer({ id: 'v1' })], sharingId: 'v1' })
    expect(w.find('[data-role="chip-sharing"]').exists()).toBe(true)

    await w.find('[data-action="participant-stop-screen"]').trigger('click')
    expect(w.emitted('stop-screen')).toEqual([['v1']])
  })

  it('marks a viewer whose page is hidden as away', () => {
    const w = mountList({ viewers: [viewer({ id: 'v1' })], presence: { v1: false } })
    expect(w.find('[data-role="chip-away"]').exists()).toBe(true)
    expect(w.find('.mp-row[data-viewer-id="v1"]').classes()).toContain('mp-row--away')
  })

  it('answers a control request with text buttons, not icons', async () => {
    const w = mountList({
      viewers: [viewer({ id: 'v1' })],
      controlRequests: new Set(['v1']),
    })
    expect(w.text()).toContain('Asked to control')

    await w.find('[data-action="control-allow"]').trigger('click')
    expect(w.emitted('allow-control')).toEqual([['v1']])
    await w.find('[data-action="control-deny"]').trigger('click')
    expect(w.emitted('deny-control')).toEqual([['v1']])
  })

  it('says the host cannot grant control instead of offering a button that fails', () => {
    const w = mountList({
      viewers: [viewer({ id: 'v1' })],
      controlRequests: new Set(['v1']),
      controlSupported: false,
    })
    expect(w.find('[data-role="control-unsupported"]').exists()).toBe(true)
    expect(w.find('[data-action="control-allow"]').exists()).toBe(false)
  })

  it('shows an empty state, not just a count, when nobody has joined', () => {
    const w = mountList()
    // The host is always there, so "no viewers" must be stated in words.
    expect(w.find('[data-role="participants-empty"]').exists()).toBe(true)
    expect(w.findAll('.mp-row')).toHaveLength(1)
  })
})
