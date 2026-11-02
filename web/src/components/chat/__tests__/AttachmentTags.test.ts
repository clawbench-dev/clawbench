import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import AttachmentTags from '../AttachmentTags.vue'

// Pass the real module through and override only `useI18n`: the component now
// reaches `useFileUpload` (the shared batch-terminate implementation), whose
// import graph includes `@/i18n` — a mock that exported nothing but `useI18n`
// made that transitive import fail with "No createI18n export".
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/utils/fileIcon', () => ({
  buildPathThumbUrl: (path: string) => `http://localhost/thumb/${path}`,
  getFileIconUrl: vi.fn().mockResolvedValue('http://localhost/icon.svg'),
  getFolderIconUrl: vi.fn().mockResolvedValue('http://localhost/folder.svg'),
}))

describe('AttachmentTags', () => {
  it('renders attached files correctly', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: 'src/main.go', isDir: false }],
      },
    })
    expect(wrapper.find('.attachment-filename').text()).toBe('main.go')
  })

  // ── URL attachment chips ──

  it('renders a URL attachment as a link', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: 'acme/widgets#1', kind: 'url', url: 'https://github.com/acme/widgets/issues/1' }],
      },
    })
    const link = wrapper.find('a.attachment-url')
    expect(link.exists()).toBe(true)
    expect(link.attributes('href')).toBe('https://github.com/acme/widgets/issues/1')
    expect(link.attributes('target')).toBe('_blank')
    expect(wrapper.text()).toContain('acme/widgets#1')
  })

  it('renders a non-http(s) URL inert instead of a live link', () => {
    // This renderer draws the same persisted FileEntry shape as the message
    // bubble, so it needs the same guard: history navigation can restore a
    // stored entry straight into the composer's attachments.
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: 'evil', kind: 'url', url: 'javascript:alert(1)' }],
      },
    })
    const link = wrapper.find('a.attachment-url')
    expect(link.attributes('href')).toBeUndefined()
    expect(link.classes()).toContain('attachment-url-inert')
  })

  it('renders the line range suffix on a ranged reference card', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 8 }],
      },
    })
    expect(wrapper.find('.attachment-filename').text()).toContain('guide.md')
    expect(wrapper.find('.attachment-filename .attachment-range').text()).toBe(':5-8')
  })

  it('falls back to icon + filename for SVG (image the backend cannot thumbnail)', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: '.clawbench/uploads/diagram.svg', isDir: false }],
      },
    })
    // Image-but-not-thumbable must never render a blank pill
    expect(wrapper.find('.attachment-image-only').exists()).toBe(false)
    expect(wrapper.find('.attachment-thumb-img').exists()).toBe(false)
    expect(wrapper.find('.attachment-filename').text()).toContain('diagram.svg')
  })

  it('emits the full entry on card click and on remove', async () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 8 }],
      },
    })
    await wrapper.find('.attachment-ref').trigger('click')
    expect(wrapper.emitted('file-click')![0]).toEqual([{ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 8 }])

    await wrapper.find('.attachment-ref .attachment-close-btn').trigger('click')
    expect(wrapper.emitted('remove')![0]).toEqual([{ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 8 }])
  })

  it('uses a composite key so two ranges of one file render as separate cards', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [
          { path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 8 },
          { path: 'docs/guide.md', isDir: false, startLine: 20, endLine: 30 },
        ],
      },
    })
    expect(wrapper.findAll('.attachment-ref')).toHaveLength(2)
  })

  it('renders pending files with local Blob image preview and progress overlay', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [],
        pendingFiles: [
          {
            path: '',
            previewUrl: 'blob:http://localhost/test-blob-123',
            isImage: true,
            uploading: true,
            progress: 45,
            size: 1024,
          },
        ],
      },
    })

    const pendingItem = wrapper.find('.attachment-pending')
    expect(pendingItem.exists()).toBe(true)
    const img = wrapper.find('.attachment-uploading-img')
    expect(img.attributes('src')).toBe('blob:http://localhost/test-blob-123')
    expect(wrapper.find('.attachment-progress-text').text()).toBe('45%')
  })

  it('does not render completed pending files alongside attached files', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: '.clawbench/uploads/screenshot.png', isDir: false }],
        pendingFiles: [{
          path: '.clawbench/uploads/screenshot.png', previewUrl: 'blob:test', isImage: true,
          uploading: false, progress: 100, size: 1024,
        }],
      },
    })

    expect(wrapper.find('.attachment-pending').exists()).toBe(false)
    expect(wrapper.findAll('.attachment-ref')).toHaveLength(1)
  })

  it('emits remove-pending when clicking remove on a pending file', async () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [],
        pendingFiles: [
          {
            path: '',
            previewUrl: 'blob:http://localhost/test-blob-123',
            isImage: true,
            uploading: true,
            progress: 50,
            size: 2048,
          },
        ],
      },
    })

    const removeBtn = wrapper.find('.attachment-pending .attachment-close-btn')
    await removeBtn.trigger('click')

    expect(wrapper.emitted('remove-pending')).toBeTruthy()
    expect(wrapper.emitted('remove-pending')![0]).toEqual([0])
  })

  // ── Whole-batch terminate ──
  //
  // The drawer's terminate button is unreachable during an upload: picking
  // files closes the drawer, so this strip is the only thing on screen. Its
  // per-card "×" aborts ONE file and the sequential loop continues, so without
  // this control there was no way to stop the batch.
  it('renders no batch-terminate control when nothing is uploading', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [{ path: 'src/main.go' }],
        pendingFiles: [{
          path: 'src/done.go', previewUrl: null, isImage: false,
          uploading: false, progress: 100, size: 10,
        }],
      },
    })
    expect(wrapper.find('.chat-attachment-terminate').exists()).toBe(false)
  })

  it('renders the batch-terminate control while a file is uploading', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        files: [],
        pendingFiles: [{
          path: '', previewUrl: null, isImage: false,
          uploading: true, progress: 40, size: 10,
        }],
      },
    })
    const btn = wrapper.find('.chat-attachment-terminate')
    expect(btn.exists()).toBe(true)
    // The i18n mock returns the key, so this pins the reuse of the drawer's
    // label rather than a second wording for the same action.
    expect(btn.attributes('title')).toBe('chat.attach.terminateUpload')
    expect(btn.attributes('aria-label')).toBe('chat.attach.terminateUpload')
  })

  it('cancels the WHOLE batch on click, not just one file', async () => {
    const { useFileUpload } = await import('@/composables/useFileUpload')
    const upload = useFileUpload()
    upload.clearPendingFiles()
    upload.attachedFiles.value.splice(0)

    const wrapper = mount(AttachmentTags, {
      props: {
        files: [],
        pendingFiles: [{
          path: '', previewUrl: null, isImage: false,
          uploading: true, progress: 40, size: 10,
        }],
      },
    })

    await wrapper.find('.chat-attachment-terminate').trigger('click')

    // The batch flag stops the sequential loop in useFileUpload — the per-card
    // "×" (remove-pending) never sets it, which is exactly why one file's "×"
    // lets the rest of the batch continue.
    expect(upload.chatUploadCancelled.value).toBe(true)
    // Every in-flight row is dropped here rather than left at its last
    // percentage (abort() alone never touches pendingFiles).
    expect(upload.pendingFiles.value.filter(f => f.uploading)).toHaveLength(0)
    // Distinct from the per-card "×", which only emits remove-pending.
    expect(wrapper.emitted('remove-pending')).toBeFalsy()
  })

  it('does NOT set the batch flag when only one pending card is removed', async () => {
    // Contrast that pins the difference between the two controls: the "×" is
    // a per-file abort, so the rest of the batch keeps uploading.
    const { useFileUpload } = await import('@/composables/useFileUpload')
    const upload = useFileUpload()
    upload.clearPendingFiles()
    upload.chatUploadCancelled.value = false

    const wrapper = mount(AttachmentTags, {
      props: {
        files: [],
        pendingFiles: [{
          path: '', previewUrl: null, isImage: false,
          uploading: true, progress: 40, size: 10,
        }],
      },
    })

    await wrapper.find('.attachment-pending .attachment-close-btn').trigger('click')

    expect(upload.chatUploadCancelled.value).toBe(false)
  })
})

