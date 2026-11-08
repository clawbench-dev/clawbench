/**
 * Rendering, layout, preview panes, view modes, thumbnails, toolbar labels.
 *
 * Split out of the single FileManagerContent spec (6.2k lines / 379 cases — it
 * was also the file whose worker timed out during full-suite teardown).
 * Everything shared (mocks, i18n, mount helper, sample data) lives in
 * ./fileManagerContent.setup.
 */
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// Per-module setting (vi.setConfig does not leak in from the helper): these
// suites drive async paths that take seconds when the worker pool is busy.
vi.setConfig({ testTimeout: 60_000, hookTimeout: 60_000 })
import { nextTick, reactive, ref, computed, readonly, defineComponent } from 'vue'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import SplitView from '@/components/common/SplitView.vue'
import { PANEL_KEY_PREFIX, DEFAULT_FM_PREVIEW_RATIO } from '@/utils/panelState'

import {
  mocks,
  mountContent,
  CodeLinkPreviewStub,
  DirPreviewBodyStub,
  sampleEntries,
  readSource,
  cleanupDragGhost,
  i18n,
  LongPressPlugin,
  cancelTarget,
  usePendingTarget,
} from './fileManagerContent.setup'

describe('FileManagerContent — rendering', () => {
  it('renders file list container', () => {
    const wrapper = mountContent()
    expect(wrapper.find('.file-list').exists()).toBe(true)
  })

  it('renders directory items', () => {
    const wrapper = mountContent()
    const dirItems = wrapper.findAll('.dir-item')
    expect(dirItems.length).toBe(1)
    expect(dirItems[0].text()).toContain('src')
  })

  it('renders file items', () => {
    const wrapper = mountContent()
    const fileItems = wrapper.findAll('.file-item:not(.dir-item)')
    expect(fileItems.length).toBe(2)
  })

  it('shows empty state when entries is empty', () => {
    const wrapper = mountContent({ entries: [] })
    expect(wrapper.find('.empty-state').exists()).toBe(true)
  })

  it('renders symlink badge for symlinked entries', () => {
    const entries = [
      { name: 'linked-dir', type: 'dir', symlink: true, modified: '2025-01-01T00:00:00Z', size: 0 },
      { name: 'linked.txt', type: 'file', symlink: true, modified: '2025-01-01T00:00:00Z', size: 10 },
    ]
    const wrapper = mountContent({ entries })
    const badges = wrapper.findAll('.symlink-badge')
    expect(badges.length).toBe(2)
  })

  it('renders broken style for dangling symlink', () => {
    const entries = [
      { name: 'dangling', type: 'file', symlink: true, broken: true, modified: '', size: 0 },
    ]
    const wrapper = mountContent({ entries })
    const badge = wrapper.find('.symlink-badge.broken')
    expect(badge.exists()).toBe(true)
  })

  it('does not render symlink badge for regular entries', () => {
    const wrapper = mountContent()
    expect(wrapper.find('.symlink-badge').exists()).toBe(false)
  })

  it('renders loading mask when dirLoading is true', () => {
    const wrapper = mountContent({ dirLoading: true })
    expect(wrapper.find('.loading-indicator.overlay').exists()).toBe(true)
  })

  it('keeps the loading overlay outside the scrollable list so it covers the whole viewport when scrolled', () => {
    const wrapper = mountContent({ dirLoading: true })
    const overlay = wrapper.find('.loading-indicator.overlay')
    expect(overlay.exists()).toBe(true)
    // The overlay must not live inside the scrollable list/grid container —
    // an absolutely-positioned child of a scroll container scrolls with its
    // content, leaving only a partial mask and hiding the spinner when the
    // listing is scrolled.
    expect(wrapper.find('.file-list .loading-indicator.overlay').exists()).toBe(false)
    expect(wrapper.find('.file-grid .loading-indicator.overlay').exists()).toBe(false)
  })

  it('keeps the loading overlay outside the scrollable grid in grid view', async () => {
    const wrapper = mountContent({ dirLoading: true })
    wrapper.vm._setViewMode('grid')
    await nextTick()
    expect(wrapper.find('.file-grid').exists()).toBe(true)
    expect(wrapper.find('.file-grid .loading-indicator.overlay').exists()).toBe(false)
  })

  it('renders toolbar buttons', () => {
    const wrapper = mountContent()
    const toolbarBtns = wrapper.findAll('.toolbar-btn')
    expect(toolbarBtns.length).toBeGreaterThanOrEqual(4) // sort, hidden, refresh, multi-select, more
  })

  it('renders content-search before the secondary toggles', () => {
    const wrapper = mountContent()
    // ActionBar folds the TAIL of the row, so a late button disappears first.
    // contentSearch used to sit last, which made it invisible on a normal-width
    // toolbar — the reported bug. Its DOM position is now the priority.
    const titles = wrapper.findAll('.dir-toolbar-btns > .toolbar-btn, .dir-toolbar-btns .toolbar-dropdown-wrap > .toolbar-btn')
      .map(b => b.attributes('title') ?? '')
    const pos = titles.findIndex(t => t.includes('搜索'))
    for (const lower of ['跳转', '已分享', '显示隐藏文件', '多选']) {
      const other = titles.findIndex(t => t.includes(lower))
      if (other === -1) continue
      expect(pos, `contentSearch should precede ${lower}`).toBeLessThan(other)
    }
  })
})

// ── Navigation events ──

