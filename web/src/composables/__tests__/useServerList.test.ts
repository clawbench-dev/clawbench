import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useServerList } from '@/composables/useServerList'

describe('useServerList', () => {
  beforeEach(() => {
    localStorage.clear()
    // Clear any AndroidNative bridge
    delete (window as any).AndroidNative
  })

  afterEach(() => {
    localStorage.clear()
  })

  // ── load() with localStorage (web mode) ──

  describe('load', () => {
    it('loads servers from localStorage', () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
        { url: 'http://server2:9090', password: 'pass2' },
      ]))

      const { servers, load } = useServerList()
      load()

      expect(servers.value).toHaveLength(2)
      expect(servers.value[0].url).toBe('http://server1:8080')
      expect(servers.value[1].password).toBe('pass2')
    })

    it('returns empty array when localStorage is empty', () => {
      const { servers, load } = useServerList()
      load()
      expect(servers.value).toEqual([])
    })

    it('handles invalid JSON in localStorage', () => {
      localStorage.setItem('clawbench-servers', 'not-json')

      const { servers, load } = useServerList()
      load()

      expect(servers.value).toEqual([])
    })

    it('filters entries without url field', () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
        { password: 'orphan' },
        { url: 'http://server2:9090', password: 'pass2' },
      ]))

      const { servers, load } = useServerList()
      load()

      expect(servers.value).toHaveLength(2)
    })

    it('handles non-array JSON in localStorage', () => {
      localStorage.setItem('clawbench-servers', JSON.stringify({ url: 'test' }))

      const { servers, load } = useServerList()
      load()

      expect(servers.value).toEqual([])
    })

    it('loads from AndroidNative bridge when available', () => {
      (window as any).AndroidNative = {
        getServerList: () => JSON.stringify([
          { url: 'http://native:8080', password: 'nativepass' },
        ]),
      }

      const { servers, load } = useServerList()
      load()

      expect(servers.value).toHaveLength(1)
      expect(servers.value[0].url).toBe('http://native:8080')

      delete (window as any).AndroidNative
    })
  })

  // ── save() ──

  describe('save', () => {
    it('adds a new server to localStorage', () => {
      const { servers, load, save } = useServerList()
      load()

      save('http://newserver:8080', 'newpass')

      expect(servers.value).toHaveLength(1)
      expect(servers.value[0].url).toBe('http://newserver:8080')
      expect(servers.value[0].password).toBe('newpass')

      const stored = JSON.parse(localStorage.getItem('clawbench-servers')!)
      expect(stored).toHaveLength(1)
      expect(stored[0].url).toBe('http://newserver:8080')
    })

    it('updates password for an existing server', () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'oldpass' },
      ]))

      const { servers, load, save } = useServerList()
      load()

      save('http://server1:8080', 'newpass')

      expect(servers.value).toHaveLength(1)
      expect(servers.value[0].password).toBe('newpass')

      const stored = JSON.parse(localStorage.getItem('clawbench-servers')!)
      expect(stored[0].password).toBe('newpass')
    })

    it('uses AndroidNative.saveServer when available', () => {
      const mockSave = vi.fn()
      const mockGetList = vi.fn(() => JSON.stringify([
        { url: 'http://existing:8080', password: 'pass' },
      ]))
      ;(window as any).AndroidNative = {
        getServerList: mockGetList,
        saveServer: mockSave,
      }

      const { load, save } = useServerList()
      load()

      save('http://newserver:8080', 'newpass')

      expect(mockSave).toHaveBeenCalledWith('http://newserver:8080', 'newpass')

      delete (window as any).AndroidNative
    })
  })

  // ── remove() ──

  describe('remove', () => {
    it('removes a server from localStorage', () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
        { url: 'http://server2:9090', password: 'pass2' },
      ]))

      const { servers, load, remove } = useServerList()
      load()

      remove('http://server1:8080')

      expect(servers.value).toHaveLength(1)
      expect(servers.value[0].url).toBe('http://server2:9090')

      const stored = JSON.parse(localStorage.getItem('clawbench-servers')!)
      expect(stored).toHaveLength(1)
    })

    it('handles removing non-existent server gracefully', () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
      ]))

      const { servers, load, remove } = useServerList()
      load()

      remove('http://nonexistent:8080')

      // Server list unchanged
      expect(servers.value).toHaveLength(1)
    })

    it('uses AndroidNative.removeServer when available', () => {
      const mockRemove = vi.fn()
      const mockGetList = vi.fn(() => JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
      ]))
      ;(window as any).AndroidNative = {
        getServerList: mockGetList,
        removeServer: mockRemove,
      }

      const { load, remove } = useServerList()
      load()

      remove('http://server1:8080')

      expect(mockRemove).toHaveBeenCalledWith('http://server1:8080')

      delete (window as any).AndroidNative
    })
  })

  // ── getPassword() ──

  describe('getPassword', () => {
    it('returns password for a known server URL', () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
      ]))

      const { load, getPassword } = useServerList()
      load()

      expect(getPassword('http://server1:8080')).toBe('pass1')
    })

    it('returns empty string for unknown server URL', () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
      ]))

      const { load, getPassword } = useServerList()
      load()

      expect(getPassword('http://unknown:8080')).toBe('')
    })

    it('returns empty string before load is called', () => {
      const { getPassword } = useServerList()
      expect(getPassword('http://any:8080')).toBe('')
    })
  })
})
