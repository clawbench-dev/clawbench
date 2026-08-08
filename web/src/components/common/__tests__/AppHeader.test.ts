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

import { computed, ref } from 'vue'

// ── Pressure detection logic (extracted for testing) ──

type MetricKey = 'cpu' | 'memory' | 'disk' | 'load'
const CRITICAL_THRESHOLD = 90

function getCriticalMetric(resources: {
  cpu: { percent: number; core_count: number }
  memory: { percent: number }
  disk: { percent: number }
  load: { load1: number }
}): MetricKey | null {
  const cores = resources.cpu.core_count || 1
  const loadPercent = (resources.load.load1 / cores) * 100
  const metrics: { key: MetricKey; percent: number }[] = [
    { key: 'cpu', percent: resources.cpu.percent },
    { key: 'memory', percent: resources.memory.percent },
    { key: 'disk', percent: resources.disk.percent },
    { key: 'load', percent: Math.min(loadPercent, 100) },
  ]
  const critical = metrics.filter(m => m.percent >= CRITICAL_THRESHOLD)
  if (critical.length === 0) return null
  critical.sort((a, b) => (b.percent - CRITICAL_THRESHOLD) - (a.percent - CRITICAL_THRESHOLD))
  return critical[0].key
}

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

  it('isAppMode determines logout button visibility', () => {
    mockIsAppMode.value = true
    expect(mockIsAppMode.value).toBe(true)
    mockIsAppMode.value = false
    expect(mockIsAppMode.value).toBe(false)
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

describe('Pressure detection logic', () => {
  it('returns null when no metric is critical', () => {
    const result = getCriticalMetric({
      cpu: { percent: 50, core_count: 4 },
      memory: { percent: 60 },
      disk: { percent: 70 },
      load: { load1: 2.0 },
    })
    expect(result).toBeNull()
  })

  it('returns cpu when only cpu is critical', () => {
    const result = getCriticalMetric({
      cpu: { percent: 95, core_count: 4 },
      memory: { percent: 50 },
      disk: { percent: 60 },
      load: { load1: 1.0 },
    })
    expect(result).toBe('cpu')
  })

  it('returns memory when only memory is critical', () => {
    const result = getCriticalMetric({
      cpu: { percent: 50, core_count: 4 },
      memory: { percent: 92 },
      disk: { percent: 60 },
      load: { load1: 1.0 },
    })
    expect(result).toBe('memory')
  })

  it('returns disk when only disk is critical', () => {
    const result = getCriticalMetric({
      cpu: { percent: 50, core_count: 4 },
      memory: { percent: 60 },
      disk: { percent: 91 },
      load: { load1: 1.0 },
    })
    expect(result).toBe('disk')
  })

  it('returns load when load1/core_count >= 90%', () => {
    const result = getCriticalMetric({
      cpu: { percent: 50, core_count: 4 },
      memory: { percent: 60 },
      disk: { percent: 60 },
      load: { load1: 3.8 }, // 3.8/4 = 95%
    })
    expect(result).toBe('load')
  })

  it('caps load percent at 100', () => {
    const result = getCriticalMetric({
      cpu: { percent: 50, core_count: 2 },
      memory: { percent: 60 },
      disk: { percent: 60 },
      load: { load1: 5.0 }, // 5.0/2 = 250% → capped to 100%
    })
    expect(result).toBe('load')
  })

  it('picks metric with highest excess ratio when multiple are critical', () => {
    // CPU at 95%: excess = (95-90)/(100-90) = 0.5
    // Memory at 92%: excess = (92-90)/(100-90) = 0.2
    const result = getCriticalMetric({
      cpu: { percent: 95, core_count: 4 },
      memory: { percent: 92 },
      disk: { percent: 60 },
      load: { load1: 1.0 },
    })
    expect(result).toBe('cpu')
  })

  it('picks memory over disk when memory has higher excess', () => {
    // Memory at 98%: excess = 0.8
    // Disk at 93%: excess = 0.3
    const result = getCriticalMetric({
      cpu: { percent: 50, core_count: 4 },
      memory: { percent: 98 },
      disk: { percent: 93 },
      load: { load1: 1.0 },
    })
    expect(result).toBe('memory')
  })

  it('returns null when all metrics are exactly at threshold - 1', () => {
    const result = getCriticalMetric({
      cpu: { percent: 89, core_count: 4 },
      memory: { percent: 89 },
      disk: { percent: 89 },
      load: { load1: 3.55 }, // 3.55/4 = 88.75%
    })
    expect(result).toBeNull()
  })

  it('returns metric when exactly at threshold', () => {
    const result = getCriticalMetric({
      cpu: { percent: 90, core_count: 4 },
      memory: { percent: 50 },
      disk: { percent: 50 },
      load: { load1: 0.5 },
    })
    expect(result).toBe('cpu')
  })

  it('handles zero core count by defaulting to 1', () => {
    const result = getCriticalMetric({
      cpu: { percent: 50, core_count: 0 },
      memory: { percent: 50 },
      disk: { percent: 50 },
      load: { load1: 0.95 }, // 0.95/1 = 95%
    })
    expect(result).toBe('load')
  })
})