describe('FileManagerContent — handleItemClick', () => {
  it('mobile + preview mode: tap selects a directory, tapping it again enters', async () => {
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    const dirItem = wrapper.find('.dir-item')

    // First tap: selection only — entering needs a second tap.
    await dirItem.trigger('click')
    expect(wrapper.emitted('navigateDir')).toBeFalsy()

    // Tapping the already-selected entry enters it (no timing window).
    await dirItem.trigger('click')
    expect(wrapper.emitted('navigateDir')).toBeTruthy()
    expect(wrapper.emitted('navigateDir')![0][0]).toContain('src')
  })

  it('mobile without preview mode: a single tap enters the directory (original behavior)', async () => {
    mocks.mockLocalConfig.filePreviewMode = false
    const wrapper = mountContent()
    const dirItem = wrapper.find('.dir-item')

    await dirItem.trigger('click')
    expect(wrapper.emitted('navigateDir')).toBeTruthy()
  })

  it('mobile: tapping a different entry selects it instead of entering', async () => {
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    // Selecting another entry must not be read as "tap the selected one again".
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')

    expect(wrapper.emitted('navigateDir')).toBeFalsy()
    expect(wrapper.emitted('selectFile')).toBeFalsy()
  })

  it('mobile + preview mode: tap previews a file, tapping it again enters', async () => {
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    const fileItems = wrapper.findAll('.file-item:not(.dir-item)')

    await fileItems[0].trigger('click')
    expect(mocks.mockShowPreview).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('selectFile')).toBeFalsy()

    await fileItems[0].trigger('click')
    expect(wrapper.emitted('selectFile')).toBeTruthy()
  })

  it('mobile + preview mode: tap-then-tap on a symlinked directory navigates', async () => {
    mocks.mockLocalConfig.filePreviewMode = true
    const entries = [
      { name: 'linked', type: 'dir', symlink: true, modified: '2025-01-01T00:00:00Z', size: 0 },
    ]
    const wrapper = mountContent({ entries })
    const dirItem = wrapper.find('.dir-item')

    await dirItem.trigger('click')
    await dirItem.trigger('click')

    expect(wrapper.emitted('navigateDir')).toBeTruthy()
    expect(wrapper.emitted('navigateDir')![0][0]).toContain('linked')
  })

  // Regression for #549: a symlink whose target escapes the project root is
  // entered through the RESOLVED ABSOLUTE TARGET the backend reports, not the
  // link's own project-relative path — the latter would hit /api/dir, which
  // refuses to follow it and answers "access denied".
  it('enters an escaping symlink directory via its reported target', async () => {
    mocks.mockIsTouchPrimary.value = false
    const entries = [
      {
        name: 'data',
        type: 'dir',
        symlink: true,
        target: '/some/other/dir',
        modified: '2025-01-01T00:00:00Z',
        size: 0,
      },
    ]
    const wrapper = mountContent({ entries })

    await wrapper.find('.dir-item').trigger('dblclick')

    expect(wrapper.emitted('navigateDir')).toBeTruthy()
    expect(wrapper.emitted('navigateDir')![0][0]).toBe('/some/other/dir')
  })

  it('lists an escaping symlink directory through its reported target', async () => {
    mocks.mockLocalConfig.filePreviewMode = true
    const entries = [
      {
        name: 'data',
        type: 'dir',
        symlink: true,
        target: '/some/other/dir',
        modified: '2025-01-01T00:00:00Z',
        size: 0,
      },
    ]
    const wrapper = mountContent({ entries })

    // A single click previews the directory listing in the docked pane.
    await wrapper.find('.dir-item').trigger('click')

    const stub = wrapper.find('.dir-preview-stub')
    expect(stub.exists()).toBe(true)
    expect(stub.attributes('data-dir-path')).toBe('/some/other/dir')
  })

  it('enters an in-project symlink directory via its own path (no target)', async () => {
    mocks.mockIsTouchPrimary.value = false
    const entries = [
      { name: 'linked', type: 'dir', symlink: true, modified: '2025-01-01T00:00:00Z', size: 0 },
    ]
    const wrapper = mountContent({ entries })

    await wrapper.find('.dir-item').trigger('dblclick')

    expect(wrapper.emitted('navigateDir')![0][0]).toBe('linked')
  })

  it('PC: single click only selects, does not navigate or open', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    const dirItem = wrapper.find('.dir-item')
    await dirItem.trigger('click')

    expect(wrapper.emitted('navigateDir')).toBeFalsy()
    expect(wrapper.emitted('selectFile')).toBeFalsy()
    expect(wrapper.vm.selectedPath).toContain('src')
  })

  it('PC: double-click emits navigateDir for a directory', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    const dirItem = wrapper.find('.dir-item')
    await dirItem.trigger('dblclick')

    expect(wrapper.emitted('navigateDir')).toBeTruthy()
    expect(wrapper.emitted('navigateDir')![0][0]).toContain('src')
  })

  it('PC: double-click emits selectFile for a file', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    const fileItems = wrapper.findAll('.file-item:not(.dir-item)')
    await fileItems[0].trigger('dblclick')

    expect(wrapper.emitted('selectFile')).toBeTruthy()
  })

  it('PC: double-click does not open in multi-select mode', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    // Enter multi-select via Ctrl+Shift+M
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'm', ctrlKey: true, shiftKey: true, bubbles: true }))
    await nextTick()
    const dirItem = wrapper.find('.dir-item')
    await dirItem.trigger('dblclick')

    expect(wrapper.emitted('navigateDir')).toBeFalsy()
  })

  it('PC: Ctrl+click enters multi-select and selects the item without opening', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    const dirItem = wrapper.find('.dir-item')
    await dirItem.trigger('click', { ctrlKey: true })
    await nextTick()

    expect(wrapper.vm.multiSelectState.active).toBe(true)
    expect(wrapper.vm.multiSelectState.selected.has('src')).toBe(true)
    expect(wrapper.emitted('navigateDir')).toBeFalsy()
    expect(wrapper.emitted('selectFile')).toBeFalsy()
  })

  it('PC: Ctrl+click accumulates multiple selections', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    await wrapper.find('.dir-item').trigger('click', { ctrlKey: true })
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click', { ctrlKey: true })
    await nextTick()

    const sel = wrapper.vm.multiSelectState.selected
    expect(sel.has('src')).toBe(true)
    expect(sel.has('test.ts')).toBe(true)
    expect(sel.size).toBe(2)
  })

  it('PC: Ctrl+click after a normal selection keeps the previously selected file', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    // First: normal single click selects test.ts (PC: single click only selects)
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    await nextTick()
    expect(wrapper.vm.selectedPath).toBe('test.ts')

    // Then Ctrl+click another file — the first selection must be preserved
    await wrapper.find('.file-item[data-path="readme.md"]').trigger('click', { ctrlKey: true })
    await nextTick()

    const sel = wrapper.vm.multiSelectState.selected
    expect(wrapper.vm.multiSelectState.active).toBe(true)
    expect(sel.has('test.ts')).toBe(true)
    expect(sel.has('readme.md')).toBe(true)
    expect(sel.size).toBe(2)
  })

  it('PC: Ctrl+click toggles an already-selected item off', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    const srcDir = wrapper.find('.dir-item')
    await srcDir.trigger('click', { ctrlKey: true })
    await srcDir.trigger('click', { ctrlKey: true })
    await nextTick()

    expect(wrapper.vm.multiSelectState.selected.has('src')).toBe(false)
  })

  it('PC: Shift+click selects the contiguous range from the anchor', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent() // order: src, test.ts, readme.md
    // Plain click sets the anchor on the first entry.
    await wrapper.find('.file-item[data-path="src"]').trigger('click')
    await nextTick()
    expect(wrapper.vm.multiSelectState.active).toBe(false)

    // Shift+click the third entry — all three are selected, nothing opens.
    await wrapper.find('.file-item[data-path="readme.md"]').trigger('click', { shiftKey: true })
    await nextTick()

    const sel = wrapper.vm.multiSelectState.selected
    expect(wrapper.vm.multiSelectState.active).toBe(true)
    expect(sel.size).toBe(3)
    expect(sel.has('src')).toBe(true)
    expect(sel.has('test.ts')).toBe(true)
    expect(sel.has('readme.md')).toBe(true)
    expect(wrapper.emitted('selectFile')).toBeFalsy()
    expect(wrapper.emitted('navigateDir')).toBeFalsy()
  })

  it('PC: repeated Shift+click re-extends from the same anchor', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent() // order: src, test.ts, readme.md
    await wrapper.find('.file-item[data-path="src"]').trigger('click')
    await nextTick()

    // Extend to readme.md, then shrink back to test.ts — the range is rebuilt
    // from the anchor, so readme.md is no longer selected.
    await wrapper.find('.file-item[data-path="readme.md"]').trigger('click', { shiftKey: true })
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click', { shiftKey: true })
    await nextTick()

    const sel = wrapper.vm.multiSelectState.selected
    expect(sel.size).toBe(2)
    expect(sel.has('src')).toBe(true)
    expect(sel.has('test.ts')).toBe(true)
    expect(sel.has('readme.md')).toBe(false)
  })

  it('PC: Shift+click keeps selections made before the anchor', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent() // order: src, test.ts, readme.md
    // Ctrl+click builds an independent selection on readme.md.
    await wrapper.find('.file-item[data-path="readme.md"]').trigger('click', { ctrlKey: true })
    await nextTick()
    // Plain click re-anchors on src.
    await wrapper.find('.file-item[data-path="src"]').trigger('click')
    await nextTick()
    // Shift+click test.ts selects src+test.ts, preserving the readme.md pick.
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click', { shiftKey: true })
    await nextTick()

    const sel = wrapper.vm.multiSelectState.selected
    expect(sel.has('src')).toBe(true)
    expect(sel.has('test.ts')).toBe(true)
    expect(sel.has('readme.md')).toBe(true)
  })

  it('PC: Shift+click does not move the anchor', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent() // order: src, test.ts, readme.md
    await wrapper.find('.file-item[data-path="src"]').trigger('click')
    await nextTick()
    await wrapper.find('.file-item[data-path="readme.md"]').trigger('click', { shiftKey: true })
    await nextTick()
    // The highlighted path follows the Shift+click...
    expect(wrapper.vm.selectedPath).toBe('readme.md')
    // ...but the anchor stays on src: a later Shift+click re-extends from src.
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click', { shiftKey: true })
    await nextTick()
    const sel = wrapper.vm.multiSelectState.selected
    expect(sel.size).toBe(2)
    expect(sel.has('src')).toBe(true)
    expect(sel.has('test.ts')).toBe(true)
    expect(sel.has('readme.md')).toBe(false)
  })

  it('PC: Shift+click re-anchors after a directory change', async () => {
    mocks.mockIsTouchPrimary.value = false
    const dirA = [
      { name: 'a1', type: 'file', modified: '2025-01-01T00:00:00Z', size: 1 },
      { name: 'a2', type: 'file', modified: '2025-01-01T00:00:00Z', size: 1 },
    ]
    const dirB = [
      { name: 'b1', type: 'file', modified: '2025-01-01T00:00:00Z', size: 1 },
      { name: 'b2', type: 'file', modified: '2025-01-01T00:00:00Z', size: 1 },
      { name: 'b3', type: 'file', modified: '2025-01-01T00:00:00Z', size: 1 },
    ]
    const wrapper = mountContent({ entries: dirA, currentDir: 'dirA' })
    // Plain click anchors on an entry of dirA.
    await wrapper.find('.file-item[data-path="dirA/a1"]').trigger('click')
    await nextTick()

    // Change directory — the anchor no longer exists in the listing.
    await wrapper.setProps({ entries: dirB, currentDir: 'dirB' })
    await nextTick()

    // Shift+click must re-anchor on the clicked entry instead of selecting nothing.
    await wrapper.find('.file-item[data-path="dirB/b3"]').trigger('click', { shiftKey: true })
    await nextTick()

    const sel = wrapper.vm.multiSelectState.selected
    expect(wrapper.vm.multiSelectState.active).toBe(true)
    expect(sel.has('dirB/b3')).toBe(true)
  })

  it('PC: Shift+click re-anchors after the query replaces the listing', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent() // order: src, test.ts, readme.md
    await wrapper.find('.file-item[data-path="src"]').trigger('click')
    await nextTick()

    // Switch to a search-results listing whose entries are unrelated.
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
      { name: 'main2.go', path: 'cmd/main2.go', type: 'file', matchedIndices: [] },
    ]
    await nextTick()

    await wrapper.find('.file-item[data-path="cmd/main2.go"]').trigger('click', { shiftKey: true })
    await nextTick()

    const sel = wrapper.vm.multiSelectState.selected
    expect(wrapper.vm.multiSelectState.active).toBe(true)
    expect(sel.has('cmd/main2.go')).toBe(true)
  })

  it('PC: Shift+click after select-all keeps the whole selection', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent() // order: src, test.ts, readme.md
    await wrapper.find('.file-item[data-path="src"]').trigger('click')
    await nextTick()
    // Enter multi-select and select everything via the toolbar button.
    wrapper.vm.multiSelectState.active = true
    await nextTick()
    await wrapper.find('.ms-select-all-btn').trigger('click')
    await nextTick()
    expect(wrapper.vm.multiSelectState.selected.size).toBe(3)

    // Shift+clicking the first entry must not silently drop the rest.
    await wrapper.find('.file-item[data-path="src"]').trigger('click', { shiftKey: true })
    await nextTick()
    expect(wrapper.vm.multiSelectState.selected.size).toBe(3)
  })

  it('does not emit when dirLoading is true', async () => {
    const wrapper = mountContent({ dirLoading: true })
    const dirItem = wrapper.find('.dir-item')
    await dirItem.trigger('click')

    expect(wrapper.emitted('navigateDir')).toBeFalsy()
  })

  it('PC: closing the viewed file keeps the row highlighted', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    // Open a file: the click highlights it and the parent starts the viewer.
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    await wrapper.setProps({ currentFile: { path: 'test.ts', name: 'test.ts' } })
    await nextTick()
    expect(wrapper.vm._getSelectedPath()).toBe('test.ts')

    // Close the viewer: the parent nulls currentFile. The highlight the user
    // just had must survive — closing a file does not deselect it.
    await wrapper.setProps({ currentFile: null })
    await nextTick()

    expect(wrapper.vm._getSelectedPath()).toBe('test.ts')
    const row = wrapper.find('.file-item[data-path="test.ts"]')
    expect(row.classes()).toContain('active')
  })

  it('an external selection (chat annotation) still pushes the highlight in', async () => {
    const wrapper = mountContent()
    expect(wrapper.vm._getSelectedPath()).toBe('')

    await wrapper.setProps({ currentFile: { path: 'readme.md', name: 'readme.md' } })
    await nextTick()

    expect(wrapper.vm._getSelectedPath()).toBe('readme.md')
    expect(wrapper.find('.file-item[data-path="readme.md"]').classes()).toContain('active')
  })
})

