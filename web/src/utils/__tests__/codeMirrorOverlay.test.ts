import { describe, expect, it } from 'vitest'
import { EditorState } from '@codemirror/state'
import { javascript } from '@codemirror/lang-javascript'
import {
  buildOverlayDecorations,
  diffLineClass,
} from '@/utils/codeMirrorOverlay.ts'

function makeState(content: string) {
  return EditorState.create({ doc: content, extensions: [javascript()] })
}

function collectClasses(set: { iter(): any }): string[] {
  const classes: string[] = []
  const iter = set.iter()
  while (iter.value) {
    const spec = (iter.value as any).spec
    classes.push(spec?.class || '')
    iter.next()
  }
  return classes
}

describe('diffLineClass', () => {
  it('maps marker type to a line class', () => {
    expect(diffLineClass('M')).toBe('cm-diff-line-M')
    expect(diffLineClass('D')).toBe('cm-diff-line-D')
  })
})

describe('buildOverlayDecorations', () => {
  it('adds line decorations for diff markers and flash marks', () => {
    const state = makeState('const a = 1\nconst b = 2\nconst c = 3\n')
    const marker = {
      id: 'm1',
      type: 'M',
      label: 'M',
      lineNumbers: [1, 2],
      charDiff: null,
      ariaLabel: 'modified',
    } as any

    const { decorations, diffLines } = buildOverlayDecorations(
      state,
      [marker],
      [{ line: 3, start: 0, end: 5 }],
      'add',
    )

    expect(diffLines.get(1)).toBe(marker)
    const classes = collectClasses(decorations)
    expect(classes).toContain('cm-diff-line-M')
    expect(classes).toContain('char-flash-add')
  })

  it('uses delete flash class for delete type', () => {
    const state = makeState('abc\ndef\n')
    const { decorations } = buildOverlayDecorations(
      state,
      [],
      [{ line: 1, start: 0, end: 2 }],
      'delete',
    )
    expect(collectClasses(decorations)).toContain('char-flash-delete')
  })

  it('clamps line numbers beyond the document', () => {
    const state = makeState('only one line')
    const marker = { id: 'm2', type: 'D', label: 'D', lineNumbers: [999], charDiff: null, ariaLabel: 'x' } as any
    const { decorations, diffLines } = buildOverlayDecorations(state, [marker], [], 'add')
    expect(diffLines.get(1)).toBe(marker)
    expect(collectClasses(decorations)).toContain('cm-diff-line-D')
  })
})
