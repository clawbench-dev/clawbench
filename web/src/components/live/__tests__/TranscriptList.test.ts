import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import TranscriptList from '../TranscriptList.vue'
import i18n from '@/i18n'
import type { NoteBlock } from '@/composables/liveNotes'

vi.mock('@/utils/lazyAvatar', () => ({
  generateAvatarSvgWithStyle: async (seed: string) => `<svg data-seed="${seed}"/>`,
}))

/**
 * Path verification is a network round trip; the wiring is what this file
 * asserts. The real detector still runs (it is what produces the chips), only
 * the `/api/file/batch-exists` step is replaced.
 */
const { verifyFilePathsMock, openFilePathMock } = vi.hoisted(() => ({
  verifyFilePathsMock: vi.fn(async (_paths: string[], _el: HTMLElement) => {}),
  openFilePathMock: vi.fn(async () => true),
}))

vi.mock('@/composables/useFilePathAnnotation.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useFilePathAnnotation.ts')>()
  return {
    ...actual,
    verifyFilePaths: verifyFilePathsMock,
    // The component reads the primitives through the composable, so its return
    // value has to carry the stubs as well.
    useFilePathAnnotation: () => ({
      ...actual.useFilePathAnnotation(),
      verifyFilePaths: verifyFilePathsMock,
      openFilePath: openFilePathMock,
    }),
  }
})

function block(key: string, speaker: string, text: string): NoteBlock {
  return { key, channel: speaker, speaker, startedAt: 0, text }
}