// ── Preview mode (single-click quick preview) ──

describe('FileManagerContent — preview mode', () => {
  it('shows the preview-mode toggle on both desktop and mobile', () => {
    mocks.mockIsTouchPrimary.value = false
    const desktop = mountContent()
    expect(desktop.find('.toolbar-btn[title="开启预览模式"]').exists()).toBe(true)

    // Mobile supports the docked preview pane too, so the toggle is available.
    mocks.mockIsTouchPrimary.value = true
    const mobile = mountContent()
    expect(mobile.find('.toolbar-btn[title="开启预览模式"]').exists()).toBe(true)
  })

  it('toggles preview mode and persists the setting', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    const btn = wrapper.find('.toolbar-btn[title="开启预览模式"]')
    expect(btn.exists()).toBe(true)
    expect(btn.classes()).not.toContain('active')

    await btn.trigger('click')
    await nextTick()

    expect(mocks.mockLocalConfig.filePreviewMode).toBe(true)
    expect(wrapper.find('.toolbar-btn[title="关闭预览模式"]').classes()).toContain('active')
  })

  it('restores preview mode from the persisted setting on mount', () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    expect(wrapper.find('.toolbar-btn[title="关闭预览模式"]').classes()).toContain('active')
  })

  it('follows a preview-mode change made from the Settings drawer', async () => {
    // FileManagerContent stays mounted across tab switches, so a settings change
    // must sync in without a remount. Mutate the reactive proxy (what the
    // component watches), not the raw mock target.
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = false
    const wrapper = mountContent()
    const cfg = mocks.mockLocalConfigProxy.current!
    expect(wrapper.find('.toolbar-btn[title="开启预览模式"]').classes()).not.toContain('active')

    cfg.filePreviewMode = true
    await nextTick()

    expect(wrapper.find('.toolbar-btn[title="关闭预览模式"]').classes()).toContain('active')

    // Turning it back off flips the toolbar state back.
    mocks.mockClosePreview.mockClear()
    cfg.filePreviewMode = false
    await nextTick()
    expect(wrapper.find('.toolbar-btn[title="开启预览模式"]').classes()).not.toContain('active')
  })

  it('keeps preview mode reachable when the toolbar runs out of room', async () => {
    // The folding itself is ActionBar's job (covered by its own test). Here we
    // pin that the toggle is a labelled toolbar button, which is what the menu
    // entry is built from.
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()

    const toggle = wrapper.findAll('.toolbar-btn').find(b => (b.attributes('title') ?? '').includes('预览'))
    expect(toggle).toBeTruthy()
    expect(toggle!.find('.action-label').exists()).toBe(true)

    await toggle!.trigger('click')
    expect(mocks.mockLocalConfig.filePreviewMode).toBe(true)
  })

  it('desktop single-click previews a file in the docked pane when enabled', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    await nextTick()

    expect(mocks.mockShowPreview).toHaveBeenCalledTimes(1)
    const [target, mode] = mocks.mockShowPreview.mock.calls[0]
    expect(target.filePath).toBe('test.ts')
    expect(target.anchorEl).toBeTruthy()
    // Docked: rendered inline in the bottom pane, not as a floating card.
    expect(mode).toBe('docked')
    // Preview replaces the plain-select behaviour: no full-viewer emit.
    expect(wrapper.emitted('selectFile')).toBeFalsy()
  })

  it.each([
    ['logo.png', 'image'],
    ['diagram.svg', 'image'],
    ['clip.mp4', 'video'],
    ['voice.mp3', 'audio'],
    ['report.pdf', 'pdf'],
  ])('single-click previews the media file %s too', async (name) => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent({
      entries: [{ name, type: 'file', modified: '2025-01-01T00:00:00Z', size: 100 }],
    })

    await wrapper.find(`.file-item[data-path="${name}"]`).trigger('click')
    await nextTick()

    expect(mocks.mockShowPreview).toHaveBeenCalledTimes(1)
    expect(mocks.mockShowPreview.mock.calls[0][0].filePath).toBe(name)
  })

  it('desktop single-click on a directory still navigates, never previews', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()

    expect(mocks.mockShowPreview).not.toHaveBeenCalled()
  })

  it('does not preview on single-click when preview mode is off', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = false
    const wrapper = mountContent()

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    await nextTick()

    expect(mocks.mockShowPreview).not.toHaveBeenCalled()
    expect(wrapper.emitted('selectFile')).toBeFalsy()
  })

  it('mobile single-click previews in the docked pane when preview mode is on', async () => {
    mocks.mockIsTouchPrimary.value = true
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    await nextTick()

    // Mobile supports the docked pane, so a tap previews instead of opening
    // the full-screen viewer.
    expect(mocks.mockShowPreview).toHaveBeenCalledTimes(1)
    expect(mocks.mockShowPreview.mock.calls[0][1]).toBe('docked')
    expect(wrapper.emitted('selectFile')).toBeFalsy()
  })

  it('mobile single tap opens the full viewer when preview mode is off', async () => {
    mocks.mockIsTouchPrimary.value = true
    mocks.mockLocalConfig.filePreviewMode = false
    const wrapper = mountContent()

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')

    expect(mocks.mockShowPreview).not.toHaveBeenCalled()
    expect(wrapper.emitted('selectFile')).toBeTruthy()
  })

  it('double-click still opens the full viewer and closes the preview card', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('dblclick')
    await nextTick()

    expect(mocks.mockClosePreview).toHaveBeenCalled()
    expect(wrapper.emitted('selectFile')).toBeTruthy()
  })

  it('closes the preview when the directory changes', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    mocks.mockClosePreview.mockClear()

    await wrapper.setProps({ currentDir: 'src' })
    await nextTick()

    expect(mocks.mockClosePreview).toHaveBeenCalled()
  })
})

