import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * The jump dialog accepts an absolute path OR a path relative to the project
 * root, and the placeholder is the only place that tells the user so. The
 * wording previously said "inside the project", which wrongly implied absolute
 * paths were rejected even after the file manager learned to browse external
 * directories. These assertions pin the documented capability, not the exact
 * phrasing — reword freely, but do not drop the absolute-path mention.
 */
describe('jump placeholder documents accepted input', () => {
  const enJump = en.jump as Record<string, string>
  const zhJump = zh.jump as Record<string, string>

  it('en and zh expose the same jump keys', () => {
    expect(Object.keys(enJump).filter(k => !(k in zhJump))).toEqual([])
    expect(Object.keys(zhJump).filter(k => !(k in enJump))).toEqual([])
  })

  it('the file-manager placeholder mentions absolute paths in both locales', () => {
    // "absolute" / "绝对路径"
    expect(enJump.placeholder.toLowerCase()).toContain('absolute')
    expect(zhJump.placeholder).toContain('绝对路径')
  })

  it('the file-manager placeholder mentions project-relative paths in both locales', () => {
    expect(enJump.placeholder.toLowerCase()).toContain('relative')
    expect(zhJump.placeholder).toContain('相对路径')
  })

  it('the browse placeholder asks for an absolute path in both locales', () => {
    // ProjectDialog's picker has no project concept: /api/projects answers 400
    // for a relative path, so its placeholder must not suggest one.
    expect(enJump.placeholderBrowse.toLowerCase()).toContain('absolute')
    expect(zhJump.placeholderBrowse).toContain('绝对路径')
  })

  it('the file-manager placeholder no longer claims paths must be inside the project', () => {
    expect(enJump.placeholder.toLowerCase()).not.toContain('inside the project')
    expect(zhJump.placeholder).not.toContain('项目内')
  })
})
