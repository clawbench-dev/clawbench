import { describe, it, expect, beforeEach } from 'vitest'
import {
  deriveSearchQuery,
  openInertPathPicker,
  closeInertPathPicker,
  takeInertPathTarget,
  inertPathPickerState,
  _resetInertPathPickerForTesting,
} from '@/composables/useInertPathPicker'

beforeEach(() => {
  _resetInertPathPickerForTesting()
})

describe('deriveSearchQuery', () => {
  it('reduces a full path to its basename', () => {
    // The backend's `exact` match compares against a directory entry's NAME
    // (dir_search.go matchName(d.Name(), …)), so the query must be the
    // basename — passing the whole path would never match.
    expect(deriveSearchQuery('internal/service/chat_history.go')).toBe('chat_history.go')
    expect(deriveSearchQuery('web/src/App.vue')).toBe('App.vue')
  })

  it('keeps a bare filename unchanged', () => {
    expect(deriveSearchQuery('README.md')).toBe('README.md')
  })

  it('drops a trailing line reference', () => {
    expect(deriveSearchQuery('internal/service/chat_history.go:42')).toBe('chat_history.go')
    expect(deriveSearchQuery('src/main.go:42-48')).toBe('main.go')
    expect(deriveSearchQuery('src/main.go#L42')).toBe('main.go')
    // Multi-range suffix.
    expect(deriveSearchQuery('src/main.go:42,90-91')).toBe('main.go')
  })

  it('handles Windows separators', () => {
    expect(deriveSearchQuery('web\\src\\App.vue')).toBe('App.vue')
  })

  it('strips glob metacharacters defensively', () => {
    // Glob chips are not clickable, so this should not be reached in practice;
    // a stray `*` must not poison the literal filename query.
    expect(deriveSearchQuery('src/ma*in.go')).toBe('main.go')
    expect(deriveSearchQuery('src/file?.ts')).toBe('file.ts')
  })

  it('falls back to the raw text when stripping empties the result', () => {
    // `**/` reduces to nothing after glob-stripping; the user should still see
    // what was clicked rather than an empty query.
    expect(deriveSearchQuery('**/')).toBe('**/')
  })

  it('returns an empty string for empty input', () => {
    expect(deriveSearchQuery('')).toBe('')
    expect(deriveSearchQuery('   ')).toBe('')
  })
})

describe('openInertPathPicker', () => {
  it('opens with the derived query and stashed line target', () => {
    openInertPathPicker({
      path: 'internal/service/chat_history.go',
      lineStart: 42,
      lineEnd: 48,
      source: 'chat',
    })

    expect(inertPathPickerState.open).toBe(true)
    expect(inertPathPickerState.query).toBe('chat_history.go')
    expect(inertPathPickerState.sourcePath).toBe('internal/service/chat_history.go')
    expect(inertPathPickerState.lineStart).toBe(42)
    expect(inertPathPickerState.lineEnd).toBe(48)
    expect(inertPathPickerState.source).toBe('chat')
  })

  it('carries a multi-range target through', () => {
    openInertPathPicker({ path: 'src/main.go', lineStart: 90, lineEnd: 91, lineRanges: '90-91,309' })
    expect(inertPathPickerState.lineRanges).toBe('90-91,309')
  })

  it('does not open when no query can be derived', () => {
    openInertPathPicker({ path: '' })
    expect(inertPathPickerState.open).toBe(false)
  })
})

describe('closeInertPathPicker', () => {
  it('clears every field so a stale search cannot linger', () => {
    openInertPathPicker({ path: 'src/main.go', lineStart: 5, source: 'chat' })
    closeInertPathPicker()

    expect(inertPathPickerState.open).toBe(false)
    expect(inertPathPickerState.query).toBe('')
    expect(inertPathPickerState.sourcePath).toBe('')
    expect(inertPathPickerState.lineStart).toBeUndefined()
    expect(inertPathPickerState.source).toBeUndefined()
  })
})

describe('takeInertPathTarget', () => {
  it('returns the target and closes in one step', () => {
    openInertPathPicker({ path: 'src/main.go', lineStart: 7, lineEnd: 9, source: 'task' })

    const target = takeInertPathTarget()

    expect(target).toEqual({ lineStart: 7, lineEnd: 9, lineRanges: undefined, source: 'task' })
    // Reading the target must not leave the panel open with a stale query.
    expect(inertPathPickerState.open).toBe(false)
    expect(inertPathPickerState.query).toBe('')
  })
})