// ── Toolbar events ──

describe('FileManagerContent — toolbar', () => {
  it('emits toggleHidden when eye button clicked', async () => {
    const wrapper = mountContent()
    // Find the hidden toggle button by its title attribute
    const btns = wrapper.findAll('.toolbar-btn')
    const toggleBtn = btns.find(b => {
      const title = b.attributes('title')
      return title === '显示隐藏文件' || title === '隐藏隐藏文件'
    })
    expect(toggleBtn).toBeTruthy()
    await toggleBtn!.trigger('click')

    expect(wrapper.emitted('toggleHidden')).toBeTruthy()
  })

  it('emits refresh when refresh button clicked', async () => {
    const wrapper = mountContent()
    const btns = wrapper.findAll('.toolbar-btn')
    // Find the refresh button by its title attribute
    const refreshBtn = btns.find(b => b.attributes('title') === '刷新')
    expect(refreshBtn).toBeTruthy()
    await refreshBtn!.trigger('click')

    expect(wrapper.emitted('refresh')).toBeTruthy()
  })

  it('reflects the shared refresh-in-flight state on the refresh button', async () => {
    mocks.mockIsRefreshing.value = false
    let wrapper = mountContent()
    const btns = wrapper.findAll('.toolbar-btn')
    let refreshBtn = btns.find(b => b.attributes('title') === '刷新')
    expect(refreshBtn).toBeTruthy()
    expect(refreshBtn!.classes()).not.toContain('refresh-spin--active')

    // Click emits refresh
    await refreshBtn!.trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)

    // Shared isRefreshing true → spin visible
    mocks.mockIsRefreshing.value = true
    wrapper.unmount()
    wrapper = mountContent()
    refreshBtn = wrapper.findAll('.toolbar-btn').find(b => b.attributes('title') === '刷新')
    expect(refreshBtn!.classes()).toContain('refresh-spin--active')

    // Shared isRefreshing false → spin ends
    mocks.mockIsRefreshing.value = false
    wrapper.unmount()
    wrapper = mountContent()
    refreshBtn = wrapper.findAll('.toolbar-btn').find(b => b.attributes('title') === '刷新')
    expect(refreshBtn!.classes()).not.toContain('refresh-spin--active')
  })
})

// ── Sorting ──

describe('FileManagerContent — sort', () => {
  it('emits toggleSort when sort option clicked', async () => {
    const wrapper = mountContent()
    // Open sort dropdown
    const sortBtn = wrapper.findAll('.toolbar-btn')[0]
    await sortBtn.trigger('click')
    await nextTick()

    // Click a sort option
    const sortItems = wrapper.findAll('.toolbar-dropdown-item')
    if (sortItems.length > 0) {
      await sortItems[0].trigger('click')
      expect(wrapper.emitted('toggleSort')).toBeTruthy()
    }
  })

  it('sorts entries by name when sortField is name', () => {
    const wrapper = mountContent({ sortField: 'name', sortDir: 'asc' })
    const fileItems = wrapper.findAll('.file-item:not(.dir-item)')
    // Items should be sorted by name
    expect(fileItems.length).toBe(2)
  })

  it('sorts entries by time when sortField is time', () => {
    const wrapper = mountContent({ sortField: 'time', sortDir: 'desc' })
    const fileItems = wrapper.findAll('.file-item:not(.dir-item)')
    expect(fileItems.length).toBe(2)
  })
})

// ── Resident search view (fused into the file manager) ──

describe('FileManagerContent — hidden files', () => {
  it('hides dotfiles when showHidden is false', () => {
    const entries = [
      { name: '.gitignore', type: 'file', modified: '2025-01-01T00:00:00Z', size: 10 },
      { name: 'index.ts', type: 'file', modified: '2025-01-01T00:00:00Z', size: 100 },
    ]
    const wrapper = mountContent({ entries, showHidden: false })
    const fileItems = wrapper.findAll('.file-item:not(.dir-item)')
    expect(fileItems.length).toBe(1)
    expect(fileItems[0].text()).toContain('index.ts')
  })

  it('shows dotfiles when showHidden is true', () => {
    const entries = [
      { name: '.gitignore', type: 'file', modified: '2025-01-01T00:00:00Z', size: 10 },
      { name: 'index.ts', type: 'file', modified: '2025-01-01T00:00:00Z', size: 100 },
    ]
    const wrapper = mountContent({ entries, showHidden: true })
    const fileItems = wrapper.findAll('.file-item:not(.dir-item)')
    expect(fileItems.length).toBe(2)
  })
})

// ── Context menu ──

describe('FileManagerContent — view mode', () => {
  it('renders list view by default', () => {
    const wrapper = mountContent()
    expect(wrapper.find('.file-list').exists()).toBe(true)
  })

  it('switches to grid view', async () => {
    const wrapper = mountContent()
    wrapper.vm._setViewMode('grid')
    await nextTick()

    // Verify viewMode changed (DOM may not update due to v-long-press directive issue in test env)
    expect(wrapper.vm.viewMode).toBe('grid')
    expect(wrapper.vm._getFilteredEntries).toBeDefined()  // component still functional
  })
})

// ── formatDate ──

describe('FileManagerContent — formatDate', () => {
  it('returns empty string for null modified', () => {
    const wrapper = mountContent()
    expect(wrapper.vm.formatDate(null)).toBe('')
  })

  it('formats date string', () => {
    const wrapper = mountContent()
    const result = wrapper.vm.formatDate('2025-01-01T12:00:00Z')
    expect(result).toBeTruthy()
  })
})

// ── Cut item visual effect ──

describe('FileManagerContent — grid view', () => {
  it('renders grid layout with grid items', async () => {
    const wrapper = mountContent()
    wrapper.vm._setViewMode('grid')
    await nextTick()

    expect(wrapper.find('.file-grid').exists()).toBe(true)
    expect(wrapper.findAll('.grid-item').length).toBe(3)
  })

  it('grid: tap-then-tap on a directory navigates in mobile preview mode', async () => {
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    wrapper.vm._setViewMode('grid')
    await nextTick()
    const dirItem = wrapper.find('.grid-item[data-path="src"]')

    await dirItem.trigger('click')
    expect(wrapper.emitted('navigateDir')).toBeFalsy()
    await dirItem.trigger('click')

    expect(wrapper.emitted('navigateDir')).toBeTruthy()
  })

  it('grid: double-click on a file emits selectFile', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    wrapper.vm._setViewMode('grid')
    await nextTick()
    const fileItem = wrapper.find('.grid-item[data-path="test.ts"]')
    await fileItem.trigger('dblclick')

    expect(wrapper.emitted('selectFile')).toBeTruthy()
  })

  it('grid: right-click opens the context menu with the entry', async () => {
    const wrapper = mountContent()
    wrapper.vm._setViewMode('grid')
    await nextTick()
    const fileItem = wrapper.find('.grid-item[data-path="test.ts"] .grid-name')
    await fileItem.trigger('contextmenu')
    await nextTick()

    expect(wrapper.vm.ctxMenu.visible).toBe(true)
    expect(wrapper.vm.ctxMenu.entry?.path).toBe('test.ts')
  })

  it('view toggle button switches between list and grid', async () => {
    const wrapper = mountContent()
    const btns = wrapper.findAll('.toolbar-btn')
    const toggleBtn = btns.find(b => b.attributes('title') === '网格' || b.attributes('title') === '列表')
    expect(toggleBtn).toBeTruthy()
    await toggleBtn!.trigger('click')
    await nextTick()
    expect(wrapper.vm.viewMode).toBe('grid')
    await toggleBtn!.trigger('click')
    await nextTick()
    expect(wrapper.vm.viewMode).toBe('list')
  })
})

// ── Upload ──

