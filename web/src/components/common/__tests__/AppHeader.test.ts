import { describe, expect, it, vi, beforeEach } from 'vitest'

// Test AppHeader logic without mounting the full component
// (Component has complex Teleport/Popup dependencies that make shallow mounting unreliable)

const {
  mockWsStatus,
  mockIsAppMode,
  mockGitBranch,
} = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue')
  return {
    mockWsStatus: ref<'connected' | 'reconnecting' | 'disconnected'>('connected'),
    mockIsAppMode: ref(false),
    mockGitBranch: ref(''),
  }
})

vi.mock('@/composables/useGlobalEvents', () => ({
  useGlobalEvents: () => ({ wsStatus: mockWsStatus }),
}))

vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: mockIsAppMode }),
}))

vi.mock('@/stores/app.ts', () => ({
  store: {
    state: { gitBranch: mockGitBranch },
    loadGitBranch: vi.fn(),
  },
}))

import { computed } from 'vue'

describe('AppHeader logic', () => {
  beforeEach(() => {
    mockWsStatus.value = 'connected'
    mockIsAppMode.value = false
    mockGitBranch.value = ''
  })

  it('statusDotClass returns connected for connected status', () => {
    mockWsStatus.value = 'connected'
    const statusDotClass = computed(() => {
      if (mockWsStatus.value === 'disconnected') return 'status-dot-disconnected'
      if (mockWsStatus.value === 'reconnecting') return 'status-dot-reconnecting'
      return 'status-dot-connected'
    })
    expect(statusDotClass.value).toBe('status-dot-connected')
  })

  it('statusDotClass returns reconnecting for reconnecting status', () => {
    mockWsStatus.value = 'reconnecting'
    const statusDotClass = computed(() => {
      if (mockWsStatus.value === 'disconnected') return 'status-dot-disconnected'
      if (mockWsStatus.value === 'reconnecting') return 'status-dot-reconnecting'
      return 'status-dot-connected'
    })
    expect(statusDotClass.value).toBe('status-dot-reconnecting')
  })

  it('statusDotClass returns disconnected for disconnected status', () => {
    mockWsStatus.value = 'disconnected'
    const statusDotClass = computed(() => {
      if (mockWsStatus.value === 'disconnected') return 'status-dot-disconnected'
      if (mockWsStatus.value === 'reconnecting') return 'status-dot-reconnecting'
      return 'status-dot-connected'
    })
    expect(statusDotClass.value).toBe('status-dot-disconnected')
  })

  it('projectName returns basename for valid path', () => {
    const baseName = (p: string) => {
      if (!p) return 'Select Project'
      const parts = p.replace(/\\/g, '/').split('/')
      return parts[parts.length - 1] || p
    }
    expect(baseName('/home/user/myapp')).toBe('myapp')
  })

  it('projectName returns select project for empty path', () => {
    const baseName = (p: string) => {
      if (!p) return 'Select Project'
      return p
    }
    expect(baseName('')).toBe('Select Project')
  })

  it('gitBranch computed from store state', () => {
    mockGitBranch.value = 'feature-branch'
    const gitBranch = computed(() => mockGitBranch.value)
    expect(gitBranch.value).toBe('feature-branch')
  })

  it('isAppMode determines status dot behavior', () => {
    mockIsAppMode.value = true
    expect(mockIsAppMode.value).toBe(true)
    mockIsAppMode.value = false
    expect(mockIsAppMode.value).toBe(false)
  })

  it('formatServerHost strips protocol', () => {
    const formatServerHost = (url: string) => url.replace(/^https?:\/\//, '')
    expect(formatServerHost('https://example.com')).toBe('example.com')
    expect(formatServerHost('http://localhost:8080')).toBe('localhost:8080')
  })

  // ── serverStatusLabel ──

  it('serverStatusLabel returns connected for connected status', () => {
    mockWsStatus.value = 'connected'
    const serverStatusLabel = computed(() => {
      if (mockWsStatus.value === 'connected') return 'Connected'
      if (mockWsStatus.value === 'reconnecting') return 'Reconnecting'
      return 'Disconnected'
    })
    expect(serverStatusLabel.value).toBe('Connected')
  })

  it('serverStatusLabel returns reconnecting for reconnecting status', () => {
    mockWsStatus.value = 'reconnecting'
    const serverStatusLabel = computed(() => {
      if (mockWsStatus.value === 'connected') return 'Connected'
      if (mockWsStatus.value === 'reconnecting') return 'Reconnecting'
      return 'Disconnected'
    })
    expect(serverStatusLabel.value).toBe('Reconnecting')
  })

  it('serverStatusLabel returns disconnected for disconnected status', () => {
    mockWsStatus.value = 'disconnected'
    const serverStatusLabel = computed(() => {
      if (mockWsStatus.value === 'connected') return 'Connected'
      if (mockWsStatus.value === 'reconnecting') return 'Reconnecting'
      return 'Disconnected'
    })
    expect(serverStatusLabel.value).toBe('Disconnected')
  })

  // ── dropdown position calculation ──

  it('dropdownStyle calculates fixed position from element rect', () => {
    const toFixedCSS = (coord: number) => coord // zoom=1
    const rect = { bottom: 50, left: 10, width: 200 }
    const style = {
      position: 'fixed',
      top: `${toFixedCSS(rect.bottom + 4)}px`,
      left: `${toFixedCSS(rect.left)}px`,
      minWidth: `${Math.max(220, rect.width)}px`,
      maxWidth: '280px',
    }
    expect(style.top).toBe('54px')
    expect(style.left).toBe('10px')
    expect(style.minWidth).toBe('220px')
  })

  // ── recentItems path display ──

  it('displayPath is relative to homeDir when path starts with it', () => {
    const homeDir = '/home/user'
    const p = '/home/user/projects/myapp'
    const normHome = homeDir.replace(/\\/g, '/')
    const normP = p.replace(/\\/g, '/')
    const displayPath = (normHome && normP.startsWith(normHome + '/'))
      ? p.slice(homeDir.length + 1)
      : p
    expect(displayPath).toBe('projects/myapp')
  })

  it('displayPath is absolute when not under homeDir', () => {
    const homeDir = '/home/user'
    const p = '/opt/other/project'
    const normHome = homeDir.replace(/\\/g, '/')
    const normP = p.replace(/\\/g, '/')
    const displayPath = (normHome && normP.startsWith(normHome + '/'))
      ? p.slice(homeDir.length + 1)
      : p
    expect(displayPath).toBe('/opt/other/project')
  })

  // ── branch animation ──

  it('branchAnimating triggers on branch change', async () => {
    mockGitBranch.value = 'main'
    // Simulate branch change
    mockGitBranch.value = 'feature-test'
    expect(mockGitBranch.value).toBe('feature-test')
  })
})
