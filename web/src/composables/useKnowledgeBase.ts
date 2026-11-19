/**
 * Knowledge base state and API calls.
 *
 * The server keeps two independent indexes (keyword + semantic) per collection.
 * A document becomes searchable as soon as its keyword index is written; the
 * semantic flag arrives later. That is why the panel polls while a document is
 * queued/parsing OR while it is keyword-ready but not vector-ready — the row's
 * badge changes in place instead of the user having to refresh.
 */
import { computed, ref } from 'vue'
import { apiDelete, apiGet, apiPatch, apiPost, apiPostForm } from '@/utils/api'
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
  /** Pinned libraries sort first within their scope group. */
  pinned: boolean
  doc_count: number
  /** Documents whose extraction failed; shown on the collection rail. */
  failed_count: number
  /** Queued or extracting documents. */
  pending_count: number
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
  /** Derived at read time: the original file is gone / newer than the copy. */
  source_missing?: boolean
  source_stale?: boolean
  /** Comma-separated free-form labels. */
  tags?: string
  created_at: string
  updated_at: string
}

export interface KBMatchRange {
  start: number
  end: number
}

/** One indexed passage, as returned by the document-detail endpoint. */
export interface KBChunk {
  id: number
  document_id: number
  collection_id: number
  project_id: number
  chunk_index: number
  chunk_text: string
  location?: string
  token_count: number
  has_embedding: boolean
  embedding_dim: number
  created_at: string
}

export interface KBDocumentDetail {
  document: KBDocument
  chunks: KBChunk[]
  /** The chunk the caller asked for, or 0 when it was not requested/gone. */
  focus_chunk_id: number
}

