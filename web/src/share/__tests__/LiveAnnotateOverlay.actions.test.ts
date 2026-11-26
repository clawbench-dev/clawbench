/**
 * 留存画面 / 录制 live on the drawing surface too.
 *
 * The meeting footer holds both, but a full-screen share puts the surface OVER
 * it: while the annotations are on screen those buttons cannot be clicked, and
 * closing the surface erases the ink. So a frame captured from the footer could
 * never carry an annotation — the two actions have to exist where the host's
 * hands already are.
 */
import { beforeAll, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import LiveAnnotateOverlay from '../LiveAnnotateOverlay.vue'

// jsdom does not implement media playback, and the overlay's stream watcher
// calls `play()` as a promise; without this every mount logs an unhandled
// watcher error.
beforeAll(() => {
  Object.defineProperty(HTMLMediaElement.prototype, 'play', {
    configurable: true,
    value: () => Promise.resolve(),
  })
})

function mountOverlay(props: { recording?: boolean; canShot?: boolean; canRecord?: boolean } = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(LiveAnnotateOverlay, {
    props: { stream: null, ...props },
    global: { plugins: [i18n], stubs: { LiveInkLayer: true } },
  })
}

describe('annotate surface: capture actions', () => {
  it('offers 留存画面 and 录制 on the toolbar', () => {
    const wrapper = mountOverlay({ canShot: true, canRecord: true })
    expect(wrapper.find('[data-action="anno-shot"]').exists()).toBe(true)
    expect(wrapper.find('[data-action="anno-record"]').exists()).toBe(true)
  })

  it('emits the action instead of doing it itself', async () => {
    // The surface owns no meeting state: the panel starts the recording and
    // grabs the frame, exactly as it does for the footer's buttons.
    const wrapper = mountOverlay({ canShot: true, canRecord: true })

    await wrapper.find('[data-action="anno-shot"]').trigger('click')
    await wrapper.find('[data-action="anno-record"]').trigger('click')

    expect(wrapper.emitted('shot')).toHaveLength(1)
    expect(wrapper.emitted('toggleRecord')).toHaveLength(1)
  })

  it('hides the actions when they would do nothing', () => {
    // Key frames paused, or no recording possible: a button that silently
    // no-ops reads as broken.
    const wrapper = mountOverlay({ canShot: false, canRecord: false })
    expect(wrapper.find('[data-action="anno-shot"]').exists()).toBe(false)
    expect(wrapper.find('[data-action="anno-record"]').exists()).toBe(false)
  })

  it('turns the record button into a stop button while recording', () => {
    const wrapper = mountOverlay({ canRecord: true, recording: true })
    const btn = wrapper.find('[data-action="anno-record"]')
    expect(btn.attributes('aria-pressed')).toBe('true')
    expect(btn.classes()).toContain('anno__btn--rec')
    expect(btn.attributes('title')).toBe('Stop recording')

    const idle = mountOverlay({ canRecord: true })
    expect(idle.find('[data-action="anno-record"]').attributes('aria-pressed')).toBe('false')
    expect(idle.find('[data-action="anno-record"]').classes()).not.toContain('anno__btn--rec')
  })
})