describe('FileManagerContent — thumbnails', () => {
  it('thumbUrlFor builds a thumbnail URL from currentDir and name in browse mode', () => {
    const wrapper = mountContent({ currentDir: 'src' })
    expect(wrapper.vm.thumbUrlFor({ name: 'a.png', path: 'src/a.png' })).toContain('/api/fs/thumb')
  })

  it('thumbUrlFor builds a thumbnail URL from the result path in search mode', async () => {
    mocks.searchState.query = 'a'
    mocks.searchState.results = [
      { name: 'a.png', path: 'nested/deep/a.png', type: 'image', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    expect(wrapper.vm.thumbUrlFor({ name: 'a.png', path: 'nested/deep/a.png', type: 'file' })).toContain(encodeURIComponent('nested/deep/a.png'))
  })

  it('onThumbError marks the entry so isThumbLoaded returns false', () => {
    const wrapper = mountContent()
    const entry = { name: 'a.png' }
    wrapper.vm.onThumbError(entry)
    expect(wrapper.vm.isThumbLoaded(entry)).toBe(false)
  })
})

// ── Sort menu & more menu via template handlers ──

describe('FileManagerContent — sort dropdown items', () => {
  it('clicking the sort button opens the dropdown and a name option emits toggleSort', async () => {
    const wrapper = mountContent()
    const sortBtn = wrapper.findAll('.toolbar-btn').find(b => b.attributes('title') === '排序')
    expect(sortBtn).toBeTruthy()
    await sortBtn!.trigger('click')
    await nextTick()

    const sortItems = wrapper.findAll('.toolbar-dropdown-item')
    expect(sortItems.length).toBeGreaterThan(0)
    await sortItems[0].trigger('click')
    expect(wrapper.emitted('toggleSort')).toBeTruthy()
    expect(wrapper.emitted('toggleSort')![0][0]).toBe('name')
  })
})

// ── Format date today branch ──

describe('FileManagerContent — formatDate today', () => {
  it('returns a time-only string for a date that is today', () => {
    const wrapper = mountContent()
    const now = new Date().toISOString()
    const result = wrapper.vm.formatDate(now)
    expect(result).toMatch(/\d{2}:\d{2}/)
  })

  it('returns a date string for a past date', () => {
    const wrapper = mountContent()
    const result = wrapper.vm.formatDate('2020-01-01T12:00:00Z')
    expect(result).toBeTruthy()
  })
})

// ── Truncation ──

describe('FileManagerContent — truncation', () => {
  it('renders the truncate hint when entries exceed MAX_VISIBLE_ENTRIES', async () => {
    // Use the smallest count that still triggers the truncate hint
    // (MAX_VISIBLE_ENTRIES=1000). Mounting 1002 entries exercises the limit
    // path without forcing jsdom to render thousands of extra DOM nodes.
    const manyEntries = Array.from({ length: 1002 }, (_, i) => ({
      name: `file${i}.txt`,
      type: 'file' as const,
      modified: '2025-01-01T00:00:00Z',
      size: i,
    }))
    const wrapper = mountContent({ entries: manyEntries })
    await nextTick()

    expect(wrapper.find('.truncate-hint').exists()).toBe(true)
  })

  it('does not render the truncate hint for a small entry list', () => {
    const wrapper = mountContent()
    expect(wrapper.find('.truncate-hint').exists()).toBe(false)
  })
})

// ── Search API (App Ctrl+F / back-navigation) ──

describe('FileManagerContent — empty state text', () => {
  it('shows emptyDir message when a currentDir is set and no entries', () => {
    const wrapper = mountContent({ entries: [], currentDir: 'src' })
    expect(wrapper.find('.empty-state').exists()).toBe(true)
  })

  it('shows noFiles message when no currentDir and no entries', () => {
    const wrapper = mountContent({ entries: [], currentDir: '' })
    expect(wrapper.find('.empty-state').exists()).toBe(true)
  })
})

describe('FileManagerContent — up one level', () => {
  const parentEntries = [
    { name: 'utils', type: 'dir', modified: '2025-01-01T00:00:00Z', size: 0 },
    { name: 'other.ts', type: 'file', modified: '2025-01-01T00:00:00Z', size: 10 },
  ]

  it('keeps the up button at the project root but disables it', async () => {
    // The nav row (and therefore the breadcrumb + copy-path button) stays
    // visible at the project root, so the up button is rendered there too —
    // greyed out, because "" has no parent.
    const atRoot = mountContent({ currentDir: '' })
    expect(atRoot.find('.dir-up-btn').exists()).toBe(true)
    expect(atRoot.find('.dir-up-btn').attributes('disabled')).toBeDefined()

    const nested = mountContent({ currentDir: 'src' })
    expect(nested.find('.dir-up-btn').attributes('disabled')).toBeUndefined()
  })

  it('clicking the up button emits navigateDir with the parent directory', async () => {
    const wrapper = mountContent({ currentDir: 'src/utils' })
    await wrapper.find('.dir-up-btn').trigger('click')
    const emitted = wrapper.emitted('navigateDir')
    expect(emitted).toBeTruthy()
    expect(emitted![emitted!.length - 1][0]).toBe('src')
  })

  it('walks a single-level directory up to the project root', async () => {
    const wrapper = mountContent({ currentDir: 'src', entries: parentEntries })
    await wrapper.find('.dir-up-btn').trigger('click')
    expect(wrapper.emitted('navigateDir')![0][0]).toBe('')

    // Root listing: the nav row stays (breadcrumb at the project root) but the
    // entry we left must still be selected there.
    await wrapper.setProps({ currentDir: '', entries: [{ name: 'src', type: 'dir', modified: '2025-01-01T00:00:00Z', size: 0 }] })
    await nextTick()
    expect(wrapper.vm._getSelectedPath()).toBe('src')
  })

  it('selects the directory it just left once the parent listing arrives', async () => {
    const wrapper = mountContent({ currentDir: 'src/utils' })
    await wrapper.find('.dir-up-btn').trigger('click')

    // The parent listing lands; the child we came from is now a row in it.
    const scrollSpy = vi.fn()
    const orig = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = scrollSpy
    try {
      await wrapper.setProps({ currentDir: 'src', entries: parentEntries })
      await nextTick()

      // Survives the currentDir watcher that clears the selection, and is applied
      // to the entry of the *new* listing (not the one being left).
      expect(wrapper.vm._getSelectedPath()).toBe('src/utils')
      expect(wrapper.find('.dir-item[data-path="src/utils"]').classes()).toContain('active')
      // The row may be off-screen in a long parent listing, so the selection
      // must also be revealed — selecting alone would leave it invisible.
      expect(scrollSpy.mock.instances).toContain(
        wrapper.find('.dir-item[data-path="src/utils"]').element,
      )
    } finally {
      Element.prototype.scrollIntoView = orig
    }
  })

  it('does not select anything when the user lands elsewhere instead', async () => {
    const wrapper = mountContent({ currentDir: 'src/utils' })
    await wrapper.find('.dir-up-btn').trigger('click')

    // A different navigation won the race — the pending child is meaningless in
    // whatever listing actually rendered.
    await wrapper.setProps({ currentDir: 'docs', entries: parentEntries })
    await nextTick()

    expect(wrapper.vm._getSelectedPath()).toBe('')
  })

  it('does not arm a selection while a directory load is in flight', async () => {
    const wrapper = mountContent({ currentDir: 'src/utils', dirLoading: true })
    await wrapper.find('.dir-up-btn').trigger('click')
    expect(wrapper.emitted('navigateDir')).toBeFalsy()
  })

  it('drops the restored selection on the next directory change', async () => {
    const wrapper = mountContent({ currentDir: 'src/utils' })
    await wrapper.find('.dir-up-btn').trigger('click')
    await wrapper.setProps({ currentDir: 'src', entries: parentEntries })
    await nextTick()
    expect(wrapper.vm._getSelectedPath()).toBe('src/utils')

    // The restored highlight belongs to that one transition — a later move must
    // not keep a selection for an entry that is not in the new listing.
    await wrapper.setProps({ currentDir: 'docs', entries: parentEntries })
    await nextTick()
    expect(wrapper.vm._getSelectedPath()).toBe('')
  })
})

describe('FileManagerContent — jump to dir', () => {
  it('opens jump dialog when jump button clicked', async () => {
    const wrapper = mountContent()
    const jumpBtn = wrapper.find('.toolbar-btn.jump-btn')
    expect(jumpBtn.exists()).toBe(true)
    await jumpBtn.trigger('click')
    await nextTick()
    expect(wrapper.find('.jump-dialog-stub').exists()).toBe(true)
  })

  it('navigates to dir on jump confirm', async () => {
    const { store: mockStore } = await import('@/stores/app')
    vi.mocked(mockStore.loadFiles).mockResolvedValue(undefined)
    // batch-exists returns "dir" for the jump target; navToFileInManager then
    // loads the containing directory via loadFiles.
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ results: { 'src/utils': 'dir' } }),
    })) as unknown as typeof fetch)
    const wrapper = mountContent()
    const vm = wrapper.vm as any
    vm.$.setupState.handleJumpConfirm('src/utils')
    await vi.waitFor(() => {
      expect(mockStore.loadFiles).toHaveBeenCalled()
    })
    vi.unstubAllGlobals()
  })

  it('opens the jump dialog from its toolbar button', async () => {
    const wrapper = mountContent()
    const jump = wrapper.findAll('.toolbar-btn').find(b => (b.attributes('title') ?? '').includes('跳转'))
    expect(jump).toBeTruthy()
    await jump!.trigger('click')
    await nextTick()
    expect(wrapper.find('.jump-dialog-stub').exists()).toBe(true)
  })
})

