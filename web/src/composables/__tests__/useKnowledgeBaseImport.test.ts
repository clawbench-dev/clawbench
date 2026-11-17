import { beforeEach, describe, expect, it, vi } from 'vitest'

const h = vi.hoisted(() => ({
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiPatch: vi.fn(),
  apiDelete: vi.fn(),
  apiPostForm: vi.fn(),
}))

vi.mock('@/utils/api', () => ({
  apiGet: h.apiGet,
  apiPost: h.apiPost,
  apiPatch: h.apiPatch,
  apiDelete: h.apiDelete,
  apiPostForm: h.apiPostForm,
}))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))

import {
  KB_DIR_IMPORT_TIMEOUT_MS,
  kbImportDirectory,
  kbPreviewDirectory,
} from '@/composables/useKnowledgeBase'

/**
 * The folder import is one synchronous server request that walks the tree and
 * copies up to 500 files. Under the API default (10s) any real folder hit the
 * client timeout while the server kept copying, so the user saw a failure for
 * an import that was in fact running.
 */
describe('knowledge base folder import', () => {
  beforeEach(() => {
    h.apiPost.mockReset()
    h.apiPost.mockResolvedValue({ added: 1, duplicates: 0, skipped: 0, failed: 0, truncated: false })
  })

  it('gives the import a budget far beyond the API default', () => {
    // The default is 10s (web/src/utils/api.ts); anything near it would time
    // out mid-import again.
    expect(KB_DIR_IMPORT_TIMEOUT_MS).toBeGreaterThanOrEqual(120_000)
  })

  it('passes the extended timeout to the import request', async () => {
    await kbImportDirectory(7, '/docs')
    expect(h.apiPost).toHaveBeenCalledWith(
      '/api/kb/documents/import',
      { collection_id: 7, path: '/docs' },
      { timeoutMs: KB_DIR_IMPORT_TIMEOUT_MS },
    )
  })

  it('passes it to the preview too, which hashes every file', async () => {
    await kbPreviewDirectory(7, '/docs')
    expect(h.apiPost).toHaveBeenCalledWith(
      '/api/kb/documents/import',
      { collection_id: 7, path: '/docs', dry_run: true },
      { timeoutMs: KB_DIR_IMPORT_TIMEOUT_MS },
    )
  })
})

/**
 * Cancellation contract between the client and the server: when the request is
 * abandoned (timeout, tab closed) the composable must surface the abort as a
 * normal rejection and leave the panel state alone, so the caller's own error
 * handling decides what to show. The server side stops copying on its own (see
 * the kb package's ImportDirectory ctx test).
 */
describe('knowledge base folder import cancellation', () => {
  beforeEach(() => {
    h.apiPost.mockReset()
  })

  it('rejects with the abort reason instead of swallowing it', async () => {
    const abort = new DOMException('request timed out after 10ms', 'AbortError')
    h.apiPost.mockRejectedValueOnce(abort)
    await expect(kbImportDirectory(1, '/docs')).rejects.toBe(abort)
  })
})
