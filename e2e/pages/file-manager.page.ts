import { type Locator, type Page, expect } from '@playwright/test'

/**
 * Page Object Model for the File Manager panel.
 *
 * Key selectors (from FileManagerContent.vue):
 * - .file-list    → file list container (list view mode)
 * - .file-grid    → file grid container (grid view mode)
 * - .file-item    → individual file or directory item (list view)
 * - .dir-item     → directory item (also has .file-item, list view)
 * - .grid-item    → individual file or directory item (grid view)
 * - .grid-dir     → directory item in grid view
 * - .file-viewer  → file content viewer
 *
 * The file manager supports two view modes (list and grid).
 * Use the view-agnostic selectors when the specific mode doesn't matter.
 */
export class FileManagerPage {
  readonly page: Page
  readonly fileList: Locator
  readonly fileGrid: Locator
  readonly fileViewer: Locator

  constructor(page: Page) {
    this.page = page
    this.fileList = page.locator('.file-list')
    this.fileGrid = page.locator('.file-grid')
    this.fileViewer = page.locator('.file-viewer')
  }

  /**
   * View-agnostic selector: any file/directory item in either list or grid mode.
   * Matches .file-item (list mode) and .grid-item (grid mode).
   */
  get anyItem(): Locator {
    return this.page.locator('.file-item, .grid-item').first()
  }

  /**
   * View-agnostic selector: first directory item in either list or grid mode.
   * Matches .file-item.dir-item (list) or .grid-item.grid-dir (grid).
   */
  get firstDirectory(): Locator {
    return this.page.locator('.file-item.dir-item, .grid-item.grid-dir').first()
  }

  /** Get a file item by name (works in both view modes) */
  getFileItem(name: string): Locator {
    return this.page.locator('.file-item, .grid-item', { hasText: name })
  }

  /** Get the first directory item (works in both view modes) */
  getFirstDirectory(): Locator {
    return this.firstDirectory
  }

  /** Get the first regular file item (works in both view modes) */
  getFirstFile(): Locator {
    return this.page.locator('.file-item:not(.dir-item), .grid-item:not(.grid-dir)').first()
  }

  /** Navigate into a directory by double-clicking it (single click only selects on PC) */
  async openDirectory(name: string) {
    await this.getFileItem(name).dblclick()
  }

  /** Open a file in the viewer by double-clicking it */
  async openFile(name: string) {
    await this.getFileItem(name).dblclick()
  }

  /** Expect a file/directory with the given name to be visible */
  async expectFileVisible(name: string) {
    await expect(this.getFileItem(name)).toBeVisible()
  }

  /** Expect the file viewer to be visible */
  async expectFileViewerVisible() {
    await expect(this.fileViewer).toBeVisible()
  }

  /**
   * Wait for file manager content to be loaded and rendered.
   * Polls for any file item, empty state, or loading indicator.
   */
  async waitForContent(timeout = 10000): Promise<void> {
    await expect.poll(async () => {
      const fileItem = this.page.locator('.file-item, .grid-item').first()
      const emptyState = this.page.locator('.empty-state')
      const loadingOverlay = this.page.locator('.dir-loading-overlay')
      // Content is ready when items are visible or empty state is shown (and not still loading)
      if (await fileItem.isVisible().catch(() => false)) return true
      if (await emptyState.isVisible().catch(() => false)) return true
      // Still loading — not ready yet
      if (await loadingOverlay.isVisible().catch(() => false)) return false
      // Neither loading nor content — might still be initializing
      return false
    }, { timeout }).toBe(true)
  }
}
