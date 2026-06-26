import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useDoubleClickCopy } from '@/composables/useDoubleClickCopy'
import { copyText } from '@/utils/clipboard'

// Mock clipboard
vi.mock('@/utils/clipboard', () => ({
  copyText: vi.fn((_text: string, onDone?: () => void) => { onDone?.() }),
}))

// Mock doubleClickUtils
vi.mock('@/utils/doubleClickUtils', () => ({
  isExternalLink: (href: string) => href.startsWith('http://') || href.startsWith('https://'),
  isAnchorLink: (href: string) => href.startsWith('#'),
  slugifyForHeading: (s: string) => s.toLowerCase().replace(/\s+/g, '-'),
  stripLeadingNumbering: (s: string) => s.replace(/^\d+[.\s]+/, ''),
}))

// Mock useLocale
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

// Ensure CSS.escape is available in jsdom
if (typeof (globalThis as any).CSS === 'undefined') {
  ;(globalThis as any).CSS = {}
}
if (typeof (globalThis as any).CSS.escape === 'undefined') {
  ;(globalThis as any).CSS.escape = (s: string) => s.replace(/[!"#$%&'()*+,./:;<=>?@[\\\]^`{|}~]/g, '\\$&')
}

describe('useDoubleClickCopy', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  describe('handleAnchorClick — percent-encoded href decoding', () => {
    it('decodes percent-encoded Chinese href and calls onOpenFile with decoded path', () => {
      const { handleDblClick } = useDoubleClickCopy()
      const onOpenFile = vi.fn()

      // Create an anchor with percent-encoded Chinese href
      const anchor = document.createElement('a')
      anchor.setAttribute('href', '%E4%B8%AD%E6%96%87/%E6%96%87%E4%BB%B6.md')
      anchor.textContent = '链接'
      document.body.appendChild(anchor)

      const event = new MouseEvent('click', { bubbles: true, cancelable: true })
      anchor.dispatchEvent(event)
      // Re-create event to have the correct target
      Object.defineProperty(event, 'target', { value: anchor, writable: false })

      handleDblClick(event, onOpenFile)

      // onOpenFile should receive the decoded path, not the percent-encoded one
      expect(onOpenFile).toHaveBeenCalledWith('中文/文件.md')

      document.body.removeChild(anchor)
    })

    it('passes already-decoded Chinese href as-is to onOpenFile', () => {
      const { handleDblClick } = useDoubleClickCopy()
      const onOpenFile = vi.fn()

      const anchor = document.createElement('a')
      anchor.setAttribute('href', '中文/文件.md')
      anchor.textContent = '链接'
      document.body.appendChild(anchor)

      const event = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(event, 'target', { value: anchor, writable: false })

      handleDblClick(event, onOpenFile)

      expect(onOpenFile).toHaveBeenCalledWith('中文/文件.md')

      document.body.removeChild(anchor)
    })

    it('does not call onOpenFile for external https links with percent encoding', () => {
      const { handleDblClick } = useDoubleClickCopy()
      const onOpenFile = vi.fn()

      const anchor = document.createElement('a')
      anchor.setAttribute('href', 'https://example.com/%E4%B8%AD%E6%96%87')
      anchor.textContent = '外部链接'
      document.body.appendChild(anchor)

      const event = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(event, 'target', { value: anchor, writable: false })

      handleDblClick(event, onOpenFile)

      expect(onOpenFile).not.toHaveBeenCalled()

      document.body.removeChild(anchor)
    })

    it('decodes mixed ASCII and percent-encoded Chinese segments', () => {
      const { handleDblClick } = useDoubleClickCopy()
      const onOpenFile = vi.fn()

      const anchor = document.createElement('a')
      anchor.setAttribute('href', 'src/%E5%B7%A5%E5%85%B7/utils.ts')
      anchor.textContent = '工具'
      document.body.appendChild(anchor)

      const event = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(event, 'target', { value: anchor, writable: false })

      handleDblClick(event, onOpenFile)

      expect(onOpenFile).toHaveBeenCalledWith('src/工具/utils.ts')

      document.body.removeChild(anchor)
    })

    it('handles malformed percent encoding gracefully', () => {
      const { handleDblClick } = useDoubleClickCopy()
      const onOpenFile = vi.fn()

      const anchor = document.createElement('a')
      // %ZZ is not valid percent encoding
      anchor.setAttribute('href', 'path/%ZZfile.md')
      anchor.textContent = 'bad'
      document.body.appendChild(anchor)

      const event = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(event, 'target', { value: anchor, writable: false })

      handleDblClick(event, onOpenFile)

      // Should still call onOpenFile with the original (un-decoded) href
      expect(onOpenFile).toHaveBeenCalledWith('path/%ZZfile.md')

      document.body.removeChild(anchor)
    })

    it('does not call onOpenFile for anchor links (#section)', () => {
      const { handleDblClick } = useDoubleClickCopy()
      const onOpenFile = vi.fn()

      const anchor = document.createElement('a')
      anchor.setAttribute('href', '#section')
      anchor.textContent = 'jump'
      document.body.appendChild(anchor)

      const event = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(event, 'target', { value: anchor, writable: false })

      handleDblClick(event, onOpenFile)

      expect(onOpenFile).not.toHaveBeenCalled()

      document.body.removeChild(anchor)
    })

    it('does not call onOpenFile when no onOpenFile handler is provided', () => {
      const { handleDblClick } = useDoubleClickCopy()

      const anchor = document.createElement('a')
      anchor.setAttribute('href', '%E4%B8%AD%E6%96%87.md')
      anchor.textContent = '中文'
      document.body.appendChild(anchor)

      const event = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(event, 'target', { value: anchor, writable: false })

      // Should not throw
      expect(() => handleDblClick(event)).not.toThrow()

      document.body.removeChild(anchor)
    })
  })

  describe('handleDblClick — double-click copy', () => {
    afterEach(() => {
      vi.useRealTimers()
    })

    it('copies text on double-click of same element within threshold', () => {
      vi.useRealTimers()
      const { handleDblClick } = useDoubleClickCopy()

      const p = document.createElement('p')
      p.textContent = 'hello world'
      document.body.appendChild(p)

      const event1 = new MouseEvent('click', { bubbles: true })
      Object.defineProperty(event1, 'target', { value: p, writable: false })

      const event2 = new MouseEvent('click', { bubbles: true })
      Object.defineProperty(event2, 'target', { value: p, writable: false })

      handleDblClick(event1)
      handleDblClick(event2)

      expect(copyText).toHaveBeenCalled()

      document.body.removeChild(p)
    })

    it('does not copy on single click', () => {
      vi.useRealTimers()
      const { handleDblClick } = useDoubleClickCopy()

      const p = document.createElement('p')
      p.textContent = 'single'
      document.body.appendChild(p)

      const event = new MouseEvent('click', { bubbles: true })
      Object.defineProperty(event, 'target', { value: p, writable: false })

      vi.mocked(copyText).mockClear()
      handleDblClick(event)

      expect(copyText).not.toHaveBeenCalled()

      document.body.removeChild(p)
    })

    it('does not copy when clicking different elements', () => {
      vi.useRealTimers()
      const { handleDblClick } = useDoubleClickCopy()

      const p1 = document.createElement('p')
      p1.textContent = 'first'
      const p2 = document.createElement('p')
      p2.textContent = 'second'
      document.body.appendChild(p1)
      document.body.appendChild(p2)

      const event1 = new MouseEvent('click', { bubbles: true })
      Object.defineProperty(event1, 'target', { value: p1, writable: false })
      const event2 = new MouseEvent('click', { bubbles: true })
      Object.defineProperty(event2, 'target', { value: p2, writable: false })

      vi.mocked(copyText).mockClear()
      handleDblClick(event1)
      handleDblClick(event2)

      expect(copyText).not.toHaveBeenCalled()

      document.body.removeChild(p1)
      document.body.removeChild(p2)
    })
  })

  describe('doCopy with lineSelector', () => {
    it('copies from .code-text in line mode', () => {
      vi.useRealTimers()
      const { handleDblClick } = useDoubleClickCopy({ lineSelector: '.code-line' })

      const line = document.createElement('div')
      line.className = 'code-line'

      const codeText = document.createElement('span')
      codeText.className = 'code-text'
      codeText.textContent = 'import foo from bar'
      line.appendChild(codeText)
      document.body.appendChild(line)

      const event1 = new MouseEvent('click', { bubbles: true })
      Object.defineProperty(event1, 'target', { value: line, writable: false })
      const event2 = new MouseEvent('click', { bubbles: true })
      Object.defineProperty(event2, 'target', { value: line, writable: false })

      vi.mocked(copyText).mockClear()
      handleDblClick(event1)
      handleDblClick(event2)

      expect(copyText).toHaveBeenCalled()

      document.body.removeChild(line)
    })
  })

  describe('doCopy with onCopy callback', () => {
    it('calls onCopy callback after successful copy', () => {
      vi.useRealTimers()
      const onCopy = vi.fn()
      const { handleDblClick } = useDoubleClickCopy({ onCopy })

      const p = document.createElement('p')
      p.textContent = 'callback text'
      document.body.appendChild(p)

      const event1 = new MouseEvent('click', { bubbles: true })
      Object.defineProperty(event1, 'target', { value: p, writable: false })
      const event2 = new MouseEvent('click', { bubbles: true })
      Object.defineProperty(event2, 'target', { value: p, writable: false })

      handleDblClick(event1)
      handleDblClick(event2)

      expect(onCopy).toHaveBeenCalledWith(p, 'callback text')

      document.body.removeChild(p)
    })
  })
})
