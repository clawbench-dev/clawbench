import { describe, expect, it, vi, afterEach } from 'vitest'
import { ref } from 'vue'
import { nextTick } from 'vue'
import { useSystemPressure, criticalMetricOf, CRITICAL_THRESHOLD, metricIcons } from '@/composables/useSystemPressure'
import type { SystemResources } from '@/composables/useSystemResources'

function makeResources(overrides: Partial<{
  cpu: { percent: number; core_count: number }
  memory: { percent: number }
  disk: { percent: number }
  load: { load1: number }
}> = {}): SystemResources {
  const cpu = overrides.cpu ?? { percent: 0, core_count: 4 }
  const memory = overrides.memory ?? { percent: 0 }
  const disk = overrides.disk ?? { percent: 0 }
  const load = overrides.load ?? { load1: 0 }
  return {
    cpu: { percent: cpu.percent, core_count: cpu.core_count },
    memory: { used: 0, total: 0, percent: memory.percent },
    disk: { used: 0, total: 0, percent: disk.percent },
    disk_io: { read_rate: 0, write_rate: 0 },
    network: { upload_rate: 0, download_rate: 0 },
    load: { load1: load.load1, load5: 0, load15: 0 },
  }
}

describe('criticalMetricOf', () => {
  it('returns null when no metric is critical', () => {
    const result = criticalMetricOf(makeResources({
      cpu: { percent: 50, core_count: 4 },
      memory: { percent: 60 },
      disk: { percent: 70 },
      load: { load1: 2.0 },
    }))
    expect(result).toBeNull()
  })

  it('returns cpu when only cpu is critical', () => {
    const result = criticalMetricOf(makeResources({
      cpu: { percent: 95, core_count: 4 },
      memory: { percent: 50 },
      disk: { percent: 60 },
      load: { load1: 1.0 },
    }))
    expect(result).toBe('cpu')
  })

  it('returns memory when only memory is critical', () => {
    const result = criticalMetricOf(makeResources({
      cpu: { percent: 50, core_count: 4 },
      memory: { percent: 92 },
      disk: { percent: 60 },
      load: { load1: 1.0 },
    }))
    expect(result).toBe('memory')
  })

  it('returns disk when only disk is critical', () => {
    const result = criticalMetricOf(makeResources({
      cpu: { percent: 50, core_count: 4 },
      memory: { percent: 60 },
      disk: { percent: 91 },
      load: { load1: 1.0 },
    }))
    expect(result).toBe('disk')
  })

  it('returns load when load1/core_count >= 90%', () => {
    const result = criticalMetricOf(makeResources({
      cpu: { percent: 50, core_count: 4 },
      memory: { percent: 60 },
      disk: { percent: 60 },
      load: { load1: 3.8 }, // 3.8/4 = 95%
    }))
    expect(result).toBe('load')
  })

  it('caps load percent at 100', () => {
    const result = criticalMetricOf(makeResources({
      cpu: { percent: 50, core_count: 2 },
      memory: { percent: 60 },
      disk: { percent: 60 },
      load: { load1: 5.0 }, // 5.0/2 = 250% → capped to 100%
    }))
    expect(result).toBe('load')
  })

  it('picks metric with highest excess ratio when multiple are critical', () => {
    // CPU at 95%: excess = (95-90)/(100-90) = 0.5
    // Memory at 92%: excess = (92-90)/(100-90) = 0.2
    const result = criticalMetricOf(makeResources({
      cpu: { percent: 95, core_count: 4 },
      memory: { percent: 92 },
      disk: { percent: 60 },
      load: { load1: 1.0 },
    }))
    expect(result).toBe('cpu')
  })

  it('picks memory over disk when memory has higher excess', () => {
    // Memory at 98%: excess = 0.8
    // Disk at 93%: excess = 0.3
    const result = criticalMetricOf(makeResources({
      cpu: { percent: 50, core_count: 4 },
      memory: { percent: 98 },
      disk: { percent: 93 },
      load: { load1: 1.0 },
    }))
    expect(result).toBe('memory')
  })

  it('returns null when all metrics are exactly at threshold - 1', () => {
    const result = criticalMetricOf(makeResources({
      cpu: { percent: CRITICAL_THRESHOLD - 1, core_count: 4 },
      memory: { percent: CRITICAL_THRESHOLD - 1 },
      disk: { percent: CRITICAL_THRESHOLD - 1 },
      load: { load1: 3.55 }, // 3.55/4 = 88.75%
    }))
    expect(result).toBeNull()
  })

  it('returns metric when exactly at threshold', () => {
    const result = criticalMetricOf(makeResources({
      cpu: { percent: CRITICAL_THRESHOLD, core_count: 4 },
      memory: { percent: 50 },
      disk: { percent: 50 },
      load: { load1: 0.5 },
    }))
    expect(result).toBe('cpu')
  })

  it('handles zero core count by defaulting to 1', () => {
    const result = criticalMetricOf(makeResources({
      cpu: { percent: 50, core_count: 0 },
      memory: { percent: 50 },
      disk: { percent: 50 },
      load: { load1: 0.95 }, // 0.95/1 = 95%
    }))
    expect(result).toBe('load')
  })
})

describe('useSystemPressure blink', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('blinks while under pressure and stops when pressure clears', async () => {
    vi.useFakeTimers()
    const resources = ref(makeResources({ cpu: { percent: 95, core_count: 4 } }))
    const hidden = ref(false)
    const p = useSystemPressure({
      resources,
      isDocumentHidden: () => hidden.value,
    })

    // Starts blinking immediately (immediate watcher).
    expect(p.isUnderPressure.value).toBe(true)
    expect(p.criticalMetric.value).toBe('cpu')
    expect(p.PressureIcon.value).toBe(metricIcons.cpu)

    expect(p.showMetricIcon.value).toBe(false)
    vi.advanceTimersByTime(1000)
    expect(p.showMetricIcon.value).toBe(true)
    vi.advanceTimersByTime(1000)
    expect(p.showMetricIcon.value).toBe(false)

    // Pressure clears → blink stops and icon resets.
    resources.value = makeResources({ cpu: { percent: 50, core_count: 4 } })
    await nextTick()
    expect(p.isUnderPressure.value).toBe(false)
    expect(p.showMetricIcon.value).toBe(false)
    expect(p.PressureIcon.value).toBeNull()
  })

  it('pauses the blink when the document is hidden and resumes on return', async () => {
    vi.useFakeTimers()
    const resources = ref(makeResources({ cpu: { percent: 95, core_count: 4 } }))
    const hidden = ref(false)
    const p = useSystemPressure({
      resources,
      isDocumentHidden: () => hidden.value,
    })

    // Blink advances to the metric icon.
    vi.advanceTimersByTime(1000)
    expect(p.showMetricIcon.value).toBe(true)

    // Hide → blink stops and icon resets.
    hidden.value = true
    p.onVisibilityChange()
    expect(p.showMetricIcon.value).toBe(false)
    vi.advanceTimersByTime(5000)
    expect(p.showMetricIcon.value).toBe(false) // timer paused

    // Show → blink resumes (timer restarted, icon advances).
    hidden.value = false
    p.onVisibilityChange()
    expect(p.showMetricIcon.value).toBe(false)
    vi.advanceTimersByTime(1000)
    expect(p.showMetricIcon.value).toBe(true)
  })
})
