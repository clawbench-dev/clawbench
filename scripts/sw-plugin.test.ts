import { describe, it, expect } from 'vitest'
import {
  computeVersion,
  buildPrecacheList,
  renderSw,
} from './sw-plugin'

describe('computeVersion', () => {
  it('changes when output files change', () => {
    const a = computeVersion(['index-a.js', 'vendor-b.js'])
    const b = computeVersion(['index-a.js', 'vendor-c.js'])
    expect(a).not.toBe(b)
  })
  it('is stable for the same output', () => {
    const files = ['index-a.js', 'vendor-b.js']
    expect(computeVersion(files)).toBe(computeVersion([...files]))
  })
})

describe('buildPrecacheList', () => {
  const known = new Set(['index.html', 'index-a.js', 'vendor-b.js', 'manifest.json', 'favicon.png'])
  const exists = (p: string) => known.has(p)

  it('includes index.html and entry/vendor chunks', () => {
    const precache = buildPrecacheList(
      ['index-a.js', 'vendor-b.js', 'vendor-c.js', 'sw.js'],
      exists,
    )
    expect(precache).toContain('/')
    expect(precache).toContain('/index.html')
    expect(precache).toContain('/index-a.js')
    expect(precache).toContain('/vendor-b.js')
  })

  it('excludes missing files (no 404 precache entries)', () => {
    const precache = buildPrecacheList(
      ['index-a.js', 'vendor-b.js', 'vendor-c.js', 'sw.js'],
      exists,
    )
    expect(precache).not.toContain('/vendor-c.js') // missing from `known`
    expect(precache).not.toContain('/sw.js') // never self-cache
    for (const p of precache) {
      const rel = p === '/' ? 'index.html' : p.slice(1)
      expect(known.has(rel)).toBe(true)
    }
  })
})

describe('renderSw', () => {
  it('injects version and precache array', () => {
    const template = 'const VERSION="__VERSION__";const PRECACHE=__PRECACHE__;'
    const out = renderSw(template, 'abc123', ['/', '/index.html'])
    expect(out).toContain('const VERSION="abc123"')
    expect(out).toContain('const PRECACHE=["/","/index.html"]')
  })
})
