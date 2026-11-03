/**
 * The host's view of a member's screen uses the SAME layout as the member's own
 * share page.
 *
 * It used to get a floating 280×320 chat box while the member had a full-height
 * panel with 聊天 / 听记 tabs, so one conversation had two different shapes
 * depending on which end of the call you were on. jsdom has no layout engine, so
 * "same layout" is asserted structurally: the picture and the panel are siblings
 * inside the shared `.live-shell`, and the panel is the shared tab strip.
 */
import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import MemberScreenSurface from '../MemberScreenSurface.vue'

vi.mock('lucide-vue-next', async (importOriginal) => {
  const actual = await importOriginal<typeof import('lucide-vue-next')>()
  const stub = { render: () => null }
  return { ...actual, Maximize: stub, Minimize: stub, ScreenShareOff: stub, X: stub, MessageSquare: stub, FileText: stub }
})

const BLOCKS = [
  { id: 1, channel: 'mic', speaker: 'Me', startedAt: 1000, durationMs: 900, text: 'hello there' },
]

function mountSurface(over: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MemberScreenSurface, {
    props: {
      stream: null,
      sharer: 'Alice',
      unread: 0,
      chatOpen: true,
      messages: [{ id: 'm1', from: 'host', name: 'Host', text: 'hi', at: 1 }],
      noteBlocks: [],
      notesEnabled: false,
      variant: 'fullscreen',
      canSwitch: false,
      ...over,
    },
    global: { plugins: [i18n] },
  })
}

describe('MemberScreenSurface', () => {
  it('puts the picture and the panel in the shared live layout', () => {
    const w = mountSurface()
    // Same shell as the member's own page: picture column and panel as siblings.
    expect(w.find('.live-shell > .live-stage-col > .member-screen__video').exists()).toBe(true)
    expect(w.find('.live-shell > .live-side').exists()).toBe(true)
    // ...and the panel is the shared tab strip, not a private chat box.
    expect(w.find('[data-panel="side"] [data-tab="chat"]').exists()).toBe(true)
    expect(w.find('.member-screen__chat').exists()).toBe(false)
  })

  it('offers the 听记 tab only when there is a transcript to read', async () => {
    const withoutNotes = mountSurface()
    expect(withoutNotes.find('[data-tab="notes"]').exists()).toBe(false)

    const withNotes = mountSurface({ noteBlocks: BLOCKS, notesEnabled: true })
    expect(withNotes.find('[data-tab="notes"]').exists()).toBe(true)
    // The pane draws the shared rows, not a second transcript implementation.
    expect(withNotes.find('[data-panel="notes"] .transcript__row').exists()).toBe(true)
  })

  it('labels the 听记 button with a sentence, never a raw key', () => {
    // It shipped as the literal "liveShare.notes": that path resolves to the
    // notes SUB-NAMESPACE (an object), so vue-i18n rendered the key itself.
    const w = mountSurface({ noteBlocks: BLOCKS, notesEnabled: true })
    const label = w.find('[data-action="screen-notes"]').text()
    expect(label).toBe(enMessages.share.live.notes)
    expect(label).not.toContain('liveShare')
  })

  it('asks the caller to open the panel when 听记 is pressed while it is closed', async () => {
    const w = mountSurface({ chatOpen: false, noteBlocks: BLOCKS, notesEnabled: true })
    await w.find('[data-action="screen-notes"]').trigger('click')
    await flushPromises()

    // The panel's open/closed state belongs to the caller (it also drives the
    // unread count), so the button reports the intent and selects the pane.
    expect(w.emitted('open-panel')).toHaveLength(1)
    expect(w.find('[data-tab="notes"]').classes()).toContain('live-side-tab--on')
  })

  it('switches panes without asking again when the panel is already open', async () => {
    const w = mountSurface({ chatOpen: true, noteBlocks: BLOCKS, notesEnabled: true })
    await w.find('[data-action="screen-notes"]').trigger('click')
    await flushPromises()

    expect(w.emitted('open-panel')).toBeUndefined()
    expect(w.find('[data-tab="notes"]').classes()).toContain('live-side-tab--on')
    expect(w.find('[data-panel="notes"]').attributes('style') ?? '').not.toContain('display: none')
    expect(w.find('[data-panel="chat"]').attributes('style') ?? '').toContain('display: none')
  })

  it('closes the panel from the shared close button', async () => {
    const w = mountSurface()
    await w.find('[data-action="side-close"]').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })
})
