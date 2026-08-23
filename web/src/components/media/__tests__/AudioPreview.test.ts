import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AudioPreview from '@/components/media/AudioPreview.vue'

vi.mock('@/utils/download.ts', () => ({
  buildLocalFileUrl: (path: string) => `/api/local-file/${path}`,
}))

describe('AudioPreview', () => {
  function mountAudio(props: Record<string, unknown> = {}) {
    return mount(AudioPreview, {
      props: { file: { path: 'media/song.mp3', name: 'song.mp3', size: 2048 }, ...props },
    })
  }

  it('renders container', () => {
    const wrapper = mountAudio()
    expect(wrapper.find('.audio-preview-container').exists()).toBe(true)
  })

  it('renders audio filename', () => {
    const wrapper = mountAudio()
    expect(wrapper.find('.audio-name').text()).toBe('song.mp3')
  })

  it('formats file size in bytes when size < 1024', () => {
    const wrapper = mountAudio({ file: { path: 'a.mp3', name: 'a.mp3', size: 500 } })
    expect(wrapper.find('.audio-size').text()).toContain('500 B')
  })

  it('formats file size in KB when size < 1MB', () => {
    const wrapper = mountAudio()
    expect(wrapper.find('.audio-size').text()).toContain('KB')
  })

  it('formats file size in MB when size >= 1MB', () => {
    const wrapper = mountAudio({ file: { path: 'b.mp3', name: 'b.mp3', size: 5 * 1024 * 1024 } })
    expect(wrapper.find('.audio-size').text()).toContain('MB')
  })

  it('hides file size when no size provided', () => {
    const wrapper = mountAudio({ file: { path: 'c.mp3', name: 'c.mp3' } })
    expect(wrapper.find('.audio-size').exists()).toBe(false)
  })

  it('renders audio element with src and cache buster', () => {
    const wrapper = mountAudio()
    const audio = wrapper.find('audio.audio-player')
    expect(audio.exists()).toBe(true)
    expect(audio.attributes('src')).toContain('/api/local-file/media/song.mp3')
    expect(audio.attributes('src')).toMatch(/t=\d+/)
  })
})
