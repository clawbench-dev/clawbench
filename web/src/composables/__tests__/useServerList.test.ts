import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useServerList, findNameConflict, normalizeServerName } from '@/composables/useServerList'

describe('useServerList', () => {
  beforeEach(() => {
    localStorage.clear()
    // Clear any native bridge
    delete (window as any).ClawBenchNative
  })

  afterEach(() => {
    localStorage.clear()
  })

  // ── load() with localStorage (web mode) ──

  describe('load', () => {
    it('loads servers from localStorage', async () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
        { url: 'http://server2:9090', password: 'pass2' },
      ]))

      const { servers, load } = useServerList()
      await load()

      expect(servers.value).toHaveLength(2)
      expect(servers.value[0].url).toBe('http://server1:8080')
      expect(servers.value[1].password).toBe('pass2')
    })

    it('returns empty array when localStorage is empty', async () => {
      const { servers, load } = useServerList()
      await load()
      expect(servers.value).toEqual([])
    })

    it('handles invalid JSON in localStorage', async () => {
      localStorage.setItem('clawbench-servers', 'not-json')

      const { servers, load } = useServerList()
      await load()

      expect(servers.value).toEqual([])
    })

    it('filters entries without url field', async () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
        { password: 'orphan' },
        { url: 'http://server2:9090', password: 'pass2' },
      ]))

      const { servers, load } = useServerList()
      await load()

      expect(servers.value).toHaveLength(2)
    })

    it('handles non-array JSON in localStorage', async () => {
      localStorage.setItem('clawbench-servers', JSON.stringify({ url: 'test' }))

      const { servers, load } = useServerList()
      await load()

      expect(servers.value).toEqual([])
    })

    it('loads from native bridge when available', async () => {
      ;(window as any).ClawBenchNative = {
        getServerList: () => JSON.stringify([
          { url: 'http://native:8080', password: 'nativepass' },
        ]),
      }

      const { servers, load } = useServerList()
      await load()

      expect(servers.value).toHaveLength(1)
      expect(servers.value[0].url).toBe('http://native:8080')

      delete (window as any).ClawBenchNative
    })

    it('resolves async getServerList results', async () => {
      ;(window as any).ClawBenchNative = {
        getServerList: () => Promise.resolve(JSON.stringify([
          { url: 'http://native:8080', password: 'nativepass' },
        ])),
      }

      const { servers, load } = useServerList()
      await load()

      expect(servers.value).toHaveLength(1)
      expect(servers.value[0].url).toBe('http://native:8080')

      delete (window as any).ClawBenchNative
    })
  })

  // ── save() ──

  describe('save', () => {
    it('adds a new server to localStorage', async () => {
      const { servers, load, save } = useServerList()
      await load()

      await save('http://newserver:8080', 'newpass')

      expect(servers.value).toHaveLength(1)
      expect(servers.value[0].url).toBe('http://newserver:8080')
      expect(servers.value[0].password).toBe('newpass')

      const stored = JSON.parse(localStorage.getItem('clawbench-servers')!)
      expect(stored).toHaveLength(1)
      expect(stored[0].url).toBe('http://newserver:8080')
    })

    it('updates password for an existing server', async () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'oldpass' },
      ]))

      const { servers, load, save } = useServerList()
      await load()

      await save('http://server1:8080', 'newpass')

      expect(servers.value).toHaveLength(1)
      expect(servers.value[0].password).toBe('newpass')

      const stored = JSON.parse(localStorage.getItem('clawbench-servers')!)
      expect(stored[0].password).toBe('newpass')
    })

    it('uses native.saveServer when available', async () => {
      const mockSave = vi.fn()
      const mockGetList = vi.fn(() => JSON.stringify([
        { url: 'http://existing:8080', password: 'pass' },
      ]))
      ;(window as any).ClawBenchNative = {
        getServerList: mockGetList,
        saveServer: mockSave,
      }

      const { load, save } = useServerList()
      await load()

      await save('http://newserver:8080', 'newpass')

      expect(mockSave).toHaveBeenCalledWith('http://newserver:8080', 'newpass')

      delete (window as any).ClawBenchNative
    })

    it('prefers native.saveServerNamed and passes a trimmed name', async () => {
      const mockNamed = vi.fn()
      const mockSave = vi.fn()
      ;(window as any).ClawBenchNative = {
        getServerList: () => JSON.stringify([]),
        saveServer: mockSave,
        saveServerNamed: mockNamed,
      }

      const { load, save } = useServerList()
      await load()

      await save('http://newserver:8080', 'newpass', '  Home NAS  ')

      expect(mockNamed).toHaveBeenCalledWith('http://newserver:8080', 'newpass', 'Home NAS')
      // The unnamed variant must NOT also fire, or the name would be clobbered.
      expect(mockSave).not.toHaveBeenCalled()

      delete (window as any).ClawBenchNative
    })

    it('falls back to saveServer when the host lacks saveServerNamed', async () => {
      const mockSave = vi.fn()
      ;(window as any).ClawBenchNative = {
        getServerList: () => JSON.stringify([]),
        saveServer: mockSave,
      }

      const { load, save } = useServerList()
      await load()

      await save('http://newserver:8080', 'newpass', 'Home NAS')

      // The entry still persists; the name is dropped rather than blocking.
      expect(mockSave).toHaveBeenCalledWith('http://newserver:8080', 'newpass')

      delete (window as any).ClawBenchNative
    })

    it('stores the name in localStorage in web mode', async () => {
      const { servers, load, save } = useServerList()
      await load()

      await save('http://named:8080', 'pw', 'Lab')

      expect(servers.value[0].name).toBe('Lab')
      const stored = JSON.parse(localStorage.getItem('clawbench-servers')!)
      expect(stored[0].name).toBe('Lab')
    })

    it('clearing the name removes the field rather than storing an empty string', async () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://named:8080', password: 'pw', name: 'Lab' },
      ]))

      const { load, save } = useServerList()
      await load()

      await save('http://named:8080', 'pw', '   ')

      const stored = JSON.parse(localStorage.getItem('clawbench-servers')!)
      expect(stored[0].name).toBeUndefined()
    })
  })

  // ── getLabel() ──

  describe('getLabel', () => {
    it('prefers the name and falls back to the url', async () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://named:8080', password: 'pw', name: 'Home' },
        { url: 'http://unnamed:8080', password: 'pw' },
      ]))

      const { servers, load, getLabel } = useServerList()
      await load()

      expect(getLabel(servers.value[0])).toBe('Home')
      expect(getLabel(servers.value[1])).toBe('http://unnamed:8080')
    })

    it('treats a whitespace-only name as no name', async () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://blank:8080', password: 'pw', name: '   ' },
      ]))

      const { servers, load, getLabel } = useServerList()
      await load()

      expect(getLabel(servers.value[0])).toBe('http://blank:8080')
    })
  })

  // ── findNameConflict() ──

  describe('findNameConflict', () => {
    const list = [
      { url: 'http://a:8080', password: '', name: 'Home' },
      { url: 'http://b:8080', password: '' },
    ]

    it('normalises names by trimming', () => {
      expect(normalizeServerName('  Home  ')).toBe('Home')
      expect(normalizeServerName(undefined)).toBe('')
    })

    it('matches case-insensitively', () => {
      expect(findNameConflict(list, 'home')?.url).toBe('http://a:8080')
      expect(findNameConflict(list, '  HOME  ')?.url).toBe('http://a:8080')
    })

    it('returns null for a free name', () => {
      expect(findNameConflict(list, 'Lab')).toBeNull()
    })

    it('never matches an empty name', () => {
      expect(findNameConflict(list, '')).toBeNull()
      expect(findNameConflict(list, '   ')).toBeNull()
    })

    it('exempts the entry being edited', () => {
      expect(findNameConflict(list, 'Home', 'http://a:8080')).toBeNull()
      // ...but still catches a collision with a different server.
      expect(findNameConflict(list, 'Home', 'http://b:8080')?.url).toBe('http://a:8080')
    })
  })

  // ── remove() ──

  describe('remove', () => {
    it('removes a server from localStorage', async () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
        { url: 'http://server2:9090', password: 'pass2' },
      ]))

      const { servers, load, remove } = useServerList()
      await load()

      await remove('http://server1:8080')

      expect(servers.value).toHaveLength(1)
      expect(servers.value[0].url).toBe('http://server2:9090')

      const stored = JSON.parse(localStorage.getItem('clawbench-servers')!)
      expect(stored).toHaveLength(1)
    })

    it('handles removing non-existent server gracefully', async () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
      ]))

      const { servers, load, remove } = useServerList()
      await load()

      await remove('http://nonexistent:8080')

      // Server list unchanged
      expect(servers.value).toHaveLength(1)
    })

    it('uses native.removeServer when available', async () => {
      const mockRemove = vi.fn()
      const mockGetList = vi.fn(() => JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
      ]))
      ;(window as any).ClawBenchNative = {
        getServerList: mockGetList,
        removeServer: mockRemove,
      }

      const { load, remove } = useServerList()
      await load()

      await remove('http://server1:8080')

      expect(mockRemove).toHaveBeenCalledWith('http://server1:8080')

      delete (window as any).ClawBenchNative
    })
  })

  // ── getPassword() ──

  describe('getPassword', () => {
    it('returns password for a known server URL', async () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
      ]))

      const { load, getPassword } = useServerList()
      await load()

      expect(getPassword('http://server1:8080')).toBe('pass1')
    })

    it('returns empty string for unknown server URL', async () => {
      localStorage.setItem('clawbench-servers', JSON.stringify([
        { url: 'http://server1:8080', password: 'pass1' },
      ]))

      const { load, getPassword } = useServerList()
      await load()

      expect(getPassword('http://unknown:8080')).toBe('')
    })

    it('returns empty string before load is called', () => {
      const { getPassword } = useServerList()
      expect(getPassword('http://any:8080')).toBe('')
    })
  })
})
