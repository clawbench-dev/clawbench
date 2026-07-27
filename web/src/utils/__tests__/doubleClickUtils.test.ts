import { describe, expect, it } from 'vitest'
import {
  isExternalLink,
  isAnchorLink,
  slugifyForHeading,
  stripLeadingNumbering,
  buildQuoteMessage,
} from '@/utils/doubleClickUtils'

describe('doubleClickUtils', () => {
  // --- isExternalLink ---

  describe('isExternalLink', () => {
    it('returns true for http links', () => {
      expect(isExternalLink('http://example.com')).toBe(true)
    })

    it('returns true for https links', () => {
      expect(isExternalLink('https://example.com')).toBe(true)
    })

    it('returns true for mailto links', () => {
      expect(isExternalLink('mailto:user@example.com')).toBe(true)
    })

    it('returns true for tel links', () => {
      expect(isExternalLink('tel:+1234567890')).toBe(true)
    })

    it('returns true for protocol-relative links', () => {
      expect(isExternalLink('//cdn.example.com/script.js')).toBe(true)
    })

    it('returns false for relative paths', () => {
      expect(isExternalLink('src/main.go')).toBe(false)
    })

    it('returns false for anchor links', () => {
      expect(isExternalLink('#section')).toBe(false)
    })

    it('returns false for ./ paths', () => {
      expect(isExternalLink('./src/main.go')).toBe(false)
    })
  })

  // --- isAnchorLink ---

  describe('isAnchorLink', () => {
    it('returns true for # links', () => {
      expect(isAnchorLink('#section')).toBe(true)
    })

    it('returns true for # with complex id', () => {
      expect(isAnchorLink('#my-section-1')).toBe(true)
    })

    it('returns false for empty #', () => {
      expect(isAnchorLink('#')).toBe(true)
    })

    it('returns false for relative paths', () => {
      expect(isAnchorLink('src/main.go')).toBe(false)
    })

    it('returns false for http links', () => {
      expect(isAnchorLink('http://example.com')).toBe(false)
    })
  })

  // --- slugifyForHeading ---

  describe('slugifyForHeading', () => {
    it('converts to lowercase', () => {
      expect(slugifyForHeading('Hello World')).toBe('hello-world')
    })

    it('replaces spaces with dashes', () => {
      expect(slugifyForHeading('section one')).toBe('section-one')
    })

    it('handles CJK characters', () => {
      expect(slugifyForHeading('第四部分')).toBe('第四部分')
    })

    it('handles mixed CJK and ASCII', () => {
      expect(slugifyForHeading('Section 1: 第四部分')).toBe('section-1-第四部分')
    })

    it('strips leading and trailing dashes', () => {
      expect(slugifyForHeading('--hello--')).toBe('hello')
    })

    it('replaces multiple non-word chars with single dash', () => {
      expect(slugifyForHeading('a   b!!!c')).toBe('a-b-c')
    })

    it('handles empty string', () => {
      expect(slugifyForHeading('')).toBe('')
    })

    it('handles special characters', () => {
      expect(slugifyForHeading('hello@world!')).toBe('hello-world')
    })

    it('handles underscores (word characters)', () => {
      expect(slugifyForHeading('hello_world')).toBe('hello_world')
    })
  })

  // --- stripLeadingNumbering ---

  describe('stripLeadingNumbering', () => {
    it('strips "5. " prefix', () => {
      expect(stripLeadingNumbering('5. 第四部分')).toBe('第四部分')
    })

    it('strips "3: " prefix', () => {
      expect(stripLeadingNumbering('3: Something')).toBe('Something')
    })

    it('strips "1、 " prefix', () => {
      expect(stripLeadingNumbering('1、第一项')).toBe('第一项')
    })

    it('strips "2： " prefix', () => {
      expect(stripLeadingNumbering('2：第二项')).toBe('第二项')
    })

    it('does not strip text without leading numbers', () => {
      expect(stripLeadingNumbering('Hello World')).toBe('Hello World')
    })

    it('handles just a number', () => {
      expect(stripLeadingNumbering('42')).toBe('')
    })

    it('handles empty string', () => {
      expect(stripLeadingNumbering('')).toBe('')
    })

    it('strips "1.2.3 " prefix', () => {
      expect(stripLeadingNumbering('1.2.3 Deep section')).toBe('Deep section')
    })
  })

  // --- buildQuoteMessage ---

  describe('buildQuoteMessage', () => {
    it('builds message with language and single line', () => {
      const result = buildQuoteMessage('Explain this', 'const x = 1', 'src/main.go', 'go', 10, 10)
      expect(result).toBe('Explain this\n\n```go:src/main.go:10\nconst x = 1\n```')
    })

    it('builds message with language and line range', () => {
      const result = buildQuoteMessage('Explain this', 'code', 'src/main.go', 'go', 10, 15)
      expect(result).toBe('Explain this\n\n```go:src/main.go:10-15\ncode\n```')
    })

    it('builds message without language (uses colon prefix)', () => {
      const result = buildQuoteMessage('Explain this', 'text', 'readme.md', '', 5, 5)
      expect(result).toBe('Explain this\n\n```:readme.md:5\ntext\n```')
    })

    it('builds message without line numbers', () => {
      const result = buildQuoteMessage('Explain this', 'text', 'readme.md', '', 0, 0)
      expect(result).toBe('Explain this\n\n```:readme.md\ntext\n```')
    })

    it('trims user message', () => {
      const result = buildQuoteMessage('  Explain this  ', 'code', 'file.ts', 'ts', 1, 1)
      expect(result).toBe('Explain this\n\n```ts:file.ts:1\ncode\n```')
    })

    it('handles empty user message', () => {
      const result = buildQuoteMessage('', 'code', 'file.ts', 'ts', 1, 1)
      expect(result).toBe('\n\n```ts:file.ts:1\ncode\n```')
    })
  })
})