/**
 * Read-only mode: surfaces that only DISPLAY attachments (the queued-message
 * row) must not offer a remove control — removal there is a separate row
 * action, and a dead "×" that emits into the void reads as broken.
 */
describe('AttachmentTags (readonly)', () => {
  it('renders the same cards without any remove button', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        readonly: true,
        files: [
          { path: 'src/main.go', isDir: false },
          { path: 'acme/widgets#1', kind: 'url', url: 'https://github.com/acme/widgets/issues/1' },
        ],
      },
    })

    // URL chips render before file cards, so assert on the whole set rather
    // than on the first match.
    expect(wrapper.findAll('.attachment-filename').map((w) => w.text())).toContain('main.go')
    expect(wrapper.find('a.attachment-url').exists()).toBe(true)
    expect(wrapper.findAll('.attachment-close-btn'), 'no dead × on a read-only surface').toHaveLength(0)
    expect(wrapper.classes()).toContain('is-readonly')
  })

  it('keeps the remove buttons by default', () => {
    const wrapper = mount(AttachmentTags, {
      props: { files: [{ path: 'src/main.go', isDir: false }] },
    })
    expect(wrapper.findAll('.attachment-close-btn')).toHaveLength(1)
    expect(wrapper.classes()).not.toContain('is-readonly')
  })

  it('gives an image its square thumbnail tile in read-only mode', () => {
    const wrapper = mount(AttachmentTags, {
      props: { readonly: true, files: [{ path: 'shot.png', isDir: false }] },
    })
    const card = wrapper.find('.chat-file-attachment')
    expect(card.classes()).toContain('attachment-image-only')
    expect(card.find('img.attachment-thumb-img').exists()).toBe(true)
  })

  it('does not reserve the × gutter on image-only cards (it squeezed the thumbnail)', () => {
    // jsdom has no CSS engine, so the guard reads the rule. The bug was a
    // specificity fight: the three-class readonly rule outranks
    // `.chat-file-attachment.attachment-image-only` (two classes), so a plain
    // `padding-right` there shrinks a fixed 40dp tile's content box and the
    // thumbnail renders narrower than its square — reported as "the image is
    // not centred".
    const src = readFileSync(resolve(__dirname, '../AttachmentTags.vue'), 'utf8')
    const m = src.match(/\.chat-attachment-tags\.is-readonly\s+\.chat-file-attachment[^{]*\{([^}]*)\}/)
    expect(m, 'the readonly padding rule must exist').not.toBeNull()
    expect(m![0], 'image-only tiles must be excluded from the gutter').toContain(':not(.attachment-image-only)')
  })

  /**
   * Read-only cards are not clickable, so they must not show the hand cursor or
   * the hover tint that advertise a click. Regression: `readonly` only dropped
   * the "×", leaving a hand cursor (and a hover highlight) on a card that emits
   * `file-click` into the void — clicking the thumbnail in the queued row did
   * nothing while the same card in the composer opened a preview.
   *
   * jsdom DOES cascade plain properties from a real stylesheet, and `cursor` is
   * one of them (it carries no var()), so this asserts the computed value on
   * the mounted card rather than sniffing the source.
   */
  describe('no pointer affordance when read-only', () => {
    let styleEl: HTMLStyleElement

    beforeEach(() => {
      const src = readFileSync(resolve(__dirname, '../AttachmentTags.vue'), 'utf8')
      const css = src.match(/<style>([\s\S]*?)<\/style>/)?.[1] ?? ''
      expect(css, 'the component style block must be found').not.toBe('')
      styleEl = document.createElement('style')
      styleEl.textContent = css
      document.head.appendChild(styleEl)
    })

    afterEach(() => { styleEl.remove() })

    function cursorOf(wrapper: ReturnType<typeof mount>, selector: string): string {
      const el = wrapper.find(selector).element
      return getComputedStyle(el).cursor
    }

    it('shows a default cursor on a read-only file card', () => {
      const wrapper = mount(AttachmentTags, {
        props: { readonly: true, files: [{ path: 'src/main.go', isDir: false }] },
        attachTo: document.body,
      })
      expect(cursorOf(wrapper, '.attachment-ref')).toBe('default')
      wrapper.unmount()
    })

    it('still shows a pointer cursor on an editable card', () => {
      // Contrast case: without it a rule that disabled the cursor everywhere
      // would also pass.
      const wrapper = mount(AttachmentTags, {
        props: { files: [{ path: 'src/main.go', isDir: false }] },
        attachTo: document.body,
      })
      expect(cursorOf(wrapper, '.attachment-ref')).toBe('pointer')
      wrapper.unmount()
    })

    it('keeps the pointer cursor on a read-only URL chip (it is a real link)', () => {
      const wrapper = mount(AttachmentTags, {
        props: {
          readonly: true,
          files: [{ path: 'acme/widgets#1', kind: 'url', url: 'https://github.com/acme/widgets/issues/1' }],
        },
        attachTo: document.body,
      })
      expect(cursorOf(wrapper, 'a.attachment-url')).toBe('pointer')
      wrapper.unmount()
    })

    it('gates the hover tint on not being read-only', () => {
      // :hover cannot be simulated in jsdom, so this one reads the rule — the
      // assertion is on the SELECTOR (the tint must be excluded, not overridden
      // back to its resting colour, which would still flash on hover).
      const src = readFileSync(resolve(__dirname, '../AttachmentTags.vue'), 'utf8')
      const hoverRule = src.match(/@media \(hover: hover\) \{\s*\/\*[\s\S]*?\*\/\s*([^{]*\.attachment-ref:hover[^{]*)\{/)
      expect(hoverRule, 'the attachment hover rule must exist').not.toBeNull()
      expect(hoverRule![1]).toContain(':not(.is-readonly)')
    })
  })
})

/**
 * `clickable: false` — removable but not openable.
 *
 * The queued-message edit sheet needs exactly this combination: its cards keep
 * the "×" (removal is the point of the sheet), but it has no file viewer, so a
 * click must do nothing AND must not be advertised. The obvious shortcut —
 * passing `readonly` — would "fix" the cursor by deleting the remove button,
 * which is why the assertions below pin both halves at once.
 */
describe('AttachmentTags (clickable: false)', () => {
  let styleEl: HTMLStyleElement

  beforeEach(() => {
    const src = readFileSync(resolve(__dirname, '../AttachmentTags.vue'), 'utf8')
    const css = src.match(/<style>([\s\S]*?)<\/style>/)?.[1] ?? ''
    expect(css, 'the component style block must be found').not.toBe('')
    styleEl = document.createElement('style')
    styleEl.textContent = css
    document.head.appendChild(styleEl)
  })

  afterEach(() => { styleEl.remove() })

  it('swallows the card click but keeps remove working', async () => {
    const wrapper = mount(AttachmentTags, {
      props: { clickable: false, files: [{ path: 'docs/guide.md', isDir: false }] },
    })

    await wrapper.find('.attachment-ref').trigger('click')
    expect(wrapper.emitted('file-click'), 'nothing can open this file here').toBeUndefined()

    await wrapper.find('.attachment-ref .attachment-close-btn').trigger('click')
    expect(wrapper.emitted('remove'), 'removal is still this surface’s job').toHaveLength(1)
  })

  it('shows a default cursor and drops the "open file" tooltip', () => {
    const off = mount(AttachmentTags, {
      props: { clickable: false, files: [{ path: 'docs/guide.md', isDir: false }] },
      attachTo: document.body,
    })
    expect(getComputedStyle(off.find('.attachment-ref').element).cursor).toBe('default')
    // The tooltip must describe the file, not promise an action.
    expect(off.find('.attachment-ref').attributes('title')).toBe('guide.md')
    off.unmount()

    // Contrast: without it, a rule that disabled the cursor everywhere passes.
    const on = mount(AttachmentTags, {
      props: { files: [{ path: 'docs/guide.md', isDir: false }] },
      attachTo: document.body,
    })
    expect(getComputedStyle(on.find('.attachment-ref').element).cursor).toBe('pointer')
    expect(on.find('.attachment-ref').attributes('title')).toBe('chat.attach.openFile')
    on.unmount()
  })

  it('swallows the click on a read-only strip too (readonly implies not clickable)', async () => {
    // The queued ROW passes only `readonly` and listens for nothing; without
    // this implication it kept a click handler that emitted into the void.
    const wrapper = mount(AttachmentTags, {
      props: { readonly: true, files: [{ path: 'docs/guide.md', isDir: false }] },
    })
    await wrapper.find('.attachment-ref').trigger('click')
    expect(wrapper.emitted('file-click')).toBeUndefined()
  })

  it('keeps the URL chip a real link', () => {
    const wrapper = mount(AttachmentTags, {
      props: {
        clickable: false,
        files: [{ path: 'acme/widgets#1', kind: 'url', url: 'https://github.com/acme/widgets/issues/1' }],
      },
      attachTo: document.body,
    })
    expect(wrapper.find('a.attachment-url').attributes('href')).toBe('https://github.com/acme/widgets/issues/1')
    expect(getComputedStyle(wrapper.find('a.attachment-url').element).cursor).toBe('pointer')
    wrapper.unmount()
  })

  it('gates the hover tint on not-clickable too', () => {
    // Same rationale as the read-only case: exclude the selector rather than
    // overriding the tint back, otherwise it still flashes on hover.
    const src = readFileSync(resolve(__dirname, '../AttachmentTags.vue'), 'utf8')
    const hoverRule = src.match(/@media \(hover: hover\) \{\s*\/\*[\s\S]*?\*\/\s*([^{]*\.attachment-ref:hover[^{]*)\{/)
    expect(hoverRule, 'the attachment hover rule must exist').not.toBeNull()
    expect(hoverRule![1]).toContain(':not(.is-not-clickable)')
  })
})
