import { describe, expect, it } from 'vitest'
import {
  isExternalLink,
  isAnchorLink,
  slugifyForHeading,
  stripLeadingNumbering,
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

    it.each(['ftp://example.com/file', 'sms:+1234567890', 'callto:user', 'cid:part1', 'xmpp:user@example.com'])(
      'returns true for sanitizer-supported external link %s',
      (href) => {
        expect(isExternalLink(href)).toBe(true)
      },
    )

    it('returns false for file URIs handled by the in-app file viewer', () => {
      expect(isExternalLink('file:///workspace/src/main.go')).toBe(false)
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
})
