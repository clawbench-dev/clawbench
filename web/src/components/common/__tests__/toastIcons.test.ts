import { describe, expect, it } from 'vitest'
import { resolveToastIcon } from '@/components/common/toastIcons'

describe('toastIcons', () => {
  it('resolves a named semantic key to a glyph component', () => {
    expect(resolveToastIcon('attach', 'success')).toBeTruthy()
    expect(resolveToastIcon('copy', 'success')).toBeTruthy()
    expect(resolveToastIcon('link', 'success')).toBeTruthy()
  })

  // The icon is structural: a toast that names no key still gets a glyph, drawn
  // from its type's default. This is what guarantees "every toast has an icon".
  it('falls back to the type default when no key is given', () => {
    expect(resolveToastIcon('', 'success')).toBeTruthy()
    expect(resolveToastIcon('', 'error')).toBeTruthy()
    expect(resolveToastIcon('', 'info')).toBeTruthy()
    expect(resolveToastIcon('', 'loading')).toBeTruthy()
  })

  it('type defaults are distinct so the type still reads at a glance', () => {
    const success = resolveToastIcon('', 'success')
    const error = resolveToastIcon('', 'error')
    const info = resolveToastIcon('', 'info')
    expect(success).not.toBe(error)
    expect(success).not.toBe(info)
    expect(error).not.toBe(info)
  })

  it('a named key overrides the type default', () => {
    expect(resolveToastIcon('attach', 'success')).not.toBe(resolveToastIcon('', 'success'))
  })
})
