import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiGet } from '@/utils/api'

vi.mock('@/utils/api', () => ({ apiGet: vi.fn() }))

// useUsageMeter registers its WS listener through a dynamic import of
// useGlobalEvents. Stub it so the import resolves instantly and no real socket
// wiring is touched; the callback is captured for the tests that need it.
const eventHandlers: Array<(event: string, data: unknown) => void> = []
vi.mock('@/composables/useGlobalEvents', () => ({
  useGlobalEvents: () => ({
    onEvent: (fn: (event: string, data: unknown) => void) => {
      eventHandlers.push(fn)
      return () => {}
    },
  }),
}))

const mockedGet = vi.mocked(apiGet)

async function load() {
  const mod = await import('@/composables/useUsageMeter')
  mod.__resetUsageMeterForTest()
  return mod
}

const ENABLED = {
  enabled: true,
  metric: 'cost' as const,
  anchorDate: '2026-10-05',
  periodDays: 30,
  quota: 20,
  cycleStart: '2026-10-05T00:00:00+08:00',
  cycleEnd: '2026-11-04T00:00:00+08:00',
  dayInCycle: 10,
  actual: 9.2,
  expected: 6.67,
  remainingDays: 20,
  projectedExhaustDay: 22,
  series: [
    { day: '2026-10-05', actual: 0, expected: 0.67 },
    { day: '2026-10-06', actual: 9.2, expected: 1.33 },
  ],
}

