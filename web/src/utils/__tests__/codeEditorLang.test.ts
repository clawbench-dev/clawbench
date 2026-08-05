import { describe, it, expect } from 'vitest'
import { buildLangExtension } from '@/utils/codeEditorLang'
import { javascript } from '@codemirror/lang-javascript'
import { go } from '@codemirror/lang-go'

describe('buildLangExtension', () => {
  it('returns a truthy extension for mapped languages', () => {
    expect(buildLangExtension('javascript')).toBeTruthy()
    expect(buildLangExtension('go')).toBeTruthy()
    expect(buildLangExtension('json')).toBeTruthy()
  })
  it('maps typescript to typescript mode', () => {
    expect(buildLangExtension('typescript')).toBeTruthy()
  })
  it('returns empty array for unknown languages (plain text fallback)', () => {
    expect(buildLangExtension('bash')).toEqual([])
    expect(buildLangExtension('toml')).toEqual([])
  })
  it('returns empty array for empty string', () => {
    expect(buildLangExtension('')).toEqual([])
  })
})