function mountList(blocks: NoteBlock[], fill = false) {
  return mount(TranscriptList, {
    props: { blocks, fill },
    global: {
      // The row text goes through the shared annotation pipeline, whose inline
      // preview card is a real component (it calls useI18n) — exactly what the
      // meeting record and the live pane mount.
      plugins: [i18n],
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
    // A stored row is re-said in today's wording, so an old meeting reads the
    // same as a new one.
    expect(row.find('.transcript__text').text()).toBe(
      i18n.global.t('liveShare.fileOpened', { path: 'docs/a.md' }),
    )
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

  describe('references in the spoken text', () => {
    it('annotates a file path with the shared chip and open button', () => {
      const w = mountList([block('n1', '我', '入口在 web/src/main.ts 里')])
      const chip = w.find('.transcript__text .chat-file-path')
      expect(chip.exists()).toBe(true)
      expect(chip.text()).toBe('web/src/main.ts')
      expect(chip.attributes('data-file-path')).toBe('web/src/main.ts')
      // The open button is what "跳转" uses; the chip text is the inline
      // preview target. Both come from the same annotator as the minutes.
      expect(
        w.find('.transcript__text .chat-file-open-btn[data-file-path="web/src/main.ts"]').exists(),
      ).toBe(true)
    })

    it('keeps the line target so a jump lands on the cited lines', () => {
      const w = mountList([block('n1', '我', '见 web/src/main.ts:42-48')])
      const chip = w.find('.transcript__text .chat-file-path')
      expect(chip.attributes('data-line-start')).toBe('42')
      expect(chip.attributes('data-line-end')).toBe('48')
    })

    it('links a bare URL through the shared external-link node', () => {
      const w = mountList([block('n1', '我', '参考 github.com/foo/bar 的实现')])
      const link = w.find('.transcript__text .md-external-link')
      expect(link.exists()).toBe(true)
      // A span, not an <a>: the Android long-press menu needs selectable text,
      // and the document-level click layer turns the tap into a confirmation.
      expect(link.attributes('data-href')).toBe('https://github.com/foo/bar')
      expect(link.attributes('role')).toBe('link')
    })

    it('escapes the utterance before annotating it', () => {
      const w = mountList([block('n1', '我', '<img src=x onerror="alert(1)"> 与 a/b.md')])
      expect(w.find('.transcript__text img').exists()).toBe(false)
      expect(w.find('.transcript__text').text()).toContain('<img src=x')
      // …and annotation still runs on the escaped text.
      expect(w.find('.transcript__text .chat-file-path').exists()).toBe(true)
    })

    it('verifies the detected paths so the chips become interactive', async () => {
      verifyFilePathsMock.mockClear()
      const w = mountList([block('n1', '我', '入口在 web/src/main.ts')])
      await nextTick()
      await nextTick()
      expect(verifyFilePathsMock).toHaveBeenCalled()
      const [paths, el] = verifyFilePathsMock.mock.calls.at(-1)!
      expect(paths).toContain('web/src/main.ts')
      expect(el).toBe(w.element)
    })

    it('opens the file when the chip\'s open button is clicked', async () => {
      openFilePathMock.mockClear()
      const w = mountList([block('n1', '我', '入口在 web/src/main.ts')])
      await w.find('.transcript__text .chat-file-open-btn').trigger('click')
      // The navigation source is 'live': Back from the viewer returns to the
      // meeting, exactly like a path in the minutes.
      expect(openFilePathMock).toHaveBeenCalledWith('web/src/main.ts', undefined, undefined, 'live')
    })

    it('annotates the file a live file event names', () => {
      // "打开 docs/a.md" is the row that says which file the meeting looked at;
      // the path in it must open like any other reference.
      const w = mountList([
        { key: 'n1', channel: 'file', speaker: '', startedAt: 0, text: 'docs/a.md', kind: 'file', fileAction: 'open' },
      ])
      const chip = w.find('.transcript__text .chat-file-path')
      expect(chip.exists()).toBe(true)
      expect(chip.attributes('data-file-path')).toBe('docs/a.md')
    })

    it('annotates a stored file event sentence the same way', () => {
      // The record stores the rendered sentence, not the bare path.
      const w = mountList([
        { key: 'n1', channel: 'file', speaker: '文件', startedAt: 0, text: '打开 docs/a.md', kind: 'file' },
      ])
      expect(w.find('.transcript__text .chat-file-path').attributes('data-file-path')).toBe('docs/a.md')
      expect(w.find('.transcript__text').text()).toBe(
        i18n.global.t('liveShare.fileOpened', { path: 'docs/a.md' }),
      )
    })

    it('annotates a root-level file event, which the path is a bare name for', () => {
      // A meeting that opens AGENTS.md records exactly "打开 AGENTS.md" — no
      // directory. The annotator skips single-segment names on purpose, so the
      // row used to be dead text; the file event must still be openable.
      const w = mountList([
        { key: 'n1', channel: 'file', speaker: '文件', startedAt: 0, text: '打开 AGENTS.md', kind: 'file' },
      ])
      const chip = w.find('.transcript__text .chat-file-path')
      expect(chip.exists()).toBe(true)
      expect(chip.attributes('data-file-path')).toBe('AGENTS.md')
      // The `./` used to make it detectable must not show up in the row.
      expect(chip.text()).toBe('AGENTS.md')
      expect(w.find('.transcript__text').text()).toBe(
        i18n.global.t('liveShare.fileOpened', { path: 'AGENTS.md' }),
      )
    })

    it('keeps the duration suffix on a close event', () => {
      const w = mountList([
        { key: 'n1', channel: 'file', speaker: '文件', startedAt: 0, text: '关闭 README.md（21 秒）', kind: 'file' },
      ])
      expect(w.find('.transcript__text .chat-file-path').attributes('data-file-path')).toBe('README.md')
      expect(w.find('.transcript__text').text()).toBe(
        i18n.global.t('liveShare.fileClosed', { path: 'README.md（21 秒）' }),
      )
    })

    it('leaves a bare file name in ordinary speech alone', () => {
      // Only a file EVENT is known to name a file; a bare `AGENTS.md` in a
      // sentence is the ambiguous case the annotator refuses on purpose.
      const w = mountList([block('n1', '我', '记得看 AGENTS.md')])
      expect(w.find('.transcript__text .chat-file-path').exists()).toBe(false)
    })

    it('annotates the frame caption with the file it was taken for', async () => {
      verifyFilePathsMock.mockClear()
      const w = mountList([
        {
          key: 'n1',
          channel: 'shot',
          speaker: '',
          startedAt: 0,
          text: '',
          kind: 'shot',
          shot: { id: 1, thumbUrl: '/t/1', fullUrl: '/f/1', reason: 'open-file', ref: 'web/src/main.ts' },
        },
      ])
      const chip = w.find('.transcript__shot-meta .chat-file-path')
      expect(chip.exists()).toBe(true)
      // The caption stays short (base name)…
      expect(chip.text()).toBe('main.ts')
      // …but the click target is the whole path, or verification could only
      // ever say "not found" and the chip would stay inert.
      expect(chip.attributes('data-file-path')).toBe('web/src/main.ts')
      await nextTick()
      await nextTick()
      const [paths] = verifyFilePathsMock.mock.calls.at(-1)!
      expect(paths).toContain('web/src/main.ts')
    })

    it('annotates a root-level file in the frame caption too', () => {
      // Same trap as the file event: a root-level file's ref is a bare name,
      // so the caption used to read as dead text.
      const w = mountList([
        {
          key: 'n1',
          channel: 'shot',
          speaker: '',
          startedAt: 0,
          text: '',
          kind: 'shot',
          shot: { id: 1, thumbUrl: '/t/1', fullUrl: '/f/1', reason: 'file', ref: 'AGENTS.md' },
        },
      ])
      const chip = w.find('.transcript__shot-meta .chat-file-path')
      expect(chip.exists()).toBe(true)
      expect(chip.attributes('data-file-path')).toBe('AGENTS.md')
      expect(chip.text()).toBe('AGENTS.md')
    })

    it('words the frame caption and the file event the same way', () => {
      // One meeting, two marker rows: they must not be labelled two different
      // ways ("打开 x" vs "打开文件 · x").
      const caption = mountList([
        {
          key: 'n1',
          channel: 'shot',
          speaker: '',
          startedAt: 0,
          text: '',
          kind: 'shot',
          shot: { id: 1, thumbUrl: '/t/1', fullUrl: '/f/1', reason: 'file', ref: 'AGENTS.md' },
        },
      ])
      const event = mountList([
        { key: 'n2', channel: 'file', speaker: '文件', startedAt: 0, text: '打开 AGENTS.md', kind: 'file' },
      ])
      expect(caption.find('.transcript__shot-meta').text()).toBe(
        event.find('.transcript__text').text(),
      )
    })

    it('keeps the short caption when the frame ref cannot be annotated', () => {
      // A path with spaces would only produce a fragment chip — it must fall
      // back to the caption the row has always shown, not to the raw ref.
      const spaced = mountList([
        {
          key: 'n2',
          channel: 'shot',
          speaker: '',
          startedAt: 0,
          text: '',
          kind: 'shot',
          shot: { id: 2, thumbUrl: '/t/2', fullUrl: '/f/2', reason: 'file', ref: '/p/My Project/main.ts' },
        },
      ])
      const caption = spaced.find('.transcript__shot-meta').text()
      expect(caption).toContain('main.ts')
      expect(caption).not.toContain('/p/My Project')
    })
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