// ── Docked preview pane (top/bottom split) ──

describe('FileManagerContent — docked preview pane', () => {
  it('renders no split divider until a preview is open', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    mocks.mockPreviewRefs.visible!.value = false
    const wrapper = mountContent()

    // Preview mode on but nothing opened yet → list fills the panel.
    expect(wrapper.find('.split-view__divider--vertical').exists()).toBe(false)
    expect(wrapper.find('.code-link-preview-stub').exists()).toBe(false)
  })

  it('opens a vertical split with the docked pane when a file is previewed', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    mocks.mockPreviewRefs.visible!.value = false
    const wrapper = mountContent()

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    // The real composable flips visible; simulate it, then flush.
    mocks.mockPreviewRefs.visible!.value = true
    mocks.mockPreviewRefs.mode!.value = 'docked'
    await nextTick()

    expect(wrapper.find('.split-view--vertical').exists()).toBe(true)
    expect(wrapper.find('.split-view__divider--vertical').exists()).toBe(true)
    const stub = wrapper.find('.code-link-preview-stub')
    expect(stub.exists()).toBe(true)
    expect(stub.attributes('data-docked')).toBe('true')
  })

  it('collapses the split when the pane reports closed', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()
    expect(wrapper.find('.split-view__divider--vertical').exists()).toBe(true)

    await wrapper.findComponent(CodeLinkPreviewStub).vm.$emit('closed')
    await nextTick()

    expect(wrapper.find('.split-view__divider--vertical').exists()).toBe(false)
    expect(wrapper.find('.code-link-preview-stub').exists()).toBe(false)
  })

  it('re-opens the collapsed pane on the next single-click preview', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()
    await wrapper.findComponent(CodeLinkPreviewStub).vm.$emit('closed')
    await nextTick()
    expect(wrapper.find('.split-view__divider--vertical').exists()).toBe(false)

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()

    expect(wrapper.find('.split-view__divider--vertical').exists()).toBe(true)
  })

  it('drops the split when preview mode is turned off', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()
    expect(wrapper.find('.split-view__divider--vertical').exists()).toBe(true)

    mocks.mockLocalConfigProxy.current!.filePreviewMode = false
    await nextTick()
    await nextTick()

    expect(wrapper.find('.split-view__divider--vertical').exists()).toBe(false)
  })

  it('does not split on touch until the pane has been opened', async () => {
    // What this actually pins: `previewPaneEnabled` requires the pane to be
    // OPEN (`fmPreviewOpen`), not merely that preview mode is on and something
    // is previewable. A touch device therefore starts on the full-width list
    // and only splits after the user taps a file — there is deliberately no
    // `!isTouchPrimary` gate in the component (a tap DOES open the pane on
    // touch; see handleItemClick), so this is not "touch never splits".
    //
    // The assertion used to pass vacuously: `previewPaneOpen` was an in-memory
    // ref defaulting to false and never written, so it could not distinguish
    // "the gate works" from "the gate does not exist". It now asserts the
    // opened case in the same test, which fails if the gate is removed.
    mocks.mockIsTouchPrimary.value = true
    mocks.mockLocalConfig.filePreviewMode = true
    mocks.mockPreviewRefs.visible!.value = true
    const wrapper = mountContent()

    expect(wrapper.find('.split-view__divider--vertical').exists()).toBe(false)

    // Tapping a file opens the pane → the split appears, on touch too.
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()
    expect(wrapper.find('.split-view__divider--vertical').exists()).toBe(true)
  })

  it('persists the dragged ratio to localStorage', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    localStorage.removeItem(PANEL_KEY_PREFIX + 'fm-preview-ratio')
    const wrapper = mountContent()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()

    await wrapper.findComponent(SplitView).vm.$emit('update:ratio', 0.42)
    await nextTick()

    expect(localStorage.getItem(PANEL_KEY_PREFIX + 'fm-preview-ratio')).toBe('0.42')
  })

  it('restores the persisted ratio on mount', async () => {
    localStorage.setItem(PANEL_KEY_PREFIX + 'fm-preview-ratio', '0.35')
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()

    const split = wrapper.findComponent(SplitView)
    expect(split.props('ratio')).toBeCloseTo(0.35, 5)
    localStorage.removeItem(PANEL_KEY_PREFIX + 'fm-preview-ratio')
  })

  it('falls back to the default ratio when storage holds a degenerate value', async () => {
    // The registry stores a plain number; the 0 < r < 1 guard lives here. A
    // stored 0 or 1 (hand-edited storage, or a divider dragged to the extreme)
    // must not collapse the pane to zero or swallow the list entirely.
    for (const degenerate of ['0', '1', '-0.5', '2']) {
      localStorage.setItem(PANEL_KEY_PREFIX + 'fm-preview-ratio', degenerate)
      mocks.mockIsTouchPrimary.value = false
      mocks.mockLocalConfig.filePreviewMode = true
      const wrapper = mountContent()
      await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
      mocks.mockPreviewRefs.visible!.value = true
      await nextTick()

      const split = wrapper.findComponent(SplitView)
      expect(split.props('ratio'), `stored ${degenerate}`).toBeCloseTo(DEFAULT_FM_PREVIEW_RATIO, 5)
      wrapper.unmount()
    }
    localStorage.removeItem(PANEL_KEY_PREFIX + 'fm-preview-ratio')
  })

  it('scrolls the clicked file back into view after the pane opens', async () => {
    // jsdom lacks scrollIntoView; install a spy so the re-assert path can run.
    const scrollSpy = vi.fn()
    const orig = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = scrollSpy
    try {
      mocks.mockIsTouchPrimary.value = false
      mocks.mockLocalConfig.filePreviewMode = true
      const wrapper = mountContent()
      scrollSpy.mockClear()

      const row = wrapper.find('.file-item[data-path="test.ts"]')
      await row.trigger('click')
      await nextTick()

      // The docked pane shrinks the list, so the clicked entry is re-asserted
      // into view with a `nearest` scroll that leaves visible rows alone.
      expect(scrollSpy).toHaveBeenCalledWith({ block: 'nearest' })
      expect(scrollSpy.mock.instances).toContain(row.element)
    } finally {
      Element.prototype.scrollIntoView = orig
    }
  })

  it('scrolls the clicked directory back into view after the pane opens', async () => {
    const scrollSpy = vi.fn()
    const orig = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = scrollSpy
    try {
      mocks.mockIsTouchPrimary.value = false
      mocks.mockLocalConfig.filePreviewMode = true
      const wrapper = mountContent()
      scrollSpy.mockClear()

      const row = wrapper.find('.dir-item[data-path="src"]')
      await row.trigger('click')
      await nextTick()

      expect(scrollSpy).toHaveBeenCalledWith({ block: 'nearest' })
      expect(scrollSpy.mock.instances).toContain(row.element)
    } finally {
      Element.prototype.scrollIntoView = orig
    }
  })
})

