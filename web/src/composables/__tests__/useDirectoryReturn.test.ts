import { beforeEach, describe, expect, it } from 'vitest'
import { ref } from 'vue'
import { useDirectoryReturn, _resetForTesting as _resetDirectoryReturnForTesting } from '../useDirectoryReturn'
import { useNavigationContext } from '../useNavigationContext'
import { _resetForTesting, useFileNavStack } from '../useFileNavStack'
import { useFileBackTarget } from '../useFileBackTarget'

beforeEach(() => {
  _resetForTesting()
  _resetDirectoryReturnForTesting()
  useNavigationContext().resetForTesting()
})

describe('Markdown directory round trips', () => {
  it.each([true, false])('restores the complete visit on repeated returns (browse=%s)', (fromBrowse) => {
    const browse = ref(fromBrowse)
    const navigation = useNavigationContext()
    const files = useFileNavStack()
    const directory = useDirectoryReturn(browse)
    if (!fromBrowse) navigation.start({ surface: 'chat', tab: 'chat', label: 'Chat' })
    files.openFile('README.md', { viewMode: 'rendered', scrollTop: 240 })
    const originalFiles = files.snapshot()
    const originalOrigin = navigation.snapshot()
    const target = useFileBackTarget(() => ({ browseSession: browse.value, canGoBackFile: files.canGoBack.value, hasOrigin: navigation.hasOrigin.value }))

    for (let visit = 0; visit < 3; visit++) {
      directory.enter({ surface: 'file', tab: 'view', label: 'README', filePath: 'README.md', scrollTop: 240, viewMode: 'rendered' })
      expect(navigation.origin.value?.filePath).toBe('README.md')
      expect(files.canGoBack.value).toBe(false)
      expect(files.overlayOpen.value).toBe(false)
      expect(browse.value).toBe(false)
      expect(directory.restore()).toEqual({ directory: null })
      expect(files.snapshot()).toEqual(originalFiles)
      expect(navigation.snapshot()).toEqual(originalOrigin)
      expect(browse.value).toBe(fromBrowse)
      expect(target.value).toBe(fromBrowse ? 'browse' : 'origin')
    }
    expect(directory.restore()).toBeNull()
  })

  it('suspends earlier file history and preserves forward history and locations', () => {
    const files = useFileNavStack()
    const directory = useDirectoryReturn(ref(false))
    files.openFile('A.md')
    files.openFile('B.md', { lineStart: 12, scrollTop: 420, viewMode: 'raw' })
    files.openFile('C.md')
    files.goBack()
    const expected = files.snapshot()
    directory.enter({ surface: 'file', tab: 'view', label: 'B', filePath: 'B.md' })
    files.openFile('docs/other.md')
    directory.restore()
    expect(files.snapshot()).toEqual(expected)
    expect(files.goForward()).toBe('C.md')
    expect(files.goBack()).toBe('B.md')
    expect(files.goBack()).toBe('A.md')
  })

  it('remembers the directory the file was opened from so the excursion unwinds there', () => {
    const browse = ref(true)
    const files = useFileNavStack()
    const directory = useDirectoryReturn(browse)
    files.openFile('docs/guide/a.md')
    // The file lives in docs/guide; the user jumps to an unrelated directory.
    directory.enter({ surface: 'file', tab: 'view', label: 'a.md', filePath: 'docs/guide/a.md' }, 'docs/guide')
    expect(files.overlayOpen.value).toBe(false)
    expect(browse.value).toBe(false)
    // …and opens a file from the jumped-to directory.
    files.openFile('src/internal/b.md')
    browse.value = true
    // Unwinding: the file returns to the directory, then the excursion returns
    // to the original file AND its directory.
    expect(directory.restore()).toEqual({ directory: 'docs/guide' })
    expect(files.currentLocation.value?.path).toBe('docs/guide/a.md')
  })

  it('shares one suspended stack across callers', () => {
    // The state it snapshots (file history + navigation origin) is module-level,
    // so a per-call stack would let two callers disagree about which visit is
    // suspended.
    const first = useDirectoryReturn(ref(false))
    const second = useDirectoryReturn(ref(true))

    first.enter({ surface: 'file', tab: 'view', label: 'A', filePath: 'A.md' }, 'docs')
    expect(second.pending()).toBe(true)
    expect(second.restore()).toEqual({ directory: 'docs' })
    expect(first.pending()).toBe(false)
  })

  it('clears suspended visits on project switch', () => {
    const directory = useDirectoryReturn(ref(true))
    directory.enter({ surface: 'file', tab: 'view', label: 'A', filePath: 'A.md' })
    directory.clear()
    expect(directory.restore()).toBeNull()
  })
})
