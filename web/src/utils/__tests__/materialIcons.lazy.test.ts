import { describe, it, expect, vi, beforeEach } from 'vitest'

/**
 * Regression guard for the lazy manifest init.
 *
 * `generateManifest()` walks the whole material-icon-theme dataset and measured
 * ~370ms. It used to run at module init, which made the lazily-loaded chunk
 * that contains this module block the main thread for most of a second right
 * after evaluation (Chrome trace: 727ms `v8.evaluateModule`).
 *
 * These tests assert the timing contract, not just the output:
 *   1. importing the module must NOT call generateManifest
 *   2. the first resolve must call it exactly once
 *   3. further resolves must reuse the memoized maps
 *
 * `vi.resetModules()` + dynamic import is what makes (1) observable — a static
 * import at the top of this file would have already evaluated the module.
 */

const generateManifestSpy = vi.fn()

vi.mock('material-icon-theme', () => ({
  generateManifest: () => generateManifestSpy(),
}))

function fakeManifest() {
  return {
    fileExtensions: { go: 'go', ts: 'typescript' },
    fileNames: { dockerfile: 'docker' },
    folderNames: { src: 'folder-src' },
    folderNamesExpanded: { src: 'folder-src-open' },
    file: 'file',
    folder: 'folder',
    folderExpanded: 'folder-open',
  }
}

beforeEach(() => {
  generateManifestSpy.mockReset()
  generateManifestSpy.mockImplementation(fakeManifest)
})

describe('materialIcons lazy manifest init', () => {
  it('does not call generateManifest at import time', async () => {
    vi.resetModules()
    generateManifestSpy.mockClear()

    await import('@/utils/materialIcons')

    expect(generateManifestSpy).not.toHaveBeenCalled()
  })

  it('calls generateManifest exactly once on first resolve, then reuses it', async () => {
    vi.resetModules()
    generateManifestSpy.mockClear()

    const mod = await import('@/utils/materialIcons')

    expect(generateManifestSpy).not.toHaveBeenCalled()

    // First resolve pays the cost
    expect(mod.getFileIconName('main.go')).toBe('go')
    expect(generateManifestSpy).toHaveBeenCalledTimes(1)

    // Subsequent resolves reuse the memoized maps — no extra manifest builds
    expect(mod.getFileIconName('app.ts')).toBe('typescript')
    expect(mod.getFileIconName('Dockerfile')).toBe('docker')
    expect(mod.getFolderIconName('src')).toBe('folder-src')
    expect(mod.getFolderIconName('src', true)).toBe('folder-src-open')
    expect(mod.getFolderIconName('unknown-folder')).toBe('folder')
    expect(mod.getFileIconName('data.xyz')).toBe('file')
    expect(generateManifestSpy).toHaveBeenCalledTimes(1)
  })

  it('resolves defaults from the lazily-built manifest, not hardcoded literals', async () => {
    vi.resetModules()
    generateManifestSpy.mockReset()
    // Distinct defaults prove the values come from the manifest object.
    generateManifestSpy.mockImplementation(() => ({
      fileExtensions: {},
      fileNames: {},
      folderNames: {},
      folderNamesExpanded: {},
      file: 'my-default-file',
      folder: 'my-default-folder',
      folderExpanded: 'my-default-folder-open',
    }))

    const mod = await import('@/utils/materialIcons')

    expect(mod.getFileIconName('unknown.xyz')).toBe('my-default-file')
    expect(mod.getFolderIconName('unknown-folder')).toBe('my-default-folder')
    expect(mod.getFolderIconName('unknown-folder', true)).toBe('my-default-folder-open')
  })
})
