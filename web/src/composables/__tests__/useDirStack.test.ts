import { describe, expect, it, vi, beforeEach } from 'vitest'
import { useDirStack, _resetForTesting } from '@/composables/useDirStack'

beforeEach(() => {
  _resetForTesting()
})

describe('useDirStack', () => {
  it('initial state: empty stack, currentDir is "", canGoBack is false', () => {
    const ds = useDirStack()
    expect(ds.currentDir.value).toBe('')
    expect(ds.canGoBack.value).toBe(false)
    expect(ds.dirStack.value).toEqual([])
  })

  it('pushDir: adds to stack and updates currentDir', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    expect(ds.currentDir.value).toBe('src')
    expect(ds.canGoBack.value).toBe(false)
    expect(ds.dirStack.value).toEqual(['src'])
  })

  it('pushDir multiple times: builds navigation stack', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    ds.pushDir('src/composables/useDirStack')
    expect(ds.currentDir.value).toBe('src/composables/useDirStack')
    expect(ds.canGoBack.value).toBe(true)
    expect(ds.dirStack.value).toEqual(['src', 'src/composables', 'src/composables/useDirStack'])
  })

  it('pushDir dedup: no-op if pushing same path as current top', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src') // same as top — should be no-op
    expect(ds.dirStack.value).toEqual(['src'])
    expect(ds.canGoBack.value).toBe(false)
  })

  it('popDir: steps back through the stack', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    ds.pushDir('src/composables/useDirStack')

    const back1 = ds.popDir()
    expect(back1).toBe('src/composables')
    expect(ds.currentDir.value).toBe('src/composables')

    const back2 = ds.popDir()
    expect(back2).toBe('src')
    expect(ds.currentDir.value).toBe('src')
  })

  it('popDir at stack bottom returns null and does not change stack', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    expect(ds.canGoBack.value).toBe(false)
    const result = ds.popDir()
    expect(result).toBeNull()
    expect(ds.currentDir.value).toBe('src')
    expect(ds.dirStack.value).toEqual(['src'])
  })

  it('popDir on empty stack returns null', () => {
    const ds = useDirStack()
    expect(ds.canGoBack.value).toBe(false)
    const result = ds.popDir()
    expect(result).toBeNull()
  })

  it('truncateToDir: truncates stack to the target ancestor', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    ds.pushDir('src/composables/useDirStack')

    ds.truncateToDir('src')
    expect(ds.currentDir.value).toBe('src')
    expect(ds.dirStack.value).toEqual(['src'])
  })

  it('truncateToDir: truncates to intermediate ancestor', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    ds.pushDir('src/composables/useDirStack')

    ds.truncateToDir('src/composables')
    expect(ds.currentDir.value).toBe('src/composables')
    expect(ds.dirStack.value).toEqual(['src', 'src/composables'])
  })

  it('truncateToDir with path not in stack: resets to [path]', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')

    ds.truncateToDir('other')
    expect(ds.currentDir.value).toBe('other')
    expect(ds.dirStack.value).toEqual(['other'])
  })

  it('resetStack: clears the stack', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    ds.resetStack()
    expect(ds.currentDir.value).toBe('')
    expect(ds.canGoBack.value).toBe(false)
    expect(ds.dirStack.value).toEqual([])
  })

  it('resetStack with path: initializes stack with single entry', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    ds.resetStack('other')
    expect(ds.currentDir.value).toBe('other')
    expect(ds.canGoBack.value).toBe(false)
    expect(ds.dirStack.value).toEqual(['other'])
  })

  it('replaceTop: replaces current directory without adding history', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    ds.replaceTop('other')
    expect(ds.currentDir.value).toBe('other')
    expect(ds.dirStack.value).toEqual(['src', 'other'])
    // Going back should return to 'src', not 'src/composables'
    const back = ds.popDir()
    expect(back).toBe('src')
  })

  it('replaceTop on empty stack: creates single-entry stack', () => {
    const ds = useDirStack()
    ds.replaceTop('src')
    expect(ds.currentDir.value).toBe('src')
    expect(ds.dirStack.value).toEqual(['src'])
  })

  it('after resetStack, pushDir starts fresh', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    ds.resetStack()

    ds.pushDir('other')
    expect(ds.currentDir.value).toBe('other')
    expect(ds.canGoBack.value).toBe(false)
    expect(ds.dirStack.value).toEqual(['other'])
  })

  it('module-level singleton: multiple calls return same instance', () => {
    const ds1 = useDirStack()
    ds1.pushDir('src')

    const ds2 = useDirStack()
    expect(ds2.currentDir.value).toBe('src')
    expect(ds2.canGoBack.value).toBe(false)

    // Mutating through one reference is visible through the other
    ds2.pushDir('src/composables')
    expect(ds1.currentDir.value).toBe('src/composables')
    expect(ds1.canGoBack.value).toBe(true)
  })

  it('pushDir with empty string (project root)', () => {
    const ds = useDirStack()
    ds.pushDir('')
    expect(ds.currentDir.value).toBe('')
    expect(ds.dirStack.value).toEqual([''])
    expect(ds.canGoBack.value).toBe(false)
  })

  it('root-seeded stack: pushDir into subdir enables canGoBack', () => {
    // Simulates app init: loadFiles('') seeds root, then user navigates into a subdir
    const ds = useDirStack()
    ds.pushDir('')          // root seeded by loadFiles
    expect(ds.canGoBack.value).toBe(false)
    ds.pushDir('src')       // user navigates into subdir
    expect(ds.dirStack.value).toEqual(['', 'src'])
    expect(ds.canGoBack.value).toBe(true)
    expect(ds.currentDir.value).toBe('src')
    // Swipe back pops to root
    const back = ds.popDir()
    expect(back).toBe('')
    expect(ds.currentDir.value).toBe('')
    expect(ds.canGoBack.value).toBe(false)
  })

  it('full navigation lifecycle: push, push, truncate, pop, replace', () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    ds.pushDir('src/composables/useDirStack')
    // Breadcrumb jump back to 'src'
    ds.truncateToDir('src')
    expect(ds.currentDir.value).toBe('src')
    expect(ds.dirStack.value).toEqual(['src'])
    // Push again
    ds.pushDir('src/utils')
    expect(ds.dirStack.value).toEqual(['src', 'src/utils'])
    // Pop
    const back = ds.popDir()
    expect(back).toBe('src')
    expect(ds.dirStack.value).toEqual(['src'])
    // Replace
    ds.replaceTop('lib')
    expect(ds.dirStack.value).toEqual(['lib'])
  })

  // ─── *AndLoad async methods with rollback ───

  it('pushDirAndLoad: pushes and calls loadFn', async () => {
    const ds = useDirStack()
    const loadFn = vi.fn().mockResolvedValue(undefined)
    await ds.pushDirAndLoad('src', loadFn)
    expect(ds.currentDir.value).toBe('src')
    expect(loadFn).toHaveBeenCalledOnce()
  })

  it('pushDirAndLoad: rolls back on loadFn failure', async () => {
    const ds = useDirStack()
    ds.pushDir('src')
    const loadFn = vi.fn().mockRejectedValue(new Error('fail'))
    await ds.pushDirAndLoad('new', loadFn)
    expect(ds.currentDir.value).toBe('src')
    expect(ds.dirStack.value).toEqual(['src'])
  })

  it('pushDirAndLoad: dedup — no-op if pushing same top', async () => {
    const ds = useDirStack()
    ds.pushDir('src')
    const loadFn = vi.fn().mockResolvedValue(undefined)
    await ds.pushDirAndLoad('src', loadFn)
    expect(loadFn).not.toHaveBeenCalled()
    expect(ds.dirStack.value).toEqual(['src'])
  })

  it('popDirAndLoad: pops and calls loadFn', async () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    const loadFn = vi.fn().mockResolvedValue(undefined)
    const result = await ds.popDirAndLoad(loadFn)
    expect(result).toBe('src')
    expect(ds.currentDir.value).toBe('src')
    expect(loadFn).toHaveBeenCalledOnce()
  })

  it('popDirAndLoad: returns null when stack too shallow', async () => {
    const ds = useDirStack()
    ds.pushDir('src')
    const loadFn = vi.fn().mockResolvedValue(undefined)
    const result = await ds.popDirAndLoad(loadFn)
    expect(result).toBeNull()
    expect(loadFn).not.toHaveBeenCalled()
  })

  it('popDirAndLoad: rolls back on loadFn failure', async () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    const loadFn = vi.fn().mockRejectedValue(new Error('fail'))
    const result = await ds.popDirAndLoad(loadFn)
    expect(result).toBe('src')
    // Stack should be rolled back to ['src', 'src/composables']
    expect(ds.dirStack.value).toEqual(['src', 'src/composables'])
  })

  it('truncateToDirAndLoad: truncates and calls loadFn', async () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    const loadFn = vi.fn().mockResolvedValue(undefined)
    await ds.truncateToDirAndLoad('src', loadFn)
    expect(ds.dirStack.value).toEqual(['src'])
    expect(loadFn).toHaveBeenCalledOnce()
  })

  it('truncateToDirAndLoad: rolls back on loadFn failure', async () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    const loadFn = vi.fn().mockRejectedValue(new Error('fail'))
    await ds.truncateToDirAndLoad('src', loadFn)
    expect(ds.dirStack.value).toEqual(['src', 'src/composables'])
  })

  it('replaceTopAndLoad: replaces and calls loadFn', async () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    const loadFn = vi.fn().mockResolvedValue(undefined)
    await ds.replaceTopAndLoad('other', loadFn)
    expect(ds.dirStack.value).toEqual(['src', 'other'])
    expect(loadFn).toHaveBeenCalledOnce()
  })

  it('replaceTopAndLoad: rolls back on loadFn failure', async () => {
    const ds = useDirStack()
    ds.pushDir('src')
    ds.pushDir('src/composables')
    const loadFn = vi.fn().mockRejectedValue(new Error('fail'))
    await ds.replaceTopAndLoad('other', loadFn)
    expect(ds.dirStack.value).toEqual(['src', 'src/composables'])
  })
})
