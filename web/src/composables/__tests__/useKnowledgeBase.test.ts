import { beforeEach, describe, expect, it, vi } from 'vitest'

const h = vi.hoisted(() => ({
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiPatch: vi.fn(),
  apiDelete: vi.fn(),
}))

vi.mock('@/utils/api', () => ({
  apiGet: h.apiGet,
  apiPost: h.apiPost,
  apiPatch: h.apiPatch,
  apiDelete: h.apiDelete,
}))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))

import { useKnowledgeBase } from '@/composables/useKnowledgeBase'

const collections = [
  { id: 1, name: 'Global', description: '', scope: 'global', auto_inject: true, doc_count: 0, created_at: '', updated_at: '' },
  { id: 2, name: 'Project', description: '', scope: 'project', auto_inject: true, doc_count: 0, created_at: '', updated_at: '' },
]

// A fresh object per call: loadProgress mutates rows in place, and a shared
// fixture would leak a status change into the next test.
function docsFixture() {
  return [
    { id: 10, collection_id: 2, title: 'a.txt', size_bytes: 10, status: 'ready', chunk_count: 1, fts_ready: true, vector_ready: true, created_at: '', updated_at: '' },
  ]
}

describe('useKnowledgeBase', () => {
  beforeEach(() => {
    h.apiGet.mockReset()
    h.apiPost.mockReset()
    h.apiPatch.mockReset()
    h.apiDelete.mockReset()

    h.apiGet.mockImplementation(async (url: string) => {
      if (url === '/api/kb/collections') return { collections }
      if (url.startsWith('/api/kb/documents')) return { documents: docsFixture() }
      if (url === '/api/kb/status') {
        return { available: true, vector_enabled: true, stats: { collections: 2, documents: 1, ready_docs: 1, failed_docs: 0, pending_docs: 0, chunks: 1, embedded_chunks: 1, stored_bytes: 10 } }
      }
      return {}
    })
  })

  it('prefers a project collection and loads its documents', async () => {
    const kb = useKnowledgeBase()
    await kb.loadCollections()
    await kb.loadStatus()

    expect(kb.collections.value).toHaveLength(2)
    expect(kb.selectedId.value).toBe(2)
    expect(kb.documents.value).toHaveLength(1)
    expect(kb.available.value).toBe(true)
    expect(kb.vectorEnabled.value).toBe(true)
    expect(kb.stats.value?.documents).toBe(1)
  })

  it('merges polled progress in place without replacing the row', async () => {
    const kb = useKnowledgeBase()
    await kb.loadCollections()
    const row = kb.documents.value[0]

    h.apiGet.mockResolvedValueOnce({
      documents: [
        { id: 10, status: 'failed', error: 'no_text', warning: '', chunk_count: 0, fts_ready: false, vector_ready: false },
      ],
    })
    await kb.loadProgress(2)

    expect(kb.documents.value[0]).toBe(row)
    expect(row.status).toBe('failed')
    expect(row.error).toBe('no_text')
    expect(row.vector_ready).toBe(false)
  })

  it('ignores progress for a collection the user has left', async () => {
    const kb = useKnowledgeBase()
    await kb.loadCollections()
    const row = kb.documents.value[0]

    h.apiGet.mockResolvedValueOnce({
      documents: [
        { id: 10, status: 'failed', error: 'no_text', warning: '', chunk_count: 0, fts_ready: false, vector_ready: false },
      ],
    })
    await kb.loadProgress(1)

    expect(row.status).toBe('ready')
  })

  it('searches and clears the result', async () => {
    const kb = useKnowledgeBase()
    await kb.loadCollections()

    h.apiPost.mockResolvedValueOnce({
      results: [
        {
          chunk_id: 9,
          document_id: 10,
          document_title: 'a.txt',
          collection_id: 2,
          collection_name: 'Project',
          location: 'p.1',
          chunk_text: 'hello',
          score: 1,
        },
      ],
      total: 1,
      mode: 'fts',
    })

    kb.searchQuery.value = 'hello'
    await kb.runSearch()

    expect(kb.searching.value).toBe(false)
    expect(kb.searchResult.value?.total).toBe(1)
    expect(h.apiPost).toHaveBeenCalledWith('/api/kb/search', {
      q: 'hello',
      collection_ids: [2],
      mode: 'hybrid',
    })

    kb.clearSearch()
    expect(kb.searchResult.value).toBeNull()
  })

  it('does not search an empty query', async () => {
    const kb = useKnowledgeBase()
    kb.searchQuery.value = '   '
    await kb.runSearch()
    expect(h.apiPost).not.toHaveBeenCalled()
    expect(kb.searchResult.value).toBeNull()
  })

  it('toggles auto-inject in place', async () => {
    const kb = useKnowledgeBase()
    await kb.loadCollections()
    h.apiPatch.mockResolvedValueOnce({ ...collections[1], auto_inject: false })
    await kb.setAutoInject(2, false)
    expect(kb.collections.value.find((c) => c.id === 2)?.auto_inject).toBe(false)
  })
})
