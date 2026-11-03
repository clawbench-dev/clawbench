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

  it('keeps the paragraph break but drops the repeated name on a continuation row', () => {
    const w = mountList([
      block('n1', '我', '第一段'),
      { ...block('n2', '我', '第二段'), continuation: true },
    ])
    const rows = w.findAll('.transcript__row')
    expect(rows).toHaveLength(2)
    expect(rows[1].find('.sa').exists()).toBe(false)
    expect(rows[1].find('.transcript__speaker').text()).toBe('')
    expect(rows[1].find('.transcript__text').text()).toBe('第二段')
    // The avatar cell stays so the text keeps the same left edge as every
    // other row instead of jumping into the avatar column.
    expect(rows[1].find('.transcript__avatar').exists()).toBe(true)
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

  it('draws a screen-sharer change as a monitor row, not a speaker', () => {
    const w = mountList([
      { key: 'n1', channel: 'screen', speaker: '屏幕', startedAt: 0, text: '张三 开始共享屏幕', kind: 'screen' },
    ])
    const row = w.findAll('.transcript__row')[0]
    expect(row.find('.sa').exists()).toBe(false)
    expect(row.find('.transcript__file-icon').exists()).toBe(true)
    expect(row.find('.transcript__text').text()).toBe('张三 开始共享屏幕')
  })

  it('draws a key frame with the shared media figure, thumbnail inline', () => {
    const w = mountList([
      {
        key: 'n1',
        channel: 'shot',
        speaker: '',
        startedAt: 0,
        text: '',
        kind: 'shot',
        shot: { id: 4, thumbUrl: '/t/4', fullUrl: '/f/4', reason: 'manual' },
      },
    ])
    const figure = w.find('[data-role="shot-open"] .image-block-wrapper')
    // The markup comes from the SHARED media-block factory (the same producer
    // markdown images go through), so the chrome cannot drift from markdown
    // preview — the `--thumb` modifier is passed through the factory too.
    expect(figure.exists()).toBe(true)
    expect(figure.classes()).toContain('image-block-wrapper--thumb')
    expect(figure.find('.image-block-view-btn').exists()).toBe(true)
    // Same chrome as a markdown figure: the reset button is hidden at rest by
    // the shared stylesheet and revealed while the figure holds an inline zoom.
    expect(figure.find('.image-block-reset-btn').exists()).toBe(true)

    const img = figure.find('img.lightbox-img')
    // The row paints the thumbnail; the viewer loads `data-full-src`.
    expect(img.attributes('src')).toBe('/t/4')
    expect(img.attributes('data-full-src')).toBe('/f/4')
  })

  it('marks the list as one image gallery so the viewer can flip between frames', () => {
    // Without a gallery container the lightbox opens a single image with no
    // prev/next; `.markdown-body` cannot be used here (it would restyle the
    // whole transcript).
    expect(mountList([block('n1', '我', 'a')]).attributes('data-media-gallery')).toBeDefined()
  })

  it('keeps frame opening out of the component: no bespoke click handler', () => {
    // Opening is the Lightbox's document-level listener, so the row must not
    // emit an event its hosts would have to wire (the share SPA has no host
    // handler and its frames were dead before this).
    const w = mountList([
      {
        key: 'n1',
        channel: 'shot',
        speaker: '',
        startedAt: 0,
        text: '',
        kind: 'shot',
        shot: { id: 4, thumbUrl: '/t/4', fullUrl: '/f/4', reason: 'manual' },
      },
    ])
    expect(w.find('[data-role="shot-open"]').element.tagName).not.toBe('BUTTON')
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

  it('follows a block that grows in place, not just a new row', async () => {
    // One speaker never changes the row count; the panel used to stop following
    // as soon as the first block was the only block.
    const w = mountList([block('n1', '我', 'a')], true)
    const readTop = fakeScroller(w.element as HTMLElement, {
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 600,
    })

    await w.setProps({ blocks: [block('n1', '我', 'a 又说了很长的一句')] })
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
