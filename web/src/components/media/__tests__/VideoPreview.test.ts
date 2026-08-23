import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import VideoPreview from '@/components/media/VideoPreview.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual: any = await importOriginal()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/utils/download.ts', () => ({
  buildLocalFileUrl: (path: string) => `/api/local-file/${path}`,
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
    expect(video.attributes('src')).toContain('/api/local-file/media/clip.mp4')
    expect(video.attributes('src')).toMatch(/t=\d+/)
  })

  it('shows fallback text for browsers without video support', () => {
    const wrapper = mountVideo()
    expect(wrapper.html()).toContain('media.videoNotSupported')
  })
})
