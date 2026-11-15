import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import VideoPreview from '@/components/media/VideoPreview.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual: any = await importOriginal()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/utils/download.ts', () => ({
  buildLocalFileUrl: (path: string) => `/api/fs/raw/${path}`,
}))

describe('VideoPreview', () => {
  function mountVideo(props: Record<string, unknown> = {}) {
    return mount(VideoPreview, {
      props: { file: { path: 'media/clip.mp4', name: 'clip.mp4' }, ...props },
    })
  }

  it('renders container', () => {
    const wrapper = mountVideo()
    expect(wrapper.find('.video-preview-container').exists()).toBe(true)
  })

  it('renders video element with correct src', () => {
    const wrapper = mountVideo()
    const video = wrapper.find('video.video-player')
    expect(video.exists()).toBe(true)
    expect(video.attributes('src')).toContain('/api/fs/raw/media/clip.mp4')
    expect(video.attributes('src')).toMatch(/t=\d+/)
  })

  it('shows fallback text for browsers without video support', () => {
    const wrapper = mountVideo()
    expect(wrapper.html()).toContain('media.videoNotSupported')
  })

  it('starts the clip muted so the browser will auto-play it', () => {
    const wrapper = mountVideo()
    const el = wrapper.find('video.video-player').element as HTMLVideoElement
    expect(el.autoplay).toBe(true)
    // Browsers only auto-start MUTED media — without this the viewer opens on
    // a paused first frame.
    expect(el.muted).toBe(true)
    // Without this iOS/Android take the clip fullscreen on its own.
    expect(wrapper.find('video.video-player').attributes('playsinline')).toBeDefined()
  })

  it('re-mutes the player when the next clip is opened', async () => {
    // The same <video> is reused across files; a clip the user unmuted would
    // otherwise block the next one's autoplay (unmuted autoplay needs a
    // gesture), leaving it paused.
    const wrapper = mountVideo()
    const el = wrapper.find('video.video-player').element as HTMLVideoElement
    el.muted = false

    await wrapper.setProps({ file: { path: 'media/next.mp4', name: 'next.mp4' } })
    await wrapper.find('video').trigger('loadedmetadata')

    expect(el.muted).toBe(true)
  })

  it('shows the shared failure element when the file cannot be loaded', async () => {
    // A missing / unreadable file previously left a black box with a dead
    // player — the browser reports nothing on its own.
    const wrapper = mountVideo()
    expect(wrapper.find('.media-load-error').exists()).toBe(false)

    await wrapper.find('video').trigger('error')

    const err = wrapper.find('.media-load-error')
    expect(err.exists()).toBe(true)
    expect(err.text()).toContain('Media failed to load')
    expect(err.text()).toContain('clip.mp4')
    expect(err.find('.media-load-error-icon svg').attributes('class')).toContain('lucide-video-off')
  })

  it('hides the dead player once it has failed, and revives it on load', async () => {
    const wrapper = mountVideo()
    await wrapper.find('video').trigger('error')
    expect(wrapper.find('video.video-player').classes()).toContain('local-media-hidden')

    // Hidden rather than removed, so a later successful load restores it.
    await wrapper.find('video').trigger('loadedmetadata')

    expect(wrapper.find('.media-load-error').exists()).toBe(false)
    expect(wrapper.find('video.video-player').classes()).not.toContain('local-media-hidden')
  })
})