/** The volatile slice of a document that polling merges in place. */
export interface KBDocumentProgress {
  id: number
  status: KBDocStatus
  error?: string
  warning?: string
  chunk_count: number
  fts_ready: boolean
  vector_ready: boolean
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

/** Outcome of importing a folder: what landed, what was skipped as
 *  unindexable, what already existed, and what failed outright. */
export interface KBDirImportResult {
  added: number
  duplicates: number
  skipped: number
  failed: number
  truncated: boolean
  /** Skipped files grouped by extraction reason (`legacy_doc`, `image`, …). */
  skip_reasons?: Record<string, number>
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

// ── Stateless actions ──
// Shared by the panel and by entry points outside it (file manager, chat,
// meeting notes). They take an explicit collection id instead of reading the
// panel's selection.

export async function kbAddByPath(collectionId: number, path: string): Promise<KBDocument> {
  return apiPost<KBDocument>('/api/kb/documents', { collection_id: collectionId, path })
}

export interface KBDocumentUsage {
  document_id: number
  count: number
  last_used: string
}

export async function fetchDocumentUsage(collectionId: number): Promise<KBDocumentUsage[]> {
  const res = await apiGet<{ usage: KBDocumentUsage[] }>(
    `/api/kb/documents/usage?collection_id=${collectionId}`,
  )
  return res.usage ?? []
}

export async function kbAddFromURL(collectionId: number, url: string): Promise<KBDocument> {
  return apiPost<KBDocument>('/api/kb/documents/url', { collection_id: collectionId, url })
}

/**
 * Folder import and its preview are synchronous requests on the server: one
 * HTTP call walks the folder, copies up to 500 files and writes their rows.
 * The default 10s budget cuts that off in the middle for anything but a tiny
 * folder, and the user then sees a failure for an import that is still going.
 */
export const KB_DIR_IMPORT_TIMEOUT_MS = 300_000

export async function kbImportDirectory(
  collectionId: number,
  path: string,
): Promise<KBDirImportResult> {
  return apiPost<KBDirImportResult>(
    '/api/kb/documents/import',
    { collection_id: collectionId, path },
    { timeoutMs: KB_DIR_IMPORT_TIMEOUT_MS },
  )
}

export async function kbPreviewDirectory(
  collectionId: number,
  path: string,
): Promise<KBDirImportResult> {
  return apiPost<KBDirImportResult>(
    '/api/kb/documents/import',
    { collection_id: collectionId, path, dry_run: true },
    // The preview hashes every file in the folder to detect duplicates, so it
    // is I/O bound in the same way the real import is.
    { timeoutMs: KB_DIR_IMPORT_TIMEOUT_MS },
  )
}

export async function kbCreateCollection(
  name: string,
  description: string,
  scope: KBScope,
): Promise<KBCollection> {
  return apiPost<KBCollection>('/api/kb/collections', { name, description, scope })
}

/**
 * Create a document from text (an AI answer, a meeting summary). Uses the
 * multipart upload path so the server treats it exactly like an uploaded file:
 * same size limits, same extraction, same duplicate check.
 */
export async function kbUploadText(
  collectionId: number,
  title: string,
  text: string,
): Promise<KBDocument> {
  const form = new FormData()
  form.append('collection_id', String(collectionId))
  form.append('file', new Blob([text], { type: 'text/markdown' }), title)
  return apiPostForm<KBDocument>('/api/kb/documents/upload', form)
}

/**
 * Standalone so the document drawer can fetch without creating a second copy
 * of the panel's state.
 */
export async function fetchDocumentDetail(
  id: number,
  chunkId?: number | null,
): Promise<KBDocumentDetail> {
  const focus = chunkId && chunkId > 0 ? `&chunk_id=${chunkId}` : ''
  return apiGet<KBDocumentDetail>(`/api/kb/documents?id=${id}${focus}`)
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
  /** Monotonic id so a slow response cannot overwrite a newer query. */
  let searchSeq = 0

  const selectedCollection = () => collections.value.find((c) => c.id === selectedId.value) ?? null

  async function loadStatus(): Promise<void> {
    try {
      // Project scope: the panel lists global + current-project collections, so
      // its counters must describe the same set. Without the scope a pending
      // queue in another project would show up here and never move.
      const res = await apiGet<KBStatusResponse>('/api/kb/status?scope=project')
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
        // The scope select follows the selection: "this collection" is
        // meaningless (and disabled) when nothing is selected.
        searchScope.value = selectedId.value != null ? 'current' : 'all'
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

  /**
   * Polling path: fetch only the fields that change while indexing and merge
   * them into the existing rows. Mutating in place keeps object identity, so
   * Vue re-renders only the rows whose status actually changed.
   */
  async function loadProgress(collectionId: number): Promise<void> {
    const res = await apiGet<{ documents: KBDocumentProgress[] }>(
      `/api/kb/documents/progress?collection_id=${collectionId}`,
    )
    if (selectedId.value !== collectionId) return
    const byId = new Map((res.documents ?? []).map((d) => [d.id, d]))
    for (const doc of documents.value) {
      const p = byId.get(doc.id)
      if (!p) continue
      doc.status = p.status
      doc.error = p.error ?? ''
      doc.warning = p.warning ?? ''
      doc.chunk_count = p.chunk_count
      doc.fts_ready = p.fts_ready
      doc.vector_ready = p.vector_ready
    }
  }

  /** Document detail plus the passages around `chunkId` (when given). */
  async function loadDocument(id: number, chunkId?: number | null): Promise<KBDocumentDetail> {
    return fetchDocumentDetail(id, chunkId)
  }

  async function selectCollection(id: number): Promise<void> {
    if (selectedId.value === id) return
    selectedId.value = id
    documents.value = []
    // A collection is a filter, not a separate search page: entering one means
    // "search here", leaving it means "search everything".
    searchScope.value = 'current'
    clearSearch()
    await loadDocuments(id)
  }

  /** Leave the detail pane (narrow-screen back, project switch). */
  function clearSelection(): void {
    selectedId.value = null
    documents.value = []
    searchScope.value = 'all'
    clearSearch()
  }

  /**
   * Re-read everything after the project changes. The panel is kept alive by
   * its TabPanel, so without this it would keep showing the previous project's
   * collections and then fail with 403 when adding a file to one of them.
   */
  async function reloadForProject(): Promise<void> {
    stopPolling()
    collections.value = []
    clearSelection()
    await loadCollections(true)
    await loadStatus()
  }

  async function refresh(preferSelection = true): Promise<void> {
    await loadCollections(preferSelection)
    await loadStatus()
  }

  async function createCollection(name: string, description: string, scope: KBScope): Promise<KBCollection> {
    const created = await kbCreateCollection(name, description, scope)
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
      searchScope.value = selectedId.value != null ? 'current' : 'all'
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

  async function setPinned(id: number, value: boolean): Promise<void> {
    const updated = await apiPatch<KBCollection>(`/api/kb/collections?id=${id}`, { pinned: value })
    const idx = collections.value.findIndex((c) => c.id === id)
    if (idx >= 0) collections.value[idx] = updated
  }

  /** Rename a collection / change its description (scope stays immutable). */
  async function updateCollection(
    id: number,
    fields: { name?: string; description?: string },
  ): Promise<KBCollection> {
    const updated = await apiPatch<KBCollection>(`/api/kb/collections?id=${id}`, fields)
    const idx = collections.value.findIndex((c) => c.id === id)
    if (idx >= 0) collections.value[idx] = updated
    return updated
  }

  function bumpDocCount(delta: number): void {
    const col = collections.value.find((c) => c.id === selectedId.value)
    if (col) col.doc_count = Math.max(0, col.doc_count + delta)
  }

  async function addFile(path: string): Promise<KBDocument> {
    if (selectedId.value == null) throw new Error('no collection selected')
    const doc = await kbAddByPath(selectedId.value, path)
    bumpDocCount(1)
    await loadDocuments(selectedId.value, true)
    startPolling()
    return doc
  }

  /** Add every supported file under a folder. One request: the server walks
   *  the tree (and applies its own skip rules), so a big folder is not hundreds
   *  of round trips. */
  async function importDirectory(path: string): Promise<KBDirImportResult> {
    if (selectedId.value == null) throw new Error('no collection selected')
    const result = await kbImportDirectory(selectedId.value, path)
    if (result.added > 0) bumpDocCount(result.added)
    await loadDocuments(selectedId.value, true)
    startPolling()
    return result
  }

  /** What `importDirectory` would do, without copying anything. */
  async function previewDirectory(path: string): Promise<KBDirImportResult> {
    if (selectedId.value == null) throw new Error('no collection selected')
    return kbPreviewDirectory(selectedId.value, path)
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

  /** Replace a document's comma-separated tag list. */
  async function setDocumentTags(id: number, tags: string): Promise<KBDocument> {
    const updated = await apiPatch<KBDocument>(`/api/kb/documents?id=${id}`, { tags })
    const idx = documents.value.findIndex((d) => d.id === id)
    if (idx >= 0) documents.value[idx] = updated
    return updated
  }

  /** Move one document to another collection (requeues its index). */
  async function moveDocument(id: number, targetCollectionId: number): Promise<void> {
    await apiPost('/api/kb/documents/move', { id, collection_id: targetCollectionId })
  }

  /** Re-copy the original file and reindex only when its content changed. */
  async function refreshDocument(id: number): Promise<boolean> {
    const res = await apiPost<{ changed: boolean }>(`/api/kb/documents/refresh?id=${id}`, {})
    if (selectedId.value != null) await loadDocuments(selectedId.value, true)
    if (res.changed) startPolling()
    return res.changed
  }

  async function reindexCollection(id: number): Promise<void> {
    await apiPost('/api/kb/reindex', { collection_id: id })
    if (selectedId.value === id) await loadDocuments(id, true)
    startPolling()
  }

  function clearSearch(): void {
    // Bump the sequence so an in-flight response cannot repopulate the list
    // the user just dismissed.
    searchSeq++
    searchQuery.value = ''
    searchResult.value = null
    searchError.value = ''
    searching.value = false
  }

  /**
   * `mode` overrides the select for this one request: typing runs a cheap
   * keyword search, pressing Enter runs the chosen (usually hybrid) mode.
   */
  async function runSearch(mode?: KBSearchMode): Promise<void> {
    const q = searchQuery.value.trim()
    if (!q) {
      clearSearch()
      return
    }
    const seq = ++searchSeq
    searching.value = true
    searchError.value = ''
    try {
      const ids = searchScope.value === 'current' && selectedId.value != null ? [selectedId.value] : []
      const res = await apiPost<KBSearchResult>('/api/kb/search', {
        q,
        collection_ids: ids,
        mode: mode ?? searchMode.value,
      })
      if (seq !== searchSeq) return
      searchResult.value = res
    } catch (e) {
      if (seq !== searchSeq) return
      searchError.value = messageOf(e)
      searchResult.value = null
    } finally {
      if (seq === searchSeq) searching.value = false
    }
  }

  function needsPolling(): boolean {
    return documents.value.some(
      (d) =>
        d.status === 'pending' ||
        d.status === 'extracting' ||
        // Only meaningful when semantic search is configured: with no embedder
        // `vector_ready` stays false forever and this would poll endlessly.
        (vectorEnabled.value && d.status === 'ready' && d.fts_ready && !d.vector_ready),
    )
  }

  /**
   * Whether the project-wide counters still have work behind them. The
   * per-document check above only knows about the selected collection; the
   * progress strip is project-wide, so an empty selection must still keep the
   * numbers moving while another collection is being indexed.
   */
  const statsActive = computed(() => {
    const s = stats.value
    if (!s) return false
    if (s.pending_docs > 0) return true
    return vectorEnabled.value && s.chunks > s.embedded_chunks
  })

  function startPolling(): void {
    if (pollTimer != null) return
    pollTimer = setInterval(() => {
      const docsPending = needsPolling()
      if (!docsPending && !statsActive.value) {
        stopPolling()
        return
      }
      // Keep the global counters fresh as well: they drive the progress strip
      // and, unlike the per-document rows, are not touched by loadProgress.
      void loadStatus()
      if (selectedId.value != null && docsPending) {
        void loadProgress(selectedId.value)
      }
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
    statsActive,
    searchQuery,
    searchScope,
    searchMode,
    searching,
    searchResult,
    searchError,
    loadCollections,
    loadDocuments,
    loadProgress,
    loadDocument,
    loadStatus,
    selectCollection,
    clearSelection,
    reloadForProject,
    refresh,
    createCollection,
    updateCollection,
    deleteCollection,
    setAutoInject,
    setPinned,
    addFile,
    importDirectory,
    previewDirectory,
    deleteDocument,
    reindexDocument,
    refreshDocument,
    moveDocument,
    setDocumentTags,
    reindexCollection,
    clearSearch,
    runSearch,
    startPolling,
    stopPolling,
  }
}
