import { beforeEach, describe, expect, it, vi } from 'vitest'
// vue-i18n tries to install its devtools plugin in dev builds; the rejection is
// unrelated to these assertions and leaks into the NEXT test file in the same
// worker (vitest then reports a file-level failure with zero failing tests).
// This suite does not use <i18n-t>, so skipping the full install is safe.
;(globalThis as { __VUE_I18N_FULL_INSTALL__?: boolean }).__VUE_I18N_FULL_INSTALL__ = false
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'

const h = vi.hoisted(() => ({
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiPatch: vi.fn(),
  apiDelete: vi.fn(),
  confirm: vi.fn().mockResolvedValue(true),
  toast: vi.fn(),
}))

vi.mock('@/utils/api', () => ({
  apiGet: h.apiGet,
  apiPost: h.apiPost,
  apiPatch: h.apiPatch,
  apiDelete: h.apiDelete,
}))
vi.mock('@/composables/useWideScreenLayout', () => ({
  useWideScreenLayout: () => ({ isWideScreen: { value: true } }),
}))
vi.mock('@/composables/useDialog', () => ({ useDialog: () => ({ confirm: h.confirm }) }))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: h.toast }) }))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))
vi.mock('@/stores/app', () => ({ store: { state: { projectRoot: '/p', currentFile: null } } }))
vi.mock('@/composables/useMarkdownRenderer.ts', () => ({
  renderMarkdownHtml: (text: string) => `<p>${text}</p>`,
}))

import KnowledgePanel from '@/components/kb/KnowledgePanel.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  missingWarn: false,
  fallbackWarn: false,
  messages: {
    en: {
      common: { loading: 'Loading', retry: 'Retry', cancel: 'Cancel', delete: 'Delete', close: 'Close', back: 'Back' },
      kb: {
        title: 'Knowledge base',
        count: '{count} collections',
        refresh: 'Refresh',
        newCollection: 'New collection',
        emptyCollections: 'No collections yet',
        emptyCollectionsDesc: 'Create one',
        globalGroup: 'Global',
        projectGroup: 'This project',
        scopeGlobal: 'Global',
        scopeProject: 'Project',
        autoInjectOn: 'Used in chat',
        autoInjectOff: 'Not used in chat',
        documents: '{count} files',
        emptyDocuments: 'No files',
        emptyDocumentsDesc: 'Add one',
        addFile: 'Add file',
        added: 'Added',
        addPartial: '{first} ({count} more failed)',
        statusPending: 'Queued',
        statusExtracting: 'Parsing',
        statusReady: 'Searchable',
        statusReadyFts: 'Keywords only',
        statusFailed: 'Failed',
        chunks: '{count} chunks',
        vectorReady: 'Semantic ready',
        vectorPending: 'Semantic indexing',
        warningTruncated: 'Truncated',
        searchPlaceholder: 'Search',
        searchThis: 'This collection',
        searchAll: 'All',
        modeHybrid: 'Hybrid',
        modeFts: 'Keywords',
        searchEmpty: 'Nothing found',
        searchEmptyDesc: 'Try again',
        results: '{count} results',
        inCollection: 'from {name}',
        reindex: 'Reindex',
        deleteCollection: 'Delete collection',
        loadFailed: 'Could not load',
        locationPage: 'p.{n}',
        locationSlide: 'Slide {n}',
        locationSheetRow: 'Sheet {name}, row {n}',
        locationSheetRows: 'Sheet {name}, rows {start}–{end}',
        locationLines: 'Lines {start}–{end}',
        locationLine: 'Line {n}',
        locationParagraphs: 'Paragraphs {start}–{end}',
        locationParagraph: 'Paragraph {n}',
        locationTitle: 'Title',
      },
    },
  },
})

function doc(id: number, over: Record<string, unknown> = {}) {
  return {
    id,
    collection_id: 2,
    title: `doc-${id}.txt`,
    size_bytes: 1024,
    status: 'ready',
    chunk_count: 3,
    fts_ready: true,
    vector_ready: id === 1,
    created_at: '',
    updated_at: '',
    ...over,
  }
}

