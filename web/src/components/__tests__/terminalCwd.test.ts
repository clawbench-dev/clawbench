import { describe, expect, it } from 'vitest'
import { resolveTerminalCwd, shouldPromptForTerminalReopen } from '@/components/terminal/terminalCwd'

describe('terminal cwd resolution', () => {
  it('uses the opened file directory before the file manager directory', () => {
    expect(resolveTerminalCwd({ currentFilePath: 'web/src/App.vue', currentDir: 'docs' })).toBe('web/src')
  })

  it('uses project root for files at project root', () => {
    expect(resolveTerminalCwd({ currentFilePath: 'README.md', currentDir: 'docs' })).toBe('')
  })

  it('falls back to the current file manager directory when no file is open', () => {
    expect(resolveTerminalCwd({ currentFilePath: '', currentDir: 'internal/terminal' })).toBe('internal/terminal')
  })

  it('uses an explicit requested cwd for open-terminal-here actions', () => {
    expect(resolveTerminalCwd({ currentFilePath: 'README.md', currentDir: '', requestedCwd: 'cmd/server' })).toBe('cmd/server')
  })

  it('prompts before reopening when an existing terminal runs in a different directory', () => {
    expect(shouldPromptForTerminalReopen('/repo/internal/terminal', '/repo/web/src')).toBe(true)
  })

  it('does not prompt when the target directory matches the existing terminal directory', () => {
    expect(shouldPromptForTerminalReopen('/repo/web/src/', '/repo/web/src')).toBe(false)
  })
})

describe('cwd normalization (via resolveTerminalCwd)', () => {
  it('strips trailing slashes from requested cwd', () => {
    expect(resolveTerminalCwd({ requestedCwd: 'cmd/server///' })).toBe('cmd/server')
  })

  it('keeps an absolute requested cwd absolute', () => {
    // The file manager can browse project-external directories and offers
    // "open terminal here" there; stripping the root would open the terminal
    // at the project root instead of the browsed directory.
    expect(resolveTerminalCwd({ requestedCwd: '/tmp/scratch' })).toBe('/tmp/scratch')
    expect(resolveTerminalCwd({ requestedCwd: '/tmp/scratch///' })).toBe('/tmp/scratch')
  })

  it('keeps an absolute currentDir absolute', () => {
    expect(resolveTerminalCwd({ currentDir: '/var/log' })).toBe('/var/log')
  })

  it('normalizes a project-relative currentDir (no leading slash in practice)', () => {
    // The backend returns rootless project-relative paths for in-project
    // directories; a stray leading slash would mean project-external instead.
    expect(resolveTerminalCwd({ currentDir: 'internal/terminal' })).toBe('internal/terminal')
  })

  it('strips a trailing slash from a project-relative requestedCwd', () => {
    expect(resolveTerminalCwd({ requestedCwd: 'cmd/server/' })).toBe('cmd/server')
  })
})

describe('dirname (via resolveTerminalCwd)', () => {
  it('returns empty string for a root-level file with no slash', () => {
    expect(resolveTerminalCwd({ currentFilePath: 'Makefile', currentDir: 'docs' })).toBe('')
  })

  it('returns parent directory for deeply nested file', () => {
    expect(resolveTerminalCwd({ currentFilePath: 'a/b/c/d.txt', currentDir: '' })).toBe('a/b/c')
  })

  it('treats a leading-slash path as absolute (project-external)', () => {
    // The backend returns rootless project-relative paths for in-project files
    // and absolute paths for external ones, so a leading "/" means external —
    // it must keep its root, or "open terminal here" from an external directory
    // would open at the project root instead.
    expect(resolveTerminalCwd({ currentFilePath: '/web/src/App.vue', currentDir: '' })).toBe('/web/src')
  })

  it('handles file path with trailing slash (treats last segment as directory)', () => {
    // dirname of "web/src/" → after normalization becomes "web/src", slash is at end → returns "web"
    expect(resolveTerminalCwd({ currentFilePath: 'web/src/', currentDir: '' })).toBe('web')
  })
})

describe('resolveTerminalCwd edge cases', () => {
  it('returns empty string when all inputs are empty or null', () => {
    expect(resolveTerminalCwd({})).toBe('')
  })

  it('returns empty string when all inputs are null', () => {
    expect(resolveTerminalCwd({ currentFilePath: null, currentDir: null, requestedCwd: null })).toBe('')
  })

  it('returns empty string when all inputs are empty strings', () => {
    expect(resolveTerminalCwd({ currentFilePath: '', currentDir: '', requestedCwd: '' })).toBe('')
  })

  it('prioritizes requestedCwd over currentFilePath and currentDir', () => {
    expect(resolveTerminalCwd({ currentFilePath: 'web/src/App.vue', currentDir: 'docs', requestedCwd: 'cmd/server' })).toBe('cmd/server')
  })

  it('prioritizes currentFilePath over currentDir', () => {
    expect(resolveTerminalCwd({ currentFilePath: 'web/src/App.vue', currentDir: 'docs' })).toBe('web/src')
  })

  it('normalizes a project-relative requested cwd', () => {
    expect(resolveTerminalCwd({ requestedCwd: 'a/b/' })).toBe('a/b')
  })

  it('keeps an absolute requested cwd absolute', () => {
    expect(resolveTerminalCwd({ requestedCwd: '/a/b/' })).toBe('/a/b')
  })
})

describe('shouldPromptForTerminalReopen edge cases', () => {
  it('does not prompt when current cwd is empty', () => {
    expect(shouldPromptForTerminalReopen('', '/repo/web/src')).toBe(false)
  })

  it('does not prompt when target cwd is empty', () => {
    expect(shouldPromptForTerminalReopen('/repo/web/src', '')).toBe(false)
  })

  it('does not prompt when both cwds are empty', () => {
    expect(shouldPromptForTerminalReopen('', '')).toBe(false)
  })

  it('prompts when paths differ only by trailing slash normalization', () => {
    // Both get trailing slashes stripped, so '/repo/web/src/' → '/repo/web/src' === '/repo/web/src'
    expect(shouldPromptForTerminalReopen('/repo/web/src/', '/repo/web/src')).toBe(false)
  })

  it('prompts when one path has multiple trailing slashes', () => {
    expect(shouldPromptForTerminalReopen('/repo/web/src///', '/repo/web/src')).toBe(false)
  })

  it('handles paths with only a single slash', () => {
    expect(shouldPromptForTerminalReopen('/', '/')).toBe(false)
  })
})
