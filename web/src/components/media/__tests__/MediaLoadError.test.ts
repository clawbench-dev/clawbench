import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import MediaLoadError from '@/components/media/MediaLoadError.vue'

/**
 * The single "this media could not be loaded" element.
 *
 * Before this component every surface rolled its own: the media preview card
 * had an icon + text overlay, the chat figure got a hand-built DOM placeholder
 * from localMediaFallback.ts, and the lightbox / audio / video viewers showed
 * nothing at all (a broken glyph or an empty pane). Sharing one component is
 * what keeps them from drifting again.
 *
 * Deliberately NOT using `useI18n()`: this component is also mounted
 * imperatively by localMediaFallback.ts for v-html surfaces (chat messages,
 * share pages), which have no Vue app instance — `useI18n()` throws there
 * ("Cannot read properties of null (reading '__VUE_I18N_SYMBOL__')"). `gt()`
 * resolves against the global i18n instance and works in both contexts.
 */
describe('MediaLoadError', () => {
  it('renders the label and the file name', () => {
    const wrapper = mount(MediaLoadError, { props: { name: 'broken.png' } })
    const root = wrapper.find('.media-load-error')
    expect(root.exists()).toBe(true)
    expect(root.text()).toContain('broken.png')
    // The label comes from the global i18n instance, so assert against the real
    // resolved string rather than a key — a missing key renders the key itself.
    expect(root.text()).not.toContain('media.loadFailed')
    expect(root.text().length).toBeGreaterThan('broken.png'.length)
  })

  it('is announced as an image with a title naming the file', () => {
    const wrapper = mount(MediaLoadError, { props: { name: 'broken.png' } })
    const root = wrapper.find('.media-load-error')
    expect(root.attributes('role')).toBe('img')
    const title = root.attributes('title') || ''
    expect(title).toContain('broken.png')
    expect(root.attributes('aria-label')).toBe(title)
  })

  it('omits the name line when no name can be derived', () => {
    const wrapper = mount(MediaLoadError)
    expect(wrapper.find('.media-load-error-name').exists()).toBe(false)
    // The label must still be present — a nameless failure is still a failure.
    expect(wrapper.find('.media-load-error-text').text().length).toBeGreaterThan(0)
  })

  it('picks the icon from the media kind', () => {
    const iconOf = (kind: 'image' | 'video' | 'audio' | 'file') =>
      mount(MediaLoadError, { props: { kind } }).find('.media-load-error-icon svg').attributes('class')

    expect(iconOf('image')).toContain('lucide-image-off')
    expect(iconOf('video')).toContain('lucide-video-off')
    expect(iconOf('audio')).toContain('lucide-audio-lines')
    // lucide's `FileWarning` export renders as `file-exclamation-point`.
    expect(iconOf('file')).toContain('lucide-file-exclamation-point')
  })

  it('fills the pane only when asked, so an inline figure stays chip-sized', () => {
    // A figure in the chat column must not stretch to the whole pane; a viewer
    // with nothing else to show should.
    expect(mount(MediaLoadError).find('.media-load-error').classes()).not.toContain('media-load-error--fill')
    expect(
      mount(MediaLoadError, { props: { fill: true } }).find('.media-load-error').classes(),
    ).toContain('media-load-error--fill')
  })

  it('lets a caller override the message', () => {
    const wrapper = mount(MediaLoadError, { props: { message: 'Custom failure' } })
    expect(wrapper.find('.media-load-error-text').text()).toBe('Custom failure')
  })
})
