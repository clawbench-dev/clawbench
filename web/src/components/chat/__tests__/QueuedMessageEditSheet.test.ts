import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import QueuedMessageEditSheet from '../QueuedMessageEditSheet.vue'
import enLocale from '@/i18n/locales/en'
import type { QueuedMessage } from '@/composables/useMessageQueue'

const pendingFiles = ref<Array<Record<string, unknown>>>([])
const removeFile = vi.fn()
const removePendingByPath = vi.fn()

// AttachmentTags (rendered inside the sheet) reads the upload composable for its
// terminate button; the sheet itself only uses it to drop the upload mirror.
vi.mock('@/composables/useFileUpload', () => ({
  useFileUpload: () => ({ pendingFiles, removeFile, removePendingByPath, terminateChatUploadBatch: vi.fn() }),
}))

// The file picker owns uploads and is covered by its own tests; here it only
// has to exist as the place "add attachment" opens, and to forward its events.
const AttachDrawerStub = defineComponent({
  name: 'AttachDrawer',
  props: { open: Boolean, attachedFiles: { type: Array, default: () => [] } },
  emits: ['close', 'add-attached', 'remove-attached'],
  setup(_props, { expose }) {
    expose({})
    return () => h('div', { class: 'attach-drawer-stub' })
  },
})

// BottomSheet teleports to <body> and animates; the sheet's own contract is the
// draft/save behaviour, so the shell is replaced by a plain conditional wrapper
// that still renders both slots (the footer holds the save button).
const BottomSheetStub = defineComponent({
  name: 'BottomSheet',
  props: { open: Boolean, title: { type: String, default: '' } },
  emits: ['close'],
  setup(props, { slots }) {
    return () => (props.open
      ? h('div', { class: 'bs-stub' }, [slots.default?.(), slots.footer?.()])
      : null)
  },
})

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enLocale } })

function entry(overrides: Partial<QueuedMessage> = {}): QueuedMessage {
  return {
    queueId: 'q-1',
    text: 'original text',
    files: [{ path: '/project/a.png', isDir: false }],
    createdAt: '',
    ...overrides,
  }
}

function mountSheet(props: Record<string, unknown> = {}) {
  return mount(QueuedMessageEditSheet, {
    props: { open: true, entry: entry(), saving: false, ...props },
    global: {
      plugins: [i18n],
      stubs: { BottomSheet: BottomSheetStub, AttachDrawer: AttachDrawerStub },
    },
  })
}

beforeEach(() => {
  pendingFiles.value = []
  removeFile.mockReset()
  removePendingByPath.mockReset()
})

describe('QueuedMessageEditSheet (draft seeding)', () => {
  it('seeds the textarea and attachment chips from the entry when opened', async () => {
    const wrapper = mountSheet({ entry: entry({ files: [{ path: '/project/notes.md' }] }) })
    await nextTick()

    expect((wrapper.find('.qme-textarea').element as HTMLTextAreaElement).value).toBe('original text')
    // AttachmentTags renders one card per file entry (a non-image shows its name).
    const chips = wrapper.findAll('.chat-file-attachment')
    expect(chips).toHaveLength(1)
    expect(chips[0].text()).toContain('notes.md')
  })

  it('renders quote attachments too (AttachmentTags drops them)', async () => {
    const wrapper = mountSheet({
      entry: entry({
        files: [
          { path: 'main.go:12', kind: 'quote', id: 'quote-1', text: 'func main() {}' },
          { path: '/project/b.png' },
        ],
      }),
    })
    await nextTick()

    const quoteCards = wrapper.findAll('.attachment-quote')
    expect(quoteCards, 'a quote is an attachment the user may remove').toHaveLength(1)
    expect(quoteCards[0].text()).toContain('main.go:12')
    // The non-quote file still renders through the shared attachment strip, so
    // the two kinds sit side by side rather than one replacing the other.
    expect(wrapper.findAll('.chat-file-attachment:not(.attachment-quote)')).toHaveLength(1)
  })

  it('does NOT re-seed while open when the entry prop changes', async () => {
    // Another device can edit the same entry mid-typing (queue_added upsert).
    // Re-seeding then would overwrite the draft under the user's cursor.
    const wrapper = mountSheet()
    await nextTick()
    await wrapper.find('.qme-textarea').setValue('my in-progress edit')

    await wrapper.setProps({ entry: entry({ text: 'changed by another device' }) })
    await nextTick()

    expect((wrapper.find('.qme-textarea').element as HTMLTextAreaElement).value).toBe('my in-progress edit')
  })

  it('re-seeds on the next OPEN (the draft is not carried over)', async () => {
    const wrapper = mountSheet()
    await nextTick()
    await wrapper.find('.qme-textarea').setValue('temporary')

    await wrapper.setProps({ open: false })
    await wrapper.setProps({ entry: entry({ text: 'next entry', files: [] }), open: true })
    await nextTick()

    expect((wrapper.find('.qme-textarea').element as HTMLTextAreaElement).value).toBe('next entry')
    expect(wrapper.findAll('.chat-file-attachment')).toHaveLength(0)
  })
})

