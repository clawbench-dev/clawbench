/**
 * Knowledge base state and API calls.
 *
 * The server keeps two independent indexes (keyword + semantic) per collection.
 * A document becomes searchable as soon as its keyword index is written; the
 * semantic flag arrives later. That is why the panel polls while a document is
 * queued/parsing OR while it is keyword-ready but not vector-ready — the row's
 * badge changes in place instead of the user having to refresh.
 */
import { ref } from 'vue'
import { apiDelete, apiGet, apiPatch, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'

const TAG = 'KnowledgeBase'

export type KBScope = 'global' | 'project'
export type KBDocStatus = 'pending' | 'extracting' | 'ready' | 'failed'
export type KBSearchMode = 'hybrid' | 'fts'

export interface KBCollection {
  id: number
  name: string
  description: string
  scope: KBScope
  project_path?: string
  auto_inject: boolean
  doc_count: number
  created_at: string
  updated_at: string
}

export interface KBDocument {
  id: number
  collection_id: number
  title: string
  source_path?: string
  ext?: string
  size_bytes: number
  status: KBDocStatus
  error?: string
  warning?: string
  chunk_count: number
  fts_ready: boolean
  vector_ready: boolean
  created_at: string
  updated_at: string
}

export interface KBMatchRange {
  start: number
  end: number
}

export interface KBSearchHit {
  chunk_id: number
  document_id: number
  document_title: string
  collection_id: number
  collection_name: string
  location?: string
  chunk_text: string
  score: number
  vector_score?: number
  fts_score?: number
  match_positions?: KBMatchRange[]
}

export interface KBSearchResult {
  results: KBSearchHit[]
  total: number
  mode: 'hybrid' | 'vector' | 'fts'
}

export interface KBStats {
  collections: number
  documents: number
  ready_docs: number
  failed_docs: number
  pending_docs: number
  chunks: number
  embedded_chunks: number
  stored_bytes: number
}

interface KBStatusResponse {
  available: boolean
  vector_enabled: boolean
  stats: KBStats
}

function messageOf(err: unknown): string {
  if (err instanceof Error) return err.message
  return String(err)
}

export function useKnowledgeBase() {
  const collections = ref<KBCollection[]>([])
  const selectedId = ref<number | null>(null)
  const documents = ref<KBDocument[]>([])

  const loadingCollections = ref(false)
  const loadingDocuments = ref(false)
  const error = ref('')

  const available = ref(true)
  const vectorEnabled = ref(false)
  const stats = ref<KBStats | null>(null)

  const searchQuery = ref('')
  const searchScope = ref<'current' | 'all'>('current')
  const searchMode = ref<KBSearchMode>('hybrid')
  const searching = ref(false)
  const searchResult = ref<KBSearchResult | null>(null)
  const searchError = ref('')

  let pollTimer: ReturnType<typeof setInterval> | null = null

  const selectedCollection = () => collections.value.find((c) => c.id === selectedId.value) ?? null

  async function loadStatus(): Promise<void> {
    try {
      const res = await apiGet<KBStatusResponse>('/api/kb/status')
      available.value = res.available
      vectorEnabled.value = res.vector_enabled
      stats.value = res.stats
    } catch (e) {
      appLog.w(TAG, `status failed: ${messageOf(e)}`)
    }
  }

  async function loadCollections(preferSelection = true): Promise<void> {
    loadingCollections.value = true
    error.value = ''
    try {
      const res = await apiGet<{ collections: KBCollection[] }>('/api/kb/collections')
      collections.value = res.collections ?? []
      // Keep the current selection when it still exists; otherwise prefer a
      // project collection (the one the user is most likely working in) over a
      // global one.
      if (!collections.value.some((c) => c.id === selectedId.value)) {
        if (preferSelection) {
          const project = collections.value.find((c) => c.scope === 'project')
          selectedId.value = project?.id ?? collections.value[0]?.id ?? null
        } else {
          selectedId.value = null
        }
      }
      if (selectedId.value != null) {
        await loadDocuments(selectedId.value)
      } else {
        documents.value = []
      }
    } catch (e) {
      error.value = messageOf(e)
      appLog.w(TAG, `collections failed: ${error.value}`)
    } finally {
      loadingCollections.value = false
    }
  }

  /**
   * silent=true is the polling path: it must not toggle `loadingDocuments`,
   * otherwise the document list is replaced by a skeleton every 2.5 seconds.
   */
  async function loadDocuments(collectionId: number, silent = false): Promise<void> {
    if (!silent) loadingDocuments.value = true
    try {
      const res = await apiGet<{ documents: KBDocument[] }>(
        `/api/kb/documents?collection_id=${collectionId}`,
      )
      // A slow response for a collection the user has since left must not
      // overwrite the new list.
      if (selectedId.value === collectionId) {
        documents.value = res.documents ?? []
      }
    } catch (e) {
      if (!silent) error.value = messageOf(e)
      appLog.w(TAG, `documents failed: ${messageOf(e)}`)
    } finally {
      if (!silent) loadingDocuments.value = false
    }
  }

  async function selectCollection(id: number): Promise<void> {
    if (selectedId.value === id) return
    selectedId.value = id
    documents.value = []
    clearSearch()
    await loadDocuments(id)
  }

  async function refresh(preferSelection = true): Promise<void> {
    await loadCollections(preferSelection)
    await loadStatus()
  }

  async function createCollection(name: string, description: string, scope: KBScope): Promise<KBCollection> {
    const created = await apiPost<KBCollection>('/api/kb/collections', { name, description, scope })
    collections.value = [...collections.value, created]
    selectedId.value = created.id
    documents.value = []
    clearSearch()
    return created
  }

  async function deleteCollection(id: number): Promise<void> {
    await apiDelete(`/api/kb/collections?id=${id}`)
    collections.value = collections.value.filter((c) => c.id !== id)
    if (selectedId.value === id) {
      selectedId.value = collections.value[0]?.id ?? null
      documents.value = []
      if (selectedId.value != null) await loadDocuments(selectedId.value)
    }
  }

  async function setAutoInject(id: number, value: boolean): Promise<void> {
    const updated = await apiPatch<KBCollection>(`/api/kb/collections?id=${id}`, {
      auto_inject: value,
    })
    const idx = collections.value.findIndex((c) => c.id === id)
    if (idx >= 0) collections.value[idx] = updated
  }

  function bumpDocCount(delta: number): void {
    const col = collections.value.find((c) => c.id === selectedId.value)
    if (col) col.doc_count = Math.max(0, col.doc_count + delta)
  }

  async function addFile(path: string): Promise<KBDocument> {
    if (selectedId.value == null) throw new Error('no collection selected')
    const doc = await apiPost<KBDocument>('/api/kb/documents', {
      collection_id: selectedId.value,
      path,
    })
    bumpDocCount(1)
    await loadDocuments(selectedId.value, true)
    startPolling()
    return doc
  }

  async function uploadFile(file: File): Promise<KBDocument> {
    if (selectedId.value == null) throw new Error('no collection selected')
    const form = new FormData()
    form.append('collection_id', String(selectedId.value))
    form.append('file', file)
    const resp = await fetch('/api/kb/documents/upload', { method: 'POST', body: form })
    const data = (await resp.json().catch(() => ({}))) as Record<string, unknown>
    if (!resp.ok) {
      const err = new Error(data.error ? String(data.error) : resp.statusText)
      ;(err as Error & { status?: number }).status = resp.status
      throw err
    }
    bumpDocCount(1)
    await loadDocuments(selectedId.value, true)
    startPolling()
    return data as unknown as KBDocument
  }

  async function deleteDocument(id: number): Promise<void> {
    await apiDelete(`/api/kb/documents?id=${id}`)
    documents.value = documents.value.filter((d) => d.id !== id)
    bumpDocCount(-1)
  }

  async function reindexDocument(id: number): Promise<void> {
    await apiPost(`/api/kb/documents/reindex?id=${id}`, {})
    if (selectedId.value != null) await loadDocuments(selectedId.value, true)
    startPolling()
  }

  async function reindexCollection(id: number): Promise<void> {
    await apiPost('/api/kb/reindex', { collection_id: id })
    if (selectedId.value === id) await loadDocuments(id, true)
    startPolling()
  }

  function clearSearch(): void {
    searchQuery.value = ''
    searchResult.value = null
    searchError.value = ''
    searching.value = false
  }

  async function runSearch(): Promise<void> {
    const q = searchQuery.value.trim()
    if (!q) {
      clearSearch()
      return
    }
    searching.value = true
    searchError.value = ''
    try {
      const ids = searchScope.value === 'current' && selectedId.value != null ? [selectedId.value] : []
      searchResult.value = await apiPost<KBSearchResult>('/api/kb/search', {
        q,
        collection_ids: ids,
        mode: searchMode.value,
      })
    } catch (e) {
      searchError.value = messageOf(e)
      searchResult.value = null
    } finally {
      searching.value = false
    }
  }

  function needsPolling(): boolean {
    return documents.value.some(
      (d) =>
        d.status === 'pending' ||
        d.status === 'extracting' ||
        (d.status === 'ready' && d.fts_ready && !d.vector_ready),
    )
  }

  function startPolling(): void {
    if (pollTimer != null) return
    pollTimer = setInterval(() => {
      if (selectedId.value == null) return
      if (!needsPolling()) {
        stopPolling()
        return
      }
      void loadDocuments(selectedId.value, true)
    }, 2500)
  }

  function stopPolling(): void {
    if (pollTimer != null) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  return {
    collections,
    selectedId,
    selectedCollection,
    documents,
    loadingCollections,
    loadingDocuments,
    error,
    available,
    vectorEnabled,
    stats,
    searchQuery,
    searchScope,
    searchMode,
    searching,
    searchResult,
    searchError,
    loadCollections,
    loadDocuments,
    loadStatus,
    selectCollection,
    refresh,
    createCollection,
    deleteCollection,
    setAutoInject,
    addFile,
    uploadFile,
    deleteDocument,
    reindexDocument,
    reindexCollection,
    clearSearch,
    runSearch,
    startPolling,
    stopPolling,
  }
}