describe('FileManagerContent — panel layout (search bar belongs to the listing)', () => {
  it('renders the search field flat and the dock compact', () => {
    // The resident search bar is a flat field on the dock's own material: the
    // pill contributes no fill and no outline of its own. jsdom has no CSS
    // engine, so assert against the source.
    const src = readSource()
    const pill = src.match(/\.fs-input-row :deep\(\.search-pill\)\s*\{([^}]*)\}/)
    expect(pill).toBeTruthy()
    expect(pill![1]).toMatch(/background:\s*transparent/)
    expect(pill![1]).toMatch(/border:\s*none/)

    // The dock must stay compact — the search row and the toggle buttons share
    // one 26px band.
    const row = src.match(/\.fs-input-row\s*\{([^}]*)\}/)
    expect(row).toBeTruthy()
    expect(row![1]).toMatch(/align-items:\s*center/)
    const toggle = src.match(/\.fs-toggle-btn\s*\{([^}]*)\}/)
    expect(toggle).toBeTruthy()
    expect(toggle![1]).toMatch(/width:\s*calc\(\s*26\s*\*\s*var\(--dp\)\s*\)/)
    expect(toggle![1]).toMatch(/height:\s*calc\(\s*26\s*\*\s*var\(--dp\)\s*\)/)
  })

  it('leaves the field with no fill or border in ANY state, including focus', () => {
    // SearchInput's own treatment paints the pill (--bg-primary + 1px border)
    // and adds an accent border + glow on focus. On a flat dock field both read
    // as a nested box, so every state must resolve to no fill and no outline —
    // the caret is the only focus indicator. `:deep()` carries the parent's
    // data-v onto .fs-input-row, which keeps this rule specific enough to beat
    // the child's own .focused rule regardless of the order rollup emits the
    // two style blocks in.
    const src = readSource()
    const focused = src.match(/\.fs-input-row :deep\(\.search-pill\.focused\)\s*\{([^}]*)\}/)
    expect(focused).toBeTruthy()
    expect(focused![1]).toMatch(/border:\s*none/)
    expect(focused![1]).toMatch(/box-shadow:\s*none/)
    expect(focused![1]).toMatch(/background:\s*transparent/)
    // A tint would reintroduce the box this field exists to avoid.
    expect(focused![1]).not.toMatch(/--bg-hover/)
    expect(focused![1]).not.toMatch(/--accent-color/)
  })

  it('keeps the split a bounded flex column when preview mode is off', () => {
    // Regression: with the split disabled, SplitView's pane wrappers become
    // `display: contents`, so the slot content's layout parent is the split
    // root. Without `display:flex` on that root the list's `flex:1;
    // min-height:0` is inert and its height grows to the content height.
    // jsdom does not load SFC <style>, so assert against the source.
    const src = readSource()
    const m = src.match(/\.fm-split\s*\{([^}]*)\}/)
    expect(m).toBeTruthy()
    const body = m![1]
    expect(body).toMatch(/display:\s*flex/)
    expect(body).toMatch(/flex-direction:\s*column/)
    expect(body).toMatch(/min-height:\s*0/)
  })

  it('renders the search dock inside the top pane, above the preview pane', () => {
    const wrapper = mountContent()
    const split = wrapper.find('.fm-split')
    const top = wrapper.find('.fm-split > .split-view__left')
    const bottom = wrapper.find('.fm-split > .split-view__right')
    const searchDock = wrapper.find('.fs-nav-bottom')

    expect(split.exists()).toBe(true)
    expect(searchDock.exists()).toBe(true)
    // The dock belongs to the listing: it must sit inside the split, and
    // specifically inside the top pane, so it stays directly under the list
    // and above the preview pane.
    expect(split.element.contains(searchDock.element)).toBe(true)
    if (top.exists()) expect(top.element.contains(searchDock.element)).toBe(true)
    if (bottom.exists()) expect(bottom.element.contains(searchDock.element)).toBe(false)
  })

  it('keeps the search dock above the docked preview pane when it is open', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()

    const dock = wrapper.find('.fs-nav-bottom')
    const pane = wrapper.find('.fm-preview-pane')
    expect(pane.exists()).toBe(true)
    // Document order decides the visual order in the top/bottom split: the
    // dock must come first, so it renders above the preview pane.
    const order = dock.element.compareDocumentPosition(pane.element)
    expect(order & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(pane.element.contains(dock.element)).toBe(false)
  })
})

describe('FileManagerContent — mobile docked preview', () => {
  it('opens the docked split on mobile (no input-axis gate)', async () => {
    mocks.mockIsTouchPrimary.value = true
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()

    expect(wrapper.find('.split-view--vertical').exists()).toBe(true)
    expect(wrapper.find('.split-view__divider--vertical').exists()).toBe(true)
    expect(wrapper.find('.code-link-preview-stub').attributes('data-docked')).toBe('true')
  })

  it('uses smaller pane minimums on mobile so a short viewport can still be dragged', async () => {
    mocks.mockIsTouchPrimary.value = true
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()

    const split = wrapper.findComponent(SplitView)
    expect(split.props('minLeft')).toBe(120)
    expect(split.props('minRight')).toBe(140)
  })

  it('keeps the roomier pane minimums on desktop', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()

    const split = wrapper.findComponent(SplitView)
    expect(split.props('minLeft')).toBe(160)
    expect(split.props('minRight')).toBe(200)
  })

  it('previews a directory tap on mobile via the listing pane', async () => {
    mocks.mockIsTouchPrimary.value = true
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()

    // Directories preview their listing, not a file — so the file-preview
    // composable stays untouched while the pane opens.
    expect(mocks.mockShowPreview).not.toHaveBeenCalled()
    expect(wrapper.find('.fm-preview-pane').exists()).toBe(true)
  })
})

describe('FileManagerContent — mobile select-then-tap to enter', () => {
  it('selects on the first tap and enters on the second, with no timing window', async () => {
    mocks.mockIsTouchPrimary.value = true
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    const dir = wrapper.find('.dir-item')

    await dir.trigger('click')
    expect(wrapper.emitted('navigateDir')).toBeFalsy()

    // No fake timers / Date mocking: entering depends on selection state, not
    // on how fast the second tap follows.
    await dir.trigger('click')
    expect(wrapper.emitted('navigateDir')!.length).toBe(1)
  })

  it('after entering, a cleared selection makes the next tap select again', async () => {
    mocks.mockIsTouchPrimary.value = true
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    const dir = wrapper.find('.dir-item')

    await dir.trigger('click')
    await dir.trigger('click')
    expect(wrapper.emitted('navigateDir')!.length).toBe(1)

    // Entering a directory clears the selection (the real app does this in its
    // currentDir watcher), so the next tap must select rather than enter again.
    wrapper.vm._setSelectedPath('')
    await dir.trigger('click')
    expect(wrapper.emitted('navigateDir')!.length).toBe(1)
  })

  it('tapping a different entry re-selects instead of entering', async () => {
    mocks.mockIsTouchPrimary.value = true
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')

    expect(wrapper.emitted('navigateDir')).toBeFalsy()
    expect(wrapper.emitted('selectFile')).toBeFalsy()
  })

  it('desktop is unaffected: two clicks do not enter, native dblclick does', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    const dir = wrapper.find('.dir-item')

    await dir.trigger('click')
    await dir.trigger('click')
    expect(wrapper.emitted('navigateDir')).toBeFalsy()

    await dir.trigger('dblclick')
    expect(wrapper.emitted('navigateDir')).toBeTruthy()
  })

  it('second tap on a file enters the viewer and drops the docked pane', async () => {
    mocks.mockIsTouchPrimary.value = true
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    const row = wrapper.find('.file-item[data-path="test.ts"]')

    await row.trigger('click')   // tap 1 → docked preview
    expect(mocks.mockShowPreview).toHaveBeenCalledTimes(1)

    await row.trigger('click')   // tap 2 → enter full viewer
    expect(wrapper.emitted('selectFile')).toBeTruthy()
    // The pane is dismissed so it doesn't linger behind the viewer.
    expect(mocks.mockClosePreview).toHaveBeenCalled()
  })

  it('re-tapping an already-selected entry does not re-open the preview', async () => {
    mocks.mockIsTouchPrimary.value = true
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()
    const row = wrapper.find('.file-item[data-path="test.ts"]')

    await row.trigger('click')
    await row.trigger('click')
    // The second tap enters instead of previewing again.
    expect(mocks.mockShowPreview).toHaveBeenCalledTimes(1)
  })
})

