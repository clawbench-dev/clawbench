import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AudioPreview from '@/components/media/AudioPreview.vue'

vi.mock('@/utils/download.ts', () => ({
  buildLocalFileUrl: (path: string) => `/api/fs/raw/${path}`,
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
    expect(audio.attributes('src')).toContain('/api/fs/raw/media/song.mp3')
    expect(audio.attributes('src')).toMatch(/t=\d+/)
  })

  it('shows the shared failure element when the file cannot be loaded', async () => {
    // A missing / unreadable file previously left a dead native player with no
    // explanation — the browser just greys out the controls.
    const wrapper = mountAudio()
    expect(wrapper.find('.media-load-error').exists()).toBe(false)

    await wrapper.find('audio').trigger('error')

    const err = wrapper.find('.media-load-error')
    expect(err.exists()).toBe(true)
    expect(err.text()).toContain('Media failed to load')
    expect(err.text()).toContain('song.mp3')
    expect(err.find('.media-load-error-icon svg').attributes('class')).toContain('lucide-audio-lines')
  })

  it('hides the dead player and its file info once it has failed', async () => {
    const wrapper = mountAudio()
    await wrapper.find('audio').trigger('error')

    // The unusable player must not sit under the failure card. It is hidden
    // rather than removed so a later successful load can revive it.
    expect(wrapper.find('audio.audio-player').classes()).toContain('local-media-hidden')
    expect(wrapper.find('.audio-info').classes()).toContain('local-media-hidden')
    expect(wrapper.find('.audio-icon').classes()).toContain('local-media-hidden')
  })

  it('clears the failure when the file later loads', async () => {
    const wrapper = mountAudio()
    await wrapper.find('audio').trigger('error')
    expect(wrapper.find('.media-load-error').exists()).toBe(true)

    await wrapper.find('audio').trigger('loadedmetadata')

    expect(wrapper.find('.media-load-error').exists()).toBe(false)
    expect(wrapper.find('audio.audio-player').classes()).not.toContain('local-media-hidden')
  })
})
