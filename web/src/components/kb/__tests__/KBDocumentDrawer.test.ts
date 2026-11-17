import { beforeEach, describe, expect, it, vi } from 'vitest'
;(globalThis as { __VUE_I18N_FULL_INSTALL__?: boolean }).__VUE_I18N_FULL_INSTALL__ = false
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

const h = vi.hoisted(() => ({ fetchDetail: vi.fn(), fetchUsage: vi.fn() }))

vi.mock('@/composables/useKnowledgeBase', () => ({
  fetchDocumentDetail: h.fetchDetail,
  fetchDocumentUsage: h.fetchUsage,
}))
vi.mock('@/composables/useMarkdownRenderer.ts', () => ({
  renderMarkdownHtml: (text: string) => `<p>${text}</p>`,
}))

import KBDocumentDrawer from '@/components/kb/KBDocumentDrawer.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  missingWarn: false,
  fallbackWarn: false,
  messages: {
    en: {
      common: { loading: 'Loading', retry: 'Retry', close: 'Close', delete: 'Delete' },
      kb: {
        drawerTitle: 'File details',
        drawerChunks: 'Parsed content',
        drawerNoChunksDesc: 'Nothing parsed yet',
        drawerSource: 'Source file',
        drawerFocus: 'Search hit',
        quoteChunk: 'Quote to chat',
        tagsPlaceholder: 'Tags',
        usageTitle: 'Usage',
        usageCount: 'Cited {count} times',
        usageRecent: 'Cited {count} times · last {date}',
        tagsSave: 'Save tags',
        sourceStale: 'Original changed',
        sourceMissing: 'Original missing',
        sourceStaleHint: 'Original is newer',
        sourceMissingHint: 'Original is gone',
        chunks: '{count} chunks',
        vectorReady: 'Semantic ready',
        vectorPending: 'Semantic indexing',
        warningTruncated: 'Truncated',
        reindex: 'Reindex',
        refreshFromSource: 'Update from original',
        refreshFromSourceHint: 'Re-read the original',
        refreshNoSource: 'No original path',
        statusReady: 'Searchable',
        statusReadyFts: 'Keywords only',
        statusFailed: 'Failed',
        statusPending: 'Queued',
        statusExtracting: 'Parsing',
        locationPage: 'p.{n}',
        locationLine: 'Line {n}',
        locationTitle: 'Title',
        reason: {
          pdf_no_text: 'No extractable text — this may be a scanned PDF',
          no_text: 'No extractable text',
        },
      },
    },
  },
})

const BottomSheetStub = {
  props: ['open'],
  template: '<div v-if="open"><slot name="header" /><slot /><slot name="footer" /></div>',
}

function detail(over: Record<string, unknown> = {}) {
  return {
    document: {
      id: 1,
      collection_id: 2,
      title: 'handbook.pdf',
      source_path: '/docs/handbook.pdf',
      ext: '.pdf',
      size_bytes: 2048,
      status: 'ready',
      error: '',
      warning: '',
      chunk_count: 2,
      fts_ready: true,
      vector_ready: false,
      created_at: '',
      updated_at: '',
      ...(over.document as Record<string, unknown> | undefined),
    },
    chunks: over.chunks ?? [
      {
        id: 10,
        document_id: 1,
        collection_id: 2,
        project_id: 0,
        chunk_index: 5,
        chunk_text: 'first passage',
        location: 'p.5',
        token_count: 10,
        has_embedding: false,
        embedding_dim: 0,
        created_at: '',
      },
      {
        id: 11,
        document_id: 1,
        collection_id: 2,
        project_id: 0,
        chunk_index: 6,
        chunk_text: 'matched passage',
        location: 'p.6',
        token_count: 10,
        has_embedding: false,
        embedding_dim: 0,
        created_at: '',
      },
    ],
    focus_chunk_id: over.focus_chunk_id ?? 11,
  }
}

function mountDrawer(props: Record<string, unknown> = {}) {
  return mount(KBDocumentDrawer, {
    props: { open: true, documentId: 1, focusChunkId: 11, ...props },
    global: {
      plugins: [i18n],
      stubs: { BottomSheet: BottomSheetStub },
    },
  })
}

