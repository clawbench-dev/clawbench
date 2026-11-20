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
  apiPostForm: vi.fn(),
  confirm: vi.fn().mockResolvedValue(true),
  prompt: vi.fn(),
  toast: vi.fn(),
  uploadFilesToCollection: vi.fn(),
}))

vi.mock('@/utils/api', () => ({
  apiGet: h.apiGet,
  apiPost: h.apiPost,
  apiPatch: h.apiPatch,
  apiDelete: h.apiDelete,
  apiPostForm: h.apiPostForm,
}))
vi.mock('@/composables/useWideScreenLayout', () => ({
  useWideScreenLayout: () => ({ isWideScreen: { value: true } }),
}))
vi.mock('@/composables/useDialog', () => ({ useDialog: () => ({ confirm: h.confirm, prompt: h.prompt }) }))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: h.toast }) }))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))
vi.mock('@/stores/app', async () => {
  const { reactive } = await import('vue')
  return { store: reactive({ state: { projectRoot: '/p', currentFile: null } }) }
})
vi.mock('@/composables/useMarkdownRenderer.ts', () => ({
  renderMarkdownHtml: (text: string) => `<p>${text}</p>`,
}))
vi.mock('@/composables/useKBIndexStatus', () => ({ startKBIndexWatch: vi.fn() }))
// The panel now owns a desktop-drop upload. The real composable talks XHR and
// holds module-level singletons; here only the contract matters (which files,
// which library, what the panel does with the outcome).
vi.mock('@/composables/useFileUpload', async () => {
  const { ref } = await import('vue')
  return {
    useFileUpload: () => ({
      dirUploading: ref(false),
      dirUploadProgress: ref(0),
      dirUploadSpeed: ref(0),
      dirUploadTotal: ref(0),
      dirUploadDone: ref(0),
      cancelDirUpload: vi.fn(),
      uploadFilesToCollection: h.uploadFilesToCollection,
    }),
  }
})

