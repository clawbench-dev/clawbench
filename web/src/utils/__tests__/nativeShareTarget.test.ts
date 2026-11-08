import { describe, it, expect } from 'vitest'
import { nativeShareTargetResult, nativeShareTargetSavedElsewhere, stageShareTargetResult } from '../shareTarget'

describe('native share imports', () => {
  const result = { projectPath: '/fixture', paths: ['.clawbench/share-in/example.txt'], failed: 0 }

  it('stages only the explicitly confirmed project and preserves partial failure', () => {
    expect(nativeShareTargetResult(result, '/other')).toBeNull()
    const parsed = nativeShareTargetResult({ ...result, failed: 1 }, '/fixture')
    expect(parsed).toEqual({ paths: result.paths, failed: 1 })
    const attached: string[] = []
    const state = stageShareTargetResult(parsed!, path => { attached.push(path); return true })
    expect(attached).toEqual(result.paths)
    expect(state.toastKey).toBe('chat.attach.shareTargetPartial')
  })

  it('rejects cancellation, traversal, invalid paths and malformed batches', () => {
    for (const detail of [null, { ...result, cancelled: true }, { ...result, failed: -1 },
      { ...result, paths: ['.clawbench/share-in/../../private'] },
      { ...result, paths: ['/absolute/file'] }, { ...result, paths: [null] },
      { ...result, paths: new Array(21).fill(result.paths[0]) }]) {
      expect(nativeShareTargetResult(detail, '/fixture')).toBeNull()
    }
  })

  it('reports an upload failure without staging an empty attachment', () => {
    expect(nativeShareTargetResult({ ...result, paths: [], failed: 1 }, '/fixture'))
      .toEqual({ paths: [], failed: 1, failure: 'save' })
  })
})

describe('native share saved into another project', () => {
  const result = { projectPath: '/fixture', paths: ['.clawbench/share-in/example.txt'], failed: 0 }

  it('flags a stored share whose project is not the one on screen', () => {
    // The files are on disk under /fixture; the app is showing /other. The
    // user must be told where they went rather than seeing nothing happen.
    expect(nativeShareTargetSavedElsewhere(result, '/other')).toBe(true)
  })

  it('stays quiet when the project matches, so the normal path is not doubled', () => {
    expect(nativeShareTargetSavedElsewhere(result, '/fixture')).toBe(false)
  })

  it('does not claim a share was saved when the payload carries no usable path', () => {
    for (const detail of [null, { ...result, projectPath: '' }, { ...result, paths: [] },
      { ...result, paths: ['.clawbench/share-in/../../private'] }, { ...result, paths: [null] }]) {
      expect(nativeShareTargetSavedElsewhere(detail, '/other')).toBe(false)
    }
  })
})