describe('QueuedMessageEditSheet (attachments)', () => {
  it('emits the remaining attachments after one is removed', async () => {
    const wrapper = mountSheet({
      entry: entry({ files: [{ path: '/project/a.png' }, { path: '/project/b.png' }] }),
    })
    await nextTick()

    const removeButtons = wrapper.findAll('.chat-file-attachment .attachment-close-btn')
    expect(removeButtons).toHaveLength(2)
    await removeButtons[0].trigger('click')
    await wrapper.find('[data-action="queue-edit-save"]').trigger('click')

    const payload = wrapper.emitted('save')![0][0] as { text: string; files: Array<{ path: string }> }
    expect(payload.files.map((f) => f.path)).toEqual(['/project/b.png'])
  })

  it('adds a file picked in the drawer, deduping the same path', async () => {
    const wrapper = mountSheet({ entry: entry({ files: [] }) })
    await nextTick()

    const drawer = wrapper.findComponent(AttachDrawerStub)
    drawer.vm.$emit('add-attached', '/project/new.pdf', false)
    drawer.vm.$emit('add-attached', '/project/new.pdf', false)
    await nextTick()

    expect(wrapper.findAll('.chat-file-attachment')).toHaveLength(1)
    await wrapper.find('[data-action="queue-edit-save"]').trigger('click')

    const payload = wrapper.emitted('save')![0][0] as { files: Array<{ path: string }> }
    expect(payload.files).toEqual([{ path: '/project/new.pdf', isDir: false }])
  })

  it('removes a quote attachment through its card’s remove control', async () => {
    const wrapper = mountSheet({
      entry: entry({ files: [{ path: 'main.go:12', kind: 'quote', id: 'quote-1', text: 'x' }] }),
    })
    await nextTick()

    await wrapper.find('.attachment-quote .attachment-close-btn').trigger('click')
    await wrapper.find('[data-action="queue-edit-save"]').trigger('click')

    const payload = wrapper.emitted('save')![0][0] as { files: unknown[] }
    expect(payload.files).toEqual([])
  })

  it('drops the upload mirror so the file is not attached to the next message', async () => {
    // The shared picker parks the finished file in `pendingFiles`, which is the
    // chat input's send payload. It belongs to the queued entry, so the mirror
    // must go — otherwise the next ordinary message carries this file too.
    const wrapper = mountSheet({ entry: entry({ files: [] }) })
    await nextTick()

    wrapper.findComponent(AttachDrawerStub).vm.$emit('add-attached', '/project/new.pdf', false)
    await nextTick()

    expect(removePendingByPath).toHaveBeenCalledWith('/project/new.pdf')
  })

  it('removes a line-ranged attachment toggled off by path in the picker', async () => {
    // AttachDrawer reports removal as {path} only, while the entry carries a
    // line range — the composite key never matches, so a path fallback is what
    // makes "untick in the drawer" work.
    const wrapper = mountSheet({
      entry: entry({ files: [{ path: '/project/main.go', startLine: 10, endLine: 20 }] }),
    })
    await nextTick()

    wrapper.findComponent(AttachDrawerStub).vm.$emit('remove-attached', { path: '/project/main.go' })
    await nextTick()

    expect(wrapper.findAll('.chat-file-attachment')).toHaveLength(0)
  })

  it('renders its attachment cards as removable but not clickable', async () => {
    // This sheet has no viewer, so a card click used to emit `file-click` into
    // the void while the card still showed a hand cursor and an "open file"
    // tooltip. The fix must NOT be `readonly`: that drops the "×", and removing
    // attachments is the whole reason this sheet exists.
    const wrapper = mountSheet({ entry: entry({ files: [{ path: '/project/a.png' }] }) })
    await nextTick()

    const strip = wrapper.find('.chat-attachment-tags')
    expect(strip.classes()).toContain('is-not-clickable')
    expect(strip.classes(), 'not read-only: the × must stay').not.toContain('is-readonly')
    expect(wrapper.findAll('.chat-file-attachment .attachment-close-btn')).toHaveLength(1)
  })

  it('keeps the v-for key identity-based (a path key would collapse two quotes)', () => {
    // jsdom renders duplicate keys without failing, so the removal test alone
    // cannot catch a key regression — pin the binding at the source.
    const src = readFileSync(resolve(__dirname, '../QueuedMessageEditSheet.vue'), 'utf8')
    expect(src).toMatch(/:key="fileEntryKey\(q\)"/)
  })

  it('opens the drawer from the add button', async () => {
    const wrapper = mountSheet()
    await nextTick()

    expect(wrapper.findComponent(AttachDrawerStub).props('open')).toBe(false)
    await wrapper.find('[data-action="queue-edit-add-attachment"]').trigger('click')
    expect(wrapper.findComponent(AttachDrawerStub).props('open')).toBe(true)
  })

  it('puts the add control FIRST in the row, even when cards exist', async () => {
    const wrapper = mountSheet({
      entry: entry({ files: [{ path: '/project/a.png' }, { path: '/project/b.png' }] }),
    })
    await nextTick()

    const row = wrapper.find('.qme-chips')
    expect(row.exists()).toBe(true)
    const first = row.element.firstElementChild as HTMLElement
    expect(first.getAttribute('data-action'), 'add is always the first tile').toBe('queue-edit-add-attachment')
  })

  it('sizes the add control like the attachment cards and drops the section label', () => {
    // jsdom has no CSS engine, so the geometry and the removed label are pinned
    // at the source. Same 40dp height / lg radius as the shared cards is what
    // makes the row read as one group of tiles.
    const src = readFileSync(resolve(__dirname, '../QueuedMessageEditSheet.vue'), 'utf8')
    const m = src.match(/\.qme-add\s*\{([^}]*)\}/)
    expect(m, '.qme-add rule must exist').not.toBeNull()
    expect(m![1], 'same height as the cards').toMatch(/height:\s*calc\(40 \* var\(--dp\)\)/)
    expect(m![1], 'same corner radius as the cards').toMatch(/border-radius:\s*var\(--radius-lg\)/)
    // The "附件" label above the row is gone — the tiles say what they are.
    expect(src, 'no section label').not.toContain('qme-section-label')
  })

  it('keeps two quotes of the same range apart', async () => {
    // Both are message quotes: same (empty) label path, different ids. Keying
    // them by path would collapse the v-for keys and removing one would drop
    // both.
    const wrapper = mountSheet({
      entry: entry({
        files: [
          { path: '', kind: 'quote', id: 'quote-a', text: 'first quote' },
          { path: '', kind: 'quote', id: 'quote-b', text: 'second quote' },
        ],
      }),
    })
    await nextTick()

    const chips = wrapper.findAll('.attachment-quote')
    expect(chips).toHaveLength(2)
    await chips[0].find('.attachment-close-btn').trigger('click')
    await wrapper.find('[data-action="queue-edit-save"]').trigger('click')

    const payload = wrapper.emitted('save')![0][0] as { files: Array<{ id?: string }> }
    expect(payload.files.map((f) => f.id)).toEqual(['quote-b'])
  })

  it('does not surface or cancel the input bar’s in-flight uploads', async () => {
    // pendingFiles is a module-level singleton shared with the chat input. The
    // sheet must not render those uploads (they belong to another message) and
    // must never cancel one when the user removes an attachment here.
    pendingFiles.value = [{ path: '', uploading: true, progress: 10, isImage: false, previewUrl: null, size: 1 }]
    const wrapper = mountSheet()
    await nextTick()

    expect(wrapper.find('.attachment-pending').exists()).toBe(false)
    await wrapper.findAll('.chat-file-attachment .attachment-close-btn')[0].trigger('click')
    expect(removeFile).not.toHaveBeenCalled()
  })
})

