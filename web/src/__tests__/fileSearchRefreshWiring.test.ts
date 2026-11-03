import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring guard for "deleted file stays in the search result list".
 *
 * The result list is its own listing: App.vue's handlers reload the browsed
 * directory through the store, which never touches it. The refresh therefore
 * has to come from the mutation handlers — and it has to come AFTER the store
 * call resolves. Emitting is synchronous while the handlers are async (the
 * single-file delete even blocks on its confirmation dialog), so refreshing at
 * emit time re-scanned the tree while the file still existed and the hit never
 * left the list.
 *
 * App.vue has no component test (its only tests read it as source text), so
 * these assertions pin the ordering and the expose hop that a refactor can
 * silently drop.
 */
function handlerBody(app: string, name: string): string {
  const start = app.indexOf(`function ${name}(`)
  expect(start, `${name} not found in App.vue`).toBeGreaterThan(-1)
  const rest = app.slice(start)
  const end = rest.indexOf('\n}')
  expect(end, `${name} body not terminated`).toBeGreaterThan(-1)
  return rest.slice(0, end)
}

function expectExposed(src: string, name: string, label: string) {
  const expose = src.match(/defineExpose\(\{([\s\S]*?)\}\)/)
  expect(expose, `${label}: defineExpose not found`).toBeTruthy()
  // Strip whole-line comments: a comment between two members would otherwise
  // sit inside the `,\s*` separator and hide a real export.
  const body = expose![1].replace(/^\s*\/\/.*$/gm, '')
  expect(
    body,
    `${label}: ${name} is not a member of defineExpose (a mention in a comment does not export it)`,
  ).toMatch(new RegExp(`(?:^|,)\\s*${name}\\s*(?::[^,}]*)?(?:,|$)`))
}

describe('search result refresh after file mutations', () => {
  const app = readWebFile('src/App.vue')
  const content = readWebFile('src/components/file/FileManagerContent.vue')

  it('FileManagerContent exposes refreshSearchResults for the parent', () => {
    expectExposed(content, 'refreshSearchResults', 'FileManagerContent')
  })

  it('handleDelete refreshes the search only after store.deleteFile resolved', () => {
    const body = handlerBody(app, 'handleDelete')
    const storeCall = body.indexOf('await store.deleteFile(path)')
    const refresh = body.indexOf('fileManagerRef.value?.refreshSearchResults()')
    expect(storeCall).toBeGreaterThan(-1)
    expect(refresh).toBeGreaterThan(-1)
    expect(refresh).toBeGreaterThan(storeCall)
  })

  it('handleBatchDelete refreshes the search only after store.deleteFiles resolved', () => {
    const body = handlerBody(app, 'handleBatchDelete')
    const storeCall = body.indexOf('await store.deleteFiles(paths)')
    const refresh = body.indexOf('fileManagerRef.value?.refreshSearchResults()')
    expect(storeCall).toBeGreaterThan(-1)
    expect(refresh).toBeGreaterThan(-1)
    expect(refresh).toBeGreaterThan(storeCall)
  })

  it('handleRename refreshes the search only after a successful rename', () => {
    const body = handlerBody(app, 'handleRename')
    const storeCall = body.indexOf('await store.renameFile(path, name)')
    const refresh = body.indexOf('fileManagerRef.value?.refreshSearchResults()')
    expect(storeCall).toBeGreaterThan(-1)
    expect(refresh).toBeGreaterThan(-1)
    expect(refresh).toBeGreaterThan(storeCall)
    // The catch branch returns early, so a failed rename does not re-scan.
    const catchIdx = body.indexOf('catch (err)')
    expect(catchIdx).toBeGreaterThan(-1)
    expect(body.slice(catchIdx, refresh)).toMatch(/return/)
  })

  it('the component never refreshes the search right after emitting a mutation', () => {
    // Guard against re-introducing the bug: every mutation handler must hand
    // the refresh to the parent, which alone knows when the work finished.
    for (const name of ['doDelete', 'doBatchDelete', 'doRename']) {
      expect(handlerBody(content, name), `${name} must not refresh the search itself`).not.toContain(
        'refreshSearchResults()',
      )
    }
  })
})