describe('useUsageMeter', () => {
  beforeEach(() => {
    mockedGet.mockReset()
    eventHandlers.length = 0
  })

  it('starts disabled and loads the server state', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    expect(meter.status.value.enabled).toBe(false)

    mockedGet.mockResolvedValueOnce(ENABLED)
    await meter.refresh()

    expect(meter.status.value.enabled).toBe(true)
    expect(meter.status.value.actual).toBe(9.2)
    expect(meter.loaded.value).toBe(true)
  })

  it('derives over-plan state from actual > expected', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    mockedGet.mockResolvedValueOnce(ENABLED)
    await meter.refresh()

    expect(meter.isOverPlan.value).toBe(true)
    expect(meter.isQuotaExhausted.value).toBe(false)
    expect(meter.meterState.value).toBe('over')
  })

  it('reports full once the quota is reached, and clamps the fill', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    mockedGet.mockResolvedValueOnce({ ...ENABLED, actual: 25 })
    await meter.refresh()

    expect(meter.isQuotaExhausted.value).toBe(true)
    expect(meter.meterState.value).toBe('full')
    expect(meter.fillPercent.value).toBe(100)
  })

  it('stays "ok" when usage is exactly on the baseline', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    // expected is the allowance, so equality is still on plan.
    mockedGet.mockResolvedValueOnce({ ...ENABLED, actual: 6.67, expected: 6.67 })
    await meter.refresh()

    expect(meter.isOverPlan.value).toBe(false)
    expect(meter.meterState.value).toBe('ok')
  })

  it('keeps the previous state when a refresh fails', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    mockedGet.mockResolvedValueOnce(ENABLED)
    await meter.refresh()

    mockedGet.mockRejectedValueOnce(new Error('network down'))
    await meter.refresh()

    expect(meter.status.value.enabled).toBe(true)
    expect(meter.status.value.actual).toBe(9.2)
  })

  it('coalesces concurrent refreshes into one request', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    mockedGet.mockResolvedValue(ENABLED)

    await Promise.all([meter.refresh(), meter.refresh(), meter.refresh()])

    expect(mockedGet).toHaveBeenCalledTimes(1)
  })

  it('applies a usage_meter_update payload without waiting for a refetch', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    mockedGet.mockResolvedValue(ENABLED)
    await meter.refresh()

    // Let the dynamic import settle so the handler is registered. Two microtask
    // turns are not enough — the dynamic import resolves through the module
    // loader, so wait for the handler list to become non-empty instead of
    // guessing a tick count.
    await vi.waitFor(() => {
      expect(eventHandlers.length).toBeGreaterThan(0)
    })

    // The refetch triggered by the handler resolves to the same payload; the
    // assertion is about the immediate apply, which happens synchronously.
    for (const handler of eventHandlers) {
      handler('usage_meter_update', { metric: 'cost', actual: 15, expected: 6.67, quota: 20 })
    }

    expect(meter.status.value.actual).toBe(15)
    expect(meter.meterState.value).toBe('over')
  })

  // Turning the quota to 0 must clear the readout immediately. Without this the
  // capsule keeps showing the old quota until the user clicks it — the reported
  // bug. The frame carries no numbers on purpose, so the whole state is dropped
  // rather than patched.
  it('clears the readout when the server reports the rule was turned off', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    mockedGet.mockResolvedValue(ENABLED)
    await meter.refresh()
    expect(meter.status.value.enabled).toBe(true)
    expect(meter.status.value.quota).toBe(20)

    await vi.waitFor(() => { expect(eventHandlers.length).toBeGreaterThan(0) })
    for (const handler of eventHandlers) handler('usage_meter_tick', { enabled: false })

    expect(meter.status.value.enabled).toBe(false)
    // The quota is gone entirely, not zeroed: EMPTY has no quota field, and a
    // lingering 0 would still render as "/ 0" if a consumer checked the number
    // instead of `enabled`.
    expect(meter.status.value.quota).toBeUndefined()
    expect(meter.status.value.actual).toBe(0)
    expect(meter.meterState.value).toBe('off')
  })

  // An older frame that omits `enabled` must not resurrect a disabled meter.
  it('does not turn the meter back on from a frame without an enabled flag', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    mockedGet.mockResolvedValueOnce({ enabled: false, dayInCycle: 0, actual: 0, expected: 0, remainingDays: 0, projectedExhaustDay: 0 })
    await meter.refresh()
    expect(meter.status.value.enabled).toBe(false)

    await vi.waitFor(() => { expect(eventHandlers.length).toBeGreaterThan(0) })
    for (const handler of eventHandlers) handler('usage_meter_tick', { actual: 7, expected: 0, quota: 0 })

    expect(meter.status.value.enabled).toBe(false)
  })

  it('bumps openRequest so the capsule can open on a notification tap', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    const before = meter.openRequest.value

    mod.requestOpen()

    expect(meter.openRequest.value).toBe(before + 1)
  })

  // ── No rule configured ────────────────────────────────────────────────────
  // The backend answers {"enabled": false} with no numbers. The client renders
  // a placeholder entry in that state, so what matters here is that the derived
  // ratios stay inert and that `off` is distinguishable from `ok` — `ok` would
  // claim "you are within budget", a judgement about a quota that does not exist.

  it('reports the off state rather than "on plan" when there is no rule', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    mockedGet.mockResolvedValueOnce({ enabled: false, dayInCycle: 0, actual: 0, expected: 0, remainingDays: 0, projectedExhaustDay: 0 })
    await meter.refresh()

    expect(meter.meterState.value).toBe('off')
    expect(meter.isOverPlan.value).toBe(false)
    expect(meter.isQuotaExhausted.value).toBe(false)
  })

  it('does not report over-plan from a zero baseline', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    // Any spend exceeds an absent baseline; without the `enabled` guard this
    // would be 'over' with a nonsensical percentage.
    mockedGet.mockResolvedValueOnce({ enabled: false, dayInCycle: 0, actual: 999, expected: 0, remainingDays: 0, projectedExhaustDay: 0 })
    await meter.refresh()

    expect(meter.isOverPlan.value).toBe(false)
    expect(meter.meterState.value).toBe('off')
    // No fill either: a percentage of a zero quota is undefined, not 100%.
    expect(meter.fillPercent.value).toBe(0)
    expect(meter.expectedPercent.value).toBe(0)
  })

  it('hasData stays false until the first successful fetch', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    // The header must not flash a placeholder before the server has answered:
    // a configured user would see it appear and vanish.
    expect(meter.hasData.value).toBe(false)

    mockedGet.mockResolvedValueOnce({ ...ENABLED })
    await meter.refresh()

    expect(meter.hasData.value).toBe(true)
  })

  // A disabled answer still counts as "the fetch landed": the placeholder is
  // driven by hasData, so an unconfigured user must reach it.
  it('hasData becomes true even when no rule is configured', async () => {
    const mod = await load()
    const meter = mod.useUsageMeter()
    mockedGet.mockResolvedValueOnce({ enabled: false, dayInCycle: 0, actual: 0, expected: 0, remainingDays: 0, projectedExhaustDay: 0 })
    await meter.refresh()

    expect(meter.hasData.value).toBe(true)
  })
})