describe('QueuedMessageEditSheet (save)', () => {
  it('emits trimmed text and the full attachment list', async () => {
    const wrapper = mountSheet()
    await nextTick()

    await wrapper.find('.qme-textarea').setValue('  revised  ')
    await wrapper.find('[data-action="queue-edit-save"]').trigger('click')

    const payload = wrapper.emitted('save')![0][0] as { text: string; files: Array<{ path: string }> }
    expect(payload.text).toBe('revised')
    expect(payload.files.map((f) => f.path)).toEqual(['/project/a.png'])
  })

  it('saves with Ctrl+Enter', async () => {
    const wrapper = mountSheet()
    await nextTick()

    await wrapper.find('.qme-textarea').setValue('keyboard save')
    await wrapper.find('.qme-textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })

    expect(wrapper.emitted('save')).toHaveLength(1)
  })

  it('refuses to save an entry with neither text nor attachments', async () => {
    // Such a row would drain into an empty turn. The backend rejects it too; the
    // point of failing here is that the sheet stays open with the draft intact.
    const wrapper = mountSheet({ entry: entry({ text: '', files: [] }) })
    await nextTick()

    await wrapper.find('[data-action="queue-edit-save"]').trigger('click')

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.find('.qme-error').text()).toBe('A message needs text or an attachment')
  })

  it('allows saving an attachment-only entry', async () => {
    const wrapper = mountSheet({ entry: entry({ text: '   ', files: [{ path: '/project/a.png' }] }) })
    await nextTick()

    await wrapper.find('[data-action="queue-edit-save"]').trigger('click')

    const payload = wrapper.emitted('save')![0][0] as { text: string }
    expect(payload.text).toBe('')
  })

  it('disables the save button while a save is in flight', async () => {
    const wrapper = mountSheet({ saving: true })
    await nextTick()
    expect(wrapper.find('[data-action="queue-edit-save"]').attributes('disabled')).toBeDefined()
  })

  it('ignores close while a save is in flight', async () => {
    // Closing mid-request would discard a draft whose save may still land.
    const wrapper = mountSheet({ saving: true })
    await nextTick()
    await wrapper.find('.fbtn').trigger('click')
    expect(wrapper.emitted('close')).toBeUndefined()
  })
})
