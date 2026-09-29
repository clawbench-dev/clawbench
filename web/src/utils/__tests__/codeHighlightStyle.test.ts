import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Guard: every CodeMirror surface must resolve syntax colours through the
 * single shared HighlightStyle.
 *
 * Two editors now exist (the file viewer and the task gating-script editor),
 * and a second copy of the `HighlightStyle.define([...])` array would let the
 * two drift — one theme variable renamed in one place and the other surface
 * silently loses its colours. The mapping is small and easy to duplicate by
 * copy-paste, so this is asserted at the source level: no component may define
 * its own HighlightStyle, and both must import the shared one.
 */

const webSrc = resolve(__dirname, '../..')

function read(rel: string): string {
  return readFileSync(resolve(webSrc, rel), 'utf8')
}

describe('CodeMirror syntax highlight style is shared', () => {
  it('is defined exactly once, in codeHighlightStyle.ts', () => {
    const shared = read('utils/codeHighlightStyle.ts')
    expect(shared).toContain('HighlightStyle.define(')

    for (const rel of ['components/file/CodeMirrorViewer.vue', 'components/task/TaskScriptEditor.vue']) {
      const source = read(rel)
      expect(source, `${rel} must not define its own HighlightStyle`).not.toContain('HighlightStyle.define(')
      expect(source, `${rel} must use the shared highlight style`).toContain('codeHighlightStyle')
    }
  })

  it('maps tokens to theme variables, never to literal colours', () => {
    // The whole point of the shared style is that a theme only defines the
    // --code-syntax-* variables. A hardcoded hex here would ignore every theme.
    const shared = read('utils/codeHighlightStyle.ts')
    const styleBody = shared.slice(shared.indexOf('HighlightStyle.define('))
    const colorLiterals = styleBody.match(/color:\s*'#[0-9a-fA-F]{3,8}'/g) || []
    expect(colorLiterals, 'token colours must come from CSS variables').toHaveLength(0)
  })
})
