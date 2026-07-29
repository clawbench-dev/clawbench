import { describe, expect, it } from 'vitest'

/**
 * ProjectDialog default browse path logic (mirrors the component's computed logic):
 *
 * - On Unix (single rootPath ["/"]): default to homeDir
 * - On Windows (multiple rootPaths like ["C:\", "D:\"]): default to root level ("/")
 * - Fallback: if homeDir is empty on Unix, fall back to "/"
 *
 * The component uses:
 *   const isWindows = computed(() => store.state.rootPaths.length > 1)
 *   browsePath.value = isWindows.value ? '/' : (store.state.homeDir || '/')
 */

function getDefaultBrowsePath(rootPaths: string[], homeDir: string): string {
  const isWindows = rootPaths.length > 1
  return isWindows ? '/' : (homeDir || '/')
}

describe('ProjectDialog default browse path', () => {
  it('defaults to homeDir on Unix (single rootPaths)', () => {
    expect(getDefaultBrowsePath(['/'], '/home/testuser')).toBe('/home/testuser')
  })

  it('defaults to homeDir on macOS', () => {
    expect(getDefaultBrowsePath(['/'], '/Users/john')).toBe('/Users/john')
  })

  it('defaults to root on Windows (multiple rootPaths)', () => {
    expect(getDefaultBrowsePath(['C:\\', 'D:\\'], 'C:\\Users\\testuser')).toBe('/')
  })

  it('falls back to / when homeDir is empty on Unix', () => {
    expect(getDefaultBrowsePath(['/'], '')).toBe('/')
  })

  it('uses root even when homeDir is empty on Windows', () => {
    expect(getDefaultBrowsePath(['C:\\', 'D:\\'], '')).toBe('/')
  })

  it('works with single-drive Windows (treated as Unix-like)', () => {
    // Edge case: single-drive Windows gets rootPaths ["C:\"]
    // In this case rootPaths.length === 1, so it's treated like Unix
    expect(getDefaultBrowsePath(['C:\\'], 'C:\\Users\\admin')).toBe('C:\\Users\\admin')
  })
})
