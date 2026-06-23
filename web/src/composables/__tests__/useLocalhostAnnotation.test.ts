import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'

// Mock composables before importing the module
const mockIsAppMode = ref(false)
const mockSshInfo = ref<any>(null)
const mockEnsurePortRegistered = vi.fn().mockResolvedValue(3000)
const mockOpenPort = vi.fn()
const mockToastShow = vi.fn()

vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: mockIsAppMode }),
}))

vi.mock('@/composables/usePortForward', () => ({
  usePortForward: () => ({
    sshInfo: mockSshInfo,
    ensurePortRegistered: mockEnsurePortRegistered,
    openPort: mockOpenPort,
  }),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

import {
  isLocalhostUrl,
  parseLocalhostUrl,
  localhostOpenButtonHtml,
  annotateLocalhostUrls,
  useLocalhostUrlClickHandler,
} from '@/composables/useLocalhostAnnotation'

describe('useLocalhostAnnotation', () => {
  // --- isLocalhostUrl ---

  describe('isLocalhostUrl', () => {
    it('returns true for http://localhost with port', () => {
      expect(isLocalhostUrl('http://localhost:3000')).toBe(true)
    })

    it('returns true for https://localhost with port', () => {
      expect(isLocalhostUrl('https://localhost:3000')).toBe(true)
    })

    it('returns true for http://127.0.0.1 with port', () => {
      expect(isLocalhostUrl('http://127.0.0.1:5173')).toBe(true)
    })

    it('returns false for non-localhost URLs', () => {
      expect(isLocalhostUrl('https://example.com')).toBe(false)
    })

    it('returns false for localhost without port', () => {
      expect(isLocalhostUrl('http://localhost')).toBe(false)
    })

    it('returns false for empty string', () => {
      expect(isLocalhostUrl('')).toBe(false)
    })
  })

  // --- parseLocalhostUrl ---

  describe('parseLocalhostUrl', () => {
    it('parses http://localhost:3000', () => {
      const result = parseLocalhostUrl('http://localhost:3000')
      expect(result).toEqual({
        port: 3000,
        protocol: 'http',
        fullUrl: 'http://localhost:3000',
      })
    })

    it('parses https://127.0.0.1:5173/path', () => {
      const result = parseLocalhostUrl('https://127.0.0.1:5173/path')
      expect(result).toEqual({
        port: 5173,
        protocol: 'https',
        fullUrl: 'https://127.0.0.1:5173',
      })
    })

    it('returns null for non-localhost URL', () => {
      expect(parseLocalhostUrl('https://example.com:3000')).toBeNull()
    })

    it('returns null for localhost without port', () => {
      expect(parseLocalhostUrl('http://localhost')).toBeNull()
    })
  })

  // --- localhostOpenButtonHtml ---

  describe('localhostOpenButtonHtml', () => {
    it('generates button HTML with data attributes', () => {
      const html = localhostOpenButtonHtml(3000, 'http', 'http://localhost:3000')
      expect(html).toContain('class="chat-url-open-btn"')
      expect(html).toContain('data-port="3000"')
      expect(html).toContain('data-protocol="http"')
      expect(html).toContain('data-url="http://localhost:3000"')
      expect(html).toContain('title="Open in WebView"')
    })

    it('escapes special characters in URL', () => {
      const html = localhostOpenButtonHtml(3000, 'http', 'http://localhost:3000/path?a=1&b=2')
      expect(html).toContain('data-url="http://localhost:3000/path?a=1&amp;b=2"')
    })
  })

  // --- annotateLocalhostUrls ---

  describe('annotateLocalhostUrls', () => {
    beforeEach(() => {
      mockIsAppMode.value = false
      mockSshInfo.value = null
    })

    it('returns empty string unchanged', () => {
      expect(annotateLocalhostUrls('')).toBe('')
    })

    it('returns HTML unchanged in web mode (isAppMode=false)', () => {
      mockIsAppMode.value = false
      const html = '<p>Visit http://localhost:3000</p>'
      expect(annotateLocalhostUrls(html)).toBe(html)
    })

    it('returns HTML unchanged when SSH is disabled', () => {
      mockIsAppMode.value = true
      mockSshInfo.value = { enabled: false }
      const html = '<p>Visit http://localhost:3000</p>'
      expect(annotateLocalhostUrls(html)).toBe(html)
    })

    it('annotates bare localhost URLs when SSH enabled and app mode', () => {
      mockIsAppMode.value = true
      mockSshInfo.value = { enabled: true }
      const html = '<p>Visit http://localhost:3000</p>'
      const result = annotateLocalhostUrls(html)
      expect(result).toContain('chat-url-open-btn')
      expect(result).toContain('data-port="3000"')
    })

    it('annotates localhost URLs inside <a> tags', () => {
      mockIsAppMode.value = true
      mockSshInfo.value = { enabled: true }
      const html = '<a href="http://localhost:5173">link</a>'
      const result = annotateLocalhostUrls(html)
      expect(result).toContain('chat-url-open-btn')
      expect(result).toContain('data-port="5173"')
    })

    it('annotates localhost URLs inside <code> tags', () => {
      mockIsAppMode.value = true
      mockSshInfo.value = { enabled: true }
      const html = '<code>http://localhost:8080</code>'
      const result = annotateLocalhostUrls(html)
      expect(result).toContain('chat-url-open-btn')
      expect(result).toContain('data-port="8080"')
    })

    it('annotates inside <pre> blocks', () => {
      mockIsAppMode.value = true
      mockSshInfo.value = { enabled: true }
      const html = '<pre>http://localhost:3000</pre>'
      const result = annotateLocalhostUrls(html)
      expect(result).toContain('chat-url-open-btn')
    })

    it('annotates 127.0.0.1 URLs', () => {
      mockIsAppMode.value = true
      mockSshInfo.value = { enabled: true }
      const html = '<p>http://127.0.0.1:9090</p>'
      const result = annotateLocalhostUrls(html)
      expect(result).toContain('chat-url-open-btn')
      expect(result).toContain('data-port="9090"')
    })

    it('handles null sshInfo (not yet loaded) as enabled', () => {
      mockIsAppMode.value = true
      mockSshInfo.value = null
      const html = '<p>http://localhost:3000</p>'
      const result = annotateLocalhostUrls(html)
      // null = still loading, should annotate (same as enabled)
      expect(result).toContain('chat-url-open-btn')
    })
  })

  // --- useLocalhostUrlClickHandler ---

  describe('useLocalhostUrlClickHandler', () => {
    let handleLocalhostUrlClick: (event: MouseEvent) => boolean
    let openLocalhostUrl: (element: Element, port: number, protocol: string) => Promise<boolean>

    beforeEach(() => {
      mockIsAppMode.value = true
      mockSshInfo.value = { enabled: true }
      mockEnsurePortRegistered.mockReset().mockResolvedValue(3000)
      mockOpenPort.mockReset()
      mockToastShow.mockReset()
      const handler = useLocalhostUrlClickHandler()
      handleLocalhostUrlClick = handler.handleLocalhostUrlClick
      openLocalhostUrl = handler.openLocalhostUrl
    })

    function createClickEvent(target: Element): MouseEvent {
      const event = {
        target,
        preventDefault: vi.fn(),
        stopPropagation: vi.fn(),
      } as unknown as MouseEvent
      return event
    }

    it('returns false in web mode', () => {
      mockIsAppMode.value = false
      const btn = document.createElement('button')
      btn.className = 'chat-url-open-btn'
      const event = createClickEvent(btn)
      expect(handleLocalhostUrlClick(event)).toBe(false)
    })

    it('handles click on .chat-url-open-btn icon button', async () => {
      const btn = document.createElement('button')
      btn.className = 'chat-url-open-btn'
      btn.setAttribute('data-port', '3000')
      btn.setAttribute('data-protocol', 'http')
      const event = createClickEvent(btn)
      const result = handleLocalhostUrlClick(event)
      expect(result).toBe(true)
      expect(event.preventDefault).toHaveBeenCalled()
      expect(event.stopPropagation).toHaveBeenCalled()
      // openLocalhostUrl is fire-and-forget, wait for it
      await vi.waitFor(() => {
        expect(mockEnsurePortRegistered).toHaveBeenCalledWith(3000, 'http')
      })
      expect(mockOpenPort).toHaveBeenCalledWith(3000, 'http')
    })

    it('ignores icon button with invalid port', () => {
      const btn = document.createElement('button')
      btn.className = 'chat-url-open-btn'
      btn.setAttribute('data-port', '0')
      btn.setAttribute('data-protocol', 'http')
      const event = createClickEvent(btn)
      const result = handleLocalhostUrlClick(event)
      expect(result).toBe(true)
      expect(event.preventDefault).toHaveBeenCalled()
      // Should not call openLocalhostUrl with port <= 0
      expect(mockEnsurePortRegistered).not.toHaveBeenCalled()
    })

    it('handles click on <a> tag with localhost href', async () => {
      const anchor = document.createElement('a')
      anchor.setAttribute('href', 'http://localhost:5173')
      anchor.textContent = 'link'
      const event = createClickEvent(anchor)
      const result = handleLocalhostUrlClick(event)
      expect(result).toBe(true)
      expect(event.preventDefault).toHaveBeenCalled()
      await vi.waitFor(() => {
        expect(mockEnsurePortRegistered).toHaveBeenCalledWith(5173, 'http')
      })
    })

    it('falls back to window.open when SSH disabled (anchor click)', async () => {
      mockSshInfo.value = { enabled: false }
      const anchor = document.createElement('a')
      anchor.setAttribute('href', 'http://localhost:5173')
      anchor.textContent = 'link'
      const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
      const event = createClickEvent(anchor)
      handleLocalhostUrlClick(event)
      await vi.waitFor(() => {
        expect(openSpy).toHaveBeenCalledWith('http://localhost:5173', '_blank')
      })
      openSpy.mockRestore()
    })

    it('shows info toast when SSH disabled (anchor click)', async () => {
      mockSshInfo.value = { enabled: false }
      const anchor = document.createElement('a')
      anchor.setAttribute('href', 'http://localhost:5173')
      anchor.textContent = 'link'
      const event = createClickEvent(anchor)
      handleLocalhostUrlClick(event)
      await vi.waitFor(() => {
        expect(mockToastShow).toHaveBeenCalledWith('chat.localhost.sshDisabled', { type: 'info' })
      })
    })

    it('returns false for non-localhost anchor click', () => {
      const anchor = document.createElement('a')
      anchor.setAttribute('href', 'https://example.com')
      anchor.textContent = 'link'
      const event = createClickEvent(anchor)
      expect(handleLocalhostUrlClick(event)).toBe(false)
    })

    it('returns false for click on non-special element', () => {
      const div = document.createElement('div')
      const event = createClickEvent(div)
      expect(handleLocalhostUrlClick(event)).toBe(false)
    })

    // --- openLocalhostUrl ---

    it('openLocalhostUrl returns true on success', async () => {
      const btn = document.createElement('button')
      const result = await openLocalhostUrl(btn, 3000, 'http')
      expect(result).toBe(true)
      expect(mockEnsurePortRegistered).toHaveBeenCalledWith(3000, 'http')
      expect(mockOpenPort).toHaveBeenCalledWith(3000, 'http')
      expect(btn.classList.contains('loading')).toBe(false)
    })

    it('openLocalhostUrl returns false when SSH disabled', async () => {
      mockSshInfo.value = { enabled: false }
      const btn = document.createElement('button')
      const result = await openLocalhostUrl(btn, 3000, 'http')
      expect(result).toBe(false)
      expect(mockToastShow).toHaveBeenCalledWith('chat.localhost.sshDisabled', { type: 'info' })
    })

    it('openLocalhostUrl shows error toast on failure', async () => {
      mockEnsurePortRegistered.mockRejectedValue(new Error('fail'))
      const btn = document.createElement('button')
      const result = await openLocalhostUrl(btn, 3000, 'http')
      expect(result).toBe(true)
      expect(mockToastShow).toHaveBeenCalledWith('chat.localhost.openFailed', { type: 'error' })
      expect(btn.classList.contains('loading')).toBe(false)
    })

    it('openLocalhostUrl prevents double-click while opening', async () => {
      let resolveOpen: () => void
      mockEnsurePortRegistered.mockReturnValue(new Promise<void>(r => { resolveOpen = r }))
      const btn = document.createElement('button')
      // First call — starts opening
      const p1 = openLocalhostUrl(btn, 3000, 'http')
      // Second call while first is in progress — should return true (already opening)
      const result2 = await openLocalhostUrl(btn, 3000, 'http')
      expect(result2).toBe(true)
      // Resolve the first call
      resolveOpen!()
      await p1
    })
  })
})