describe('KBDocumentDrawer', () => {
  beforeEach(() => {
    h.fetchDetail.mockReset()
    h.fetchDetail.mockResolvedValue(detail())
    h.fetchUsage.mockReset()
    h.fetchUsage.mockResolvedValue([])
  })

  it('fetches the document and renders its parsed passages', async () => {
    const wrapper = mountDrawer()
    await flushPromises()

    expect(h.fetchDetail).toHaveBeenCalledWith(1, 11)
    expect(wrapper.text()).toContain('handbook.pdf')
    expect(wrapper.text()).toContain('/docs/handbook.pdf')
    expect(wrapper.text()).toContain('first passage')
    expect(wrapper.text()).toContain('matched passage')
    expect(wrapper.findAll('[data-kb-chunk]')).toHaveLength(2)

    const focused = wrapper.find('[data-kb-chunk="11"]')
    expect(focused.classes()).toContain('is-focus')
    expect(focused.text()).toContain('Search hit')
    wrapper.unmount()
  })

  it('hides the semantic state when no semantic index is configured', async () => {
    // The fixture document is ready but un-vectorized; without an embedder that
    // is permanent, so it must read "Searchable", not "Semantic indexing".
    const wrapper = mountDrawer({ vectorEnabled: false })
    await flushPromises()

    expect(wrapper.text()).toContain('Searchable')
    expect(wrapper.text()).not.toContain('Semantic indexing')
    wrapper.unmount()
  })

  it('translates a stable failure reason instead of showing the raw message', async () => {
    h.fetchDetail.mockResolvedValue(
      detail({
        document: { status: 'failed', error: 'pdf_no_text: scan.pdf' },
        chunks: [],
      }),
    )
    const wrapper = mountDrawer()
    await flushPromises()

    expect(wrapper.find('.kb-drawer-error').text()).toBe(
      'No extractable text — this may be a scanned PDF',
    )
    wrapper.unmount()
  })

  it('falls back to the server message for an unknown failure', async () => {
    h.fetchDetail.mockResolvedValue(
      detail({
        document: { status: 'failed', error: 'disk read failed' },
        chunks: [],
      }),
    )
    const wrapper = mountDrawer()
    await flushPromises()
    expect(wrapper.find('.kb-drawer-error').text()).toBe('disk read failed')
    wrapper.unmount()
  })

  it('emits reindex and delete for the loaded document', async () => {
    const wrapper = mountDrawer()
    await flushPromises()

    const buttons = wrapper.findAll('.fbtn')
    await buttons[1].trigger('click') // update from original
    await buttons[2].trigger('click') // reindex
    await buttons[3].trigger('click') // delete

    expect(wrapper.emitted('refresh')?.[0]).toEqual([1])
    expect(wrapper.emitted('reindex')?.[0]).toEqual([1])
    expect(wrapper.emitted('delete')?.[0]).toEqual([{ id: 1, title: 'handbook.pdf' }])
    wrapper.unmount()
  })

  it('disables "update from original" when the document has no source path', async () => {
    h.fetchDetail.mockResolvedValue(detail({ document: { source_path: '' } }))
    const wrapper = mountDrawer()
    await flushPromises()

    const refresh = wrapper.find('[data-action="kb-refresh-doc"]')
    expect(refresh.attributes('disabled')).toBeDefined()
    await refresh.trigger('click')
    expect(wrapper.emitted('refresh')).toBeUndefined()
    wrapper.unmount()
  })

  it('shows how often the document was cited', async () => {
    h.fetchUsage.mockResolvedValue([
      { document_id: 1, count: 5, last_used: '2026-11-01T08:00:00Z' },
    ])
    const wrapper = mountDrawer()
    await flushPromises()

    expect(h.fetchUsage).toHaveBeenCalledWith(2)
    expect(wrapper.text()).toContain('Cited 5 times')
    wrapper.unmount()
  })

  it('emits the edited tag list', async () => {
    const wrapper = mountDrawer()
    await flushPromises()

    await wrapper.find('[data-role="kb-tag-input"]').setValue('制度, 报销')
    await wrapper.find('[data-action="kb-save-tags"]').trigger('click')

    expect(wrapper.emitted('save-tags')?.[0]).toEqual([{ id: 1, tags: '制度, 报销' }])
    wrapper.unmount()
  })

  it('warns when the original file changed or disappeared', async () => {
    h.fetchDetail.mockResolvedValue(detail({ document: { source_stale: true } }))
    const wrapper = mountDrawer()
    await flushPromises()
    expect(wrapper.text()).toContain('Original is newer')
    wrapper.unmount()

    h.fetchDetail.mockResolvedValue(detail({ document: { source_missing: true } }))
    const second = mountDrawer()
    await flushPromises()
    expect(second.text()).toContain('Original is gone')
    second.unmount()
  })

  it('emits a quote for the clicked passage', async () => {
    const wrapper = mountDrawer()
    await flushPromises()

    const buttons = wrapper.findAll('[data-action="kb-quote-chunk"]')
    expect(buttons).toHaveLength(2)
    await buttons[1].trigger('click')

    expect(wrapper.emitted('quote')?.[0]).toEqual([
      { text: 'matched passage', title: 'handbook.pdf', location: 'p.6' },
    ])
    wrapper.unmount()
  })

  it('reloads when the target document changes', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    expect(h.fetchDetail).toHaveBeenCalledTimes(1)

    await wrapper.setProps({ documentId: 2, focusChunkId: 0 })
    await flushPromises()
    expect(h.fetchDetail).toHaveBeenLastCalledWith(2, 0)
    wrapper.unmount()
  })
})
