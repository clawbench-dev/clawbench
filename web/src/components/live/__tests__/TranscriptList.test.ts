import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import TranscriptList from '../TranscriptList.vue'
import i18n from '@/i18n'
import type { NoteBlock } from '@/composables/liveNotes'

vi.mock('@/utils/lazyAvatar', () => ({
  generateAvatarSvgWithStyle: async (seed: string) => `<svg data-seed="${seed}"/>`,
}))

function block(key: string, speaker: string, text: string): NoteBlock {
  return { key, channel: speaker, speaker, startedAt: 0, text }
}

function mountList(blocks: NoteBlock[], fill = false) {
  return mount(TranscriptList, {
    props: { blocks, fill },
    global: {
      stubs: { SpeakerAvatar: { props: ['name'], template: '<span class="sa" :data-name="name" />' } },
    },
  })
}

/** Gives the root element a scrollable geometry jsdom does not compute. */
function fakeScroller(el: HTMLElement, opts: { scrollHeight: number; clientHeight: number; scrollTop: number }) {
  let top = opts.scrollTop
  Object.defineProperty(el, 'scrollHeight', { value: opts.scrollHeight, configurable: true })
  Object.defineProperty(el, 'clientHeight', { value: opts.clientHeight, configurable: true })
  Object.defineProperty(el, 'scrollTop', {
    get: () => top,
    set: (v: number) => {
      top = v
    },
    configurable: true,
  })
  return () => top
}

describe('TranscriptList', () => {
  it('draws one avatar + name + text per speaker turn', () => {
    const w = mountList([block('n1', '我', '第一句'), block('n2', '小明', '第二句')])
    const rows = w.findAll('.transcript__row')
    expect(rows).toHaveLength(2)
    expect(rows[0].find('.sa').attributes('data-name')).toBe('我')
    expect(rows[0].find('.transcript__speaker').text()).toBe('我')
    expect(rows[0].find('.transcript__text').text()).toBe('第一句')
    expect(rows[1].find('.transcript__text').text()).toBe('第二句')
  })

  it('draws a file event as an icon row, not a speaker', () => {
    const w = mountList([
      { key: 'n1', channel: 'file', speaker: '文件', startedAt: 0, text: '打开 docs/a.md', kind: 'file' },
    ])
    const row = w.findAll('.transcript__row')[0]
    expect(row.find('.sa').exists()).toBe(false)
    expect(row.find('.transcript__file-icon').exists()).toBe(true)
    expect(row.find('.transcript__speaker').text()).toBe('文件')
    expect(row.find('.transcript__text').text()).toBe('打开 docs/a.md')
  })

  it('localizes a live file row from its action and path', () => {
    const w = mountList([
      { key: 'n1', channel: 'file', speaker: '', startedAt: 0, text: 'docs/a.md', kind: 'file', fileAction: 'open' },
    ])
    // The stored transcript carries a rendered sentence; a live row carries the
    // bare path and lets the view translate it.
    expect(w.find('.transcript__speaker').text()).toBe(i18n.global.t('liveShare.fileSpeaker'))
    expect(w.find('.transcript__text').text()).toBe(
      i18n.global.t('liveShare.fileOpened', { path: 'docs/a.md' }),
    )
  })

  it('is a plain grid until the host asks it to fill the pane', () => {
    expect(mountList([block('n1', '我', 'a')]).classes()).not.toContain('transcript--fill')
    expect(mountList([block('n1', '我', 'a')], true).classes()).toContain('transcript--fill')
  })

  it('follows new lines while the reader is at the bottom', async () => {
    const w = mountList([block('n1', '我', 'a')], true)
    const readTop = fakeScroller(w.element as HTMLElement, {
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 600,
    })

    await w.setProps({ blocks: [block('n1', '我', 'a'), block('n2', '小明', 'b')] })
    await nextTick()
    await nextTick()
    expect(readTop()).toBe(1000)
  })

  it('does not yank a reader who scrolled up to re-read', async () => {
    // The publisher's pane used to scroll unconditionally, so scrolling back
    // was undone by the next segment.
    const w = mountList([block('n1', '我', 'a')], true)
    const readTop = fakeScroller(w.element as HTMLElement, {
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 100,
    })

    await w.setProps({ blocks: [block('n1', '我', 'a'), block('n2', '小明', 'b')] })
    await nextTick()
    await nextTick()
    expect(readTop()).toBe(100)
  })
})