describe('KnowledgePanel', () => {
  beforeEach(() => {
    h.apiGet.mockReset()
    h.apiPost.mockReset()
    h.apiPatch.mockReset()
    h.apiDelete.mockReset()
    h.confirm.mockResolvedValue(true)
    h.toast.mockReset()

    h.apiGet.mockImplementation(async (url: string) => {
      if (url === '/api/kb/collections') {
        return {
          collections: [
            { id: 1, name: 'Policies', description: '', scope: 'global', auto_inject: true, doc_count: 2, created_at: '', updated_at: '' },
            { id: 2, name: 'Notes', description: '', scope: 'project', auto_inject: true, doc_count: 2, created_at: '', updated_at: '' },
          ],
        }
      }
      if (url.startsWith('/api/kb/documents')) {
        return { documents: [doc(1), doc(2)] }
      }
      if (url === '/api/kb/status') {
        return { available: true, vector_enabled: false, stats: { collections: 2, documents: 2, ready_docs: 2, failed_docs: 0, pending_docs: 0, chunks: 6, embedded_chunks: 3, stored_bytes: 2048 } }
      }
      return {}
    })
  })

  function mountPanel() {
    return mount(KnowledgePanel, {
      props: { active: true },
      global: {
        plugins: [i18n],
        stubs: {
          AttachmentPicker: true,
          FilePicker: true,
          BottomSheet: true,
          ModalDialog: true,
          PanelSkeleton: true,
          RefreshButton: { template: '<button><slot /></button>' },
          SearchInput: {
            props: ['modelValue', 'placeholder'],
            emits: ['update:modelValue', 'enter'],
            template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" @keydown.enter="$emit(\'enter\')" />',
          },
        },
      },
    })
  }

  it('lists collections grouped by scope and shows document status', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('Policies')
    expect(text).toContain('Notes')
    expect(text).toContain('This project')
    expect(text).toContain('Global')

    const rows = wrapper.findAll('[data-kb-doc]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('doc-1.txt')
    expect(rows[0].text()).toContain('Searchable')
    // The second document has no vector yet: it is keyword-searchable only.
    expect(rows[1].text()).toContain('Keywords only')
    wrapper.unmount()
  })

  it('loads the documents of the collection the user picks', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // The project collection is selected first.
    expect(h.apiGet).toHaveBeenCalledWith('/api/kb/documents?collection_id=2')

    await wrapper.find('[data-kb-collection="1"]').trigger('click')
    await flushPromises()
    expect(h.apiGet).toHaveBeenCalledWith('/api/kb/documents?collection_id=1')
    wrapper.unmount()
  })

  it('renders highlighted search results without v-html', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    h.apiPost.mockResolvedValueOnce({
      results: [
        {
          chunk_id: 9,
          document_id: 1,
          document_title: 'doc-1.txt',
          collection_id: 2,
          collection_name: 'Notes',
          location: 'p.12',
          chunk_text: '报销流程：先提交申请',
          score: 1,
          match_positions: [{ start: 5, end: 10 }],
        },
      ],
      total: 1,
      mode: 'fts',
    })

    await wrapper.find('input').setValue('报销')
    await wrapper.find('[data-action="kb-search"]').trigger('click')
    await flushPromises()
    await nextTick()

    const hit = wrapper.find('[data-kb-hit="9"]')
    expect(hit.exists()).toBe(true)
    expect(hit.text()).toContain('p.12')
    expect(hit.find('mark').text()).toBe('先提交申请')
    wrapper.unmount()
  })

  it('adds every confirmed file through the shared attachment picker', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="kb-add"]').trigger('click')
    const picker = wrapper.findComponent({ name: 'AttachmentPicker' })
    expect(picker.exists()).toBe(true)
    expect(picker.props('open')).toBe(true)
    // The panel applies the list itself; the picker must not touch the chat
    // composer and must stay open until the add finishes.
    expect(picker.props('selectionOnly')).toBe(true)
    expect(picker.props('variant')).toBe('kb')

    picker.vm.$emit('confirm', [
      { path: 'docs/a.md', isDir: false },
      { path: 'docs/b.txt', isDir: false },
    ])
    await flushPromises()

    expect(h.apiPost).toHaveBeenCalledWith('/api/kb/documents', { collection_id: 2, path: 'docs/a.md' })
    expect(h.apiPost).toHaveBeenCalledWith('/api/kb/documents', { collection_id: 2, path: 'docs/b.txt' })
    expect(h.toast).toHaveBeenCalledWith('Added', expect.anything())
    expect(wrapper.findComponent({ name: 'AttachmentPicker' }).props('open')).toBe(false)
    wrapper.unmount()
  })

  it('keeps the picker open and reports the reason when a file is rejected', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    h.apiPost.mockRejectedValueOnce(new Error('already in the collection'))

    await wrapper.find('[data-action="kb-add"]').trigger('click')
    const picker = wrapper.findComponent({ name: 'AttachmentPicker' })
    picker.vm.$emit('confirm', [{ path: 'docs/a.md', isDir: false }])
    await flushPromises()

    expect(h.toast).toHaveBeenCalledWith('a.md: already in the collection', expect.anything())
    // Still open: the selection is intact so the user can drop the bad file.
    expect(wrapper.findComponent({ name: 'AttachmentPicker' }).props('open')).toBe(true)
    wrapper.unmount()
  })
})