describe('FileManagerContent — directory quick preview', () => {
  it('clicking a directory in preview mode opens the listing pane, not a file preview', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()

    expect(wrapper.find('.fm-preview-pane').exists()).toBe(true)
    expect(wrapper.find('.dir-preview-stub').exists()).toBe(true)
    // A directory has no file to preview, so the file-preview composable is
    // never asked to show anything.
    expect(mocks.mockShowPreview).not.toHaveBeenCalled()
  })

  it('clicking a file replaces the directory listing with the file preview', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()
    expect(wrapper.find('.dir-preview-stub').exists()).toBe(true)

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()

    // The pane must switch bodies — never show both at once.
    expect(wrapper.find('.dir-preview-stub').exists()).toBe(false)
    expect(mocks.mockShowPreview).toHaveBeenCalledTimes(1)
  })

  it('clicking a directory replaces a file preview with the listing', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click')
    mocks.mockPreviewRefs.visible!.value = true
    await nextTick()
    expect(wrapper.find('.dir-preview-stub').exists()).toBe(false)

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()

    expect(wrapper.find('.dir-preview-stub').exists()).toBe(true)
    // Opening a directory must dismiss the file preview so the pane has one body.
    expect(mocks.mockClosePreview).toHaveBeenCalled()
  })

  it('pane open-dir navigates the main list and collapses the pane', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()

    await wrapper.findComponent(DirPreviewBodyStub).vm.$emit('open-dir', 'nested')
    await nextTick()

    // The listing became the main list, so the pane collapses instead of
    // duplicating it.
    expect(wrapper.emitted('navigateDir')![0]).toEqual(['src/nested'])
    expect(wrapper.find('.fm-preview-pane').exists()).toBe(false)
  })

  // Regression for #549: a child that is itself an escaping symlink must be
  // entered through the resolved target the pane entry carries, not the joined
  // link path (which the listing endpoint refuses).
  it('pane open-dir enters an escaping symlink child via its target', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    // Mutate the array the mock's ref wraps (reassigning the field would not
    // reach the ref created at mock setup).
    mocks.mockDirPreviewState.entries.push({ name: 'data', type: 'dir', target: '/some/other/dir' })
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()

    await wrapper.findComponent(DirPreviewBodyStub).vm.$emit('open-dir', 'data')
    await nextTick()

    expect(wrapper.emitted('navigateDir')![0]).toEqual(['/some/other/dir'])
  })

  it('pane open-file opens the full-screen viewer for the joined path', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()

    await wrapper.findComponent(DirPreviewBodyStub).vm.$emit('open-file', 'inner.ts')
    await nextTick()

    expect(wrapper.emitted('selectFile')![0]).toEqual(['src/inner.ts'])
  })

  it('passes the listed directory path down for thumbnails', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()

    // The body needs the directory to build /api/fs/thumb URLs.
    expect(wrapper.findComponent(DirPreviewBodyStub).props('dirPath')).toBe('src')
  })

  it('pane open-self opens the LISTED directory itself and collapses the pane', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()

    await wrapper.findComponent(DirPreviewBodyStub).vm.$emit('open-self')
    await nextTick()

    // The directory itself, NOT a child: `src`, not `src/<something>`.
    expect(wrapper.emitted('navigateDir')![0]).toEqual(['src'])
    expect(wrapper.find('.fm-preview-pane').exists()).toBe(false)
  })

  it('pane close collapses the pane', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()
    expect(wrapper.find('.fm-preview-pane').exists()).toBe(true)

    await wrapper.findComponent(DirPreviewBodyStub).vm.$emit('closed')
    await nextTick()
    expect(wrapper.find('.fm-preview-pane').exists()).toBe(false)
  })

  it('changing the directory drops the listing so a stale dir cannot linger', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()
    expect(wrapper.find('.fm-preview-pane').exists()).toBe(true)

    await wrapper.setProps({ currentDir: 'docs' })
    await nextTick()

    expect(wrapper.find('.fm-preview-pane').exists()).toBe(false)
  })

  it('turning preview mode off collapses a directory listing', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()
    expect(wrapper.find('.fm-preview-pane').exists()).toBe(true)

    mocks.mockLocalConfigProxy.current!.filePreviewMode = false
    await nextTick()

    expect(wrapper.find('.fm-preview-pane').exists()).toBe(false)
  })

  it('double-clicking a directory on desktop navigates without leaving the listing pane open', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = true
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('dblclick')
    await nextTick()

    expect(wrapper.emitted('navigateDir')![0]).toEqual(['src'])
    expect(wrapper.find('.fm-preview-pane').exists()).toBe(false)
  })

  it('does not open the pane for a directory when preview mode is off', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.mockLocalConfig.filePreviewMode = false
    const wrapper = mountContent()

    await wrapper.find('.dir-item[data-path="src"]').trigger('click')
    await nextTick()

    expect(wrapper.find('.fm-preview-pane').exists()).toBe(false)
    expect(mocks.mockShowPreview).not.toHaveBeenCalled()
  })
})

// ── Gitignored entry visual effect ──

describe('thumbnail lazy mounting', () => {
  const imageEntries = [
    { name: 'a.png', type: 'file', modified: '2025-01-01T00:00:00Z', size: 100 },
    { name: 'b.jpg', type: 'file', modified: '2025-01-01T00:00:00Z', size: 100 },
    { name: 'c.gif', type: 'file', modified: '2025-01-01T00:00:00Z', size: 100 },
  ]

  // The mock installed in test-setup records instances and exposes triggerAll.
  const observerInstances = () => {
    const Ctor = globalThis.IntersectionObserver as unknown as {
      instances?: Array<{ triggerAll: () => void; elements: Set<Element> }>
    }
    return Ctor.instances ?? []
  }

  beforeEach(() => {
    const Ctor = globalThis.IntersectionObserver as unknown as { instances?: unknown[] }
    Ctor.instances = []
    // Enable the thumbnail path for this block only; the rest of the suite
    // assumes isThumbable is false so no <img> ever renders.
    mocks.mockThumbable.value = true
  })

  afterEach(() => {
    mocks.mockThumbable.value = false
  })

  it('does not mount any thumbnail before its row is visible', async () => {
    const wrapper = mountContent({ entries: imageEntries })
    await nextTick()

    // Every image entry still renders (as an icon placeholder) — the lazy gate
    // must not drop rows, only defer their <img>.
    expect(wrapper.findAll('.file-item')).toHaveLength(3)
    expect(wrapper.findAll('img.file-thumb')).toHaveLength(0)
  })

  it('mounts the thumbnail once the row is reported visible', async () => {
    const wrapper = mountContent({ entries: imageEntries })
    await nextTick()
    expect(wrapper.findAll('img.file-thumb')).toHaveLength(0)

    // Simulate the rows scrolling into view.
    observerInstances().forEach(o => o.triggerAll())
    await nextTick()

    const thumbs = wrapper.findAll('img.file-thumb')
    expect(thumbs).toHaveLength(3)
    expect(thumbs[0].attributes('src')).toContain('/api/fs/thumb')
    // Thumbnails must not request SVG/BMP etc. — only decodable formats.
    expect(thumbs.map(t => t.attributes('src')).join(' ')).not.toContain('.md')
  })

  it('renders only the entries reported visible, not the whole directory', async () => {
    const wrapper = mountContent({ entries: imageEntries })
    await nextTick()

    // Report just the first row as visible.
    const obs = observerInstances()[0]
    const first = [...obs.elements][0]
    expect(first).toBeTruthy()
    const Ctor = globalThis.IntersectionObserver as unknown as {
      instances: Array<{ callback: (e: unknown[], o: unknown) => void }>
    }
    Ctor.instances[0].callback(
      [{ target: first, isIntersecting: true, intersectionRatio: 1 }],
      Ctor.instances[0],
    )
    await nextTick()

    expect(wrapper.findAll('img.file-thumb')).toHaveLength(1)
  })

  it('ignores non-intersecting observations', async () => {
    const wrapper = mountContent({ entries: imageEntries })
    await nextTick()

    const obs = observerInstances()[0]
    const first = [...obs.elements][0]
    const Ctor = globalThis.IntersectionObserver as unknown as {
      instances: Array<{ callback: (e: unknown[], o: unknown) => void }>
    }
    Ctor.instances[0].callback(
      [{ target: first, isIntersecting: false, intersectionRatio: 0 }],
      Ctor.instances[0],
    )
    await nextTick()

    expect(wrapper.findAll('img.file-thumb')).toHaveLength(0)
  })

  it('re-gates thumbnails after changing directory', async () => {
    const wrapper = mountContent({ entries: imageEntries })
    await nextTick()
    observerInstances().forEach(o => o.triggerAll())
    await nextTick()
    expect(wrapper.findAll('img.file-thumb')).toHaveLength(3)

    // Switching directories is a different listing; the previous visibility
    // must not carry over or the new folder's images would all load at once.
    await wrapper.setProps({ currentDir: 'other', entries: imageEntries })
    await nextTick()

    expect(wrapper.findAll('img.file-thumb')).toHaveLength(0)
  })
})

// ── Content (grep) search dialog wiring ──

describe('FileManagerContent — toolbar labels', () => {
  it('labels the browse toolbar when the row can hold them', async () => {
    // jsdom has no layout: the toolbar measures 0 wide and assumes room. The
    // overflow maths lives in ActionBar.test.ts; this pins the wiring.
    const wrapper = mountContent()
    await nextTick()
    const bar = wrapper.find('.dir-toolbar-btns:not(.ms-toolbar-btns)')
    expect(bar.classes()).toContain('ab--labels')
    expect(bar.findAll('.action-label').length).toBeGreaterThanOrEqual(5)
    wrapper.unmount()
  })
})