import KnowledgePanel from '@/components/kb/KnowledgePanel.vue'
import { store } from '@/stores/app'

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
        dropSelectCollection: 'Select a knowledge base before dropping files',
        dropToAdd: 'Drop to add to this knowledge base',
        addToCollection: 'Add to knowledge base',
        dirImportDone: 'Added {added} files',
        dirImportSummary: '{added} added, {skipped} skipped, {duplicates} already there, {failed} failed',
        dirImportTruncated: 'Too many files',
        dirImportEmpty: 'No files to add',
        dirImportAllDuplicates: 'These files are already there',
        importPreviewTitle: 'Import folder',
        importPreviewMessage: '{added} to add, {skipped} skipped, {duplicates} already there, {failed} failed. Continue?',
        importPreviewTruncated: 'Only the first 500 will be imported',
        importContinue: 'Import',
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
        editCollection: 'Edit collection',
        editTitle: 'Edit collection',
        save: 'Save',
        saved: 'Saved',
        healthFiles: '{count} files',
        healthFailed: '{count} failed',
        healthWorking: '{count} indexing',
        healthFts: '{count} keyword-only',
        filterClear: 'Clear filter',
        filterEmpty: 'No files match this filter',
        colFailed: '{count} failed',
        colPending: '{count} indexing',
        indexOffHint: 'Keyword search only',
        indexDocsProgress: 'Parsing {done} / {total}',
        indexVectorsProgress: 'Embedding {done} / {total}',
        indexFailedCount: '{count} failed',
        indexReady: 'Index ready',
        modeVector: 'Semantic',
        openDocument: 'View parsed content',
        searchScopeTitle: 'Search scope',
        nameRequired: 'Name required',
        select: 'Select',
        selectAll: 'Select all',
        selectNone: 'Select none',
        selectedCount: '{count} selected',
        tagsPlaceholder: 'Tags',
        deletedUndo: 'Deleted — click to re-add',
        undoDone: 'Re-added',
        undoFailed: 'Could not undo',
        tagsSave: 'Save tags',
        tagsSaved: 'Tags saved',
        filterTags: 'Tag filter',
        sourceStale: 'Original changed',
        sourceMissing: 'Original missing',
        sourceStaleHint: 'Original is newer',
        sourceMissingHint: 'Original is gone',
        filterCollections: 'Filter knowledge bases',
        filterCollectionsEmpty: 'No matching knowledge bases',
        pin: 'Pin',
        unpin: 'Unpin',
        move: 'Move',
        moveDone: 'Moved {count} to {name}',
        pickEmpty: 'No other libraries',
        sortRecent: 'Recently added',
        sortName: 'Name',
        sortBy: 'Sort',
        deleteSelectedConfirm: 'Delete {count}?',
        deleteSelectedDone: 'Deleted {count}',
        reindexSelectedConfirm: 'Reindex {count}?',
        reindexSelectedDone: 'Reindexing {count}',
        showMore: 'Show more ({count} remaining)',
        pasteTitle: 'Paste text',
        pasteNameLabel: 'Title (optional)',
        pasteNamePlaceholder: 'e.g. Meeting notes',
        pasteBodyLabel: 'Content',
        pastePlaceholder: 'Paste text',
        pasteConfirm: 'Add to knowledge base',
        pasteEmpty: 'Content cannot be empty',
        pasteDefaultTitle: 'Pasted note',
        pasteDone: 'Added',
        urlTitle: 'Add from link',
        urlPrompt: 'Paste a web address',
        urlConfirm: 'Fetch and add',
        urlDone: 'Added from link',
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
    h.apiPostForm.mockReset()
    h.confirm.mockReset()
    h.prompt.mockReset()
    h.confirm.mockResolvedValue(true)
    h.toast.mockReset()
    h.uploadFilesToCollection.mockReset()
    h.uploadFilesToCollection.mockResolvedValue({ added: 0, failed: 0, errors: [], cancelled: false })
    store.state.projectRoot = '/p'

    h.apiGet.mockImplementation(async (url: string) => {
      if (url === '/api/kb/collections') {
        return {
          collections: [
            { id: 1, name: 'Policies', description: '', scope: 'global', auto_inject: true, doc_count: 2, failed_count: 0, pending_count: 0, created_at: '', updated_at: '' },
            { id: 2, name: 'Notes', description: '', scope: 'project', auto_inject: true, doc_count: 2, failed_count: 1, pending_count: 0, created_at: '', updated_at: '' },
          ],
        }
      }
      if (url.startsWith('/api/kb/documents')) {
        return { documents: [doc(1), doc(2)] }
      }
      if (url.startsWith('/api/kb/status')) {
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
          ModalDialog: {
            props: ['open'],
            template: '<div v-if="open"><slot name="header" /><slot /><slot name="footer" /></div>',
          },
          KBDocumentDrawer: {
            name: 'KBDocumentDrawer',
            props: ['open', 'documentId', 'focusChunkId'],
            template: '<div />',
          },
          KBCollectionPickerDialog: {
            name: 'KBCollectionPickerDialog',
            props: ['open', 'initialName', 'allowCreate', 'excludeId'],
            template: '<div />',
          },
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
    // With a semantic index configured, a ready document that has no vector
    // yet is keyword-searchable only.
    const base = h.apiGet.getMockImplementation()!
    h.apiGet.mockImplementation(async (url: string) => {
      if (url.startsWith('/api/kb/status')) {
        return { available: true, vector_enabled: true, stats: { collections: 2, documents: 2, ready_docs: 2, failed_docs: 0, pending_docs: 0, chunks: 6, embedded_chunks: 3, stored_bytes: 2048 } }
      }
      return base(url)
    })
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

  it('does not report semantic indexing for every file when no semantic index exists', async () => {
    // Default fixture: vector_enabled is false, so `vector_ready` is false for
    // every document. That is the library's normal state, not a backlog.
    const wrapper = mountPanel()
    await flushPromises()

    const rows = wrapper.findAll('[data-kb-doc]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('Searchable')
    expect(rows[1].text()).toContain('Searchable')

    const text = wrapper.text()
    expect(text).not.toContain('Semantic indexing')
    expect(text).not.toContain('Keywords only')
    expect(text).not.toContain('keyword-only')
    expect(text).toContain('Keyword search only')
    wrapper.unmount()
  })

  it('does not start polling for vectors when no semantic index exists', async () => {
    const intervalSpy = vi.spyOn(globalThis, 'setInterval')
    const wrapper = mountPanel()
    await flushPromises()
    const polled = intervalSpy.mock.calls.some(([, ms]) => ms === 2500)
    intervalSpy.mockRestore()
    expect(polled).toBe(false)
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

  it('previews a folder, then imports it in one request', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const counts = { added: 3, duplicates: 1, skipped: 2, failed: 0, truncated: false }
    h.apiPost.mockResolvedValueOnce(counts).mockResolvedValueOnce(counts)

    await wrapper.find('[data-action="kb-add"]').trigger('click')
    const picker = wrapper.findComponent({ name: 'AttachmentPicker' })
    picker.vm.$emit('confirm', [{ path: 'docs', isDir: true }])
    await flushPromises()

    expect(h.apiPost).toHaveBeenNthCalledWith(
      1,
      '/api/kb/documents/import',
      { collection_id: 2, path: 'docs', dry_run: true },
      expect.objectContaining({ timeoutMs: expect.any(Number) }),
    )
    expect(h.confirm).toHaveBeenCalledWith(
      '3 to add, 2 skipped, 1 already there, 0 failed. Continue?',
      expect.objectContaining({ confirmText: 'Import' }),
    )
    expect(h.apiPost).toHaveBeenNthCalledWith(
      2,
      '/api/kb/documents/import',
      { collection_id: 2, path: 'docs' },
      expect.objectContaining({ timeoutMs: expect.any(Number) }),
    )
    expect(h.toast).toHaveBeenCalledWith(
      '3 added, 2 skipped, 1 already there, 0 failed',
      expect.objectContaining({ type: 'success' }),
    )
    // Folder imports close even when some files were skipped: retrying the
    // same folder would only re-add what already landed.
    expect(wrapper.findComponent({ name: 'AttachmentPicker' }).props('open')).toBe(false)
    wrapper.unmount()
  })

  it('keeps the sheet open when the import preview is declined', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    h.confirm.mockResolvedValueOnce(false)
    h.apiPost.mockResolvedValueOnce({ added: 5, duplicates: 0, skipped: 1, failed: 0, truncated: false })

    await wrapper.find('[data-action="kb-add"]').trigger('click')
    const picker = wrapper.findComponent({ name: 'AttachmentPicker' })
    picker.vm.$emit('confirm', [{ path: 'docs', isDir: true }])
    await flushPromises()

    // Only the preview ran; the selection is still there to adjust.
    expect(h.apiPost).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent({ name: 'AttachmentPicker' }).props('open')).toBe(true)
    wrapper.unmount()
  })

  it('does not import a folder whose files are all duplicates', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    h.apiPost.mockResolvedValueOnce({ added: 0, duplicates: 4, skipped: 0, failed: 0, truncated: false })

    await wrapper.find('[data-action="kb-add"]').trigger('click')
    const picker = wrapper.findComponent({ name: 'AttachmentPicker' })
    picker.vm.$emit('confirm', [{ path: 'docs', isDir: true }])
    await flushPromises()

    expect(h.apiPost).toHaveBeenCalledTimes(1)
    expect(h.confirm).not.toHaveBeenCalled()
    expect(h.toast).toHaveBeenCalledWith('These files are already there', expect.anything())
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

  it('shows collection health and filters the failed files', async () => {
    h.apiGet.mockImplementation(async (url: string) => {
      if (url === '/api/kb/collections') {
        return {
          collections: [
            { id: 1, name: 'Policies', description: '', scope: 'global', auto_inject: true, doc_count: 1, failed_count: 0, pending_count: 0, created_at: '', updated_at: '' },
            { id: 2, name: 'Notes', description: '', scope: 'project', auto_inject: true, doc_count: 2, failed_count: 1, pending_count: 0, created_at: '', updated_at: '' },
          ],
        }
      }
      if (url.startsWith('/api/kb/documents')) {
        return { documents: [doc(1), doc(2, { status: 'failed', error: 'no_text', chunk_count: 0 })] }
      }
      if (url.startsWith('/api/kb/status')) {
        return { available: true, vector_enabled: true, stats: { collections: 2, documents: 2, ready_docs: 1, failed_docs: 1, pending_docs: 0, chunks: 3, embedded_chunks: 3, stored_bytes: 2048 } }
      }
      return {}
    })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('[data-kb-collection="2"]').text()).toContain('1 failed')
    expect(wrapper.text()).toContain('2 files')
    expect(wrapper.text()).toContain('1 failed')

    await wrapper.find('[data-action="kb-filter-failed"]').trigger('click')
    expect(wrapper.findAll('[data-kb-doc]')).toHaveLength(1)
    expect(wrapper.find('[data-kb-doc="2"]').exists()).toBe(true)

    await wrapper.find('[data-action="kb-filter-failed"]').trigger('click')
    expect(wrapper.findAll('[data-kb-doc]')).toHaveLength(2)
    wrapper.unmount()
  })

  it('says when only keyword search is available', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.text()).toContain('Keyword search only')
    wrapper.unmount()
  })

  it('opens the parsed content when a file row is clicked', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-kb-doc="1"] .kb-doc-open').trigger('click')
    const drawer = wrapper.findComponent({ name: 'KBDocumentDrawer' })
    expect(drawer.props('open')).toBe(true)
    expect(drawer.props('documentId')).toBe(1)
    expect(drawer.props('focusChunkId')).toBe(0)
    wrapper.unmount()
  })

  it('opens a search hit at the matched passage', async () => {
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
          chunk_text: '报销流程',
          score: 1,
        },
      ],
      total: 1,
      mode: 'fts',
    })
    await wrapper.find('input').setValue('报销')
    await wrapper.find('[data-action="kb-search"]').trigger('click')
    await flushPromises()

    await wrapper.find('[data-kb-hit="9"]').trigger('click')
    const drawer = wrapper.findComponent({ name: 'KBDocumentDrawer' })
    expect(drawer.props('documentId')).toBe(1)
    expect(drawer.props('focusChunkId')).toBe(9)
    wrapper.unmount()
  })

  it('reloads the panel when the project changes', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    const before = h.apiGet.mock.calls.filter((c) => c[0] === '/api/kb/collections').length

    store.state.projectRoot = '/q'
    await flushPromises()

    const after = h.apiGet.mock.calls.filter((c) => c[0] === '/api/kb/collections').length
    expect(after).toBe(before + 1)
    // The new project's collection is selected again, not the previous one.
    expect(h.apiGet).toHaveBeenCalledWith('/api/kb/documents?collection_id=2')
    wrapper.unmount()
  })

  it('renames the selected collection', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="kb-edit-collection"]').trigger('click')
    await wrapper.find('[data-role="kb-name"]').setValue('Renamed')
    h.apiPatch.mockResolvedValueOnce({
      id: 2,
      name: 'Renamed',
      description: '',
      scope: 'project',
      auto_inject: true,
      doc_count: 2,
      failed_count: 1,
      pending_count: 0,
      created_at: '',
      updated_at: '',
    })
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    expect(h.apiPatch).toHaveBeenCalledWith('/api/kb/collections?id=2', {
      name: 'Renamed',
      description: '',
    })
    wrapper.unmount()
  })

  it('adds pasted text as a document in the selected collection', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="kb-paste-text"]').trigger('click')
    await wrapper.find('[data-role="kb-text-title"]').setValue('会议结论')
    await wrapper.find('[data-role="kb-text-body"]').setValue('第一行\n第二行')
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    expect(h.apiPostForm).toHaveBeenCalledWith('/api/kb/documents/upload', expect.any(FormData))
    const form = h.apiPostForm.mock.calls[0][1] as FormData
    expect(form.get('collection_id')).toBe('2')
    const file = form.get('file') as File
    expect(file.name).toBe('会议结论.md')
    wrapper.unmount()
  })

  it('adds a document from a URL', async () => {
    h.prompt.mockResolvedValue('https://example.com/policy')
    h.apiPost.mockResolvedValueOnce({ id: 5, title: 'policy.html' })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="kb-add-url"]').trigger('click')
    await flushPromises()

    expect(h.prompt).toHaveBeenCalledWith(
      'Paste a web address',
      expect.objectContaining({ singleLine: true }),
    )
    expect(h.apiPost).toHaveBeenCalledWith('/api/kb/documents/url', {
      collection_id: 2,
      url: 'https://example.com/policy',
    })
    wrapper.unmount()
  })

  it('does nothing when the URL prompt is cancelled', async () => {
    h.prompt.mockResolvedValue(null)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="kb-add-url"]').trigger('click')
    await flushPromises()

    expect(h.apiPost).not.toHaveBeenCalledWith('/api/kb/documents/url', expect.anything())
    wrapper.unmount()
  })

  it('refuses to add empty pasted text', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="kb-paste-text"]').trigger('click')
    await wrapper.find('[data-role="kb-text-body"]').setValue('   ')
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    expect(h.apiPostForm).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Content cannot be empty')
    wrapper.unmount()
  })

  it('marks a file whose original changed or disappeared', async () => {
    h.apiGet.mockImplementation(async (url: string) => {
      if (url === '/api/kb/collections') {
        return {
          collections: [
            { id: 2, name: 'Notes', description: '', scope: 'project', auto_inject: true, doc_count: 2, failed_count: 0, pending_count: 0, created_at: '', updated_at: '' },
          ],
        }
      }
      if (url.startsWith('/api/kb/documents')) {
        return { documents: [doc(1, { source_stale: true }), doc(2, { source_missing: true })] }
      }
      if (url.startsWith('/api/kb/status')) {
        return { available: true, vector_enabled: false, stats: { collections: 1, documents: 2, ready_docs: 2, failed_docs: 0, pending_docs: 0, chunks: 6, embedded_chunks: 0, stored_bytes: 2048 } }
      }
      return {}
    })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('[data-kb-doc="1"]').text()).toContain('Original changed')
    expect(wrapper.find('[data-kb-doc="2"]').text()).toContain('Original missing')
    wrapper.unmount()
  })

  it('filters the collection rail by name', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('.kb-col-search').setValue('Policies')
    expect(wrapper.find('[data-kb-collection="1"]').exists()).toBe(true)
    expect(wrapper.find('[data-kb-collection="2"]').exists()).toBe(false)

    await wrapper.find('.kb-col-search').setValue('nothing')
    expect(wrapper.text()).toContain('No matching knowledge bases')
    wrapper.unmount()
  })

  it('sorts collections by name', async () => {
    h.apiGet.mockImplementation(async (url: string) => {
      if (url === '/api/kb/collections') {
        return {
          collections: [
            { id: 1, name: 'Beta', description: '', scope: 'global', auto_inject: true, doc_count: 0, failed_count: 0, pending_count: 0, created_at: '', updated_at: '' },
            { id: 2, name: 'Alpha', description: '', scope: 'global', auto_inject: true, doc_count: 0, failed_count: 0, pending_count: 0, created_at: '', updated_at: '' },
          ],
        }
      }
      if (url.startsWith('/api/kb/documents')) return { documents: [] }
      if (url.startsWith('/api/kb/status')) {
        return { available: true, vector_enabled: false, stats: { collections: 2, documents: 0, ready_docs: 0, failed_docs: 0, pending_docs: 0, chunks: 0, embedded_chunks: 0, stored_bytes: 0 } }
      }
      return {}
    })
    const wrapper = mountPanel()
    await flushPromises()

    const names = () => wrapper.findAll('.kb-col-name').map((n) => n.text())
    expect(names()).toEqual(['Beta', 'Alpha'])

    await wrapper.find('.kb-col-sort').setValue('name')
    expect(names()).toEqual(['Alpha', 'Beta'])
    wrapper.unmount()
  })

  it('selects several files and deletes them in one action', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="kb-select-mode"]').trigger('click')
    await wrapper.find('[data-kb-doc="1"] .kb-doc-open').trigger('click')
    await wrapper.find('[data-kb-doc="2"] .kb-doc-open').trigger('click')

    expect(wrapper.text()).toContain('2 selected')
    // Selecting must not open the document drawer.
    expect(wrapper.findComponent({ name: 'KBDocumentDrawer' }).props('open')).toBe(false)

    await wrapper.find('[data-action="kb-select-delete"]').trigger('click')
    await flushPromises()

    expect(h.confirm).toHaveBeenCalledWith('Delete 2?', expect.objectContaining({ dangerous: true }))
    expect(h.apiDelete).toHaveBeenCalledWith('/api/kb/documents?id=1')
    expect(h.apiDelete).toHaveBeenCalledWith('/api/kb/documents?id=2')
    // Back to the normal toolbar.
    expect(wrapper.find('[data-action="kb-select-mode"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('filters documents by tag', async () => {
    h.apiGet.mockImplementation(async (url: string) => {
      if (url === '/api/kb/collections') {
        return { collections: [{ id: 2, name: 'Notes', description: '', scope: 'project', auto_inject: true, doc_count: 2, failed_count: 0, pending_count: 0, created_at: '', updated_at: '' }] }
      }
      if (url.startsWith('/api/kb/documents')) {
        return { documents: [doc(1, { tags: '制度, 报销' }), doc(2, { tags: '产品' })] }
      }
      if (url.startsWith('/api/kb/status')) {
        return { available: true, vector_enabled: false, stats: { collections: 1, documents: 2, ready_docs: 2, failed_docs: 0, pending_docs: 0, chunks: 6, embedded_chunks: 0, stored_bytes: 2048 } }
      }
      return {}
    })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.findAll('[data-kb-doc]')).toHaveLength(2)
    await wrapper.find('.kb-health-tag').setValue('制度')
    expect(wrapper.findAll('[data-kb-doc]')).toHaveLength(1)
    expect(wrapper.find('[data-kb-doc="1"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('offers to re-add a deleted file from its source', async () => {
    h.apiGet.mockImplementation(async (url: string) => {
      if (url === '/api/kb/collections') {
        return { collections: [{ id: 2, name: 'Notes', description: '', scope: 'project', auto_inject: true, doc_count: 1, failed_count: 0, pending_count: 0, created_at: '', updated_at: '' }] }
      }
      if (url.startsWith('/api/kb/documents')) {
        return { documents: [doc(1, { source_path: '/p/a.md' })] }
      }
      if (url.startsWith('/api/kb/status')) {
        return { available: true, vector_enabled: false, stats: { collections: 1, documents: 1, ready_docs: 1, failed_docs: 0, pending_docs: 0, chunks: 3, embedded_chunks: 0, stored_bytes: 1024 } }
      }
      return {}
    })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-kb-doc="1"] [data-action="kb-delete-doc"]').trigger('click')
    await flushPromises()

    expect(h.apiDelete).toHaveBeenCalledWith('/api/kb/documents?id=1')
    const call = h.toast.mock.calls.find((c) => c[0] === 'Deleted — click to re-add')
    expect(call).toBeTruthy()
    const opts = call![1] as { onClick?: () => void }
    expect(typeof opts.onClick).toBe('function')

    opts.onClick!()
    await flushPromises()
    expect(h.apiPost).toHaveBeenCalledWith('/api/kb/documents', { collection_id: 2, path: '/p/a.md' })
    wrapper.unmount()
  })

  it('saves tags emitted by the drawer', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    wrapper.findComponent({ name: 'KBDocumentDrawer' }).vm.$emit('save-tags', { id: 1, tags: '制度' })
    await flushPromises()

    expect(h.apiPatch).toHaveBeenCalledWith('/api/kb/documents?id=1', { tags: '制度' })
    wrapper.unmount()
  })

  it('moves the selected files to another collection', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="kb-select-mode"]').trigger('click')
    await wrapper.find('[data-kb-doc="1"] .kb-doc-open').trigger('click')
    await wrapper.find('[data-kb-doc="2"] .kb-doc-open').trigger('click')
    await wrapper.find('[data-action="kb-select-move"]').trigger('click')

    const picker = wrapper.findComponent({ name: 'KBCollectionPickerDialog' })
    expect(picker.props('open')).toBe(true)
    expect(picker.props('allowCreate')).toBe(false)
    expect(picker.props('excludeId')).toBe(2)

    picker.vm.$emit('confirm', { id: 1, name: 'Policies' })
    await flushPromises()

    expect(h.apiPost).toHaveBeenCalledWith('/api/kb/documents/move', { id: 1, collection_id: 1 })
    expect(h.apiPost).toHaveBeenCalledWith('/api/kb/documents/move', { id: 2, collection_id: 1 })
    wrapper.unmount()
  })

  it('selects every visible file at once', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="kb-select-mode"]').trigger('click')
    await wrapper.find('[data-action="kb-select-all"]').trigger('click')
    expect(wrapper.text()).toContain('2 selected')

    await wrapper.find('[data-action="kb-select-all"]').trigger('click')
    expect(wrapper.text()).toContain('0 selected')
    wrapper.unmount()
  })

  it('searches with keywords as the user types', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    h.apiPost.mockResolvedValue({ results: [], total: 0, mode: 'fts' })
    vi.useFakeTimers()
    try {
      await wrapper.find('input').setValue('报销')
      await vi.advanceTimersByTimeAsync(300)
      expect(h.apiPost).toHaveBeenCalledWith(
        '/api/kb/search',
        expect.objectContaining({ q: '报销', mode: 'fts' }),
      )
    } finally {
      vi.useRealTimers()
    }
    wrapper.unmount()
  })

  // ── Desktop drop ──
  // `expandDataTransfer` only reads `items` + `files`, so a plain object is a
  // faithful stand-in for the browser's DataTransfer here.
  function fileDrop(files: File[]) {
    return { types: ['Files'], files, items: [] }
  }

  it('uploads dropped desktop files into the selected library', async () => {
    h.uploadFilesToCollection.mockResolvedValue({ added: 2, failed: 0, errors: [], cancelled: false })
    const wrapper = mountPanel()
    await flushPromises()
    const loadsBefore = h.apiGet.mock.calls.filter((c) => c[0] === '/api/kb/documents?collection_id=2').length

    const a = new File(['a'], 'a.md', { type: 'text/markdown' })
    const b = new File(['b'], 'b.pdf', { type: 'application/pdf' })
    await wrapper.trigger('drop', { dataTransfer: fileDrop([a, b]) })
    await flushPromises()

    expect(h.uploadFilesToCollection).toHaveBeenCalledTimes(1)
    const [files, collectionId] = h.uploadFilesToCollection.mock.calls[0]
    expect(files).toEqual([a, b])
    // The project collection is the one the panel selected on mount.
    expect(collectionId).toBe(2)
    expect(h.toast).toHaveBeenCalledWith('Added 2 files', expect.objectContaining({ type: 'success' }))
    // The list must be re-read so the new documents appear (and the index
    // progress widget is started).
    const loadsAfter = h.apiGet.mock.calls.filter((c) => c[0] === '/api/kb/documents?collection_id=2').length
    expect(loadsAfter).toBe(loadsBefore + 1)
    wrapper.unmount()
  })

  it('names the rejected files when part of a desktop drop fails', async () => {
    h.uploadFilesToCollection.mockResolvedValue({
      added: 1,
      failed: 1,
      errors: ['b.exe: unsupported'],
      cancelled: false,
    })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.trigger('drop', { dataTransfer: fileDrop([new File(['x'], 'x.md')]) })
    await flushPromises()

    expect(h.toast).toHaveBeenCalledWith('b.exe: unsupported', expect.objectContaining({ type: 'error' }))
    wrapper.unmount()
  })

  it('asks for a library before accepting a desktop drop', async () => {
    h.apiGet.mockImplementation(async (url: string) => {
      if (url === '/api/kb/collections') return { collections: [] }
      if (url.startsWith('/api/kb/status')) {
        return { available: true, vector_enabled: false, stats: { collections: 0, documents: 0, ready_docs: 0, failed_docs: 0, pending_docs: 0, chunks: 0, embedded_chunks: 0, stored_bytes: 0 } }
      }
      return {}
    })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.trigger('drop', { dataTransfer: fileDrop([new File(['x'], 'x.md')]) })
    await flushPromises()

    expect(h.uploadFilesToCollection).not.toHaveBeenCalled()
    expect(h.toast).toHaveBeenCalledWith(
      'Select a knowledge base before dropping files',
      expect.anything(),
    )
    wrapper.unmount()
  })

  it('shows the desktop drop overlay while files are dragged over', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.drop-overlay').exists()).toBe(false)

    await wrapper.trigger('dragenter', { dataTransfer: fileDrop([new File(['x'], 'x.md')]) })
    expect(wrapper.find('.drop-overlay').exists()).toBe(true)
    expect(wrapper.find('.drop-overlay').text()).toContain('Drop to add to this knowledge base')

    await wrapper.trigger('dragleave')
    expect(wrapper.find('.drop-overlay').exists()).toBe(false)
    wrapper.unmount()
  })

  it('stays silent when the desktop drop upload is cancelled', async () => {
    h.uploadFilesToCollection.mockResolvedValue({ added: 0, failed: 0, errors: [], cancelled: true })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.trigger('drop', { dataTransfer: fileDrop([new File(['x'], 'x.md')]) })
    await flushPromises()

    expect(h.toast).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
